package payments

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// VelocityChecker implements rate limiting for fraud prevention.
type VelocityChecker struct {
	redis  *redis.Client
	logger *logging.Logger
	config VelocityConfig
}

// VelocityConfig contains velocity check configuration.
type VelocityConfig struct {
	// Max deposit attempts per phone per window
	MaxDepositsPerPhone int
	DepositWindowHours  int

	// Max refund requests per lead per window
	MaxRefundsPerLead int
	RefundWindowDays  int

	// Max different phones per card (if trackable)
	MaxPhonesPerCard int
	CardWindowHours  int

	// Enable/disable specific checks
	EnableDepositCheck bool
	EnableRefundCheck  bool
	EnableCardCheck    bool
}

// DefaultVelocityConfig returns default velocity limits.
func DefaultVelocityConfig() VelocityConfig {
	return VelocityConfig{
		MaxDepositsPerPhone: 3,
		DepositWindowHours:  24,
		MaxRefundsPerLead:   1,
		RefundWindowDays:    7,
		MaxPhonesPerCard:    5,
		CardWindowHours:     24,
		EnableDepositCheck:  true,
		EnableRefundCheck:   true,
		EnableCardCheck:     false, // Disabled by default - requires card tracking
	}
}

// VelocityResult contains the result of a velocity check.
type VelocityResult struct {
	Allowed      bool
	CheckType    string
	CurrentCount int
	MaxAllowed   int
	WindowExpiry time.Time
	Message      string
}

// NewVelocityChecker creates a new velocity checker.
func NewVelocityChecker(redisClient *redis.Client, config VelocityConfig, logger *logging.Logger) *VelocityChecker {
	if logger == nil {
		logger = logging.Default()
	}
	return &VelocityChecker{
		redis:  redisClient,
		logger: logger,
		config: config,
	}
}

// CheckDepositVelocity checks if a deposit is allowed for the given phone.
func (v *VelocityChecker) CheckDepositVelocity(ctx context.Context, orgID, phone string) (*VelocityResult, error) {
	ctx, span := squareTracer.Start(ctx, "velocity.check_deposit")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", orgID),
		attribute.String("velocity.check_type", "deposit"),
	)

	if !v.config.EnableDepositCheck {
		return &VelocityResult{Allowed: true, CheckType: "deposit"}, nil
	}

	key := fmt.Sprintf("velocity:deposit:%s:%s", orgID, phone)
	windowDuration := time.Duration(v.config.DepositWindowHours) * time.Hour

	count, expiry, err := v.incrementAndGet(ctx, key, windowDuration)
	if err != nil {
		v.logger.Error("velocity check failed", "error", err, "key", key)
		// Fail open - allow the transaction if Redis is down
		return &VelocityResult{Allowed: true, CheckType: "deposit", Message: "velocity check unavailable"}, nil
	}

	result := &VelocityResult{
		Allowed:      count <= v.config.MaxDepositsPerPhone,
		CheckType:    "deposit",
		CurrentCount: count,
		MaxAllowed:   v.config.MaxDepositsPerPhone,
		WindowExpiry: expiry,
	}

	if !result.Allowed {
		result.Message = fmt.Sprintf("exceeded %d deposit attempts in %d hours", v.config.MaxDepositsPerPhone, v.config.DepositWindowHours)
		v.logger.Warn("deposit velocity exceeded",
			"org_id", orgID,
			"phone", phone,
			"count", count,
			"max", v.config.MaxDepositsPerPhone,
		)
		span.SetAttributes(attribute.Bool("velocity.exceeded", true))
	}

	return result, nil
}

// CheckRefundVelocity checks if a refund request is allowed for the given lead.
func (v *VelocityChecker) CheckRefundVelocity(ctx context.Context, orgID, leadID string) (*VelocityResult, error) {
	ctx, span := squareTracer.Start(ctx, "velocity.check_refund")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", orgID),
		attribute.String("velocity.check_type", "refund"),
	)

	if !v.config.EnableRefundCheck {
		return &VelocityResult{Allowed: true, CheckType: "refund"}, nil
	}

	key := fmt.Sprintf("velocity:refund:%s:%s", orgID, leadID)
	windowDuration := time.Duration(v.config.RefundWindowDays) * 24 * time.Hour

	count, expiry, err := v.incrementAndGet(ctx, key, windowDuration)
	if err != nil {
		v.logger.Error("velocity check failed", "error", err, "key", key)
		return &VelocityResult{Allowed: true, CheckType: "refund", Message: "velocity check unavailable"}, nil
	}

	result := &VelocityResult{
		Allowed:      count <= v.config.MaxRefundsPerLead,
		CheckType:    "refund",
		CurrentCount: count,
		MaxAllowed:   v.config.MaxRefundsPerLead,
		WindowExpiry: expiry,
	}

	if !result.Allowed {
		result.Message = fmt.Sprintf("exceeded %d refund requests in %d days", v.config.MaxRefundsPerLead, v.config.RefundWindowDays)
		v.logger.Warn("refund velocity exceeded",
			"org_id", orgID,
			"lead_id", leadID,
			"count", count,
			"max", v.config.MaxRefundsPerLead,
		)
		span.SetAttributes(attribute.Bool("velocity.exceeded", true))
	}

	return result, nil
}

// CheckCardVelocity checks if too many different phones have used the same card.
func (v *VelocityChecker) CheckCardVelocity(ctx context.Context, orgID, cardFingerprint, phone string) (*VelocityResult, error) {
	ctx, span := squareTracer.Start(ctx, "velocity.check_card")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", orgID),
		attribute.String("velocity.check_type", "card"),
	)

	if !v.config.EnableCardCheck || cardFingerprint == "" {
		return &VelocityResult{Allowed: true, CheckType: "card"}, nil
	}

	key := fmt.Sprintf("velocity:card:%s:%s", orgID, cardFingerprint)
	windowDuration := time.Duration(v.config.CardWindowHours) * time.Hour

	// Use a set to track unique phones per card
	added, err := v.redis.SAdd(ctx, key, phone).Result()
	if err != nil {
		v.logger.Error("velocity check failed", "error", err, "key", key)
		return &VelocityResult{Allowed: true, CheckType: "card", Message: "velocity check unavailable"}, nil
	}

	// Set expiry on first add
	if added > 0 {
		v.redis.Expire(ctx, key, windowDuration)
	}

	count, err := v.redis.SCard(ctx, key).Result()
	if err != nil {
		v.logger.Error("velocity check failed", "error", err, "key", key)
		return &VelocityResult{Allowed: true, CheckType: "card", Message: "velocity check unavailable"}, nil
	}

	ttl, _ := v.redis.TTL(ctx, key).Result()
	expiry := time.Now().Add(ttl)

	result := &VelocityResult{
		Allowed:      int(count) <= v.config.MaxPhonesPerCard,
		CheckType:    "card",
		CurrentCount: int(count),
		MaxAllowed:   v.config.MaxPhonesPerCard,
		WindowExpiry: expiry,
	}

	if !result.Allowed {
		result.Message = fmt.Sprintf("card used by %d different phones in %d hours", count, v.config.CardWindowHours)
		v.logger.Warn("card velocity exceeded",
			"org_id", orgID,
			"card_fingerprint", cardFingerprint[:8]+"...",
			"phone_count", count,
			"max", v.config.MaxPhonesPerCard,
		)
		span.SetAttributes(attribute.Bool("velocity.exceeded", true))
	}

	return result, nil
}

// incrementAndGet increments a counter and returns the new value with expiry time.
func (v *VelocityChecker) incrementAndGet(ctx context.Context, key string, window time.Duration) (int, time.Time, error) {
	// Use INCR with EXPIRE
	count, err := v.redis.Incr(ctx, key).Result()
	if err != nil {
		return 0, time.Time{}, err
	}

	// Set expiry only on first increment
	if count == 1 {
		v.redis.Expire(ctx, key, window)
	}

	// Get TTL for expiry time
	ttl, err := v.redis.TTL(ctx, key).Result()
	if err != nil {
		ttl = window
	}

	expiry := time.Now().Add(ttl)
	return int(count), expiry, nil
}

// ResetDepositVelocity resets the deposit velocity counter for a phone (admin use).
func (v *VelocityChecker) ResetDepositVelocity(ctx context.Context, orgID, phone string) error {
	key := fmt.Sprintf("velocity:deposit:%s:%s", orgID, phone)
	return v.redis.Del(ctx, key).Err()
}

// ResetRefundVelocity resets the refund velocity counter for a lead (admin use).
func (v *VelocityChecker) ResetRefundVelocity(ctx context.Context, orgID, leadID string) error {
	key := fmt.Sprintf("velocity:refund:%s:%s", orgID, leadID)
	return v.redis.Del(ctx, key).Err()
}

// GetDepositStats returns current deposit velocity stats for a phone.
func (v *VelocityChecker) GetDepositStats(ctx context.Context, orgID, phone string) (*VelocityResult, error) {
	key := fmt.Sprintf("velocity:deposit:%s:%s", orgID, phone)

	count, err := v.redis.Get(ctx, key).Int()
	if err == redis.Nil {
		return &VelocityResult{
			Allowed:      true,
			CheckType:    "deposit",
			CurrentCount: 0,
			MaxAllowed:   v.config.MaxDepositsPerPhone,
		}, nil
	}
	if err != nil {
		return nil, err
	}

	ttl, _ := v.redis.TTL(ctx, key).Result()

	return &VelocityResult{
		Allowed:      count < v.config.MaxDepositsPerPhone,
		CheckType:    "deposit",
		CurrentCount: count,
		MaxAllowed:   v.config.MaxDepositsPerPhone,
		WindowExpiry: time.Now().Add(ttl),
	}, nil
}
