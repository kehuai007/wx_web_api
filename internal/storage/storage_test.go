package storage

import (
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
		{Ts: now - 3000, TokenLabel: "alpha", Kind: "url", Source: "external",
			Request: json.RawMessage(`{"url":"https://a"}`), Status: 0, LatencyMs: 100, Msg: ""},
		{Ts: now - 2000, TokenLabel: "alpha", Kind: "finder", Source: "admin_test",
			Request: json.RawMessage(`{"objectId":"x"}`), Status: 1, LatencyMs: 50, Msg: "fail"},
		// 401 is always kind='auth' in this system (TokenAuth aborts before
		// the handler runs). The request payload carries the attempted path.
		{Ts: now - 1000, TokenLabel: "beta", Kind: "auth", Source: "external",
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
