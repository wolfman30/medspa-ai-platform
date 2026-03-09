package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// FetchLocations retrieves all locations for a merchant from Square.
func (s *SquareOAuthService) FetchLocations(ctx context.Context, accessToken string) ([]squareLocation, error) {
	locationsURL := fmt.Sprintf("%s/v2/locations", s.baseURL())

	req, err := http.NewRequestWithContext(ctx, "GET", locationsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create locations request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Square-Version", "2024-01-18")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("locations request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read locations response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("square locations fetch failed", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("locations fetch failed: status %d", resp.StatusCode)
	}

	var locResp squareLocationsResponse
	if err := json.Unmarshal(body, &locResp); err != nil {
		return nil, fmt.Errorf("parse locations response: %w", err)
	}

	return locResp.Locations, nil
}

// GetDefaultLocation returns the first active location for a merchant.
func (s *SquareOAuthService) GetDefaultLocation(ctx context.Context, accessToken string) (string, error) {
	locations, err := s.FetchLocations(ctx, accessToken)
	if err != nil {
		return "", err
	}

	// Return the first ACTIVE location
	for _, loc := range locations {
		if loc.Status == "ACTIVE" {
			s.logger.Info("found active square location", "location_id", loc.ID, "name", loc.Name)
			return loc.ID, nil
		}
	}

	// If no active, return first available
	if len(locations) > 0 {
		s.logger.Warn("no active square locations, using first available", "location_id", locations[0].ID)
		return locations[0].ID, nil
	}

	return "", fmt.Errorf("no locations found for merchant")
}
