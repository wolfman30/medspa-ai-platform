package prospects

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/lib/pq"
)

// Touch event types that count toward the Rule of 100.
var touchEventTypes = []string{
	"text_sent",
	"dm_sent",
	"email_sent",
	"call_made",
	"comment_posted",
	"voicemail_left",
	"follow_up",
}

// Rule100Response is the JSON shape returned by GET /admin/rule100/today.
type Rule100Response struct {
	Date      string                  `json:"date"`
	Touches   int                     `json:"touches"`
	Goal      int                     `json:"goal"`
	Streak    int                     `json:"streak"`
	ByType    map[string]int          `json:"byType"`
	ByProspect []Rule100ProspectCount `json:"byProspect"`
	History   []Rule100DayHistory     `json:"history"`
}

type Rule100ProspectCount struct {
	ID     string `json:"id"`
	Clinic string `json:"clinic"`
	Count  int    `json:"count"`
}

type Rule100DayHistory struct {
	Date    string `json:"date"`
	Touches int    `json:"touches"`
}

// GetRule100Today handles GET /admin/rule100/today.
func (h *Handler) GetRule100Today(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	todayStr := today.Format("2006-01-02")

	// Today's touches by type
	byType, err := h.repo.CountTouchesByType(ctx, today)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	totalToday := 0
	for _, v := range byType {
		totalToday += v
	}

	// Today's touches by prospect
	byProspect, err := h.repo.CountTouchesByProspect(ctx, today)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Last 14 days history
	history, err := h.repo.TouchHistoryDays(ctx, today, 14)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Streak: consecutive past days with >= 100 touches
	streak := 0
	for _, day := range history {
		if day.Touches >= 100 {
			streak++
		} else {
			break
		}
	}

	resp := Rule100Response{
		Date:       todayStr,
		Touches:    totalToday,
		Goal:       100,
		Streak:     streak,
		ByType:     byType,
		ByProspect: byProspect,
		History:    history,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// CountTouchesByType returns a map of simplified type name → count for touches on the given day.
func (r *Repository) CountTouchesByType(ctx context.Context, day time.Time) (map[string]int, error) {
	dayStr := day.Format("2006-01-02")
	rows, err := r.db.QueryContext(ctx, `
		SELECT event_type, COUNT(*)
		FROM prospect_events
		WHERE event_type = ANY($1)
		  AND event_date::date = $2
		GROUP BY event_type`,
		pqStringArray(touchEventTypes), dayStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]int{}
	for rows.Next() {
		var typ string
		var cnt int
		if err := rows.Scan(&typ, &cnt); err != nil {
			return nil, err
		}
		result[simplifyType(typ)] += cnt
	}
	return result, rows.Err()
}

// CountTouchesByProspect returns per-prospect touch counts for the given day.
func (r *Repository) CountTouchesByProspect(ctx context.Context, day time.Time) ([]Rule100ProspectCount, error) {
	dayStr := day.Format("2006-01-02")
	rows, err := r.db.QueryContext(ctx, `
		SELECT pe.prospect_id, COALESCE(p.clinic_name, pe.prospect_id), COUNT(*)
		FROM prospect_events pe
		LEFT JOIN prospects p ON p.id = pe.prospect_id
		WHERE pe.event_type = ANY($1)
		  AND pe.event_date::date = $2
		GROUP BY pe.prospect_id, p.clinic_name
		ORDER BY COUNT(*) DESC`,
		pqStringArray(touchEventTypes), dayStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Rule100ProspectCount
	for rows.Next() {
		var c Rule100ProspectCount
		if err := rows.Scan(&c.ID, &c.Clinic, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []Rule100ProspectCount{}
	}
	return out, rows.Err()
}

// TouchHistoryDays returns touch counts for the N days before the given day (most recent first).
func (r *Repository) TouchHistoryDays(ctx context.Context, today time.Time, days int) ([]Rule100DayHistory, error) {
	startDay := today.AddDate(0, 0, -days)
	rows, err := r.db.QueryContext(ctx, `
		SELECT d::date, COALESCE(cnt, 0)
		FROM generate_series($1::date, ($2::date - interval '1 day')::date, '1 day') AS d
		LEFT JOIN (
			SELECT event_date::date AS day, COUNT(*) AS cnt
			FROM prospect_events
			WHERE event_type = ANY($3)
			  AND event_date::date >= $1
			  AND event_date::date < $2
			GROUP BY event_date::date
		) sub ON sub.day = d::date
		ORDER BY d DESC`,
		startDay.Format("2006-01-02"), today.Format("2006-01-02"), pqStringArray(touchEventTypes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Rule100DayHistory
	for rows.Next() {
		var d time.Time
		var cnt int
		if err := rows.Scan(&d, &cnt); err != nil {
			return nil, err
		}
		out = append(out, Rule100DayHistory{
			Date:    d.Format("2006-01-02"),
			Touches: cnt,
		})
	}
	if out == nil {
		out = []Rule100DayHistory{}
	}
	return out, rows.Err()
}

// simplifyType converts event_type like "text_sent" to "text".
func simplifyType(t string) string {
	switch t {
	case "text_sent":
		return "text"
	case "dm_sent":
		return "dm"
	case "email_sent":
		return "email"
	case "call_made":
		return "call"
	case "comment_posted":
		return "comment"
	case "voicemail_left":
		return "voicemail"
	case "follow_up":
		return "follow_up"
	default:
		return t
	}
}

// pqStringArray wraps a string slice for use with PostgreSQL ANY($1::text[]).
func pqStringArray(ss []string) interface{} {
	return pq.Array(ss)
}

