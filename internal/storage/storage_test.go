package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTempStorage(t *testing.T) *Storage {
	t.Helper()
	dir := t.TempDir()
	s := &Storage{}
	if err := s.Init(filepath.Join(dir, "test.db")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	})
	return s
}

func TestLogAndQuery(t *testing.T) {
	s := newTempStorage(t)
	now := time.Now().UnixMilli()

	rows := []RequestLog{
		{Ts: now - 3000, TokenLabel: "alpha", Kind: "url", Source: "external", ClientIP: "127.0.0.1",
			Request: json.RawMessage(`{"url":"https://a"}`), Status: 0, LatencyMs: 100, Msg: ""},
		{Ts: now - 2000, TokenLabel: "alpha", Kind: "finder", Source: "admin_test", ClientIP: "::1",
			Request: json.RawMessage(`{"objectId":"x"}`), Status: 1, LatencyMs: 50, Msg: "fail"},
		// 401 is always kind='auth' in this system (TokenAuth aborts before
		// the handler runs). The request payload carries the attempted path.
		{Ts: now - 1000, TokenLabel: "beta", Kind: "auth", Source: "external", ClientIP: "10.0.0.42",
			Request: json.RawMessage(`{"path":"POST /wx"}`), Status: 401, LatencyMs: 5, Msg: "token expired", Result: nil},
	}
	for i := range rows {
		if err := s.LogRequest(&rows[i]); err != nil {
			t.Fatalf("LogRequest[%d]: %v", i, err)
		}
	}

	// all
	p, err := s.QueryHistory(HistoryQuery{Range: "all", Page: 1, Size: 50})
	if err != nil {
		t.Fatalf("QueryHistory all: %v", err)
	}
	if p.Total != 3 {
		t.Fatalf("total = %d, want 3", p.Total)
	}
	if p.Items[0].TokenLabel != "beta" {
		t.Fatalf("newest row first: got %q", p.Items[0].TokenLabel)
	}
	// client_ip round-trips for each row (newest first: beta, alpha-finder, alpha-url)
	wantIPs := []string{"10.0.0.42", "::1", "127.0.0.1"}
	for i, want := range wantIPs {
		if p.Items[i].ClientIP != want {
			t.Fatalf("client_ip[%d] = %q, want %q", i, p.Items[i].ClientIP, want)
		}
	}

	// filter by token_label
	p, err = s.QueryHistory(HistoryQuery{Range: "all", Token: "alpha", Page: 1, Size: 50})
	if err != nil {
		t.Fatalf("QueryHistory token=alpha: %v", err)
	}
	if p.Total != 2 {
		t.Fatalf("token=alpha total = %d, want 2", p.Total)
	}

	// filter by status=auth_err
	p, err = s.QueryHistory(HistoryQuery{Range: "all", Status: "auth_err", Page: 1, Size: 50})
	if err != nil {
		t.Fatalf("QueryHistory status=auth_err: %v", err)
	}
	if p.Total != 1 {
		t.Fatalf("status=auth_err total = %d, want 1", p.Total)
	}
	if p.Items[0].Kind != "auth" {
		t.Fatalf("auth_err row should be kind=auth, got %q", p.Items[0].Kind)
	}

	// filter by text — match the path field of the auth row
	p, err = s.QueryHistory(HistoryQuery{Range: "all", Q: "POST /wx", Page: 1, Size: 50})
	if err != nil {
		t.Fatalf("QueryHistory q=POST /wx: %v", err)
	}
	if p.Total != 1 {
		t.Fatalf("q=POST /wx total = %d, want 1", p.Total)
	}

	// count helpers
	if n, _ := s.Count(); n != 3 {
		t.Fatalf("Count = %d, want 3", n)
	}
	if n, _ := s.CountSince(now - 1500); n != 1 {
		t.Fatalf("CountSince(now-1500) = %d, want 1", n)
	}
	if n, _ := s.CountErrors(); n != 2 {
		t.Fatalf("CountErrors = %d, want 2 (status=1 + status=401)", n)
	}

	// delete one
	n, err := s.DeleteByIDs([]int64{rows[0].ID})
	if err != nil {
		t.Fatalf("DeleteByIDs: %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteByIDs affected = %d, want 1", n)
	}
	if c, _ := s.Count(); c != 2 {
		t.Fatalf("Count after delete = %d, want 2", c)
	}

	// purge older
	purged, err := s.PurgeOlderThan(now - 1500)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	// rows[1] is now-2000 (older than now-1500) but not deleted by id yet → purged
	if purged != 1 {
		t.Fatalf("PurgeOlderThan affected = %d, want 1", purged)
	}
	if c, _ := s.Count(); c != 1 {
		t.Fatalf("Count after purge = %d, want 1", c)
	}

	// delete all
	all, err := s.DeleteAll()
	if err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if all != 1 {
		t.Fatalf("DeleteAll affected = %d, want 1", all)
	}
	if c, _ := s.Count(); c != 0 {
		t.Fatalf("Count after DeleteAll = %d, want 0", c)
	}
}

// TestClientIPMigration simulates the pre-client_ip schema, inserts a row,
// then re-opens with the current schema. The migration must add the column
// without dropping the existing row, and the existing row must read back
// with client_ip='' (the column default).
func TestClientIPMigration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	// Create the legacy table by hand (no client_ip column).
	{
		legacy, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open legacy: %v", err)
		}
		if _, err := legacy.Exec(`CREATE TABLE request_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			token_label TEXT NOT NULL,
			kind TEXT NOT NULL,
			source TEXT NOT NULL,
			request TEXT NOT NULL,
			status INTEGER NOT NULL,
			latency_ms INTEGER NOT NULL,
			msg TEXT NOT NULL DEFAULT '',
			result_data TEXT
		)`); err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
		if _, err := legacy.Exec(
			`INSERT INTO request_log (ts, token_label, kind, source, request, status, latency_ms, msg)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			time.Now().UnixMilli(), "legacy-token", "url", "external",
			`{"url":"https://legacy"}`, 0, 12, ""); err != nil {
			t.Fatalf("insert legacy row: %v", err)
		}
		_ = legacy.Close()
	}

	// Re-open via the production Init — should migrate, not lose data.
	s := &Storage{}
	if err := s.Init(dbPath); err != nil {
		t.Fatalf("Init (post-migration): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	page, err := s.QueryHistory(HistoryQuery{Range: "all", Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("legacy row dropped during migration: total=%d", page.Total)
	}
	if page.Items[0].TokenLabel != "legacy-token" {
		t.Fatalf("legacy row mangled: token=%q", page.Items[0].TokenLabel)
	}
	if page.Items[0].ClientIP != "" {
		t.Fatalf("backfilled client_ip should be empty, got %q", page.Items[0].ClientIP)
	}

	// New inserts now carry an IP through end-to-end.
	if err := s.LogRequest(&RequestLog{
		Ts: time.Now().UnixMilli(), TokenLabel: "new", Kind: "url", Source: "external",
		ClientIP: "192.168.1.5", Request: json.RawMessage(`{"url":"https://new"}`),
		Status: 0, LatencyMs: 7, Msg: "",
	}); err != nil {
		t.Fatalf("LogRequest post-migration: %v", err)
	}
	page, err = s.QueryHistory(HistoryQuery{Range: "all", Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("QueryHistory post-insert: %v", err)
	}
	if page.Total != 2 || page.Items[0].ClientIP != "192.168.1.5" {
		t.Fatalf("post-migration insert: total=%d, top.ClientIP=%q", page.Total, page.Items[0].ClientIP)
	}
}

func TestCountSuccessSince_ExcludesNonZeroStatus(t *testing.T) {
	s := newTempStorage(t)
	now := time.Now().UnixMilli()

	rows := []RequestLog{
		{Ts: now - 3000, TokenLabel: "alpha", Kind: "url", Source: "external", ClientIP: "1.1.1.1",
			Request: json.RawMessage(`{"url":"https://a"}`), Status: 0, LatencyMs: 10},
		{Ts: now - 2000, TokenLabel: "alpha", Kind: "url", Source: "external", ClientIP: "1.1.1.1",
			Request: json.RawMessage(`{"url":"https://b"}`), Status: 1, LatencyMs: 20, Msg: "err"},
		{Ts: now - 1000, TokenLabel: "beta", Kind: "auth", Source: "external", ClientIP: "1.1.1.1",
			Request: json.RawMessage(`{"path":"x"}`), Status: 401, LatencyMs: 5, Msg: "expired"},
	}
	for i := range rows {
		if err := s.LogRequest(&rows[i]); err != nil {
			t.Fatalf("LogRequest[%d]: %v", i, err)
		}
	}

	// since = now - 1500 ms → only the 401 row qualifies, but we want status=0
	// so the result should be 0.
	n, err := s.CountSuccessSince(now - 1500)
	if err != nil {
		t.Fatalf("CountSuccessSince: %v", err)
	}
	if n != 0 {
		t.Fatalf("CountSuccessSince(now-1500) = %d, want 0 (no status=0 rows in range)", n)
	}

	// since = 0 → all rows qualify, only 1 has status=0
	n, err = s.CountSuccessSince(0)
	if err != nil {
		t.Fatalf("CountSuccessSince(0): %v", err)
	}
	if n != 1 {
		t.Fatalf("CountSuccessSince(0) = %d, want 1", n)
	}
}

func TestCountSuccessBetween_InclusiveStart_ExclusiveEnd(t *testing.T) {
	s := newTempStorage(t)
	ts := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC).UnixMilli()
	// 3 rows: at start, between, at end
	rows := []RequestLog{
		{Ts: ts, TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1},
		{Ts: ts + 1000, TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1},
		{Ts: ts + 2000, TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1},
	}
	for i := range rows {
		if err := s.LogRequest(&rows[i]); err != nil {
			t.Fatalf("LogRequest[%d]: %v", i, err)
		}
	}

	// [ts, ts+2000) → inclusive of start, exclusive of end → 2 rows
	n, err := s.CountSuccessBetween(ts, ts+2000)
	if err != nil {
		t.Fatalf("CountSuccessBetween: %v", err)
	}
	if n != 2 {
		t.Fatalf("CountSuccessBetween(ts, ts+2000) = %d, want 2 (inclusive start, exclusive end)", n)
	}
}
