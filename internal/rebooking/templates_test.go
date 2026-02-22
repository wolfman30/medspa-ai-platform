package rebooking

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestMessageTemplate(t *testing.T) {
	booked := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	rebook := booked.Add(10 * 7 * 24 * time.Hour) // ~10 weeks

	tests := []struct {
		name     string
		service  string
		contains []string
	}{
		{
			name:     "botox",
			service:  "Botox",
			contains: []string{"Botox", "Forever 22", "Jane", "💉", "YES"},
		},
		{
			name:     "lip filler",
			service:  "Lip Filler",
			contains: []string{"lip filler", "Forever 22", "💋", "YES"},
		},
		{
			name:     "microneedling series",
			service:  "Microneedling",
			contains: []string{"microneedling", "series", "YES"},
		},
		{
			name:     "weight loss",
			service:  "Weight Loss",
			contains: []string{"weight loss", "monthly", "YES"},
		},
		{
			name:     "unknown service",
			service:  "PRP Facial",
			contains: []string{"PRP Facial", "Forever 22", "YES"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Reminder{
				ID:          uuid.New(),
				PatientName: "Jane",
				Service:     tt.service,
				BookedAt:    booked,
				RebookAfter: rebook,
			}
			msg := MessageTemplate(r, "Forever 22")
			for _, substr := range tt.contains {
				assert.True(t, strings.Contains(strings.ToLower(msg), strings.ToLower(substr)),
					"expected %q in message: %s", substr, msg)
			}
		})
	}
}

func TestMessageTemplateNoName(t *testing.T) {
	r := &Reminder{
		Service:     "Botox",
		BookedAt:    time.Now(),
		RebookAfter: time.Now().Add(10 * 7 * 24 * time.Hour),
	}
	msg := MessageTemplate(r, "Test Clinic")
	assert.Contains(t, msg, "there")
}

func TestOptOutResponse(t *testing.T) {
	msg := OptOutResponse("Forever 22")
	assert.Contains(t, msg, "Forever 22")
	assert.Contains(t, msg, "removed")
}
