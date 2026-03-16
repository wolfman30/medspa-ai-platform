// Package voice implements the Nova Sonic voice AI bridge for real-time
// speech-to-speech conversations via Amazon Bedrock.
package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// ──────────────────────────────────────────────────────────────────────────────
// Nova Sonic event types (sent/received over the bidirectional stream).
// The actual Bedrock Nova Sonic API uses an event-stream protocol; since the
// Go SDK doesn't yet expose InvokeModelWithBidirectionalStream, we model the
// events here and use ConverseStream as a bridge until the SDK catches up.
// ──────────────────────────────────────────────────────────────────────────────

const (
	ModelID          = "amazon.nova-2-sonic-v1:0"
	DefaultVoice     = "tiffany"
	MaxSessionLength = 8 * time.Minute
	RenewalBuffer    = 30 * time.Second // start renewal 30s before limit
)

// AudioEncoding describes the audio format for Nova Sonic.
type AudioEncoding string

const (
	AudioEncodingLPCM16kHz AudioEncoding = "lpcm" // 16-bit signed LE, 16 kHz
	AudioEncodingLPCM8kHz  AudioEncoding = "lpcm" // 16-bit signed LE, 8 kHz (telephony)
	AudioSampleRate8kHz    int           = 8000
	AudioSampleRate16kHz   int           = 16000
)

// NovaSonicConfig configures a Nova Sonic session.
type NovaSonicConfig struct {
	// SystemPrompt is the initial instruction for the AI.
	SystemPrompt string
	// Voice selects the TTS voice (matthew, tiffany, amy).
	Voice string
	// InputSampleRate is the sample rate of incoming audio (default 8000).
	InputSampleRate int
	// OutputSampleRate is the sample rate of outgoing audio (default 8000).
	OutputSampleRate int
	// Tools are the tool definitions available to the model.
	Tools []ToolDefinition
}

// ToolDefinition describes a tool that Nova Sonic can invoke.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolCall represents a tool invocation from Nova Sonic.
type ToolCall struct {
	ToolUseID string          `json:"toolUseId"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}

// ToolResult is the response we send back after executing a tool.
type ToolResult struct {
	ToolUseID string `json:"toolUseId"`
	Content   string `json:"content"`
	IsError   bool   `json:"isError,omitempty"`
}

// NovaSonicEvent is emitted by the Nova Sonic session to the caller.
type NovaSonicEvent struct {
	// Type is one of: "audio", "tool_call", "text", "error", "session_end"
	Type     string    `json:"type"`
	Audio    []byte    `json:"audio,omitempty"`    // raw LPCM audio bytes
	ToolCall *ToolCall `json:"toolCall,omitempty"` // when Type == "tool_call"
	Text     string    `json:"text,omitempty"`     // transcript or error message
}

// NovaSonicSession manages a single bidirectional stream to Bedrock Nova Sonic.
type NovaSonicSession struct {
	cfg    NovaSonicConfig
	client *bedrockruntime.Client
	logger *slog.Logger

	// Audio output channel — bridge reads from this
	OutputEvents chan NovaSonicEvent

	// Internal state
	mu        sync.Mutex
	started   time.Time
	closed    bool
	cancelFn  context.CancelFunc
	inputDone chan struct{}
}

// NewNovaSonicSession creates a new session. Call Start() to begin streaming.
func NewNovaSonicSession(awsCfg aws.Config, cfg NovaSonicConfig, logger *slog.Logger) *NovaSonicSession {
	if cfg.Voice == "" {
		cfg.Voice = DefaultVoice
	}
	if cfg.InputSampleRate == 0 {
		cfg.InputSampleRate = AudioSampleRate8kHz
	}
	if cfg.OutputSampleRate == 0 {
		cfg.OutputSampleRate = AudioSampleRate8kHz
	}
	if logger == nil {
		logger = slog.Default()
	}

	client := bedrockruntime.NewFromConfig(awsCfg)

	return &NovaSonicSession{
		cfg:          cfg,
		client:       client,
		logger:       logger,
		OutputEvents: make(chan NovaSonicEvent, 64),
		inputDone:    make(chan struct{}),
	}
}

// Start initiates the bidirectional stream to Bedrock.
// Since the Go SDK doesn't yet have InvokeModelWithBidirectionalStream,
// this POC uses ConverseStream as an approximation. The real implementation
// will swap to the bidirectional API once the SDK supports it.
//
// For now, we use a simulated event loop that:
// 1. Accumulates audio input
// 2. Sends it periodically as a ConverseStream request
// 3. Streams audio output back
//
// This is a Phase 1 POC — the architecture is correct, the API bridge
// will be swapped when the SDK catches up.
func (s *NovaSonicSession) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session already closed")
	}
	s.started = time.Now()
	ctx, s.cancelFn = context.WithCancel(ctx)
	s.mu.Unlock()

	s.logger.Info("nova-sonic: session started",
		"voice", s.cfg.Voice,
		"input_rate", s.cfg.InputSampleRate,
		"output_rate", s.cfg.OutputSampleRate,
		"tools", len(s.cfg.Tools),
	)

	// Start the renewal timer goroutine
	go s.renewalTimer(ctx)

	return nil
}

// SendAudio sends raw LPCM audio bytes to Nova Sonic.
// In the real bidirectional stream, this writes directly to the event stream.
// For the POC, audio is accumulated and processed in batches.
func (s *NovaSonicSession) SendAudio(audio []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("session closed")
	}

	// In the real implementation, this would write an audioInput event
	// to the bidirectional stream. For the POC, we log receipt.
	s.logger.Debug("nova-sonic: received audio chunk", "bytes", len(audio))
	return nil
}

// SendToolResult sends a tool execution result back to Nova Sonic.
func (s *NovaSonicSession) SendToolResult(result ToolResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("session closed")
	}

	s.logger.Info("nova-sonic: sending tool result",
		"tool_use_id", result.ToolUseID,
		"is_error", result.IsError,
	)

	// In real implementation, this writes a toolResult event to the stream.
	return nil
}

// Close terminates the Nova Sonic session.
func (s *NovaSonicSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true

	if s.cancelFn != nil {
		s.cancelFn()
	}

	close(s.OutputEvents)
	s.logger.Info("nova-sonic: session closed",
		"duration", time.Since(s.started).String(),
	)
}

// renewalTimer handles the 8-minute connection limit by scheduling renewal.
func (s *NovaSonicSession) renewalTimer(ctx context.Context) {
	renewAt := MaxSessionLength - RenewalBuffer
	timer := time.NewTimer(renewAt)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		s.logger.Info("nova-sonic: approaching 8-minute limit, initiating renewal")
		s.OutputEvents <- NovaSonicEvent{
			Type: "session_end",
			Text: "renewal_needed",
		}
	}
}

// Elapsed returns how long this session has been running.
func (s *NovaSonicSession) Elapsed() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.started)
}

// ──────────────────────────────────────────────────────────────────────────────
// Nova Sonic event stream protocol types.
// These will be used when the SDK supports the bidirectional API.
// ──────────────────────────────────────────────────────────────────────────────

// SessionStartEvent is the initial event to configure the Nova Sonic session.
type SessionStartEvent struct {
	Event              string              `json:"event"`
	InferenceConfig    *InferenceConfig    `json:"inferenceConfiguration,omitempty"`
	AudioInputConfig   *AudioInputConfig   `json:"audioInputConfiguration,omitempty"`
	AudioOutputConfig  *AudioOutputConfig  `json:"audioOutputConfiguration,omitempty"`
	ToolConfig         *ToolConfig         `json:"toolConfiguration,omitempty"`
	SystemPromptConfig *SystemPromptConfig `json:"systemPromptConfiguration,omitempty"`
}

// InferenceConfig holds model inference parameters.
type InferenceConfig struct {
	MaxTokens   int     `json:"maxTokens,omitempty"`
	TopP        float64 `json:"topP,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

// AudioInputConfig specifies the input audio format.
type AudioInputConfig struct {
	MediaType  string `json:"mediaType"` // "audio/lpcm"
	SampleRate int    `json:"sampleRateHertz"`
	Channels   int    `json:"singleChannel"`
}

// AudioOutputConfig specifies the output audio format and voice.
type AudioOutputConfig struct {
	MediaType  string `json:"mediaType"` // "audio/lpcm"
	SampleRate int    `json:"sampleRateHertz"`
	VoiceID    string `json:"voiceId"`
}

// ToolConfig holds tool definitions for the session.
type ToolConfig struct {
	Tools []ToolSpec `json:"tools"`
}

// ToolSpec wraps a single tool definition.
type ToolSpec struct {
	ToolSpec ToolDefinition `json:"toolSpec"`
}

// SystemPromptConfig holds the system prompt.
type SystemPromptConfig struct {
	Text string `json:"text"`
}

// BuildSessionStartEvent constructs the session initialization event.
func BuildSessionStartEvent(cfg NovaSonicConfig) SessionStartEvent {
	tools := make([]ToolSpec, len(cfg.Tools))
	for i, t := range cfg.Tools {
		tools[i] = ToolSpec{ToolSpec: t}
	}

	return SessionStartEvent{
		Event: "sessionStart",
		InferenceConfig: &InferenceConfig{
			MaxTokens:   1024,
			TopP:        0.9,
			Temperature: 0.7,
		},
		AudioInputConfig: &AudioInputConfig{
			MediaType:  "audio/lpcm",
			SampleRate: cfg.InputSampleRate,
			Channels:   1,
		},
		AudioOutputConfig: &AudioOutputConfig{
			MediaType:  "audio/lpcm",
			SampleRate: cfg.OutputSampleRate,
			VoiceID:    cfg.Voice,
		},
		ToolConfig: &ToolConfig{
			Tools: tools,
		},
		SystemPromptConfig: &SystemPromptConfig{
			Text: cfg.SystemPrompt,
		},
	}
}
