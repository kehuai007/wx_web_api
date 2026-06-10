# 解析历史 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the placeholder `解析历史` page with a fully working admin history viewer backed by a new SQLite `request_log` table. Every `/wx` and `/wx/finder` call is recorded (token label, kind, source, request, status, latency, msg, result), filterable by time/kind/status/token/text, with row expand showing the parsed payload, paginated 50/page, with single/batch/full delete. Also feeds the real `stats` field on the `/system` page and adds token-label + retention-days controls to `/settings`.

**Architecture:** New `internal/storage` package owns the SQLite connection (modernc.org/sqlite, pure Go, no CGO), schema bootstrap, and the full CRUD/query API for `request_log`. Config grows a `Token.Label` and `Config.HistoryRetentionDays` with startup-time backfill. Handler's `TokenAuth` middleware records 401 attempts (with `kind='auth'`); `ParseWxURL` / `ParseFinderFeedByObjectID` record success/failure via an async `writeLog` helper. New `internal/handler/history.go` exposes `GET /api/history` and `DELETE /api/history`. Retention is a daily goroutine started by `SystemHub.Start`. Frontend: settings page grows label + retention controls, test page sets `X-Wx-Source: admin_test` header, system page renders real `stats`, history page is a full rewrite with filter bar / list / row expand / pagination / delete.

**Tech Stack:** Go 1.25.6 + Gin v1.12.0 + `modernc.org/sqlite` v1.34.5 (new dep). Vanilla JS frontend (no build step). `embed.FS` rebuild required after web asset changes.

**Branch policy:** Implementer commits directly to `main` (user's standing instruction). All commits include `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`.

---

## File Structure

| File | Responsibility |
|---|---|
| `go.mod` / `go.sum` | **Modify.** Add `modernc.org/sqlite` v1.34.5. |
| `internal/storage/storage.go` | **Create.** `Storage` struct holding `*sql.DB`. `Init(path)`, `Close()`, `LogRequest()`, `QueryHistory()`, `DeleteByIDs()`, `DeleteAll()`, `PurgeOlderThan()`, `Count()`, `CountSince()`, `CountErrors()`. |
| `internal/storage/schema.go` | **Create.** `CREATE TABLE` and `CREATE INDEX` SQL string constants. |
| `internal/storage/log.go` | **Create.** `RequestLog`, `HistoryQuery`, `HistoryPage` types + `startOfTodayMs()` helper. |
| `internal/storage/storage_test.go` | **Create.** Round-trip test (write → read → delete → count) using `t.TempDir()` SQLite path. |
| `internal/config/config.go` | **Modify.** Add `Token.Label`; add `Config.HistoryRetentionDays`; add `loadRawJson()` helper for backfill detection. Backfill label on startup; inject `history_retention_days=30` if file lacks the key. |
| `internal/handler/handler.go` | **Modify.** Add `storage *storage.Storage` field; extend `New(pwd, storage)`. Add `writeLog` async helper. Extend `TokenAuth` to inject `token_label` + `source` into gin context, and to log 401 attempts (kind='auth'). Extend `ParseWxURL` / `ParseFinderFeedByObjectID` to call `writeLog` on every path (success, business error, bad request). |
| `internal/handler/history.go` | **Create.** `HistoryHandler` (or extend Handler) with `GetHistory` (filter+page) and `DeleteHistory` (by ids / all) handlers. Parses query string into `storage.HistoryQuery`. |
| `internal/handler/system.go` | **Modify.** Extend `collectSnapshot()` to include `stats` from storage. `Start(ctx, storage)` signature gains storage; spawns `RunRetentionLoop` internally. |
| `internal/handler/broadcaster.go` | **Modify.** Add `RunRetentionLoop(ctx, storage)` method. |
| `internal/handler/settings.go` | **Modify.** `UpdateConfig` accepts the new `history_retention_days` and `tokens[].label` fields (full PUT semantics). `GetConfig` already returns the whole config object — no change needed. |
| `main.go` | **Modify.** Call `storage.Init(exeDir+"/wx_web_api.db")`; pass to `handler.New`; pass to `SystemHub.Start(ctx, storage)`; register `GET /api/history` and `DELETE /api/history` routes. |
| `web/static/js/pages/settings.js` | **Modify.** Add `label` input to each token row; add "历史保留天数" input at the bottom of the config block; show "当前已记录: N 条" by reading `GET /api/history?size=1&page=1`. |
| `web/static/js/pages/test.js` | **Modify.** Add `X-Wx-Source: admin_test` header to both `/wx` and `/wx/finder` fetch calls. |
| `web/static/js/pages/system.js` | **Modify.** Render real `stats.total` / `stats.today` / `stats.errors` from the snapshot frame; remove the "下期接入真实数据" placeholder. |
| `web/static/js/pages/history.js` | **Create (full rewrite).** Filter bar, table with expand, pagination, single/batch/full delete, empty states, error handling. |
| `web/static/css/pages.css` | **Modify (append).** History filter / table / row / row-detail / pagination / badges / source badges. Reuse test-page `.field`, `.copy-btn`, `.result-fields`, `.result-msg`, `.empty` classes. |

Files **not** modified (per spec §"不改动的文件"):
- `internal/service/parser.go`, `internal/model/response.go`
- `web/index.html` (`pages/history.js?v=1` already wired; cache buster not bumped — rebuild is the trigger)
- `web/static/js/router.js` / `auth.js` / `api.js` / `store.js` / `app.js`
- `web/static/js/pages/dashboard.js` / `users.js` (dashboard is the next spec; users continues to be a stub)
- `internal/buildinfo/buildinfo.go` (unrelated to history)
- All `go test` infrastructure beyond the one new `storage_test.go`

---

## Design decisions worth flagging

1. **modernc.org/sqlite over mattn/go-sqlite3.** Pure-Go, no CGO. `build.bat` (line 11: `go build -ldflags "-s -w" -o dist/wx_web_api.exe .`) currently works without a C toolchain; adding CGO would force the user to install TDM-GCC / MinGW. Performance difference is irrelevant for an admin tool's QPS.
2. **Async log writes via goroutine.** Log latency must never inflate `/wx` P99 — admin callers pay for the upstream fetch, not for our WAL commit. The handler still returns the parsed payload first; the goroutine writes the log row 1-10ms later. Failures inside the goroutine only get `log.Printf` — never propagated to the caller.
3. **Store `token_label`, not the token value.** Spec §"配置改动" — DB leaking should not give an attacker every active API token. The label is admin-set in `/settings`; if not set, backfill uses value's first 8 chars + `...` (already-truncated display form, so no info loss vs. a UI display).
4. **401 = `kind='auth'`, `status=401`.** Distinct from `kind='url'`/`'finder'`+`status=1` so the kind column remains semantically pure (it's "what endpoint was called", and an auth-rejected call never reached an endpoint). UI filter `status=auth_err` surfaces these; the kind badge in the row just says `auth` for visual disambiguation.
5. **Storage layer gets one `storage_test.go`** with a round-trip + filter + delete suite, using `t.TempDir()` for an isolated DB file. This is the *only* new Go test in the project; matches the existing pattern of "smoke test, no unit test framework" for handlers/UI, while still catching SQL bugs that smoke tests would miss.
6. **Retention = first-run after 60s, then daily at 03:00 local.** A 60s warm-up lets an admin who sets `retention=1` and immediately restarts see the purge happen before they walk away, rather than waiting up to 24h.
7. **Full-PUT semantics on `/api/config`.** Admin submits the whole `Config` object; `history_retention_days=0` means "permanent" and is preserved (not overwritten by the 30-day default injection on the next start, because we probe raw JSON for the key's presence before injecting).
8. **`history.js` uses `AbortController` on in-flight fetches** so a rapid filter change cancels the previous request and prevents race-conditions on the response order. Same pattern as `system.js` watchdog.
9. **`X-Wx-Source: admin_test` is a soft marker, not a secret.** External callers are not expected to know or set this header; if they do, the only side effect is that their request shows up in the "来源" column as "admin_test", which is harmless. We do *not* gate it on a shared secret.
10. **`/history` list endpoint returns both list items and total in one response**, so the frontend can render "共 N 条 · 第 X/Y 页" without a second request. With the `idx_request_log_ts` index, a `COUNT(*) WHERE ...` over typical history sizes (10²-10⁴ rows) is sub-millisecond.

---

## Task 1: Add modernc.org/sqlite dependency + storage package skeleton

**Files:**
- Modify: `go.mod`
- Modify: `go.sum` (auto-generated by `go get`)
- Create: `internal/storage/schema.go`
- Create: `internal/storage/log.go`
- Create: `internal/storage/storage.go`
- Create: `internal/storage/storage_test.go`

- [ ] **Step 1: Add the dep**

Run from project root:
```bash
cd c:/Users/Admin/src/wx_web_api
go get modernc.org/sqlite@v1.34.5
```
Expected: `go.mod` and `go.sum` updated. The `require` block gains `modernc.org/sqlite v1.34.5`.

- [ ] **Step 2: Create `internal/storage/schema.go`**

```go
package storage

const createRequestLogTable = `
CREATE TABLE IF NOT EXISTS request_log (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  ts            INTEGER NOT NULL,
  token_label   TEXT    NOT NULL,
  kind          TEXT    NOT NULL,
  source        TEXT    NOT NULL,
  request       TEXT    NOT NULL,
  status        INTEGER NOT NULL,
  latency_ms    INTEGER NOT NULL,
  msg           TEXT    NOT NULL DEFAULT '',
  result_data   TEXT
);
`

const createIndexTs = `
CREATE INDEX IF NOT EXISTS idx_request_log_ts
  ON request_log(ts DESC);
`

const createIndexTokenTs = `
CREATE INDEX IF NOT EXISTS idx_request_log_token_ts
  ON request_log(token_label, ts DESC);
`

const createIndexKindStatusTs = `
CREATE INDEX IF NOT EXISTS idx_request_log_kind_status_ts
  ON request_log(kind, status, ts DESC);
`
```

- [ ] **Step 3: Create `internal/storage/log.go`**

```go
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
	Total int          `json:"total"`
	Page  int          `json:"page"`
	Size  int          `json:"size"`
	Items []RequestLog `json:"items"`
}

// startOfTodayMs returns the unix-millisecond timestamp of the start of the
// current local day. Kept here (storage package) because system.go's
// collectSnapshot uses the same notion of "today" and must not drift.
func startOfTodayMs() int64 {
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
		v := startOfTodayMs()
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
```

- [ ] **Step 4: Create `internal/storage/storage.go`**

```go
package storage

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

type Storage struct {
	db *sql.DB
}

func (s *Storage) Init(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("pragma journal_mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return fmt.Errorf("pragma synchronous: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return fmt.Errorf("pragma foreign_keys: %w", err)
	}
	if _, err := db.Exec(createRequestLogTable); err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	if _, err := db.Exec(createIndexTs); err != nil {
		return fmt.Errorf("create idx_ts: %w", err)
	}
	if _, err := db.Exec(createIndexTokenTs); err != nil {
		return fmt.Errorf("create idx_token_ts: %w", err)
	}
	if _, err := db.Exec(createIndexKindStatusTs); err != nil {
		return fmt.Errorf("create idx_kind_status_ts: %w", err)
	}
	s.db = db
	return nil
}

func (s *Storage) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Storage) LogRequest(r *RequestLog) error {
	if r.Source == "" {
		r.Source = "external"
	}
	res, err := s.db.Exec(
		`INSERT INTO request_log (ts, token_label, kind, source, request, status, latency_ms, msg, result_data)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Ts, r.TokenLabel, r.Kind, r.Source, string(r.Request), r.Status, r.LatencyMs, r.Msg, nullableJSON(r.Result),
	)
	if err != nil {
		return err
	}
	// Populate r.ID so callers can immediately reference the inserted row
	// (e.g. for delete-by-id round-trip in tests and for log-and-respond flows).
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

func nullableJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

func (s *Storage) QueryHistory(q HistoryQuery) (*HistoryPage, error) {
	page, size := q.pageSize()
	offset := (page - 1) * size

	var (
		conds []string
		args  []any
	)
	if v := q.tsLowerBoundPtr(); v != nil {
		conds = append(conds, "ts >= ?")
		args = append(args, *v)
	}
	if v := q.kindValue(); v != nil {
		conds = append(conds, "kind = ?")
		args = append(args, *v)
	}
	if v := q.statusValue(); v != nil {
		conds = append(conds, "status = ?")
		args = append(args, *v)
	}
	if v := q.tokenValue(); v != nil {
		conds = append(conds, "token_label = ?")
		args = append(args, *v)
	}
	if q.Q != "" {
		conds = append(conds, "request LIKE ?")
		args = append(args, "%"+q.Q+"%")
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	// total
	var total int64
	countSQL := "SELECT COUNT(*) FROM request_log" + where
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	// page
	listSQL := "SELECT id, ts, token_label, kind, source, request, status, latency_ms, msg, result_data FROM request_log" +
		where + " ORDER BY ts DESC LIMIT ? OFFSET ?"
	listArgs := append(append([]any{}, args...), size, offset)
	rows, err := s.db.Query(listSQL, listArgs...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	items := make([]RequestLog, 0, size)
	for rows.Next() {
		var (
			r          RequestLog
			reqStr     string
			resultStr  sql.NullString
		)
		if err := rows.Scan(&r.ID, &r.Ts, &r.TokenLabel, &r.Kind, &r.Source,
			&reqStr, &r.Status, &r.LatencyMs, &r.Msg, &resultStr); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		r.Request = jsonRawOrNull(reqStr)
		if resultStr.Valid {
			r.Result = json.RawMessage(resultStr.String)
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return &HistoryPage{Total: total, Page: page, Size: size, Items: items}, nil
}

func jsonRawOrNull(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}

func (s *Storage) DeleteByIDs(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	res, err := s.db.Exec("DELETE FROM request_log WHERE id IN ("+placeholders+")", args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Storage) DeleteAll() (int64, error) {
	res, err := s.db.Exec("DELETE FROM request_log")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Storage) PurgeOlderThan(cutoffMs int64) (int64, error) {
	res, err := s.db.Exec("DELETE FROM request_log WHERE ts < ?", cutoffMs)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Storage) Count() (int64, error) {
	var n int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM request_log").Scan(&n)
	return n, err
}

func (s *Storage) CountSince(sinceMs int64) (int64, error) {
	var n int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM request_log WHERE ts >= ?", sinceMs).Scan(&n)
	return n, err
}

func (s *Storage) CountErrors() (int64, error) {
	var n int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM request_log WHERE status != 0").Scan(&n)
	return n, err
}
```

(Add `import "encoding/json"` to the top of `storage.go` — it is needed for `json.RawMessage` in the `jsonRawOrNull` helper.)

- [ ] **Step 5: Create `internal/storage/storage_test.go`**

```go
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
```

- [ ] **Step 6: Run the test**

```bash
cd c:/Users/Admin/src/wx_web_api
go test ./internal/storage/ -v
```
Expected: `PASS` — `TestLogAndQuery` runs to completion, all assertions pass. The test exercises Init / LogRequest / QueryHistory (all 4 filters) / Count / CountSince / CountErrors / DeleteByIDs / PurgeOlderThan / DeleteAll.

- [ ] **Step 7: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add go.mod go.sum internal/storage/
git commit -m "feat(storage): add SQLite request_log store with full CRUD + filter API

- go get modernc.org/sqlite (pure Go, no CGO)
- internal/storage: schema (request_log + 3 indexes), LogRequest,
  QueryHistory (range/kind/status/token/q filters, page/size), 
  DeleteByIDs, DeleteAll, PurgeOlderThan, Count, CountSince, CountErrors
- storage_test.go: round-trip + filter + delete coverage using t.TempDir()
"
```

---

## Task 2: Config changes — Token.Label, HistoryRetentionDays, backfill

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Update `Config` and `Token` structs**

In `internal/config/config.go`, change the struct definitions to:

```go
type Token struct {
	Value     string `json:"value"`
	Label     string `json:"label"`
	ExpiresAt string `json:"expires_at"`
}

type Config struct {
	ApiBaseUrl           string  `json:"api_base_url"`
	Tokens               []Token `json:"tokens"`
	Port                 int     `json:"port"`
	HistoryRetentionDays int     `json:"history_retention_days"`
}
```

- [ ] **Step 2: Add `loadRawJson` helper and backfill logic**

In `internal/config/config.go`, change the `Init` function. Replace the body that ends with `defaultManager = m; return nil` with:

```go
// Backfill: empty token label → value's first 8 chars + "..."
raw, rerr := m.loadRawJson()
labelChanged := false
for i := range m.config.Tokens {
	if m.config.Tokens[i].Label == "" {
		v := m.config.Tokens[i].Value
		if len(v) > 8 {
			v = v[:8]
		}
		m.config.Tokens[i].Label = v + "..."
		labelChanged = true
	}
}

// Inject default for HistoryRetentionDays only if the raw file lacks the key.
retentionChanged := false
if rerr == nil {
	if _, ok := raw["history_retention_days"]; !ok {
		if m.config.HistoryRetentionDays == 0 {
			m.config.HistoryRetentionDays = 30
			retentionChanged = true
		}
	}
}

if labelChanged || retentionChanged {
	data, _ := json.MarshalIndent(m.config, "", "  ")
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		log.Printf("config: failed to write backfilled config: %v", err)
	}
}

defaultManager = m
return nil
```

And add this new method to the `Manager` (place it right after `Init`):

```go
func (m *Manager) loadRawJson() (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
```

- [ ] **Step 3: Verify the build**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o /tmp/wx_sanity.exe .
```
Expected: build succeeds, no output. Delete the binary (`rm /tmp/wx_sanity.exe` — on Windows bash it's a tmp file, OK to leave; it's outside the repo).

- [ ] **Step 4: Verify backfill against an existing config file**

The current `dist/wx_web_api.json` (which has 1 token with empty label and no `history_retention_days` key) should be backfilled on the next server start. To verify without running the full server, run a one-shot test:

```bash
cd c:/Users/Admin/src/wx_web_api
cat > /tmp/cfgcheck.go <<'EOF'
package main

import (
	"fmt"
	"os"
	"wx_web_api/internal/config"
)

func main() {
	if err := config.Init(os.Args[1], "wx_web_api"); err != nil {
		fmt.Println("ERR:", err); os.Exit(1)
	}
	cfg := config.Get()
	for _, t := range cfg.Tokens {
		fmt.Printf("  token label=%q value_first_8=%q expires=%q\n", t.Label, t.Value[:min(len(t.Value), 8)], t.ExpiresAt)
	}
	fmt.Printf("  retention_days=%d\n", cfg.HistoryRetentionDays)
}
EOF
go run /tmp/cfgcheck.go dist/wx_web_api.json
rm /tmp/cfgcheck.go
```
Expected output (labels will reflect whatever is in your config — example):
```
  token label="wx-web-..." value_first_8="wx-web-" expires=""
  retention_days=30
```

Then `cat dist/wx_web_api.json` and confirm the file now has the `label` field on tokens and the `history_retention_days: 30` field. If the file is reverted, the binary on the next start will re-backfill (idempotent — won't overwrite an existing label or retention key, by design).

Restore the file before moving on (we don't want untracked churn in `dist/`):
```bash
cd c:/Users/Admin/src/wx_web_api
git checkout -- dist/wx_web_api.json
```

- [ ] **Step 5: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add internal/config/config.go
git commit -m "feat(config): add Token.Label + HistoryRetentionDays with startup backfill

- Token gains Label field; backfill: empty → value[:8]+'...'
- Config gains HistoryRetentionDays; default 30 injected only if raw 
  file lacks the key (so admin's explicit 0=permanent is preserved)
- loadRawJson helper used to probe file for key presence
- backfill writes file in-place if anything changed
"
```

---

## Task 3: Log write path — handler storage hook + 401 capture

**Files:**
- Modify: `internal/handler/handler.go`

This task wires the storage layer into the request flow. After this task, every `/wx` and `/wx/finder` call lands a row in `request_log`, including 401 attempts.

- [ ] **Step 1: Extend `Handler` and `New`**

In `internal/handler/handler.go`, change the `Handler` struct to:

```go
type Handler struct {
	parser        *service.ParserService
	storage       *storage.Storage
	pwd           string
	sessMu        sync.RWMutex
	sessionTokens map[string]bool
	dateCache     sync.Map
}
```

Change the `New` function to:

```go
func New(pwd string, storage *storage.Storage) *Handler {
	return &Handler{
		parser:        service.NewParserService(),
		storage:       storage,
		pwd:           pwd,
		sessionTokens: make(map[string]bool),
	}
}
```

Add to the import block:
```go
"encoding/json"
"wx_web_api/internal/storage"
```

- [ ] **Step 2: Add `writeLog` helper**

Add this method to `*Handler` (right after `generateToken` at the bottom of the file):

```go
// writeLog records a request_log row asynchronously so that storage latency
// never inflates /wx P99. Goroutine captures a copy of all fields; the
// caller is free to return its HTTP response immediately.
//
// Pass kind='auth' for 401 attempts; request may be nil for those.
func (h *Handler) writeLog(c *gin.Context, t0 time.Time, tokenLabel, source, kind string, status int, msg string, result, request any) {
	if h.storage == nil {
		return
	}
	t0Copy := t0
	go func() {
		var reqBytes, resBytes []byte
		if request != nil {
			reqBytes, _ = json.Marshal(request)
		}
		if result != nil {
			resBytes, _ = json.Marshal(result)
		}
		rec := &storage.RequestLog{
			Ts:         time.Now().UnixMilli(),
			TokenLabel: tokenLabel,
			Kind:       kind,
			Source:     source,
			Request:    reqBytes,
			Status:     status,
			LatencyMs:  time.Since(t0Copy).Milliseconds(),
			Msg:        msg,
		}
		if len(resBytes) > 0 {
			rec.Result = resBytes
		}
		if err := h.storage.LogRequest(rec); err != nil {
			log.Printf("[storage] LogRequest failed: %v", err)
		}
	}()
}
```

Add `"log"` to the import block (it should already be there from `LogRequest failed: log.Printf`, but verify).

- [ ] **Step 3: Extend `TokenAuth` to set gin context + log 401**

Replace the entire `TokenAuth` method with:

```go
// TokenAuth middleware for external API routes.
// Reads cfg.Tokens live so admin updates take effect without restart.
// Sets "token_label" and "source" on the gin context for downstream handlers
// and for the writeLog helper to pick up. On any 401, an async log row is
// written with kind='auth' so admins can audit rejected calls.
func (h *Handler) TokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		t0 := time.Now()

		token := c.GetHeader("Authorization")
		if token != "" {
			token = strings.TrimPrefix(token, "Bearer ")
		}
		if token == "" {
			token = c.Query("token")
		}
		if token == "" {
			h.writeLog(c, t0, "", "external", "auth", 401, "missing token", nil, gin.H{"path": c.Request.Method + " " + c.FullPath()})
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
			return
		}

		cfg := config.Get()
		var matchedTok *config.Token
		for i := range cfg.Tokens {
			if cfg.Tokens[i].Value == token {
				matchedTok = &cfg.Tokens[i]
				break
			}
		}
		if matchedTok == nil {
			h.writeLog(c, t0, "", "external", "auth", 401, "unknown token", nil, gin.H{"path": c.Request.Method + " " + c.FullPath()})
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
			return
		}
		if h.isExpired(matchedTok.ExpiresAt) {
			h.writeLog(c, t0, matchedTok.Label, "external", "auth", 401, "token expired", nil, gin.H{"path": c.Request.Method + " " + c.FullPath()})
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "token expired"})
			return
		}

		// Auth ok — inject label and source for downstream handlers.
		source := c.GetHeader("X-Wx-Source")
		if source != "admin_test" {
			source = "external"
		}
		c.Set("token_label", matchedTok.Label)
		c.Set("source", source)
		c.Next()
	}
}
```

- [ ] **Step 4: Extend `ParseWxURL` to writeLog on every path**

Replace the existing `ParseWxURL` with:

```go
func (h *Handler) ParseWxURL(c *gin.Context) {
	t0 := time.Now()
	label := c.GetString("token_label")
	source := c.GetString("source")

	var req model.WxParseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeLog(c, t0, label, source, "url", 1, "url is required", nil, gin.H{"url": ""})
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "url is required"})
		return
	}

	data, err := h.parser.Parse(req.URL)
	if err != nil {
		h.writeLog(c, t0, label, source, "url", 1, err.Error(), nil, gin.H{"url": req.URL})
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: err.Error()})
		return
	}
	h.writeLog(c, t0, label, source, "url", 0, "", data, gin.H{"url": req.URL})
	c.JSON(http.StatusOK, model.WxParseResponse{Code: 0, Msg: "success", Data: data})
}
```

- [ ] **Step 5: Extend `ParseFinderFeedByObjectID` to writeLog on every path**

Replace the existing `ParseFinderFeedByObjectID` with:

```go
func (h *Handler) ParseFinderFeedByObjectID(c *gin.Context) {
	t0 := time.Now()
	label := c.GetString("token_label")
	source := c.GetString("source")

	var req model.FinderFeedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeLog(c, t0, label, source, "finder", 1, "objectId and objectNonceId are required", nil, gin.H{})
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "objectId and objectNonceId are required"})
		return
	}
	if req.ObjectID == "" || req.ObjectNonceID == "" {
		h.writeLog(c, t0, label, source, "finder", 1, "objectId and objectNonceId are required", nil,
			gin.H{"objectId": req.ObjectID, "objectNonceId": req.ObjectNonceID})
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "objectId and objectNonceId are required"})
		return
	}

	data, err := h.parser.ParseFinderFeedByObjectID(req.ObjectID, req.ObjectNonceID)
	if err != nil {
		h.writeLog(c, t0, label, source, "finder", 1, err.Error(), nil,
			gin.H{"objectId": req.ObjectID, "objectNonceId": req.ObjectNonceID})
		c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: err.Error()})
		return
	}
	h.writeLog(c, t0, label, source, "finder", 0, "", data,
		gin.H{"objectId": req.ObjectID, "objectNonceId": req.ObjectNonceID})
	c.JSON(http.StatusOK, model.WxParseResponse{Code: 0, Msg: "success", Data: data})
}
```

- [ ] **Step 6: Verify the build (storage arg not yet threaded in main.go — that's Task 4)**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o /tmp/wx_sanity.exe .
```
Expected: **build fails** because `handler.New` now takes a second arg, but `main.go` still calls it with one. The error will be something like `not enough arguments in call to handler.New`. This is expected — Task 4 threads storage in. Don't commit a broken build; do the next step which is fine because we haven't actually built in this step.

If build happens to succeed, that's also fine — that means main.go is using an older signature; we'll fix it in Task 4. Either way, **do not commit yet**.

- [ ] **Step 7: Defer commit**

This task's commit will be combined with Task 4 (which wires main.go to call `handler.New(pwd, storage)`). Skip the `git commit` step; mark this step done and move on.

---

## Task 4: Wire storage into main + add /api/history + retention loop

**Files:**
- Modify: `main.go`
- Create: `internal/handler/history.go`
- Modify: `internal/handler/system.go`
- Modify: `internal/handler/broadcaster.go`

- [ ] **Step 1: Update main.go**

In `main.go`, make these changes:

In the import block, add:
```go
"wx_web_api/internal/storage"
```

Change the body of `main()` (the section starting with `h := handler.New(effectivePwd)` through the route registration) to:

```go
// Storage init: must succeed — DB is non-optional for the service.
store := &storage.Storage{}
if err := store.Init(filepath.Join(config.ExeDir, binName+".db")); err != nil {
    log.Fatalf("storage init failed: %v", err)
}
defer store.Close()

h := handler.New(effectivePwd, store)
go handler.SystemHub.Start(context.Background(), store)
settingsHandler := handler.NewSettingsHandler()
```

(Keep all of the rest: gin.Default, CORS, web UI routes, login, config group, system routes, external API routes, NoRoute.)

Then, after the existing `r.GET("/ws/system", ...)` line and before the external API routes, add:

```go
// History routes (session-authenticated)
histGroup := r.Group("/api/history", h.SessionAuth())
{
    histGroup.GET("", h.GetHistory)
    histGroup.DELETE("", h.DeleteHistory)
}
```

- [ ] **Step 2: Update `SystemHub.Start` signature in `internal/handler/system.go`**

Change the `Start` method to accept storage and to start the retention loop internally:

```go
// Start runs the snapshot ticker. It also runs the retention loop in its own
// goroutine. Pass context.Background() from main; the loop is process-lifetime.
func (h *systemHub) Start(ctx context.Context, s *storage.Storage) {
	go h.runSnapshotLoop(ctx, s)
	go h.runRetentionLoop(ctx, s)
}

// runSnapshotLoop fans out a SystemSnapshot to all connected clients every 2s.
func (h *systemHub) runSnapshotLoop(ctx context.Context, s *storage.Storage) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := h.collectSnapshot(s)
			h.mu.RLock()
			for c := range h.clients {
				go func(c *websocket.Conn) {
					_ = c.WriteJSON(snap)
				}(c)
			}
			h.mu.RUnlock()
		}
	}
}

// runRetentionLoop purges records older than Config.HistoryRetentionDays
// at 03:00 local every day. The first run happens 60s after Start so that
// an admin who sets retention=1 and restarts sees the purge happen promptly
// rather than waiting up to 24h.
func (h *systemHub) runRetentionLoop(ctx context.Context, s *storage.Storage) {
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			h.runRetentionOnce(s)
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			timer.Reset(time.Until(next))
		}
	}
}

func (h *systemHub) runRetentionOnce(s *storage.Storage) {
	cfg := config.Get()
	if cfg.HistoryRetentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(cfg.HistoryRetentionDays) * 24 * time.Hour).UnixMilli()
	n, err := s.PurgeOlderThan(cutoff)
	if err != nil {
		log.Printf("[retention] purge failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("[retention] purged %d records older than %d days", n, cfg.HistoryRetentionDays)
	}
}
```

Note: the existing `Start` body (the old ticker loop) is renamed to `runSnapshotLoop` and now takes storage. The snapshot fan-out moves to per-client goroutines (matching the system page spec's decision #2). This is a slight cleanup beyond what the spec strictly required — it lets the snapshot loop tolerate slow clients without blocking the ticker.

Update `collectSnapshot` to take storage and return the real stats. Replace the existing function with:

```go
func (h *systemHub) collectSnapshot(s *storage.Storage) SystemSnapshot {
	snap := SystemSnapshot{
		Type:          "snapshot",
		Ts:            time.Now().Unix(),
		UptimeSeconds: int64(time.Since(processStart).Seconds()),
		Goroutines:    runtime.NumGoroutine(),
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	snap.Mem = MemStats{
		Alloc:   ms.Alloc,
		HeapSys: ms.HeapSys,
		Sys:     ms.Sys,
	}
	if s != nil {
		total, _ := s.Count()
		// startOfToday lives in the storage package; reuse it so /api/system's
		// "today" and /api/history?range=today agree to the exact same instant.
		since, _ := s.CountSince(storageStartOfToday())
		errs, _ := s.CountErrors()
		snap.Stats = &ReqStats{Total: total, Today: since, Errors: errs}
	}
	return snap
}
```

Add a small adapter at the bottom of `system.go`:

```go
// storageStartOfToday is re-exported from the storage package to keep the
// "what does 'today' mean?" definition in exactly one place.
func storageStartOfToday() int64 { return storageStartOfTodayMs() }
```

And add the storage import:
```go
"wx_web_api/internal/storage"
```

(The `storage` package exports `startOfTodayMs` lowercase; we need a same-package way to call it. Two options: (a) re-export from this file as above, (b) expose a public `StartOfTodayMs` from storage. **Use option (b)**: in `internal/storage/log.go`, change `func startOfTodayMs()` to `func StartOfTodayMs()`. Then in `system.go`, just call `storage.StartOfTodayMs()`. Replace the adapter approach in the snippet above with the direct call. **Apply this fix when implementing.**)

- [ ] **Step 3: Create `internal/handler/history.go`**

```go
package handler

import (
	"net/http"
	"strconv"
	"strings"
	"wx_web_api/internal/storage"

	"github.com/gin-gonic/gin"
)

// GetHistory serves GET /api/history. All filter values default to "all"
// (no constraint) when empty or unknown.
func (h *Handler) GetHistory(c *gin.Context) {
	q := storage.HistoryQuery{
		Range:  c.Query("range"),
		Kind:   c.Query("kind"),
		Status: c.Query("status"),
		Token:  c.Query("token"),
		Q:      c.Query("q"),
		Page:   atoiOr(c.Query("page"), 1),
		Size:   atoiOr(c.Query("size"), 50),
	}
	if q.Page < 1 {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "page must be >= 1"})
		return
	}
	if q.Size < 1 || q.Size > 200 {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "size must be 1..200"})
		return
	}
	page, err := h.storage.QueryHistory(q)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "query failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": page})
}

// DeleteHistory serves DELETE /api/history. Use ?id=1,2,3 for batch or
// ?all=1 to nuke everything.
func (h *Handler) DeleteHistory(c *gin.Context) {
	if c.Query("all") == "1" {
		n, err := h.storage.DeleteAll()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "delete failed: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"deleted": n}})
		return
	}
	raw := c.Query("id")
	if raw == "" {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "id or all required"})
		return
	}
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "invalid id: " + p})
			return
		}
		ids = append(ids, v)
	}
	n, err := h.storage.DeleteByIDs(ids)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "delete failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"deleted": n}})
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
```

- [ ] **Step 4: Verify the build**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
```
Expected: build succeeds. The binary is at `dist/wx_web_api.exe` (replacing the prior one — that is the user's standard build target; `build.bat` is the official path but a direct `go build` is fine for in-iteration checks).

- [ ] **Step 5: Smoke test — record + read + delete**

Start the binary in the background:
```bash
cd c:/Users/Admin/src/wx_web_api
./dist/wx_web_api.exe -port 13335 &
# On Windows bash this works; if it doesn't, just double-click dist/wx_web_api.exe
```

Wait 1 second, then drive it with curl:
```bash
# 1. Login
TOKEN=$(curl -s -X POST http://127.0.0.1:13335/api/login \
  -H 'Content-Type: application/json' \
  -d '{"pwd":"1","challenge":"abc","response":"<compute via JS>"}' | grep -oP '(?<="token":")[^"]+')
# Simpler: open the admin UI in a browser, log in, and grab the session token from localStorage. 
# (The challenge-response is intentionally non-trivial to script; the browser is faster.)
```

In a browser, log in to `http://127.0.0.1:13335/`. From DevTools console:
```js
var t = localStorage.getItem('wx_token');
console.log('token:', t);
```

Then in bash, with the token captured in `$T`:
```bash
T="<paste token>"
# Make sure the dist/wx_web_api.json has at least one valid token — if not, add one via the /settings page first.

# 2. Fire a /wx call with a real token (use the value, not the session)
curl -s -X POST http://127.0.0.1:13335/wx \
  -H "Authorization: $WX_TOKEN_VALUE" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://mp.weixin.qq.com/s/INVALID"}' # 预期业务错,这就是我们想要的(能验证 status=1 记录)

# 3. Fire a 401 (no auth header)
curl -s -X POST http://127.0.0.1:13335/wx \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://mp.weixin.qq.com/s/x"}'

# 4. Read history
curl -s "http://127.0.0.1:13335/api/history?range=all&size=10" \
  -H "Authorization: $T" | python -m json.tool | head -60

# Expected: items array with at least 2 rows — one kind=url status=1, one kind=auth status=401
# 5. Delete one row
ID=$(curl -s "http://127.0.0.1:13335/api/history?range=all&size=1" -H "Authorization: $T" | python -c "import sys,json;print(json.load(sys.stdin)['data']['items'][0]['id'])")
curl -s -X DELETE "http://127.0.0.1:13335/api/history?id=$ID" -H "Authorization: $T"

# 6. Verify count dropped
curl -s "http://127.0.0.1:13335/api/history?range=all&size=1" -H "Authorization: $T" | python -m json.tool
```

If any of these returns an error or empty `items`, check `dist/app.log` (created by `build.bat` line 6) — but if you started the binary directly (not via build.bat), the log goes to stdout. The most common failure: `dist/wx_web_api.db` directory not writable. Fix: `chmod`/check perms on `dist/`.

- [ ] **Step 6: Commit (combined with Task 3)**

```bash
cd c:/Users/Admin/src/wx_web_api
git add main.go internal/handler/handler.go internal/handler/system.go internal/handler/broadcaster.go internal/handler/history.go
git commit -m "feat(handler): record all /wx requests to request_log + add /api/history

- handler.New takes *storage.Storage; writeLog async helper added
- TokenAuth injects token_label + source into gin context, logs 401 with kind='auth'
- ParseWxURL / ParseFinderFeedByObjectID log on every path (success=0, business err=1)
- new internal/handler/history.go: GetHistory (filter+page), DeleteHistory (id/all)
- main.go: init storage, pass to handler.New and SystemHub.Start
- SystemHub.Start spawns runRetentionLoop in addition to snapshot loop;
  retention runs 60s after start, then daily at 03:00 local
- SystemHub.collectSnapshot now reads real stats (total/today/errors) from storage
- retention_days=0 → permanent; daily purge skips
"
```

---

## Task 5: System page — render real stats

**Files:**
- Modify: `web/static/js/pages/system.js`

The backend already pushes `stats` from Task 4. This task makes the frontend render it and removes the placeholder note.

- [ ] **Step 1: Read current `applySnapshot` to find the placeholder block**

Open `web/static/js/pages/system.js`. The relevant section is `applySnapshot(slot, snap)` — it currently has:

```js
if (snap.stats) {
  healthRows.push(realRow('请求总数', String(snap.stats.total)));
  healthRows.push(realRow('今日调用', String(snap.stats.today)));
  var errPct = snap.stats.total > 0
    ? ((snap.stats.errors / snap.stats.total) * 100).toFixed(1) + '% (' + snap.stats.errors + ' / ' + snap.stats.total + ')'
    : '—';
  healthRows.push(realRow('错误率', errPct));
} else {
  healthRows.push(placeholderRow('请求总数', '—'));
  healthRows.push(placeholderRow('今日调用', '—'));
  healthRows.push(placeholderRow('错误率', '—'));
}
```

This is *already* the structure that handles real stats. The change is to remove the `else` branch's `—` placeholders (since storage is always present now) and add a `.kv__sub` note explaining the source.

- [ ] **Step 2: Update `applySnapshot` to always show real stats**

Replace the `if (snap.stats) { ... } else { ... }` block with:

```js
if (snap.stats) {
  healthRows.push(realRow('请求总数', String(snap.stats.total)));
  healthRows.push(realRow('今日调用', String(snap.stats.today)));
  var errPct = snap.stats.total > 0
    ? ((snap.stats.errors / snap.stats.total) * 100).toFixed(1) + '% (' + snap.stats.errors + ' / ' + snap.stats.total + ')'
    : '—';
  healthRows.push(realRow('错误率', errPct));
} else {
  // Defensive: storage init failed or system page loaded before first snapshot.
  // Should not happen in practice (storage.Init is non-optional in main.go).
  healthRows.push(placeholderRow('请求总数', '—'));
  healthRows.push(placeholderRow('今日调用', '—'));
  healthRows.push(placeholderRow('错误率', '—'));
}
healthRows.push(realRow('—', '<span class="kv__sub">数据来源: /api/history 后台聚合 (request_log 表)</span>'));
```

Wait — the `realRow` signature is `(label, value, sub?)` where `sub` is a third arg rendered as a `kv__sub` span. The current line `realRow('—', '<span ...>')` is wrong (the HTML string is the value, not a sub). Use `placeholderRow` instead since "—" is the label meaning "metadata about the stats above":

```js
healthRows.push('<dt>—</dt><dd><span class="kv__sub">数据来源: /api/history 后台聚合 (request_log 表)</span></dd>');
```

Insert that line right after the `else` block. (It runs unconditionally — both branches add the same attribution line.)

- [ ] **Step 3: Verify the build embeds the new JS**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
```
Expected: build succeeds.

- [ ] **Step 4: Smoke test in browser**

1. Start (or restart) the binary.
2. Open `http://127.0.0.1:13335/system` in a browser.
3. The 健康度 card should show real numbers updating every 2s.
4. If 0/0/— — you haven't sent any /wx calls yet. Run a few via /test or curl.

- [ ] **Step 5: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add web/static/js/pages/system.js
git commit -m "feat(ui/system): render real stats from request_log

Storage-backed total/today/errors now show on the system page's
健康度 card. Adds a one-line attribution footer.
"
```

---

## Task 6: Settings page — token label + retention days + current count

**Files:**
- Modify: `web/static/js/pages/settings.js`

- [ ] **Step 1: Find the token-row render code**

In `web/static/js/pages/settings.js`, the token list is rendered by a function (look for `token-item` in the IIFE). Each row currently shows value (with mask toggle) + expires_at date input. We add a `label` input between them.

Look for the loop that creates each row and add a `label` input field. The exact insertion depends on the existing code structure; below is the diff-style patch assuming a renderRow(token, idx) helper:

```js
// inside the row HTML, after the value <input> and before the date input:
'<input class="input input--mono" data-role="label" data-idx="' + idx + '" ' +
       'placeholder="可选,默认取前 8 字符" ' +
       'value="' + escapeHtml(token.label || '') + '">'
```

- [ ] **Step 2: Wire the label input into save**

Find the place where the existing token row collects its values into the `newTokens` array. Add:

```js
var label = row.querySelector('[data-role="label"]').value.trim();
newTokens.push({
  value: value,
  label: label,        // new
  expires_at: expires
});
```

- [ ] **Step 3: Add the "历史保留天数" input to the config block**

In the same file, find the place where the "监听端口" / "API base URL" / etc. are rendered as a kv list. Add a new row at the bottom of that card:

```js
'<dt>历史保留天数</dt><dd>' +
  '<input class="input" type="number" min="0" max="365" ' +
         'data-role="retention-days" ' +
         'value="' + (cfg.history_retention_days || 0) + '"> ' +
  '<span class="kv__sub">0 = 永久</span>' +
'</dd>'
```

- [ ] **Step 4: Add "当前已记录: N 条" line**

In the same card, after the "DB 大小" row (or wherever fits), add a new line that lazily fetches the count:

```js
'<dt>当前已记录</dt><dd><span data-role="record-count">—</span> <span class="kv__sub">条</span></dd>'
```

After rendering, kick off a fetch:
```js
WXApi.authJson('/api/history?range=all&size=1').then(function(res) {
  var span = slot.querySelector('[data-role="record-count"]');
  if (span && res.data && res.data.code === 0) {
    span.textContent = String(res.data.data.total);
  }
});
```

(Place this in the same `render` / `load` flow that already does the initial config fetch.)

- [ ] **Step 5: Wire retention-days into save**

Find the `PUT /api/config` body assembly. Add `history_retention_days: Number(input.value) || 0` to the object.

- [ ] **Step 6: Verify the build embeds the new JS**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
```

- [ ] **Step 7: Smoke test in browser**

1. Open `/settings`. Each token row has a `label` input.
2. Type "marketing-bot" into the first token's label; click 保存.
3. Reload the page; label is preserved.
4. Change "历史保留天数" to 7; save; reload; it's 7.
5. The "当前已记录" line shows the count from prior test calls (from Task 4 smoke test).

- [ ] **Step 8: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add web/static/js/pages/settings.js
git commit -m "feat(ui/settings): add token label + history retention days + record count

- Each token row gains a 'label' input (saved into cfg.Tokens[].label)
- New '历史保留天数' input (number, 0=永久) on the config block
- '当前已记录' lazy-fetches GET /api/history?size=1 to display total
"
```

---

## Task 7: Test page — X-Wx-Source header

**Files:**
- Modify: `web/static/js/pages/test.js`

- [ ] **Step 1: Find the two `fetch` calls in `test.js`**

There are two: one for `POST /wx` and one for `POST /wx/finder`. Both currently look something like:

```js
fetch('/wx', { method: 'POST', headers: { 'Authorization': token, 'Content-Type': 'application/json' }, body: JSON.stringify(...) })
```

- [ ] **Step 2: Add `X-Wx-Source: admin_test` to both**

For each fetch, add the header:

```js
headers: Object.assign({ 'X-Wx-Source': 'admin_test' }, headersObj)
```

Or, simpler, inline the header in the existing object:

```js
headers: { 'Authorization': token, 'Content-Type': 'application/json', 'X-Wx-Source': 'admin_test' }
```

Apply to both calls (URL Tab and objectId Tab).

- [ ] **Step 3: Verify the build embeds the new JS**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
```

- [ ] **Step 4: Smoke test**

1. Open `/test`, pick a token, fire a /wx call.
2. Query `/api/history` and verify the row's `source` field is `"admin_test"`, not `"external"`.
3. Fire a /wx call with `curl` directly (no `X-Wx-Source` header). Verify that row's `source` is `"external"`.

- [ ] **Step 5: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add web/static/js/pages/test.js
git commit -m "feat(ui/test): mark admin test calls with X-Wx-Source: admin_test

The handler reads this header and tags request_log.source accordingly,
so admins can distinguish their debug calls from external production calls.
"
```

---

## Task 8: /history page — full frontend rewrite

**Files:**
- Create: `web/static/js/pages/history.js` (full rewrite, replacing the placeholder)
- Modify: `web/static/css/pages.css` (append)

This is the largest frontend task. The page has filter bar, table with expand, pagination, single/batch/full delete, empty states, error handling.

- [ ] **Step 1: Add the CSS**

Append to `web/static/css/pages.css` (do not modify any existing rules above):

```css
/* ============================================================
 * History page (解析历史)
 * ============================================================ */

.history-card { padding: var(--s-4) var(--s-5); }
.history-summary {
  font-size: var(--t-sm);
  color: var(--text-muted);
  margin-top: var(--s-3);
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.history-filter {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: var(--s-3);
  align-items: end;
}
.history-filter .filter-field { display: flex; flex-direction: column; gap: var(--s-1); }
.history-filter .filter-field__label {
  font-size: var(--t-xs);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.history-filter .input { width: 100%; }
.history-filter__actions {
  display: flex;
  gap: var(--s-2);
  justify-content: flex-end;
  margin-top: var(--s-3);
}

.history-table {
  display: flex;
  flex-direction: column;
  gap: 0;
}
.history-row {
  border-top: 1px solid var(--border);
  padding: var(--s-3) var(--s-2);
  display: grid;
  grid-template-columns: 28px 100px 80px 1fr 60px 80px 100px 1fr 32px;
  gap: var(--s-3);
  align-items: center;
  cursor: pointer;
  font-size: var(--t-sm);
  transition: background var(--dur-fast) var(--ease);
}
.history-row:hover { background: var(--surface-2); }
.history-row:first-child { border-top: none; }
.history-row--expanded { background: var(--surface-2); }
.history-row__cell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.history-row__cell--ts { font-family: var(--font-mono); font-size: var(--t-xs); color: var(--text-muted); }
.history-row__cell--kind { font-family: var(--font-mono); font-size: var(--t-xs); }
.history-row__cell--token { font-family: var(--font-mono); font-size: var(--t-xs); color: var(--text); }
.history-row__cell--summary {
  font-family: var(--font-mono);
  font-size: var(--t-xs);
  color: var(--text-faint);
}
.history-row__cell--latency { font-family: var(--font-mono); font-size: var(--t-xs); font-variant-numeric: tabular-nums; }
.history-row__check { cursor: pointer; }

.history-detail {
  background: var(--surface-2);
  border-top: 1px solid var(--border);
  padding: var(--s-4) var(--s-5);
  display: grid;
  gap: var(--s-3);
}
.history-detail__section-title {
  font-size: var(--t-xs);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-faint);
  margin-bottom: var(--s-1);
}
.history-detail pre {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: var(--s-3);
  font-size: var(--t-xs);
  line-height: 1.5;
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--text);
  margin: 0;
}

.history-pagination {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: var(--s-3) var(--s-2);
  border-top: 1px solid var(--border);
  gap: var(--s-3);
  flex-wrap: wrap;
}
.history-pagination__pages { display: flex; gap: var(--s-1); }
.history-pagination__btn {
  background: var(--surface);
  border: 1px solid var(--border);
  color: var(--text-muted);
  padding: 2px var(--s-2);
  font-size: var(--t-sm);
  border-radius: var(--r-sm);
  cursor: pointer;
  min-width: 28px;
  text-align: center;
}
.history-pagination__btn:hover { color: var(--text); border-color: var(--border-strong); }
.history-pagination__btn--active {
  background: var(--gradient-primary);
  color: #fff;
  border-color: transparent;
  box-shadow: var(--shadow-glow);
}
.history-pagination__btn:disabled { opacity: 0.4; cursor: not-allowed; }

.badge { display: inline-block; padding: 1px var(--s-2); border-radius: var(--r-full); font-size: var(--t-xs); font-weight: 600; }
.badge--ok       { background: rgba(34, 197, 94, 0.15);  color: var(--success); }
.badge--err      { background: rgba(239, 68, 68, 0.15);  color: var(--danger); }
.badge--auth_err { background: rgba(239, 68, 68, 0.15);  color: var(--danger); }
.badge--kind-url     { background: var(--surface-2); color: var(--text-muted); }
.badge--kind-finder  { background: var(--surface-2); color: var(--text-muted); }
.badge--kind-auth    { background: rgba(245, 158, 11, 0.15); color: var(--warning); }
.badge--source-external   { background: var(--surface-2); color: var(--text-faint); }
.badge--source-admin_test { background: rgba(99, 102, 241, 0.15); color: var(--primary); }

@media (max-width: 900px) {
  .history-row {
    grid-template-columns: 28px 1fr 80px 32px;
    grid-template-areas:
      "check ts latency menu"
      "check kind kind kind"
      "check token token token"
      "check summary summary summary"
      "check status status status";
    gap: var(--s-1) var(--s-3);
  }
  .history-row__cell--ts { grid-area: ts; }
  .history-row__cell--latency { grid-area: latency; text-align: right; }
  .history-row__cell--kind { grid-area: kind; }
  .history-row__cell--token { grid-area: token; }
  .history-row__cell--summary { grid-area: summary; white-space: normal; }
  .history-row__cell--status { grid-area: status; }
  .history-row__cell--source { grid-area: status; justify-self: end; }
  .history-row__check { grid-area: check; align-self: start; margin-top: 4px; }
  .history-row__menu { grid-area: menu; align-self: start; }
  .history-row__cell--menu { grid-area: menu; }
  .history-row > .history-row__cell:nth-child(1) { display: none; }
}
```

- [ ] **Step 2: Create the new `web/static/js/pages/history.js`**

Replace the file contents entirely:

```js
/* History page (解析历史) — admin-only log viewer.
 * Lists request_log rows with filter bar, expandable rows, pagination,
 * and single/batch/full delete. State cleanup on every render() because
 * the router re-renders on every visit.
 */

(function (global) {
  'use strict';

  var DEFAULT_SIZE = 50;

  var state = {
    filter: { range: 'today', kind: 'all', status: 'all', token: 'all', q: '' },
    page: 1,
    size: DEFAULT_SIZE,
    data: null,            // {total, page, size, items}
    expanded: new Set(),
    selected: new Set(),
    abortCtrl: null,
    tokenLabels: [],       // populated from /api/config
  };

  function escapeHtml(s) {
    var div = document.createElement('div');
    div.textContent = s == null ? '' : String(s);
    return div.innerHTML;
  }

  function pad2(n) { return n < 10 ? '0' + n : '' + n; }

  function fmtTs(ms) {
    if (!ms) return '—';
    var d = new Date(ms);
    return pad2(d.getHours()) + ':' + pad2(d.getMinutes()) + ':' + pad2(d.getSeconds());
  }

  function fmtDate(ms) {
    var d = new Date(ms);
    return d.getFullYear() + '-' + pad2(d.getMonth() + 1) + '-' + pad2(d.getDate());
  }

  function summarizeRequest(req, kind) {
    if (!req) return '—';
    if (kind === 'url') return req.url || '—';
    if (kind === 'finder') return (req.objectId || '—') + (req.objectNonceId ? ' / ' + req.objectNonceId : '');
    if (kind === 'auth') return req.path || '—';
    return JSON.stringify(req);
  }

  function statusBadge(status) {
    if (status === 0) return '<span class="badge badge--ok">成功</span>';
    if (status === 1) return '<span class="badge badge--err">业务错</span>';
    if (status === 401) return '<span class="badge badge--auth_err">鉴权失败</span>';
    return '<span class="badge">' + escapeHtml(String(status)) + '</span>';
  }

  function kindBadge(kind) {
    var label = kind === 'url' ? 'URL' : kind === 'finder' ? 'finder' : kind === 'auth' ? 'auth' : (kind || '?');
    return '<span class="badge badge--kind-' + escapeHtml(kind) + '">' + escapeHtml(label) + '</span>';
  }

  function sourceBadge(src) {
    var label = src === 'admin_test' ? 'admin_test' : 'external';
    return '<span class="badge badge--source-' + escapeHtml(src) + '">' + escapeHtml(label) + '</span>';
  }

  /* ---------- skeleton ---------- */

  function renderSkeleton() {
    return '' +
      '<div class="card history-card">' +
        '<div class="card__title">解析历史</div>' +
        '<div class="history-filter">' +
          filterField('range', '时间',
            '<option value="today">今天</option>' +
            '<option value="7d">近 7 天</option>' +
            '<option value="30d">近 30 天</option>' +
            '<option value="all">全部</option>') +
          filterField('kind', '类型',
            '<option value="all">全部</option>' +
            '<option value="url">URL</option>' +
            '<option value="finder">finder</option>' +
            '<option value="auth">鉴权失败</option>') +
          filterField('status', '状态',
            '<option value="all">全部</option>' +
            '<option value="ok">成功</option>' +
            '<option value="err">业务错</option>' +
            '<option value="auth_err">鉴权失败</option>') +
          '<div class="filter-field" data-role="token-field">' +
            '<label class="filter-field__label">Token</label>' +
            '<select class="input" data-role="filter-token"><option value="all">全部</option></select>' +
          '</div>' +
          '<div class="filter-field">' +
            '<label class="filter-field__label">搜索 (URL/objectId)</label>' +
            '<input class="input" data-role="filter-q" placeholder="LIKE 匹配 request 列">' +
          '</div>' +
        '</div>' +
        '<div class="history-filter__actions">' +
          '<button class="btn btn--secondary" data-role="filter-clear">清空过滤</button>' +
        '</div>' +
        '<div class="history-summary">' +
          '<span data-role="summary-text">加载中…</span>' +
          '<span>' +
            '<button class="btn btn--secondary" data-role="batch-delete" disabled>批量删除</button> ' +
          '</span>' +
        '</div>' +
      '</div>' +
      '<div class="card history-card">' +
        '<div data-role="list" class="history-table"></div>' +
        '<div data-role="pagination"></div>' +
      '</div>';
  }

  function filterField(role, label, options) {
    return '<div class="filter-field">' +
             '<label class="filter-field__label">' + escapeHtml(label) + '</label>' +
             '<select class="input" data-role="filter-' + role + '">' + options + '</select>' +
           '</div>';
  }

  /* ---------- event wiring ---------- */

  function wireFilter(slot) {
    var selects = slot.querySelectorAll('[data-role^="filter-"]');
    selects.forEach(function (el) {
      if (el.tagName === 'INPUT') {
        var debounceT = null;
        el.addEventListener('input', function () {
          clearTimeout(debounceT);
          debounceT = setTimeout(function () {
            state.filter.q = el.value;
            state.page = 1;
            load(slot);
          }, 300);
        });
      } else {
        el.addEventListener('change', function () {
          var role = el.getAttribute('data-role').replace('filter-', '');
          state.filter[role] = el.value;
          state.page = 1;
          load(slot);
        });
      }
    });

    var clearBtn = slot.querySelector('[data-role="filter-clear"]');
    clearBtn.addEventListener('click', function () {
      state.filter = { range: 'today', kind: 'all', status: 'all', token: 'all', q: '' };
      state.page = 1;
      syncFilterUI(slot);
      load(slot);
    });

    var batchBtn = slot.querySelector('[data-role="batch-delete"]');
    batchBtn.addEventListener('click', function () { batchDelete(slot); });
  }

  function syncFilterUI(slot) {
    var role;
    for (role in state.filter) {
      if (!Object.prototype.hasOwnProperty.call(state.filter, role)) continue;
      var el = slot.querySelector('[data-role="filter-' + role + '"]');
      if (el) el.value = state.filter[role];
    }
  }

  function populateTokenDropdown(slot) {
    var sel = slot.querySelector('[data-role="filter-token"]');
    if (!sel) return;
    // keep the "全部" option
    while (sel.options.length > 1) sel.remove(1);
    state.tokenLabels.forEach(function (lbl) {
      var opt = document.createElement('option');
      opt.value = lbl;
      opt.textContent = lbl;
      sel.appendChild(opt);
    });
  }

  /* ---------- list rendering ---------- */

  function renderList(slot) {
    var list = slot.querySelector('[data-role="list"]');
    var pag = slot.querySelector('[data-role="pagination"]');
    var sum = slot.querySelector('[data-role="summary-text"]');
    var data = state.data;
    if (!data) { list.innerHTML = ''; pag.innerHTML = ''; sum.textContent = '加载中…'; return; }

    sum.textContent = '共 ' + data.total + ' 条 · 第 ' + data.page + '/' + Math.max(1, Math.ceil(data.total / data.size)) + ' 页';

    if (data.total === 0) {
      list.innerHTML = '<div class="empty">' +
        '<div class="empty__icon">…</div>' +
        '<div class="empty__title">' + (hasActiveFilter() ? '无匹配记录' : '暂无请求记录') + '</div>' +
        '<div class="empty__desc">' + (hasActiveFilter() ? '试试调整过滤条件' : '试试在解析测试页发一次请求') + '</div>' +
      '</div>';
      pag.innerHTML = '';
      return;
    }

    var rows = data.items.map(function (r) { return renderRow(r); }).join('');
    list.innerHTML = rows;

    list.querySelectorAll('.history-row__check').forEach(function (cb) {
      cb.addEventListener('click', function (e) { e.stopPropagation(); toggleSelect(parseInt(cb.getAttribute('data-id'), 10), cb.checked); refreshBatchBtn(slot); });
    });
    list.querySelectorAll('.history-row__menu').forEach(function (btn) {
      btn.addEventListener('click', function (e) { e.stopPropagation(); oneDelete(slot, parseInt(btn.getAttribute('data-id'), 10)); });
    });
    list.querySelectorAll('.history-row__body').forEach(function (el) {
      el.addEventListener('click', function () { toggleExpand(parseInt(el.getAttribute('data-id'), 10), slot); });
    });

    pag.innerHTML = renderPagination(data.total, data.page, data.size);
    pag.querySelectorAll('[data-page]').forEach(function (b) {
      b.addEventListener('click', function () {
        var p = parseInt(b.getAttribute('data-page'), 10);
        if (!isNaN(p) && p !== state.page) {
          state.page = p;
          load(slot);
        }
      });
    });
  }

  function hasActiveFilter() {
    var f = state.filter;
    return f.range !== 'today' || f.kind !== 'all' || f.status !== 'all' || f.token !== 'all' || !!f.q;
  }

  function renderRow(r) {
    var expanded = state.expanded.has(r.id);
    var checked = state.selected.has(r.id);
    var summary = summarizeRequest(r.request, r.kind);
    // Status cell: badge always, plus a tiny inline msg snippet (truncated)
    // for non-success rows. Hover the cell to see the full message via title.
    var statusInner = statusBadge(r.status);
    if (r.status !== 0 && r.msg) {
      var short = r.msg.length > 40 ? r.msg.slice(0, 40) + '…' : r.msg;
      statusInner += ' <span class="kv__sub" title="' + escapeHtml(r.msg) + '">' + escapeHtml(short) + '</span>';
    }
    var html = '<div class="history-row__body" data-id="' + r.id + '">' +
      '<div class="history-row__check">' +
        '<input type="checkbox" data-id="' + r.id + '" ' + (checked ? 'checked' : '') + '>' +
      '</div>' +
      '<div class="history-row__cell history-row__cell--ts">' + escapeHtml(fmtTs(r.ts)) + ' <span style="color:var(--text-faint)">' + escapeHtml(fmtDate(r.ts)) + '</span></div>' +
      '<div class="history-row__cell history-row__cell--kind">' + kindBadge(r.kind) + '</div>' +
      '<div class="history-row__cell history-row__cell--token">' + escapeHtml(r.token_label || '(无)') + '</div>' +
      '<div class="history-row__cell history-row__cell--status" title="' + escapeHtml(r.msg || '') + '">' + statusInner + '</div>' +
      '<div class="history-row__cell history-row__cell--latency">' + escapeHtml(String(r.latency_ms)) + 'ms</div>' +
      '<div class="history-row__cell history-row__cell--source">' + sourceBadge(r.source) + '</div>' +
      '<div class="history-row__cell history-row__cell--summary" title="' + escapeHtml(JSON.stringify(r.request)) + '">' + escapeHtml(summary) + '</div>' +
      '<div class="history-row__cell history-row__cell--menu">' +
        '<button class="copy-btn history-row__menu" data-id="' + r.id + '" title="删除">⋯</button>' +
      '</div>' +
    '</div>';
    if (expanded) html += renderDetail(r);
    return html;
  }

  function renderDetail(r) {
    var sections = [];
    sections.push('<div class="history-detail__section-title">入参</div>');
    sections.push('<pre>' + escapeHtml(JSON.stringify(r.request, null, 2)) + '</pre>');
    if (r.msg) {
      sections.push('<div class="history-detail__section-title">业务消息</div>');
      sections.push('<div class="result-msg">' + escapeHtml(r.msg) + '</div>');
    }
    if (r.result) {
      sections.push('<div class="history-detail__section-title">解析结果</div>');
      var res = r.result;
      var rows = [];
      ['author', 'title', 'cover_url', 'video_url', 'decode_key', 'media_type'].forEach(function (k) {
        if (res[k] != null && res[k] !== '') {
          var display = (k === 'media_type') ? res[k] + ' (' + mediaTypeName(res[k]) + ')' : res[k];
          rows.push('<div class="field"><div class="field-label">' + escapeHtml(k) + '</div>' +
                    '<div class="field-value field-value--text">' + escapeHtml(String(display)) + '</div>' +
                    '<button class="copy-btn" data-copy="' + escapeHtml(String(display)) + '">复制</button></div>');
        }
      });
      if (res.cover_url) {
        // Promote cover to an <img> above its text row for thumbnail preview.
        rows.unshift('<div class="field"><div class="field-label">cover</div>' +
                     '<div class="field-value"><img class="result-cover" src="' + escapeHtml(res.cover_url) + '" alt="cover" onerror="this.style.display=\'none\'"></div></div>');
      }
      sections.push(rows.join(''));
      sections.push('<div class="history-detail__section-title">原始 JSON</div>');
      sections.push('<pre>' + escapeHtml(JSON.stringify(r.result, null, 2)) + '</pre>');
    }
    return '<div class="history-detail">' + sections.join('') +
      '<div style="text-align:right"><button class="btn btn--secondary" data-role="close-detail" data-id="' + r.id + '">关闭</button></div>' +
      '</div>';
  }

  function mediaTypeName(n) {
    if (n === 1) return '图片';
    if (n === 2) return '视频';
    if (n === 4) return '文章';
    return '未知';
  }

  function renderPagination(total, page, size) {
    var totalPages = Math.max(1, Math.ceil(total / size));
    var btns = [];
    var prevDisabled = page <= 1 ? 'disabled' : '';
    var nextDisabled = page >= totalPages ? 'disabled' : '';
    btns.push('<button class="history-pagination__btn" data-page="' + (page - 1) + '" ' + prevDisabled + '>‹</button>');
    var startP = Math.max(1, page - 2);
    var endP = Math.min(totalPages, page + 2);
    if (startP > 1) {
      btns.push('<button class="history-pagination__btn" data-page="1">1</button>');
      if (startP > 2) btns.push('<span class="history-pagination__btn" style="cursor:default">…</span>');
    }
    for (var p = startP; p <= endP; p++) {
      btns.push('<button class="history-pagination__btn ' + (p === page ? 'history-pagination__btn--active' : '') + '" data-page="' + p + '">' + p + '</button>');
    }
    if (endP < totalPages) {
      if (endP < totalPages - 1) btns.push('<span class="history-pagination__btn" style="cursor:default">…</span>');
      btns.push('<button class="history-pagination__btn" data-page="' + totalPages + '">' + totalPages + '</button>');
    }
    btns.push('<button class="history-pagination__btn" data-page="' + (page + 1) + '" ' + nextDisabled + '>›</button>');
    btns.push('<button class="btn btn--secondary" data-role="clear-all" style="margin-left:var(--s-3)">清空</button>');
    return '<div class="history-pagination">' +
             '<div class="history-pagination__pages">' + btns.join('') + '</div>' +
           '</div>';
  }

  /* ---------- toggle helpers ---------- */

  function toggleExpand(id, slot) {
    if (state.expanded.has(id)) state.expanded.delete(id); else state.expanded.add(id);
    renderList(slot);
  }

  function toggleSelect(id, on) {
    if (on) state.selected.add(id); else state.selected.delete(id);
  }

  function refreshBatchBtn(slot) {
    var btn = slot.querySelector('[data-role="batch-delete"]');
    if (!btn) return;
    var n = state.selected.size;
    btn.disabled = n === 0;
    btn.textContent = n > 0 ? '批量删除 (' + n + ')' : '批量删除';
  }

  /* ---------- delete ops ---------- */

  function oneDelete(slot, id) {
    if (!global.confirm('确定删除此条记录?')) return;
    callDelete(slot, '?id=' + id).then(function () {
      state.expanded.delete(id);
      state.selected.delete(id);
      load(slot);
    });
  }

  function batchDelete(slot) {
    var ids = Array.from(state.selected);
    if (ids.length === 0) return;
    if (!global.confirm('确定删除 ' + ids.length + ' 条记录?')) return;
    callDelete(slot, '?id=' + ids.join(',')).then(function () {
      state.selected.clear();
      load(slot);
    });
  }

  function clearAll(slot) {
    if (!state.data || state.data.total === 0) return;
    if (!global.confirm('确定删除全部 ' + state.data.total + ' 条历史?此操作不可撤销')) return;
    callDelete(slot, '?all=1').then(function () {
      state.selected.clear();
      state.expanded.clear();
      load(slot);
    });
  }

  function callDelete(slot, query) {
    return global.WXApi.authFetch('/api/history' + query, { method: 'DELETE' })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body && body.code === 0) {
          if (global.WXToast) global.WXToast('已删除 ' + (body.data ? body.data.deleted : 0) + ' 条', 'success');
        } else {
          if (global.WXToast) global.WXToast('删除失败: ' + (body && body.msg || '未知错误'), 'error');
          throw new Error(body && body.msg || 'delete failed');
        }
      })
      .catch(function (e) {
        if (e && e.isAuth) return; // already handled by api.js
        if (global.WXToast) global.WXToast('删除失败: ' + e.message, 'error');
      });
  }

  /* ---------- load ---------- */

  function buildQuery() {
    var qs = [];
    qs.push('range=' + encodeURIComponent(state.filter.range));
    qs.push('kind=' + encodeURIComponent(state.filter.kind));
    qs.push('status=' + encodeURIComponent(state.filter.status));
    qs.push('token=' + encodeURIComponent(state.filter.token));
    if (state.filter.q) qs.push('q=' + encodeURIComponent(state.filter.q));
    qs.push('page=' + state.page);
    qs.push('size=' + state.size);
    return '/api/history?' + qs.join('&');
  }

  function load(slot) {
    if (state.abortCtrl) state.abortCtrl.abort();
    state.abortCtrl = new AbortController();
    var url = buildQuery();
    var prevData = state.data;
    state.data = null;
    renderList(slot);

    global.WXApi.authJson(url)
      .then(function (res) {
        state.abortCtrl = null;
        if (res.data && res.data.code === 0 && res.data.data) {
          state.data = res.data.data;
          renderList(slot);
        } else {
          state.data = prevData;
          renderList(slot);
          renderError(slot, (res.data && res.data.msg) || '未知错误');
        }
      })
      .catch(function (e) {
        if (e && e.isAuth) return;
        state.data = prevData;
        renderList(slot);
        renderError(slot, e.message || '网络错误');
      });
  }

  function renderError(slot, msg) {
    var list = slot.querySelector('[data-role="list"]');
    if (list) {
      list.innerHTML = '<div class="result-msg">' + escapeHtml('加载失败: ' + msg) + ' <button class="btn btn--secondary" data-role="retry">重试</button></div>';
      var btn = list.querySelector('[data-role="retry"]');
      if (btn) btn.addEventListener('click', function () { load(slot); });
    }
  }

  /* ---------- config + bootstrap ---------- */

  function loadTokenLabels() {
    return global.WXApi.authJson('/api/config')
      .then(function (res) {
        if (res.data && res.data.code === 0 && res.data.data) {
          state.tokenLabels = (res.data.data.tokens || []).map(function (t) { return t.label; }).filter(Boolean);
        }
      })
      .catch(function () { /* non-fatal */ });
  }

  /* ---------- render ---------- */

  function render(slot) {
    if (state.abortCtrl) { state.abortCtrl.abort(); state.abortCtrl = null; }
    state.data = null;
    state.expanded.clear();
    state.selected.clear();
    slot.innerHTML = renderSkeleton();
    syncFilterUI(slot);
    populateTokenDropdown(slot);
    wireFilter(slot);
    var pag = slot.querySelector('[data-role="pagination"]');
    if (pag) pag.addEventListener('click', function (e) {
      if (e.target.matches('[data-role="clear-all"]')) clearAll(slot);
    });
    loadTokenLabels().then(function () { populateTokenDropdown(slot); });
    load(slot);
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.history = { render: render };
})(window);
```

- [ ] **Step 3: Verify the build embeds the new JS**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
```
Expected: build succeeds.

- [ ] **Step 4: Smoke test in browser**

1. Open `/history`. Filter bar at top, list below, pagination at bottom.
2. The list should already have rows from prior smoke tests (Task 4 + 5 + 6).
3. Click a row — detail expands showing request, msg (if any), result fields with copy buttons.
4. Change "时间" to "全部" — list refreshes with all rows.
5. Type a URL substring into 搜索 — list filters.
6. Click a checkbox — "批量删除 (1)" button lights up.
7. Click "清空" — confirm dialog appears; confirm — list empties.
8. Restart the binary (so the retention loop's 60s timer resets); wait 60s; check `dist/app.log` for `[retention] purged N records` (should be 0 since we just emptied).

- [ ] **Step 5: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add web/static/js/pages/history.js web/static/css/pages.css
git commit -m "feat(ui/history): full history page with filter, list, expand, paginate, delete

- history.js rewrite: filter bar (time/kind/status/token/q), 50/page,
  in-flight abort on filter change, row click expands to request/msg/
  result + raw JSON, copy buttons per field, single/batch/full delete
- pages.css append: history filter grid, table, row expand, pagination,
  badges (status/kind/source), mobile responsive (table → card stack <900px)
"
```

---

## End-to-end smoke test (after all 8 tasks)

1. **Fresh DB**: delete `dist/wx_web_api.db*`; build & start.
2. **Open /test**, fire one /wx call (use a token from /settings, paste a real WeChat URL). Wait for result.
3. **Open /history**. The call appears with `来源=admin_test`.
4. **Open /system**. The 健康度 card shows `今日调用=1, 请求总数=1, 错误率=0.0%` and updates every 2s.
5. **Open /settings**. Add a label "test-bot" to the token. Save. Open /history. The row's Token column now shows "test-bot".
6. **In /settings**, set 历史保留天数=1, save. Restart. Wait 60s. Check log: `[retention] purged 0 records older than 1 days` (the row is too new).
7. **Filter test**: in /history, set 时间=全部, set 状态=成功. Verify filtering works. Type a search term, verify LIKE filter.
8. **Delete test**: tick a row's checkbox, click "批量删除 (1)", confirm. Row disappears.
9. **Clear all test**: click "清空", confirm. List shows empty state.
10. **Re-login test**: log out, log back in. /history still works (session token changed; admin UI is fine).

If all 10 pass, the feature is done.
