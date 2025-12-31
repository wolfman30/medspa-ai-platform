package payments

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRedis(t *testing.T) (*redis.Client, func()) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return client, func() {
		client.Close()
		mr.Close()
	}
}

func TestVelocityChecker_CheckDepositVelocity(t *testing.T) {
	redisClient, cleanup := setupTestRedis(t)
	defer cleanup()

	config := DefaultVelocityConfig()
	config.MaxDepositsPerPhone = 3
	config.DepositWindowHours = 24

	checker := NewVelocityChecker(redisClient, config, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		orgID       string
		phone       string
		attempts    int
		wantAllowed bool
	}{
		{
			name:        "first attempt allowed",
			orgID:       "org-1",
			phone:       "+15551234567",
			attempts:    1,
			wantAllowed: true,
		},
		{
			name:        "at limit allowed",
			orgID:       "org-1",
			phone:       "+15551234568",
			attempts:    3,
			wantAllowed: true,
		},
		{
			name:        "over limit blocked",
			orgID:       "org-1",
			phone:       "+15551234569",
			attempts:    4,
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make the specified number of attempts
			var result *VelocityResult
			var err error
			for i := 0; i < tt.attempts; i++ {
				result, err = checker.CheckDepositVelocity(ctx, tt.orgID, tt.phone)
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantAllowed, result.Allowed)
			assert.Equal(t, "deposit", result.CheckType)
			assert.Equal(t, tt.attempts, result.CurrentCount)
			assert.Equal(t, config.MaxDepositsPerPhone, result.MaxAllowed)

			if !tt.wantAllowed {
				assert.NotEmpty(t, result.Message)
			}
		})
	}
}

func TestVelocityChecker_CheckRefundVelocity(t *testing.T) {
	redisClient, cleanup := setupTestRedis(t)
	defer cleanup()

	config := DefaultVelocityConfig()
	config.MaxRefundsPerLead = 1
	config.RefundWindowDays = 7

	checker := NewVelocityChecker(redisClient, config, nil)
	ctx := context.Background()

	// First refund should be allowed
	result, err := checker.CheckRefundVelocity(ctx, "org-1", "lead-123")
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, 1, result.CurrentCount)

	// Second refund should be blocked
	result, err = checker.CheckRefundVelocity(ctx, "org-1", "lead-123")
	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Equal(t, 2, result.CurrentCount)
	assert.Contains(t, result.Message, "exceeded")
}

func TestVelocityChecker_DifferentOrgsAreSeparate(t *testing.T) {
	redisClient, cleanup := setupTestRedis(t)
	defer cleanup()

	config := DefaultVelocityConfig()
	config.MaxDepositsPerPhone = 2

	checker := NewVelocityChecker(redisClient, config, nil)
	ctx := context.Background()

	phone := "+15551234567"

	// Max out org-1
	for i := 0; i < 3; i++ {
		checker.CheckDepositVelocity(ctx, "org-1", phone)
	}

	// org-1 should be blocked
	result, err := checker.CheckDepositVelocity(ctx, "org-1", phone)
	require.NoError(t, err)
	assert.False(t, result.Allowed)

	// org-2 should still be allowed (separate namespace)
	result, err = checker.CheckDepositVelocity(ctx, "org-2", phone)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, 1, result.CurrentCount)
}

func TestVelocityChecker_ResetVelocity(t *testing.T) {
	redisClient, cleanup := setupTestRedis(t)
	defer cleanup()

	config := DefaultVelocityConfig()
	config.MaxDepositsPerPhone = 2

	checker := NewVelocityChecker(redisClient, config, nil)
	ctx := context.Background()

	phone := "+15551234567"
	orgID := "org-1"

	// Max out the phone
	for i := 0; i < 3; i++ {
		checker.CheckDepositVelocity(ctx, orgID, phone)
	}

	// Should be blocked
	result, err := checker.CheckDepositVelocity(ctx, orgID, phone)
	require.NoError(t, err)
	assert.False(t, result.Allowed)

	// Reset
	err = checker.ResetDepositVelocity(ctx, orgID, phone)
	require.NoError(t, err)

	// Should be allowed again
	result, err = checker.CheckDepositVelocity(ctx, orgID, phone)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, 1, result.CurrentCount)
}

func TestVelocityChecker_DisabledCheck(t *testing.T) {
	redisClient, cleanup := setupTestRedis(t)
	defer cleanup()

	config := DefaultVelocityConfig()
	config.EnableDepositCheck = false

	checker := NewVelocityChecker(redisClient, config, nil)
	ctx := context.Background()

	// Should always be allowed when disabled
	for i := 0; i < 10; i++ {
		result, err := checker.CheckDepositVelocity(ctx, "org-1", "+15551234567")
		require.NoError(t, err)
		assert.True(t, result.Allowed)
	}
}

func TestDefaultVelocityConfig(t *testing.T) {
	config := DefaultVelocityConfig()

	assert.Equal(t, 3, config.MaxDepositsPerPhone)
	assert.Equal(t, 24, config.DepositWindowHours)
	assert.Equal(t, 1, config.MaxRefundsPerLead)
	assert.Equal(t, 7, config.RefundWindowDays)
	assert.True(t, config.EnableDepositCheck)
	assert.True(t, config.EnableRefundCheck)
	assert.False(t, config.EnableCardCheck) // Disabled by default
}
