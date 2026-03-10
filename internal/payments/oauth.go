package payments

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// SquareOAuthService handles Square OAuth operations.
type SquareOAuthService struct {
	config SquareOAuthConfig
	db     *pgxpool.Pool
	logger *logging.Logger
}

// NewSquareOAuthService creates a new Square OAuth service.
func NewSquareOAuthService(config SquareOAuthConfig, db *pgxpool.Pool, logger *logging.Logger) *SquareOAuthService {
	if logger == nil {
		logger = logging.Default()
	}
	return &SquareOAuthService{
		config: config,
		db:     db,
		logger: logger,
	}
}

// baseURL returns the Square API base URL based on environment.
func (s *SquareOAuthService) baseURL() string {
	if s.config.Sandbox {
		return "https://connect.squareupsandbox.com"
	}
	return "https://connect.squareup.com"
}

// timestamptzToPtr converts a pgtype.Timestamptz to a *time.Time.
func timestamptzToPtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	value := ts.Time
	return &value
}

// formatSquareError extracts a human-readable error message from a Square API error response body.
func formatSquareError(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var parsed struct {
		Errors []struct {
			Category string `json:"category"`
			Code     string `json:"code"`
			Detail   string `json:"detail"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil && len(parsed.Errors) > 0 {
		first := parsed.Errors[0]
		parts := []string{}
		if first.Category != "" {
			parts = append(parts, first.Category)
		}
		if first.Code != "" {
			parts = append(parts, first.Code)
		}
		label := strings.Join(parts, "/")
		if first.Detail != "" {
			if label != "" {
				return fmt.Sprintf("%s: %s", label, first.Detail)
			}
			return first.Detail
		}
		if label != "" {
			return label
		}
	}
	if len(trimmed) > MaxOAuthLabelLen {
		return trimmed[:MaxOAuthLabelLen] + "..."
	}
	return trimmed
}
