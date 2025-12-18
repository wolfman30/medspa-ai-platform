package aesthetic

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

// SelectAPIClient simulates an Aesthetic Record scheduling upstream using
// Nextech Select API (FHIR STU3) Slot responses as the closest public reference.
type SelectAPIClient struct {
	baseURL     string
	bearerToken string
	httpClient  *http.Client
}

type SelectAPIConfig struct {
	BaseURL     string
	BearerToken string
	HTTPClient  *http.Client
}

func NewSelectAPIClient(cfg SelectAPIConfig) (*SelectAPIClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("aesthetic: select api base url is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &SelectAPIClient{
		baseURL:     strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/"),
		bearerToken: strings.TrimSpace(cfg.BearerToken),
		httpClient:  httpClient,
	}, nil
}

type selectBundle struct {
	ResourceType string          `json:"resourceType"`
	Entry        []selectEntry   `json:"entry"`
	Total        json.RawMessage `json:"total,omitempty"`
}

type selectEntry struct {
	Resource json.RawMessage `json:"resource"`
}

type selectSlot struct {
	ResourceType string                 `json:"resourceType"`
	ID           string                 `json:"id"`
	Status       string                 `json:"status"`
	Start        string                 `json:"start"`
	End          string                 `json:"end"`
	Schedule     selectReference        `json:"schedule"`
	Extension    []selectExtension      `json:"extension,omitempty"`
	Contained    []json.RawMessage      `json:"contained,omitempty"`
	Other        map[string]interface{} `json:"-"`
}

type selectReference struct {
	Reference string `json:"reference"`
	Display   string `json:"display,omitempty"`
}

type selectExtension struct {
	URL            string          `json:"url"`
	ValueReference *selectRefValue `json:"valueReference,omitempty"`
	ValueString    string          `json:"valueString,omitempty"`
}

type selectRefValue struct {
	Reference string `json:"reference,omitempty"`
	Display   string `json:"display,omitempty"`
}

type resourceTypeProbe struct {
	ResourceType string `json:"resourceType"`
}

type selectPractitioner struct {
	ResourceType string            `json:"resourceType"`
	ID           string            `json:"id"`
	Name         []selectHumanName `json:"name,omitempty"`
}

type selectHumanName struct {
	Text   string   `json:"text,omitempty"`
	Family string   `json:"family,omitempty"`
	Given  []string `json:"given,omitempty"`
}

var practitionerIDFromScheduleRef = regexp.MustCompile(`(?i)practitioner_([^/]+)$`)

func (c *SelectAPIClient) GetAvailability(ctx context.Context, req emr.AvailabilityRequest) ([]emr.Slot, error) {
	if strings.TrimSpace(req.ClinicID) == "" {
		return nil, errors.New("aesthetic: ClinicID is required")
	}

	start := req.StartDate
	end := req.EndDate
	if start.IsZero() {
		start = time.Now().UTC()
	}
	if end.IsZero() || !end.After(start) {
		end = start.AddDate(0, 0, 7)
	}

	params := url.Values{}
	params.Add("start", "ge"+start.UTC().Format("2006-01-02"))
	params.Add("start", "lt"+end.UTC().Format("2006-01-02"))
	params.Add("schedule.actor", "location/"+strings.TrimSpace(req.ClinicID))
	if strings.TrimSpace(req.ProviderID) != "" {
		params.Add("schedule.actor", "practitioner/"+strings.TrimSpace(req.ProviderID))
	}
	if req.DurationMins > 0 {
		params.Set("slot-length", strconv.Itoa(req.DurationMins))
	}

	endpoint := c.baseURL + "/slot?" + params.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("aesthetic: create request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")
	if c.bearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("aesthetic: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aesthetic: upstream error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var bundle selectBundle
	if err := json.NewDecoder(resp.Body).Decode(&bundle); err != nil {
		return nil, fmt.Errorf("aesthetic: decode slot bundle: %w", err)
	}

	slots := make([]emr.Slot, 0, len(bundle.Entry))
	for _, entry := range bundle.Entry {
		var probe resourceTypeProbe
		if err := json.Unmarshal(entry.Resource, &probe); err != nil {
			continue
		}
		if probe.ResourceType != "Slot" {
			continue
		}

		var slot selectSlot
		if err := json.Unmarshal(entry.Resource, &slot); err != nil {
			continue
		}

		parsed, err := parseSelectSlot(slot)
		if err != nil {
			continue
		}
		slots = append(slots, *parsed)
	}

	return slots, nil
}

func parseSelectSlot(slot selectSlot) (*emr.Slot, error) {
	startTime, err := time.Parse(time.RFC3339, slot.Start)
	if err != nil {
		return nil, err
	}
	endTime, err := time.Parse(time.RFC3339, slot.End)
	if err != nil {
		return nil, err
	}

	out := &emr.Slot{
		ID:        strings.TrimSpace(slot.ID),
		StartTime: startTime,
		EndTime:   endTime,
		Status:    slot.Status,
	}
	if out.ID == "" {
		out.ID = derivedSlotID(slot.Schedule.Reference, slot.Start, slot.End)
	}

	providerID, providerName := providerFromContained(slot.Contained)
	if providerID == "" {
		providerID = providerFromScheduleReference(slot.Schedule.Reference)
	}
	out.ProviderID = providerID
	out.ProviderName = providerName

	out.ServiceType = serviceTypeFromExtensions(slot.Extension)

	return out, nil
}

func derivedSlotID(scheduleRef, start, end string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(scheduleRef) + "|" + strings.TrimSpace(start) + "|" + strings.TrimSpace(end)))
	return "derived_" + hex.EncodeToString(sum[:8])
}

func providerFromContained(contained []json.RawMessage) (string, string) {
	for _, raw := range contained {
		var probe resourceTypeProbe
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		if probe.ResourceType != "Practitioner" {
			continue
		}

		var practitioner selectPractitioner
		if err := json.Unmarshal(raw, &practitioner); err != nil {
			continue
		}

		name := ""
		if len(practitioner.Name) > 0 {
			h := practitioner.Name[0]
			if strings.TrimSpace(h.Text) != "" {
				name = strings.TrimSpace(h.Text)
			} else {
				parts := []string{}
				if strings.TrimSpace(h.Family) != "" {
					parts = append(parts, strings.TrimSpace(h.Family))
				}
				if len(h.Given) > 0 && strings.TrimSpace(h.Given[0]) != "" {
					parts = append(parts, strings.TrimSpace(h.Given[0]))
				}
				if len(parts) > 0 {
					if len(parts) == 2 {
						name = parts[0] + ", " + parts[1]
					} else {
						name = strings.Join(parts, " ")
					}
				}
			}
		}

		return strings.TrimSpace(practitioner.ID), name
	}

	return "", ""
}

func providerFromScheduleReference(reference string) string {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return ""
	}
	if match := practitionerIDFromScheduleRef.FindStringSubmatch(reference); len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	parts := strings.Split(reference, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func serviceTypeFromExtensions(ext []selectExtension) string {
	for _, e := range ext {
		url := strings.ToLower(e.URL)
		if !strings.Contains(url, "appointment-purpose") {
			continue
		}
		if e.ValueReference == nil {
			continue
		}
		if strings.TrimSpace(e.ValueReference.Display) == "" {
			continue
		}
		return strings.TrimSpace(e.ValueReference.Display)
	}

	for _, e := range ext {
		url := strings.ToLower(e.URL)
		if !strings.Contains(url, "appointment-type") {
			continue
		}
		if e.ValueReference == nil {
			continue
		}
		if strings.TrimSpace(e.ValueReference.Display) == "" {
			continue
		}
		return strings.TrimSpace(e.ValueReference.Display)
	}

	return ""
}
