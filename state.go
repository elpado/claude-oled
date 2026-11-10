package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Window is one usage window (5h session or 7d week) as reported by Claude Code.
type Window struct {
	UsedPercent *float64   `json:"used_percent,omitempty"`
	ResetsAt    *time.Time `json:"resets_at,omitempty"`
}

// Snapshot is what the statusline hook writes and the OLED daemon reads.
type Snapshot struct {
	UpdatedAt      time.Time `json:"updated_at"`
	SessionID      string    `json:"session_id,omitempty"`
	Model          string    `json:"model,omitempty"`
	ContextPercent *float64  `json:"context_percent,omitempty"`
	CostUSD        *float64  `json:"cost_usd,omitempty"`
	FiveHour       Window    `json:"five_hour"`
	SevenDay       Window    `json:"seven_day"`
}

func stateDir() string {
	if d := os.Getenv("CLAUDE_OLED_STATE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".claude")
}

func statePath() string { return filepath.Join(stateDir(), "oled-state.json") }

// parseStatusline turns the JSON Claude Code pipes into a statusLine command into a Snapshot.
// It is deliberately lenient: every field is optional, unknown fields are ignored.
func parseStatusline(raw []byte) (Snapshot, error) {
	var in map[string]any
	if err := json.Unmarshal(raw, &in); err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{UpdatedAt: time.Now()}
	s.SessionID, _ = in["session_id"].(string)
	if m, ok := in["model"].(map[string]any); ok {
		if n, ok := m["display_name"].(string); ok {
			s.Model = n
		} else if n, ok := m["id"].(string); ok {
			s.Model = n
		}
	}
	if cw, ok := in["context_window"].(map[string]any); ok {
		s.ContextPercent = num(cw["used_percentage"])
	}
	if c, ok := in["cost"].(map[string]any); ok {
		s.CostUSD = num(c["total_cost_usd"])
	}
	if rl, ok := in["rate_limits"].(map[string]any); ok {
		s.FiveHour = window(rl["five_hour"])
		s.SevenDay = window(rl["seven_day"])
	}
	return s, nil
}

func window(v any) Window {
	m, ok := v.(map[string]any)
	if !ok {
		return Window{}
	}
	w := Window{UsedPercent: num(m["used_percentage"])}
	if w.UsedPercent == nil {
		w.UsedPercent = num(m["utilization"])
	}
	w.ResetsAt = timestamp(m["resets_at"])
	return w
}

func num(v any) *float64 {
	switch x := v.(type) {
	case float64:
		return &x
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return &f
		}
	}
	return nil
}

// timestamp accepts unix seconds, unix milliseconds or an RFC 3339 string.
func timestamp(v any) *time.Time {
	switch x := v.(type) {
	case float64:
		var t time.Time
		if x > 1e12 {
			t = time.UnixMilli(int64(x))
		} else {
			t = time.Unix(int64(x), 0)
		}
		return &t
	case string:
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return &t
		}
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return timestamp(f)
		}
	}
	return nil
}

// mergeSnapshot keeps rate-limit windows from the previous snapshot when the new
// statusline payload has none (e.g. the very first render of a session before
// any API response). Rate limits are account-wide, so the last known value is
// still the best information we have.
func mergeSnapshot(prev, next Snapshot) Snapshot {
	if next.FiveHour.UsedPercent == nil {
		next.FiveHour = prev.FiveHour
	}
	if next.SevenDay.UsedPercent == nil {
		next.SevenDay = prev.SevenDay
	}
	return next
}

func readSnapshot(path string) (Snapshot, error) {
	var s Snapshot
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}

func writeSnapshot(path string, s Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
