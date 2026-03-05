package handlers

import (
	"encoding/json"
	"net/http"
)

// AgentStatus represents an AI agent's configuration and status.
type AgentStatus struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Emoji    string   `json:"emoji"`
	Role     string   `json:"role"`
	Goals    []string `json:"goals"`
	Skills   []string `json:"skills"`
	Schedule string   `json:"schedule"`
	Enabled  bool     `json:"enabled"`
}

var agentDefinitions = []AgentStatus{
	{
		ID: "sales", Name: "Sales Agent", Emoji: "🎯",
		Role:     "Find and qualify med spa prospects",
		Goals:    []string{"↑ pipeline", "↑ response rate", "↑ meetings"},
		Skills:   []string{"prospecting", "outreach", "qualification"},
		Schedule: "4x/day weekdays",
		Enabled:  true,
	},
	{
		ID: "ops", Name: "Ops Agent", Emoji: "🔧",
		Role:     "Keep infrastructure healthy",
		Goals:    []string{"↑ uptime", "↓ deploy failures", "↓ incident time"},
		Skills:   []string{"monitoring", "deployment", "incident-response"},
		Schedule: "Every 4h",
		Enabled:  true,
	},
	{
		ID: "qa", Name: "QA Agent", Emoji: "🧪",
		Role:     "Ensure SMS bot works flawlessly",
		Goals:    []string{"↑ E2E pass rate", "↓ prod bugs", "↑ coverage"},
		Skills:   []string{"e2e-testing", "pr-review", "regression"},
		Schedule: "2x/day + PR review every 2h",
		Enabled:  true,
	},
	{
		ID: "content", Name: "Content Agent", Emoji: "📱",
		Role:     "Create content that attracts med spa owners",
		Goals:    []string{"↑ IG followers", "↑ engagement", "↑ inbound leads"},
		Skills:   []string{"social-media", "copywriting", "scheduling"},
		Schedule: "Weekly Sunday",
		Enabled:  true,
	},
	{
		ID: "market-intel", Name: "Market Intel Agent", Emoji: "🔍",
		Role:     "Track competitors and opportunities",
		Goals:    []string{"↑ awareness", "↑ actionable insights", "↓ blind spots"},
		Skills:   []string{"research", "competitive-analysis", "reporting"},
		Schedule: "Daily 3AM ET",
		Enabled:  true,
	},
	{
		ID: "memory", Name: "Memory Agent", Emoji: "🧠",
		Role:     "Keep memory system healthy",
		Goals:    []string{"MEMORY.md < 500 lines", "high-signal", "clean history"},
		Skills:   []string{"memory-curation", "summarization"},
		Schedule: "Weekly Sunday",
		Enabled:  true,
	},
	{
		ID: "main", Name: "Andre (Main)", Emoji: "☀️",
		Role:     "Andrew's AI employee, coach, and right hand",
		Goals:    []string{"↑ revenue", "↑ automation", "↓ time-to-close"},
		Skills:   []string{"orchestration", "coaching", "strategy"},
		Schedule: "Always on (heartbeat)",
		Enabled:  true,
	},
}

// HandleAgentsStatus returns the list of AI agent definitions.
// GET /admin/agents/status
func HandleAgentsStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"agents": agentDefinitions,
	})
}
