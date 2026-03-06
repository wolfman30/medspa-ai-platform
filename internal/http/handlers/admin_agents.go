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
		ID: "prospect-researcher", Name: "Prospect Researcher", Emoji: "🔍",
		Role:     "Find and qualify new med spa prospects",
		Goals:    []string{"10+ prospects/week", "↑ research quality", "↑ qualification rate"},
		Skills:   []string{"clinic-discovery", "owner-identification", "EMR-detection", "fit-scoring"},
		Schedule: "2x/day weekdays (9AM, 3PM ET)",
		Enabled:  true,
	},
	{
		ID: "sales-closer", Name: "Sales Closer", Emoji: "🎯",
		Role:     "Coach Andrew on exactly what to do to close each prospect",
		Goals:    []string{"6+ outreach/day", "↑ reply rate", "↑ demos booked"},
		Skills:   []string{"action-plans", "DM-scripts", "follow-up-tracking", "demo-prep"},
		Schedule: "4x/day weekdays (10AM, 1PM, 4PM, 7PM ET)",
		Enabled:  true,
	},
	{
		ID: "ops", Name: "Ops Agent", Emoji: "🔧",
		Role:     "Keep infrastructure healthy",
		Goals:    []string{"↑ uptime", "↓ deploy failures", "↓ incident time"},
		Skills:   []string{"CI-monitoring", "ECS-deployment", "CloudWatch-alerts"},
		Schedule: "Every 4h",
		Enabled:  true,
	},
	{
		ID: "qa-e2e", Name: "QA Agent (E2E)", Emoji: "🧪",
		Role:     "Run E2E tests, find and fix bugs",
		Goals:    []string{"↑ E2E pass rate", "↓ prod bugs", "↑ coverage"},
		Skills:   []string{"e2e-testing", "bug-diagnosis", "auto-fix"},
		Schedule: "2x/day (10AM, 6PM ET)",
		Enabled:  true,
	},
	{
		ID: "qa-pr", Name: "QA Agent (PR Review)", Emoji: "📋",
		Role:     "Review PRs, check Sourcery comments, ensure code quality",
		Goals:    []string{"0 unreviewed PRs", "Sourcery issues fixed", "↑ merge quality"},
		Skills:   []string{"code-review", "Sourcery-triage", "PR-approval"},
		Schedule: "Every 2h",
		Enabled:  true,
	},
	{
		ID: "backend-dev", Name: "Backend Dev Agent", Emoji: "🔨",
		Role:     "Refactor Go code, keep files under 500 lines",
		Goals:    []string{"files < 500 lines", "↑ test coverage", "0 build warnings"},
		Skills:   []string{"Go-refactoring", "file-splitting", "gofmt-vet"},
		Schedule: "2x/day weekdays (2AM, 2PM ET)",
		Enabled:  true,
	},
	{
		ID: "portal", Name: "Portal Agent", Emoji: "🖥️",
		Role:     "Build admin + clinic portal features",
		Goals:    []string{"↑ portal coverage", "0 TS errors", "↑ operator UX"},
		Skills:   []string{"React-TypeScript", "API-wiring", "responsive-design"},
		Schedule: "2x/day weekdays (10AM, 4PM ET)",
		Enabled:  true,
	},
	{
		ID: "content", Name: "Content Agent", Emoji: "📱",
		Role:     "Create IG/LinkedIn content for med spa owners",
		Goals:    []string{"↑ IG followers", "↑ engagement", "↑ inbound leads"},
		Skills:   []string{"social-media", "reel-scripts", "content-calendar"},
		Schedule: "Weekly Sunday",
		Enabled:  true,
	},
	{
		ID: "market-intel", Name: "Market Intel Agent", Emoji: "📊",
		Role:     "Track competitors, industry trends, PE activity",
		Goals:    []string{"↑ competitive awareness", "↑ actionable insights", "↓ blind spots"},
		Skills:   []string{"competitor-analysis", "trend-tracking", "morning-briefs"},
		Schedule: "Daily 3AM ET",
		Enabled:  true,
	},
	{
		ID: "skill-eval", Name: "Skill Evaluator", Emoji: "⚙️",
		Role:     "Audit and optimize agent skills for reliability",
		Goals:    []string{"all skills 5/5 trigger quality", "0 cross-skill confusion", "↑ agent performance"},
		Skills:   []string{"skill-auditing", "description-optimization", "eval-benchmarking"},
		Schedule: "Weekly Sunday 3AM ET",
		Enabled:  true,
	},
	{
		ID: "taskmaster", Name: "Taskmaster", Emoji: "📋",
		Role:     "Own the Stories board — track all work, flag stale cards, enforce accountability",
		Goals:    []string{"0 stale cards (>48h)", "100% work tracked", "↑ daily velocity"},
		Skills:   []string{"board-monitoring", "card-creation", "velocity-reporting"},
		Schedule: "3x/day (8AM, 1PM, 8PM ET)",
		Enabled:  true,
	},
	{
		ID: "memory", Name: "Memory Agent", Emoji: "🧠",
		Role:     "Distill daily logs into long-term memory",
		Goals:    []string{"MEMORY.md < 500 lines", "high-signal content", "clean archives"},
		Skills:   []string{"memory-curation", "summarization", "archival"},
		Schedule: "Weekly Sunday 4AM ET",
		Enabled:  true,
	},
	{
		ID: "main", Name: "Andre (Main)", Emoji: "🐺",
		Role:     "Andrew's AI employee, sales coach, and right hand",
		Goals:    []string{"↑ revenue", "↑ automation", "close first client"},
		Skills:   []string{"orchestration", "coaching", "voice-AI", "strategy"},
		Schedule: "Always on (heartbeat every 30 min)",
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
