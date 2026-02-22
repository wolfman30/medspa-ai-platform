package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// VoiceCallState tracks the state of an active voice call in Redis.
type VoiceCallState struct {
	// CallID is the Telnyx call control ID or conversation ID.
	CallID string `json:"call_id"`
	// OrgID is the clinic/organization UUID.
	OrgID string `json:"org_id"`
	// CallerPhone is the patient's phone in E.164.
	CallerPhone string `json:"caller_phone"`
	// ClinicPhone is the Telnyx number that received the call.
	ClinicPhone string `json:"clinic_phone"`
	// ConversationID links to the shared conversation engine state.
	ConversationID string `json:"conversation_id"`
	// LeadID links to the patient lead record.
	LeadID string `json:"lead_id"`
	// Status tracks the call lifecycle: ringing, active, ended, sms_handoff.
	Status string `json:"status"`
	// TurnCount tracks how many back-and-forth exchanges have occurred.
	TurnCount int `json:"turn_count"`
	// StartedAt is when the call was answered.
	StartedAt time.Time `json:"started_at"`
	// LastActivityAt tracks the most recent interaction.
	LastActivityAt time.Time `json:"last_activity_at"`
	// Outcome records how the call ended: booked, qualified, transferred, abandoned, sms_handoff.
	Outcome string `json:"outcome,omitempty"`
	// SMSHandoffSent is true if an SMS handoff was triggered during this call.
	SMSHandoffSent bool `json:"sms_handoff_sent,omitempty"`
}

// VoiceCallTranscriptEntry is a single turn in a voice call transcript.
type VoiceCallTranscriptEntry struct {
	Role      string    `json:"role"` // "user" or "assistant"
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

const (
	voiceCallKeyPrefix       = "voice:call:"
	voiceTranscriptKeyPrefix = "voice:transcript:"
	voiceCallTTL             = 24 * time.Hour

	VoiceCallStatusRinging    = "ringing"
	VoiceCallStatusActive     = "active"
	VoiceCallStatusEnded      = "ended"
	VoiceCallStatusSMSHandoff = "sms_handoff"
)

// VoiceCallStore manages voice call state in Redis.
type VoiceCallStore struct {
	rdb *redis.Client
}

// NewVoiceCallStore creates a voice call store backed by Redis.
func NewVoiceCallStore(rdb *redis.Client) *VoiceCallStore {
	return &VoiceCallStore{rdb: rdb}
}

func voiceCallKey(callID string) string {
	return voiceCallKeyPrefix + callID
}

func voiceTranscriptKey(callID string) string {
	return voiceTranscriptKeyPrefix + callID
}

// SaveCallState persists or updates voice call state in Redis.
func (s *VoiceCallStore) SaveCallState(ctx context.Context, state *VoiceCallState) error {
	if state == nil || state.CallID == "" {
		return fmt.Errorf("voice call state: call_id required")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("voice call state: marshal: %w", err)
	}
	return s.rdb.Set(ctx, voiceCallKey(state.CallID), data, voiceCallTTL).Err()
}

// GetCallState retrieves voice call state from Redis.
func (s *VoiceCallStore) GetCallState(ctx context.Context, callID string) (*VoiceCallState, error) {
	data, err := s.rdb.Get(ctx, voiceCallKey(callID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("voice call state: get: %w", err)
	}
	var state VoiceCallState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("voice call state: unmarshal: %w", err)
	}
	return &state, nil
}

// IncrementTurn atomically increments the turn counter and updates last activity.
func (s *VoiceCallStore) IncrementTurn(ctx context.Context, callID string) error {
	state, err := s.GetCallState(ctx, callID)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("voice call state: call %s not found", callID)
	}
	state.TurnCount++
	state.LastActivityAt = time.Now().UTC()
	return s.SaveCallState(ctx, state)
}

// EndCall marks the call as ended with an outcome.
func (s *VoiceCallStore) EndCall(ctx context.Context, callID, outcome string) error {
	state, err := s.GetCallState(ctx, callID)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("voice call state: call %s not found", callID)
	}
	state.Status = VoiceCallStatusEnded
	state.Outcome = outcome
	return s.SaveCallState(ctx, state)
}

// AppendTranscript adds a transcript entry to the voice call.
func (s *VoiceCallStore) AppendTranscript(ctx context.Context, callID string, entry VoiceCallTranscriptEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("voice transcript: marshal: %w", err)
	}
	pipe := s.rdb.Pipeline()
	pipe.RPush(ctx, voiceTranscriptKey(callID), data)
	pipe.Expire(ctx, voiceTranscriptKey(callID), voiceCallTTL)
	_, err = pipe.Exec(ctx)
	return err
}

// GetTranscript retrieves the full voice call transcript.
func (s *VoiceCallStore) GetTranscript(ctx context.Context, callID string) ([]VoiceCallTranscriptEntry, error) {
	data, err := s.rdb.LRange(ctx, voiceTranscriptKey(callID), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("voice transcript: get: %w", err)
	}
	entries := make([]VoiceCallTranscriptEntry, 0, len(data))
	for _, d := range data {
		var entry VoiceCallTranscriptEntry
		if err := json.Unmarshal([]byte(d), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
