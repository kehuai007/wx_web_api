package storage

import (
	"encoding/json"
	"time"
)

// RequestLog is one row in request_log. Request and Result are stored as JSON
// strings in SQLite; we keep them as RawMessage here so callers can pass them
// through without re-encoding.
type RequestLog struct {
	ID         int64           `json:"id"`
	Ts         int64           `json:"ts"`
	TokenLabel string          `json:"token_label"`
	Kind       string          `json:"kind"`    // 'url' | 'finder' | 'auth'
	Source     string          `json:"source"`  // 'external' | 'admin_test'
	ClientIP   string          `json:"client_ip"`
	Request    json.RawMessage `json:"request"`
	Status     int             `json:"status"`  // 0 / 1 / 401
	LatencyMs  int64           `json:"latency_ms"`
	Msg        string          `json:"msg"`
	Result     json.RawMessage `json:"result,omitempty"`
}

// HistoryQuery is the filter shape accepted by QueryHistory. Empty / 'all'
// fields disable the corresponding WHERE clause.
type HistoryQuery struct {
	Range  string // 'today' | '7d' | '30d' | 'all'
	Kind   string // 'url' | 'finder' | 'auth' | 'all'
	Status string // 'ok' | 'err' | 'auth_err' | 'all'
	Token  string // token_label exact match, 'all' or '' disables
	Q      string // free-text LIKE %q% against request column
	Page   int    // 1-based
	Size   int    // 1..200, default 50 if 0
}

// HistoryPage is the response shape from QueryHistory.
type HistoryPage struct {
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Size  int          `json:"size"`
	Items []RequestLog `json:"items"`
}

// StartOfTodayMs returns the unix-millisecond timestamp of the start of the
// current local day. Kept here (storage package) because system.go's
// collectSnapshot uses the same notion of "today" and must not drift.
func StartOfTodayMs() int64 {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return start.UnixMilli()
}

// startOfTodayMsPtr returns a pointer to the value (nil = no lower bound on ts).
func (q HistoryQuery) tsLowerBoundPtr() *int64 {
	switch q.Range {
	case "":
		// empty: treat as 'all' for safety
		return nil
	case "today":
		v := StartOfTodayMs()
		return &v
	case "7d":
		v := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
		return &v
	case "30d":
		v := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
		return &v
	case "all":
		return nil
	}
	return nil
}

func (q HistoryQuery) statusValue() *int {
	switch q.Status {
	case "":
		return nil
	case "ok":
		v := 0
		return &v
	case "err":
		v := 1
		return &v
	case "auth_err":
		v := 401
		return &v
	case "all":
		return nil
	}
	return nil
}

func (q HistoryQuery) kindValue() *string {
	if q.Kind == "" || q.Kind == "all" {
		return nil
	}
	v := q.Kind
	return &v
}

func (q HistoryQuery) tokenValue() *string {
	if q.Token == "" || q.Token == "all" {
		return nil
	}
	v := q.Token
	return &v
}

func (q HistoryQuery) pageSize() (int, int) {
	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.Size
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	return page, size
}
