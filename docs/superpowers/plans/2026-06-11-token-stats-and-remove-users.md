# Token 调用统计 + 图表 + 下线"用户/角色"页 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the unused `/users` placeholder page; add per-token call stats (today/week/month/total + custom range), a custom date-range popover that appends a column/card, and a hand-rolled SVG trend chart + CSS Grid heatmap to the dashboard. Cap `history_retention_days` at 1..60.

**Architecture:** Backend adds 6 read-only aggregation methods to `storage.Storage`, extends `eventsHub.ReqStats` to carry the new fields (already broadcast over WS every 2s), and exposes 2 new REST endpoints (`/api/stats`, `/api/stats/daily`) for custom range + chart data. Frontend rewrites `dashboard.js` to render 6 stat cards + a per-token breakdown table + a token-filtered chart card, subscribes to `system.snapshot` for live numbers, and debounces `log.new` → chart refresh.

**Tech Stack:** Go (Gin, gorilla/websocket, modernc.org/sqlite), vanilla HTML/CSS/JS (no build), embedded `web/` assets.

**Spec:** `docs/superpowers/specs/2026-06-11-token-stats-and-remove-users-design.md`

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/storage/log.go` | Time helpers: `StartOfWeekMs`, `StartOfMonthMs` |
| `internal/storage/storage.go` | 6 new aggregation methods |
| `internal/storage/storage_test.go` | 7 new test cases |
| `internal/handler/broadcaster.go` | Extend `ReqStats` + `TokenStat`; rewrite `collectSnapshot` |
| `internal/handler/stats.go` (new) | `GetStats` + `GetStatsDaily` handlers |
| `internal/handler/settings.go` | Retention 1..60 validation |
| `internal/config/config.go` | Clamp legacy retention on `Init` |
| `main.go` | Register 2 new routes |
| `web/static/js/router.js` | Remove `/users` route + icon |
| `web/index.html` | Remove `users.js` script tag |
| `web/static/js/pages/dashboard.js` | Full rewrite (6 cards + table + custom range + charts) |
| `web/static/js/pages/users.js` | **Deleted** |
| `web/static/css/components.css` | `.stat-grid` → auto-fit; drop 768px media |
| `web/static/css/pages.css` | Append token breakdown table, popover, chart styles |
| `web/static/js/pages/settings.js` | Retention input `min=1 max=60` + hint text |

---

## Task 1: Add time helpers `StartOfWeekMs` and `StartOfMonthMs`

**Files:**
- Modify: `internal/storage/log.go:1-52`

- [ ] **Step 1: Append the two helpers to `internal/storage/log.go`**

Append to the end of the file (after the existing `StartOfTodayMs`):

```go
// StartOfWeekMs returns the unix-millisecond timestamp of the start of the
// current local week (Monday 00:00 local). On Sunday, returns the Monday of
// the same week (i.e. 6 days ago), not the next Monday.
func StartOfWeekMs() int64 {
	now := time.Now()
	// time.Weekday() returns Sunday=0, Monday=1, ... Saturday=6.
	// Days to subtract to reach this week's Monday:
	offset := int(now.Weekday()) - 1
	if offset < 0 {
		offset = 6 // Sunday → 6 days back to Monday
	}
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, -offset)
	return monday.UnixMilli()
}

// StartOfMonthMs returns the unix-millisecond timestamp of the start of the
// current local month (day 1, 00:00 local).
func StartOfMonthMs() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).UnixMilli()
}
```

- [ ] **Step 2: Build to confirm no errors**

Run: `go build ./...`
Expected: exits 0, no output.

- [ ] **Step 3: Commit**

```bash
git add internal/storage/log.go
git commit -m "feat(storage): add StartOfWeekMs and StartOfMonthMs time helpers"
```

---

## Task 2: Add `CountSuccessSince` and `CountSuccessBetween` with TDD

**Files:**
- Modify: `internal/storage/storage.go` (append after `CountErrors`, line 261)
- Modify: `internal/storage/storage_test.go` (append after `TestLogAndQuery`)

- [ ] **Step 1: Add the failing test**

Append to `internal/storage/storage_test.go`:

```go
func TestCountSuccessSince_ExcludesNonZeroStatus(t *testing.T) {
	s := newTempStorage(t)
	now := time.Now().UnixMilli()

	rows := []RequestLog{
		{Ts: now - 3000, TokenLabel: "alpha", Kind: "url", Source: "external", ClientIP: "1.1.1.1",
			Request: json.RawMessage(`{"url":"https://a"}`), Status: 0,  LatencyMs: 10},
		{Ts: now - 2000, TokenLabel: "alpha", Kind: "url", Source: "external", ClientIP: "1.1.1.1",
			Request: json.RawMessage(`{"url":"https://b"}`), Status: 1,  LatencyMs: 20, Msg: "err"},
		{Ts: now - 1000, TokenLabel: "beta",  Kind: "auth", Source: "external", ClientIP: "1.1.1.1",
			Request: json.RawMessage(`{"path":"x"}`),            Status: 401, LatencyMs: 5,  Msg: "expired"},
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
		{Ts: ts,         TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1},
		{Ts: ts + 1000,  TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1},
		{Ts: ts + 2000,  TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1},
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
```

- [ ] **Step 2: Run tests to confirm they fail**

Run: `go test ./internal/storage -run 'TestCountSuccess' -v`
Expected: compile error (`CountSuccessSince` / `CountSuccessBetween` undefined).

- [ ] **Step 3: Implement the methods**

Append to `internal/storage/storage.go` (after `CountErrors`):

```go
// CountSuccessSince returns the number of request_log rows with status=0
// and ts >= sinceMs. sinceMs == 0 means "no lower bound".
func (s *Storage) CountSuccessSince(sinceMs int64) (int64, error) {
	var n int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM request_log WHERE status = 0 AND ts >= ?", sinceMs).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count success since: %w", err)
	}
	return n, nil
}

// CountSuccessBetween returns the number of request_log rows with status=0
// and startMs <= ts < endMs (inclusive of start, exclusive of end).
func (s *Storage) CountSuccessBetween(startMs, endMs int64) (int64, error) {
	var n int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM request_log WHERE status = 0 AND ts >= ? AND ts < ?", startMs, endMs).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count success between: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/storage -run 'TestCountSuccess' -v`
Expected: `--- PASS: TestCountSuccessSince_ExcludesNonZeroStatus` and `--- PASS: TestCountSuccessBetween_InclusiveStart_ExclusiveEnd`.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/storage.go internal/storage/storage_test.go
git commit -m "feat(storage): add CountSuccessSince and CountSuccessBetween"
```

---

## Task 3: Add `CountSuccessByTokenSince` / `CountSuccessByTokenBetween` with TDD

**Files:**
- Modify: `internal/storage/storage.go` (append)
- Modify: `internal/storage/storage_test.go` (append)

- [ ] **Step 1: Add the failing tests**

Append to `internal/storage/storage_test.go`:

```go
func TestCountSuccessByTokenSince_GroupsCorrectly(t *testing.T) {
	s := newTempStorage(t)
	now := time.Now().UnixMilli()

	rows := []RequestLog{
		{Ts: now - 3000, TokenLabel: "alpha", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0,  LatencyMs: 1},
		{Ts: now - 2000, TokenLabel: "alpha", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0,  LatencyMs: 1},
		{Ts: now - 1500, TokenLabel: "alpha", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 1,  LatencyMs: 1, Msg: "err"},
		{Ts: now - 1000, TokenLabel: "beta",  Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0,  LatencyMs: 1},
	}
	for i := range rows {
		if err := s.LogRequest(&rows[i]); err != nil {
			t.Fatalf("LogRequest[%d]: %v", i, err)
		}
	}

	got, err := s.CountSuccessByTokenSince(0, []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("CountSuccessByTokenSince: %v", err)
	}
	if got["alpha"] != 2 {
		t.Fatalf("alpha = %d, want 2 (status=0 only)", got["alpha"])
	}
	if got["beta"] != 1 {
		t.Fatalf("beta = %d, want 1", got["beta"])
	}
}

func TestCountSuccessByTokenSince_FiltersToLabels(t *testing.T) {
	s := newTempStorage(t)
	now := time.Now().UnixMilli()
	if err := s.LogRequest(&RequestLog{
		Ts: now, TokenLabel: "in_db", Kind: "url", Source: "external", ClientIP: "",
		Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1,
	}); err != nil {
		t.Fatalf("LogRequest: %v", err)
	}

	// Pass a label list that does NOT include "in_db"
	got, err := s.CountSuccessByTokenSince(0, []string{"other"})
	if err != nil {
		t.Fatalf("CountSuccessByTokenSince: %v", err)
	}
	if _, ok := got["in_db"]; ok {
		t.Fatalf("got should not include label 'in_db' (not in requested list): %v", got)
	}
	if _, ok := got["other"]; ok {
		t.Fatalf("got should not include label 'other' (no matching rows): %v", got)
	}
}

func TestCountSuccessByTokenSince_EmptyLabelsReturnsEmptyMap(t *testing.T) {
	s := newTempStorage(t)
	now := time.Now().UnixMilli()
	if err := s.LogRequest(&RequestLog{
		Ts: now, TokenLabel: "alpha", Kind: "url", Source: "external", ClientIP: "",
		Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1,
	}); err != nil {
		t.Fatalf("LogRequest: %v", err)
	}

	got, err := s.CountSuccessByTokenSince(0, nil)
	if err != nil {
		t.Fatalf("CountSuccessByTokenSince(nil labels): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map for nil labels, got %v", got)
	}

	got, err = s.CountSuccessByTokenSince(0, []string{})
	if err != nil {
		t.Fatalf("CountSuccessByTokenSince(empty labels): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map for empty labels, got %v", got)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

Run: `go test ./internal/storage -run 'TestCountSuccessByToken' -v`
Expected: compile error (`CountSuccessByTokenSince` undefined).

- [ ] **Step 3: Implement the methods**

Append to `internal/storage/storage.go`:

```go
// CountSuccessByTokenSince returns counts grouped by token_label for rows
// with status=0 and ts >= sinceMs. Only labels in the provided list are
// returned; an empty/nil labels list short-circuits to an empty map (no SQL).
// Labels without matching rows are not present in the result map.
func (s *Storage) CountSuccessByTokenSince(sinceMs int64, labels []string) (map[string]int64, error) {
	if len(labels) == 0 {
		return map[string]int64{}, nil
	}
	placeholders := strings.Repeat("?,", len(labels))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(labels)+1)
	args = append(args, sinceMs)
	for _, l := range labels {
		args = append(args, l)
	}
	q := "SELECT token_label, COUNT(*) FROM request_log WHERE status = 0 AND ts >= ? AND token_label IN (" + placeholders + ") GROUP BY token_label"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("count success by token: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int64, len(labels))
	for rows.Next() {
		var label string
		var n int64
		if err := rows.Scan(&label, &n); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out[label] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// CountSuccessByTokenBetween is the [start, end) inclusive-start variant of
// CountSuccessByTokenSince.
func (s *Storage) CountSuccessByTokenBetween(startMs, endMs int64, labels []string) (map[string]int64, error) {
	if len(labels) == 0 {
		return map[string]int64{}, nil
	}
	placeholders := strings.Repeat("?,", len(labels))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(labels)+2)
	args = append(args, startMs, endMs)
	for _, l := range labels {
		args = append(args, l)
	}
	q := "SELECT token_label, COUNT(*) FROM request_log WHERE status = 0 AND ts >= ? AND ts < ? AND token_label IN (" + placeholders + ") GROUP BY token_label"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("count success by token between: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int64, len(labels))
	for rows.Next() {
		var label string
		var n int64
		if err := rows.Scan(&label, &n); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out[label] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/storage -run 'TestCountSuccessByToken' -v`
Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/storage.go internal/storage/storage_test.go
git commit -m "feat(storage): add CountSuccessByTokenSince/Between with IN-label filter"
```

---

## Task 4: Add `AvgLatencyTodayMs` and `DailySuccessCounts` with TDD

**Files:**
- Modify: `internal/storage/storage.go` (append)
- Modify: `internal/storage/storage_test.go` (append)

- [ ] **Step 1: Add the failing tests**

Append to `internal/storage/storage_test.go`:

```go
func TestAvgLatencyTodayMs_NoDataReturnsZero(t *testing.T) {
	s := newTempStorage(t)
	// No data at all → 0
	got, err := s.AvgLatencyTodayMs()
	if err != nil {
		t.Fatalf("AvgLatencyTodayMs empty: %v", err)
	}
	if got != 0 {
		t.Fatalf("AvgLatencyTodayMs empty = %d, want 0", got)
	}

	// Insert one row from 2 hours ago (definitely "today" locally) with status=1
	// → must NOT be counted.
	now := time.Now().UnixMilli()
	twoHoursAgo := now - 2*60*60*1000
	if err := s.LogRequest(&RequestLog{
		Ts: twoHoursAgo, TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "",
		Request: json.RawMessage(`{}`), Status: 1, LatencyMs: 9999, Msg: "err",
	}); err != nil {
		t.Fatalf("LogRequest: %v", err)
	}
	got, err = s.AvgLatencyTodayMs()
	if err != nil {
		t.Fatalf("AvgLatencyTodayMs err-row only: %v", err)
	}
	if got != 0 {
		t.Fatalf("AvgLatencyTodayMs with only status=1 row = %d, want 0", got)
	}

	// Now add a status=0 row with latency 100ms.
	if err := s.LogRequest(&RequestLog{
		Ts: twoHoursAgo + 1000, TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "",
		Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 100,
	}); err != nil {
		t.Fatalf("LogRequest: %v", err)
	}
	got, err = s.AvgLatencyTodayMs()
	if err != nil {
		t.Fatalf("AvgLatencyTodayMs mixed: %v", err)
	}
	if got != 100 {
		t.Fatalf("AvgLatencyTodayMs = %d, want 100 (only the status=0 row counts)", got)
	}
}

func TestDailySuccessCounts_GroupsByLocalDay(t *testing.T) {
	s := newTempStorage(t)
	// Pin "now" to a known instant in local time; pick a 3-day window:
	// today, today-1, today-2 — all in local time, all status=0.
	now := time.Now()
	t0Ms := now.UnixMilli()
	t1Ms := now.Add(-24 * time.Hour).UnixMilli()
	t2Ms := now.Add(-48 * time.Hour).UnixMilli()
	rows := []RequestLog{
		{Ts: t0Ms,    TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1},
		{Ts: t0Ms,    TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1}, // 2 today
		{Ts: t1Ms,    TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1}, // 1 yesterday
		{Ts: t2Ms,    TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 1, LatencyMs: 1, Msg: "err"}, // not counted
		// A row outside the window (7 days ago) must not appear.
		{Ts: now.Add(-7 * 24 * time.Hour).UnixMilli(), TokenLabel: "a", Kind: "url", Source: "external", ClientIP: "", Request: json.RawMessage(`{}`), Status: 0, LatencyMs: 1},
	}
	for i := range rows {
		if err := s.LogRequest(&rows[i]); err != nil {
			t.Fatalf("LogRequest[%d]: %v", i, err)
		}
	}

	// sinceMs = 3 days ago; we expect exactly 2 days (today + yesterday), 3 rows total.
	sinceMs := now.Add(-3 * 24 * time.Hour).UnixMilli()
	got, err := s.DailySuccessCounts(sinceMs, "")
	if err != nil {
		t.Fatalf("DailySuccessCounts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (zero-fill is caller's job, not storage's)", len(got))
	}
	// Order is by day ASC: yesterday first, then today.
	wantFirst := time.UnixMilli(t1Ms).In(now.Location())
	wantFirstDay := wantFirst.Format("2006-01-02")
	if got[0].Date != wantFirstDay {
		t.Fatalf("got[0].Date = %q, want %q", got[0].Date, wantFirstDay)
	}
	if got[0].Count != 1 {
		t.Fatalf("got[0].Count = %d, want 1", got[0].Count)
	}
	wantSecond := time.UnixMilli(t0Ms).In(now.Location())
	wantSecondDay := wantSecond.Format("2006-01-02")
	if got[1].Date != wantSecondDay {
		t.Fatalf("got[1].Date = %q, want %q", got[1].Date, wantSecondDay)
	}
	if got[1].Count != 2 {
		t.Fatalf("got[1].Count = %d, want 2", got[1].Count)
	}

	// Token filter: same data, token "a" should return 2 days; empty filter "" should also.
	gotFiltered, err := s.DailySuccessCounts(sinceMs, "a")
	if err != nil {
		t.Fatalf("DailySuccessCounts(token=a): %v", err)
	}
	if len(gotFiltered) != 2 || gotFiltered[0].Count != 1 || gotFiltered[1].Count != 2 {
		t.Fatalf("token=a filter: got %+v, want [{date1,1},{date2,2}]", gotFiltered)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

Run: `go test ./internal/storage -run 'TestAvgLatencyTodayMs|TestDailySuccessCounts' -v`
Expected: compile errors (`AvgLatencyTodayMs`, `DailySuccessCounts`, type `DailyCount` undefined).

- [ ] **Step 3: Implement the methods**

Append to `internal/storage/storage.go`:

```go
// DailyCount is one day bucket returned by DailySuccessCounts. Date is
// "yyyy-MM-dd" in the server's local time. Days with zero rows are NOT
// included — the caller (handler) is responsible for zero-filling.
type DailyCount struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// AvgLatencyTodayMs returns the average latency in milliseconds for request_log
// rows with status=0 and ts >= today's local 00:00. Returns 0 when no rows match.
func (s *Storage) AvgLatencyTodayMs() (int64, error) {
	var avg sql.NullFloat64
	err := s.db.QueryRow(
		"SELECT AVG(latency_ms) FROM request_log WHERE status = 0 AND ts >= ?",
		StartOfTodayMs(),
	).Scan(&avg)
	if err != nil {
		return 0, fmt.Errorf("avg latency today: %w", err)
	}
	if !avg.Valid {
		return 0, nil
	}
	return int64(avg.Float64), nil
}

// DailySuccessCounts groups status=0 rows by local calendar day. sinceMs is
// the lower bound; tokenLabel, if non-empty, filters to that token. Result is
// sorted by Date ascending. Days with zero rows are NOT included.
func (s *Storage) DailySuccessCounts(sinceMs int64, tokenLabel string) ([]DailyCount, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if tokenLabel == "" {
		rows, err = s.db.Query(
			`SELECT strftime('%Y-%m-%d', ts/1000, 'unixepoch', 'localtime') AS day, COUNT(*)
			 FROM request_log
			 WHERE status = 0 AND ts >= ?
			 GROUP BY day ORDER BY day`,
			sinceMs,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT strftime('%Y-%m-%d', ts/1000, 'unixepoch', 'localtime') AS day, COUNT(*)
			 FROM request_log
			 WHERE status = 0 AND ts >= ? AND token_label = ?
			 GROUP BY day ORDER BY day`,
			sinceMs, tokenLabel,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("daily success counts: %w", err)
	}
	defer rows.Close()
	out := make([]DailyCount, 0, 16)
	for rows.Next() {
		var d DailyCount
		if err := rows.Scan(&d.Date, &d.Count); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Run all storage tests to confirm everything passes**

Run: `go test ./internal/storage -v`
Expected: all tests PASS (existing 2 + new 7).

- [ ] **Step 5: Commit**

```bash
git add internal/storage/storage.go internal/storage/storage_test.go
git commit -m "feat(storage): add AvgLatencyTodayMs and DailySuccessCounts"
```

---

## Task 5: Clamp legacy retention in `config.Init`

**Files:**
- Modify: `internal/config/config.go:76-95` (the retention backfill block)

- [ ] **Step 1: Edit the retention logic to clamp to [1, 60]**

In `internal/config/config.go`, replace the entire `// Inject default for HistoryRetentionDays...` block (lines 76-95) with:

```go
	// Clamp HistoryRetentionDays into the valid [1, 60] range. The legacy
	// "0 = permanent" semantics and any value > 60 are replaced with 60.
	// The "key absent" path still injects 60 as the default for fresh installs.
	retentionChanged := false
	if m.config.HistoryRetentionDays < 1 || m.config.HistoryRetentionDays > 60 {
		m.config.HistoryRetentionDays = 60
		retentionChanged = true
	}
	// For fresh installs (no file at all), if the default injection above didn't
	// already move us off 0, force 60.
	if !fileExisted && m.config.HistoryRetentionDays == 0 {
		m.config.HistoryRetentionDays = 60
		retentionChanged = true
	}
```

Note: we drop the old `fileExisted && rerr == nil` branch that only injected 30 when the key was missing — that path is now subsumed by the unconditional clamp. The `raw["history_retention_days"]` probe is no longer used; the `loadRawJson()` call above (line 63) still parses the file but its result `raw` becomes unused. We leave the `loadRawJson` call in place to avoid changing the file's parse-error behavior; the `rerr` variable becomes unused — keep the assignment for now and use `_ = rerr` at the call site if the linter complains. (We'll address that in Task 6 step 2.)

- [ ] **Step 2: Build to confirm**

Run: `go build ./...`
Expected: exits 0, no output. If the linter complains about unused `rerr` or `raw`, add `_ = rerr` immediately after the `loadRawJson()` call on line 63. If `raw` is now unused, change the call to `_ = m.loadRawJson()` to discard.

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): clamp legacy retention to [1, 60] on Init"
```

---

## Task 6: Validate retention range in `UpdateConfig`

**Files:**
- Modify: `internal/handler/settings.go:80-87`

- [ ] **Step 1: Replace the validation block**

In `internal/handler/settings.go`, replace the comment + if block at lines 80-87:

```go
	// Pointer distinguishes "absent" (don't touch) from "explicit zero" (set to 0=permanent).
	if req.HistoryRetentionDays != nil {
		if *req.HistoryRetentionDays < 0 {
			c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "history_retention_days must be >= 0"})
			return
		}
		cfg.HistoryRetentionDays = *req.HistoryRetentionDays
	}
```

with:

```go
	// Pointer distinguishes "absent" (don't touch) from "explicit value".
	// Valid range is 1..60 — the legacy "0 = permanent" option is gone.
	if req.HistoryRetentionDays != nil {
		v := *req.HistoryRetentionDays
		if v < 1 || v > 60 {
			c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "history_retention_days 必须在 1~60 之间"})
			return
		}
		cfg.HistoryRetentionDays = v
	}
```

- [ ] **Step 2: Build to confirm**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add internal/handler/settings.go
git commit -m "feat(settings): validate retention range 1..60 on update"
```

---

## Task 7: Extend `ReqStats` and rewrite `collectSnapshot`

**Files:**
- Modify: `internal/handler/broadcaster.go:25-181`

- [ ] **Step 1: Replace `ReqStats` struct (lines 25-31) and add `TokenStat`**

Replace the existing `ReqStats` struct:

```go
// TokenStat 是单 token 的多区间成功调用计数,出现在 system.snapshot.stats.by_token 中。
// Today/Week/Month/Total 都限定 status=0;Total 的实际覆盖范围 = 近 retention 天。
type TokenStat struct {
	Label string `json:"label"`
	Today int64  `json:"today"`
	Week  int64  `json:"week"`
	Month int64  `json:"month"`
	Total int64  `json:"total"`
}

// ReqStats 是 system.snapshot 中 stats 字段的 JSON 形态。
// 既有 Total/Today/Errors 保持不变(老前端可继续用),新增字段全部限定 status=0。
// Stats 为 nil 时表示 request_log 尚未就绪(前端应显示 "—")。
type ReqStats struct {
	// 既有字段
	Total  int64 `json:"total"`
	Today  int64 `json:"today"`
	Errors int64 `json:"errors"`

	// 新增:成功调用计数
	SuccessToday int64 `json:"success_today"`
	SuccessWeek  int64 `json:"success_week"`
	SuccessMonth int64 `json:"success_month"`
	SuccessTotal int64 `json:"success_total"`

	// 新增:今日成功平均耗时(int ms,无数据时 0)
	AvgLatencyToday int64 `json:"avg_latency_today_ms"`

	// 新增:当前 retention 配置(用于"总计(近 N 天)"卡的副文案)
	RetentionDays int `json:"retention_days"`

	// 新增:按当前 cfg.Tokens 顺序排列的成功调用计数
	ByToken []TokenStat `json:"by_token"`
}
```

- [ ] **Step 2: Replace `collectSnapshot` (lines 158-181)**

Replace the existing `collectSnapshot` function with:

```go
// collectSnapshot 构造当前的 SystemSnapshot(stats 从 storage 实时拉)。
// 被 systemTickerLoop 和 EventsHub.Snapshot 共享。
func (h *eventsHub) collectSnapshot() SystemSnapshot {
	snap := SystemSnapshot{
		Type:          "system.snapshot",
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
	if h.storage == nil {
		return snap
	}

	// 既有三连(全部行,不限 status)
	total, _ := h.storage.Count()
	since, _ := h.storage.CountSince(storage.StartOfTodayMs())
	errs, _ := h.storage.CountErrors()

	// 拉当前 token 列表(取 Label,空/未初始化时为空)
	cfg := config.Get()
	tokenLabels := make([]string, 0, len(cfg.Tokens))
	for _, t := range cfg.Tokens {
		tokenLabels = append(tokenLabels, t.Label)
	}

	// 4 个区间(now,week,month,all)的全局成功计数
	now := storage.StartOfTodayMs()
	week := storage.StartOfWeekMs()
	month := storage.StartOfMonthMs()
	successToday, _ := h.storage.CountSuccessSince(now)
	successWeek, _ := h.storage.CountSuccessSince(week)
	successMonth, _ := h.storage.CountSuccessSince(month)
	successTotal, _ := h.storage.CountSuccessSince(0)

	// 按 token 分组的 4 个区间
	byNow, _ := h.storage.CountSuccessByTokenSince(now, tokenLabels)
	byWeek, _ := h.storage.CountSuccessByTokenSince(week, tokenLabels)
	byMonth, _ := h.storage.CountSuccessByTokenSince(month, tokenLabels)
	byAll, _ := h.storage.CountSuccessByTokenSince(0, tokenLabels)

	// 平均耗时
	avgLat, _ := h.storage.AvgLatencyTodayMs()

	// 合并 by_token:按 cfg.Tokens 顺序,缺的补 0
	byToken := make([]TokenStat, 0, len(tokenLabels))
	for _, label := range tokenLabels {
		byToken = append(byToken, TokenStat{
			Label: label,
			Today: byNow[label],
			Week:  byWeek[label],
			Month: byMonth[label],
			Total: byAll[label],
		})
	}

	snap.Stats = &ReqStats{
		Total:           total,
		Today:           since,
		Errors:          errs,
		SuccessToday:    successToday,
		SuccessWeek:     successWeek,
		SuccessMonth:    successMonth,
		SuccessTotal:    successTotal,
		AvgLatencyToday: avgLat,
		RetentionDays:   cfg.HistoryRetentionDays,
		ByToken:         byToken,
	}
	return snap
}
```

- [ ] **Step 3: Build to confirm**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 4: Run all tests to confirm nothing broke**

Run: `go test ./...`
Expected: all tests PASS (handler `events_test.go` may need a moment — it doesn't touch collectSnapshot but the package compiles). If `go vet` reports unused `raw` in `config.go` from Task 5, fix that there.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/broadcaster.go
git commit -m "feat(broadcaster): extend ReqStats with success counts, by_token, retention_days"
```

---

## Task 8: Create `internal/handler/stats.go` with `GetStats` and `GetStatsDaily`

**Files:**
- Create: `internal/handler/stats.go`

- [ ] **Step 1: Create the file**

Create `internal/handler/stats.go`:

```go
package handler

import (
	"net/http"
	"strconv"
	"time"
	"wx_web_api/internal/config"
	"wx_web_api/internal/model"

	"github.com/gin-gonic/gin"
)

// GetStats handles GET /api/stats?start=YYYY-MM-DD&end=YYYY-MM-DD
// Returns success-call counts for the [start, end] inclusive-of-start /
// exclusive-of-end range, both globally and per currently-configured token.
//
// start and end are required and interpreted in server local time. The
// end value is normalized to "end-of-day local 23:59:59.999".
// start must be >= today - retention_days; end must be <= today.
func (h *Handler) GetStats(c *gin.Context) {
	cfg := config.Get()
	startStr := c.Query("start")
	endStr := c.Query("end")
	if startStr == "" || endStr == "" {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "start and end are required (yyyy-MM-dd)"})
		return
	}
	startDay, err := time.ParseInLocation("2006-01-02", startStr, time.Local)
	if err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid start (want yyyy-MM-dd)"})
		return
	}
	endDay, err := time.ParseInLocation("2006-01-02", endStr, time.Local)
	if err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid end (want yyyy-MM-dd)"})
		return
	}
	if endDay.Before(startDay) {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "end must be on or after start"})
		return
	}
	// end-of-day local: next day 00:00 - 1 ms. We store ms; in CountSuccessBetween
	// the upper bound is exclusive, so the handler returns startMs and endMs+1day.
	startMs := startDay.UnixMilli()
	endExclusiveMs := endDay.AddDate(0, 0, 1).UnixMilli()

	// Retention check: start must be >= today - retentionDays
	today := time.Now()
	todayMid := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	earliestAllowed := todayMid.AddDate(0, 0, -cfg.HistoryRetentionDays).UnixMilli()
	if startMs < earliestAllowed {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "start is older than retention window"})
		return
	}
	// end (interpreted as inclusive end-day) must be <= today
	if endDay.After(todayMid) {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "end cannot be in the future"})
		return
	}

	if h.storage == nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "storage not ready"})
		return
	}

	total, err := h.storage.CountSuccessBetween(startMs, endExclusiveMs)
	if err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "storage: " + err.Error()})
		return
	}

	tokenLabels := make([]string, 0, len(cfg.Tokens))
	for _, t := range cfg.Tokens {
		tokenLabels = append(tokenLabels, t.Label)
	}
	byTokenMap, err := h.storage.CountSuccessByTokenBetween(startMs, endExclusiveMs, tokenLabels)
	if err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "storage: " + err.Error()})
		return
	}

	byToken := make([]TokenStat, 0, len(tokenLabels))
	for _, label := range tokenLabels {
		byToken = append(byToken, TokenStat{
			Label: label,
			Total: byTokenMap[label], // single field; the response uses "count" via a different shape
		})
	}

	// Use a flat shape for GetStats: { range: {start,end}, success_total, by_token: [{label, count}] }
	type byTokenOut struct {
		Label string `json:"label"`
		Count int64  `json:"count"`
	}
	out := struct {
		Range        struct{ Start, End string } `json:"range"`
		SuccessTotal int64                       `json:"success_total"`
		ByToken      []byTokenOut                `json:"by_token"`
	}{}
	out.Range.Start = startStr
	out.Range.End = endStr
	out.SuccessTotal = total
	for _, bt := range byToken {
		out.ByToken = append(out.ByToken, byTokenOut{Label: bt.Label, Count: bt.Total})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

// GetStatsDaily handles GET /api/stats/daily?token=<label|all>&days=<1..retention>
// Returns one bucket per local day for the last N days. The handler performs
// zero-filling (days with no rows appear as count=0). Result is sorted by date
// ascending.
func (h *Handler) GetStatsDaily(c *gin.Context) {
	cfg := config.Get()
	retention := cfg.HistoryRetentionDays
	if retention < 1 {
		retention = 1
	}

	daysStr := c.DefaultQuery("days", strconv.Itoa(retention))
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 1 {
		days = retention
	}
	if days > retention {
		days = retention
	}

	tokenFilter := c.Query("token")
	if tokenFilter == "all" {
		tokenFilter = ""
	}

	if h.storage == nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "storage not ready"})
		return
	}

	// Build the since cutoff: today - (days-1) at 00:00 local
	now := time.Now()
	todayMid := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sinceDay := todayMid.AddDate(0, 0, -(days - 1))
	sinceMs := sinceDay.UnixMilli()

	raw, err := h.storage.DailySuccessCounts(sinceMs, tokenFilter)
	if err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "storage: " + err.Error()})
		return
	}

	// Zero-fill: walk day-by-day from sinceDay to todayMid, emitting 0 for gaps.
	byDate := make(map[string]int64, len(raw))
	for _, d := range raw {
		byDate[d.Date] = d.Count
	}
	series := make([]storage.DailyCount, 0, days)
	for i := 0; i < days; i++ {
		d := sinceDay.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		series = append(series, storage.DailyCount{Date: key, Count: byDate[key]})
	}

	tokenOut := tokenFilter
	if tokenOut == "" {
		tokenOut = "all"
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"days":   days,
			"token":  tokenOut,
			"series": series,
		},
	})
}
```

- [ ] **Step 2: Build to confirm**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add internal/handler/stats.go
git commit -m "feat(handler): add GetStats and GetStatsDaily endpoints"
```

---

## Task 9: Register the two new routes in `main.go`

**Files:**
- Modify: `main.go:115-128`

- [ ] **Step 1: Add the route group**

In `main.go`, after the system info routes block (after line 128 `r.GET("/ws/events", ...)`), add:

```go
	// Stats routes (session-authenticated)
	statsGroup := r.Group("/api/stats", h.SessionAuth())
	{
		statsGroup.GET("", h.GetStats)
		statsGroup.GET("/daily", h.GetStatsDaily)
	}
```

- [ ] **Step 2: Build to confirm**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat(main): register /api/stats and /api/stats/daily routes"
```

---

## Task 10: Remove `/users` from `router.js`

**Files:**
- Modify: `web/static/js/router.js:14-21, 137-145`

- [ ] **Step 1: Remove the `/users` entry from `ROUTES`**

In `web/static/js/router.js`, delete line 19:

```js
    { path: '/users',     title: '用户/角色', page: 'users'    },
```

The resulting block (lines 14-21) should be:

```js
  const ROUTES = [
    { path: '/dashboard', title: '概览',     page: 'dashboard' },
    { path: '/settings',  title: '配置',     page: 'settings'  },
    { path: '/test',      title: '解析测试', page: 'test'      },
    { path: '/history',   title: '解析历史', page: 'history'   },
    { path: '/system',    title: '系统信息', page: 'system'    }
  ];
```

- [ ] **Step 2: Remove `users` from `NAV_ICONS`**

Delete line 142:

```js
    users:     '<path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75"/>',
```

- [ ] **Step 3: Commit**

```bash
git add web/static/js/router.js
git commit -m "feat(router): remove /users route and nav icon"
```

---

## Task 11: Remove `users.js` script tag and delete the file

**Files:**
- Modify: `web/index.html:72`
- Delete: `web/static/js/pages/users.js`

- [ ] **Step 1: Remove the `<script>` line from `index.html`**

Delete line 72:

```html
  <script src="/static/js/pages/users.js?v=1"></script>
```

- [ ] **Step 2: `git rm` the placeholder file**

Run:

```bash
git rm web/static/js/pages/users.js
```

Expected: `rm 'web/static/js/pages/users.js'`.

- [ ] **Step 3: Commit**

```bash
git add web/index.html
git commit -m "feat(web): remove users.js script tag and placeholder file"
```

---

## Task 12: Update `.stat-grid` in `components.css`

**Files:**
- Modify: `web/static/css/components.css:148-149`

- [ ] **Step 1: Replace the `.stat-grid` rule and remove the media query**

In `web/static/css/components.css`, replace lines 148-149:

```css
.stat-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--s-4); }
@media (max-width: 768px) { .stat-grid { grid-template-columns: 1fr; } }
```

with:

```css
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: var(--s-4);
}
```

- [ ] **Step 2: Commit**

```bash
git add web/static/css/components.css
git commit -m "feat(css): make .stat-grid auto-fit (6-7 cards per row)"
```

---

## Task 13: Update retention input bounds in `settings.js`

**Files:**
- Modify: `web/static/js/pages/settings.js:419-420`

- [ ] **Step 1: Update the retention input attributes and hint**

Replace lines 419-420:

```html
            '<input type="number" class="input" id="settingsRetentionDays" min="0" max="365" step="1" value="0">' +
            ' <span class="kv__sub">0 = 永久</span>' +
```

with:

```html
            '<input type="number" class="input" id="settingsRetentionDays" min="1" max="60" step="1" value="30">' +
            ' <span class="kv__sub">1~60 天</span>' +
```

- [ ] **Step 2: Commit**

```bash
git add web/static/js/pages/settings.js
git commit -m "feat(settings): retention input bound to 1..60"
```

---

## Task 14: Append chart / table / popover styles to `pages.css`

**Files:**
- Modify: `web/static/css/pages.css` (append to end, line 727+)

- [ ] **Step 1: Append the new CSS**

Append to the end of `web/static/css/pages.css`:

```css
/* Dashboard: token breakdown table */
.token-breakdown-card { /* inherits .card */ }
.token-breakdown-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--t-sm);
}
.token-breakdown-table th,
.token-breakdown-table td {
  padding: var(--s-2) var(--s-3);
  border-bottom: 1px solid var(--border);
  text-align: left;
}
.token-breakdown-table th {
  color: var(--text-muted);
  font-weight: 500;
  font-size: var(--t-xs);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.token-breakdown-table td.num,
.token-breakdown-table th.num {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.token-breakdown-table tr:last-child td { border-bottom: none; }
.token-breakdown-table .col-custom { box-shadow: inset 3px 0 0 var(--primary); }
.token-breakdown-table th.col-custom { color: var(--primary); }

/* Custom range popover (dashboard) */
.custom-range-wrap { position: relative; display: inline-block; }
.custom-range-btn {
  padding: 4px 10px;
  font-size: var(--t-xs);
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: var(--text-muted);
  cursor: pointer;
}
.custom-range-btn:hover { background: var(--surface); color: var(--text); }
.custom-range-btn.is-active { background: var(--primary); color: white; border-color: var(--primary); }
.custom-range-popover {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 50;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-lg);
  padding: var(--s-4);
  display: flex;
  flex-direction: column;
  gap: var(--s-2);
  min-width: 260px;
}
.custom-range-popover[hidden] { display: none; }
.custom-range-popover__row { display: flex; align-items: center; gap: var(--s-2); }
.custom-range-popover__row label { color: var(--text-muted); font-size: var(--t-sm); width: 40px; }
.custom-range-popover__actions { display: flex; gap: var(--s-2); justify-content: flex-end; margin-top: var(--s-2); }
.stat--custom { box-shadow: inset 3px 0 0 var(--primary); }

/* Dashboard: trend + heatmap card */
.charts-card { /* inherits .card */ }
.charts-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--s-4);
  gap: var(--s-3);
  flex-wrap: wrap;
}
.charts-card__head .charts-card__title { font-size: var(--t-base); font-weight: 600; }
.charts-card__head .charts-card__controls { display: flex; gap: var(--s-2); align-items: center; }
.charts-card__head select {
  padding: 4px 8px;
  font-size: var(--t-sm);
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--text);
}
.trend-chart {
  width: 100%;
  height: 200px;
  display: block;
}
.trend-chart__area { fill: var(--primary); fill-opacity: 0.15; }
.trend-chart__line { stroke: var(--primary); stroke-width: 2; fill: none; }
.trend-chart__axis { stroke: var(--border); stroke-width: 1; }
.trend-chart__label { fill: var(--text-faint); font-size: 10px; }
.trend-chart__dot { fill: var(--primary); opacity: 0; transition: opacity 0.15s; }
.trend-chart__dot.is-active { opacity: 1; }

.heatmap {
  display: grid;
  grid-template-rows: repeat(7, 14px);
  grid-auto-flow: column;
  grid-auto-columns: 14px;
  gap: 3px;
  margin-top: var(--s-4);
}
.heatmap__cell {
  width: 14px;
  height: 14px;
  border-radius: 2px;
  background: var(--surface-2);
  cursor: default;
}
.heatmap__cell[data-level="1"] { background: color-mix(in srgb, var(--primary) 25%, var(--surface-2)); }
.heatmap__cell[data-level="2"] { background: color-mix(in srgb, var(--primary) 50%, var(--surface-2)); }
.heatmap__cell[data-level="3"] { background: color-mix(in srgb, var(--primary) 75%, var(--surface-2)); }
.heatmap__cell[data-level="4"] { background: var(--primary); }
.heatmap__legend {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  margin-top: var(--s-2);
  font-size: var(--t-xs);
  color: var(--text-faint);
}
.heatmap__legend-cells { display: flex; gap: 3px; }
.heatmap__legend-cell { width: 14px; height: 14px; border-radius: 2px; }

.chart-tooltip {
  position: fixed;
  z-index: 100;
  background: var(--text);
  color: var(--surface);
  padding: 4px 8px;
  border-radius: var(--r-sm);
  font-size: var(--t-xs);
  pointer-events: none;
  white-space: nowrap;
  box-shadow: var(--shadow-md);
}
.chart-tooltip[hidden] { display: none; }
```

- [ ] **Step 2: Commit**

```bash
git add web/static/css/pages.css
git commit -m "feat(css): add token breakdown, popover, trend chart, heatmap styles"
```

---

## Task 15: Rewrite `dashboard.js`

**Files:**
- Modify: `web/static/js/pages/dashboard.js` (full rewrite — currently 186 lines, replace entirely)

- [ ] **Step 1: Write the new file**

Replace the entire content of `web/static/js/pages/dashboard.js` with:

```js
/* Dashboard page — overview with stats, per-token breakdown, custom range,
 * and trend/heatmap charts.
 *
 *  - Top 6 stat cards: Token 数 / 今日成功 / 本周成功 / 本月成功 / 总计 / 平均耗时.
 *    When a custom range is applied, a 7th "自定义" card is appended.
 *  - Per-token breakdown table: rows = current cfg.Tokens, columns = 4 fixed
 *    intervals. When custom range is active, a "自定义" column is appended.
 *  - Charts card: SVG trend line + CSS Grid heatmap. Top-row token selector
 *    filters both charts; manual refresh button re-pulls /api/stats/daily.
 *  - "最近请求" card is preserved from the previous version.
 *
 * Data flow:
 *  - system.snapshot (WS, 2s) drives stat cards + breakdown table.
 *  - config.changed (WS) reloads the token list and re-renders the table
 *    and the chart's token <select>.
 *  - log.new (WS) is debounced 3s and triggers a chart refresh.
 *  - /api/stats       (REST) — custom range popover, on Apply.
 *  - /api/stats/daily (REST) — chart data, on load / token change / manual refresh.
 */

(function (global) {
  'use strict';

  var RECENT_SIZE = 10;
  var CHART_DEBOUNCE_MS = 3000;

  var state = {
    stats: null,                  // latest ReqStats from system.snapshot
    tokens: [],                   // latest cfg.Tokens
    customRange: null,            // { start: 'yyyy-MM-dd', end: 'yyyy-MM-dd' } or null
    customStats: null,            // { success_total, by_token: [{label,count}] } from /api/stats
    chartToken: '',               // '' = all; else token label
    daily: null,                  // { days, token, series: [{date,count}] }
    chartDebounceTimer: null,
    unsubscribers: [],
  };
  var recent = [];                // last 10 rows, used by loadRecent + log.new unshift

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

  function todayDateStr() {
    var d = new Date();
    return d.getFullYear() + '-' + pad2(d.getMonth() + 1) + '-' + pad2(d.getDate());
  }

  function summarizeRequest(req, kind) {
    if (!req) return '—';
    if (kind === 'url') return req.url || '—';
    if (kind === 'finder') return (req.objectId || '—') + (req.objectNonceId ? ' / ' + req.objectNonceId : '');
    if (kind === 'auth') return req.path || '—';
    try { return JSON.stringify(req); } catch (e) { return '—'; }
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

  /* ---------- recent requests ---------- */

  function renderRecentRows(items) {
    if (!items || items.length === 0) {
      return '<div class="empty">' +
        '<div class="empty__icon">' +
          '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="24" height="24"><path d="M12 8v4l3 3"/><circle cx="12" cy="12" r="9"/></svg>' +
        '</div>' +
        '<div class="empty__title">暂无请求记录</div>' +
        '<div class="empty__desc">试试在解析测试页发一次请求</div>' +
      '</div>';
    }
    var rows = items.map(function (r) {
      var summary = summarizeRequest(r.request, r.kind);
      return '<div class="recent-row">' +
        '<div class="recent-row__ts">' + escapeHtml(fmtTs(r.ts)) + '</div>' +
        '<div class="recent-row__kind">' + kindBadge(r.kind) + '</div>' +
        '<div class="recent-row__status">' + statusBadge(r.status) + '</div>' +
        '<div class="recent-row__token" title="' + escapeHtml(r.token_label || '') + '">' + escapeHtml(r.token_label || '(无)') + '</div>' +
        '<div class="recent-row__ip" title="' + escapeHtml(r.client_ip || '') + '">' + escapeHtml(r.client_ip || '—') + '</div>' +
        '<div class="recent-row__summary" title="' + escapeHtml(summary) + '">' + escapeHtml(summary) + '</div>' +
        '<div class="recent-row__latency">' + escapeHtml(String(r.latency_ms)) + 'ms</div>' +
      '</div>';
    }).join('');
    return '<div class="recent-list">' + rows + '</div>';
  }

  function renderRecent(slot) {
    var body = slot.querySelector('[data-role="recent-body"]');
    if (!body) return;
    body.innerHTML = renderRecentRows(recent);
  }

  async function loadRecent(slot) {
    var body = slot.querySelector('[data-role="recent-body"]');
    if (!body) return;
    try {
      var res = await global.WXApi.authJson('/api/history?range=all&page=1&size=' + RECENT_SIZE);
      if (res.data && res.data.code === 0 && res.data.data) {
        var fetched = res.data.data.items || [];
        var fetchedIds = new Set(fetched.map(function (it) { return it.id; }));
        var fresher = recent.filter(function (it) { return !fetchedIds.has(it.id); });
        recent = fresher.concat(fetched).slice(0, RECENT_SIZE);
        renderRecent(slot);
      } else {
        body.innerHTML = '<div class="result-msg">加载失败: ' + escapeHtml((res.data && res.data.msg) || '未知错误') + '</div>';
      }
    } catch (e) {
      if (e && e.isAuth) return;
      body.innerHTML = '<div class="result-msg">加载失败: ' + escapeHtml(e.message || '网络错误') + '</div>';
    }
  }

  /* ---------- stat grid + custom range card ---------- */

  function statNumber(s, key) {
    if (!s || s[key] == null) return '—';
    return Number(s[key]).toLocaleString();
  }

  function renderStatGrid() {
    var grid = document.querySelector('[data-role="stat-grid"]');
    if (!grid) return;
    var s = state.stats;
    var retention = (s && s.retention_days) ? s.retention_days : 0;
    var customNum = state.customStats ? Number(state.customStats.success_total).toLocaleString() : null;

    var cards = [
      { label: '配置的 Token 数', value: state.tokens.length, note: '从 /api/config 实时读取' },
      { label: '今日成功', value: statNumber(s, 'success_today'), note: 'status=0 调用数' },
      { label: '本周成功', value: statNumber(s, 'success_week'),  note: '本周一 00:00 起' },
      { label: '本月成功', value: statNumber(s, 'success_month'), note: '本月 1 日 00:00 起' },
      { label: '总计成功', value: statNumber(s, 'success_total'), note: '近 ' + retention + ' 天' },
      { label: '平均耗时', value: (s && s.avg_latency_today_ms) ? (s.avg_latency_today_ms + 'ms') : '—', note: '今日成功调用' }
    ];
    if (state.customStats) {
      cards.push({
        label: '自定义成功',
        value: customNum,
        note: state.customRange.start + ' ~ ' + state.customRange.end,
        custom: true
      });
    }
    grid.innerHTML = cards.map(function (c) {
      return '<div class="stat' + (c.custom ? ' stat--custom' : '') + '">' +
        '<div class="stat__label">' + escapeHtml(c.label) + '</div>' +
        '<div class="stat__value">' + c.value + '</div>' +
        '<div class="stat__note">' + escapeHtml(c.note) + '</div>' +
      '</div>';
    }).join('');
  }

  function renderCustomRangeButton() {
    var wrap = document.querySelector('[data-role="custom-range-wrap"]');
    if (!wrap) return;
    var btn = wrap.querySelector('.custom-range-btn');
    if (!btn) return;
    if (state.customRange) {
      btn.classList.add('is-active');
      btn.textContent = '自定义: ' + state.customRange.start + ' ~ ' + state.customRange.end + ' ✕';
    } else {
      btn.classList.remove('is-active');
      btn.textContent = '自定义区间';
    }
  }

  function openCustomRangePopover() {
    var pop = document.querySelector('[data-role="custom-range-popover"]');
    if (!pop) return;
    var sIn = pop.querySelector('[data-role="custom-start"]');
    var eIn = pop.querySelector('[data-role="custom-end"]');
    var retention = (state.stats && state.stats.retention_days) ? state.stats.retention_days : 60;
    var minDay = new Date();
    minDay.setDate(minDay.getDate() - (retention - 1));
    var minStr = minDay.getFullYear() + '-' + pad2(minDay.getMonth() + 1) + '-' + pad2(minDay.getDate());
    if (sIn) { sIn.min = minStr; sIn.max = todayDateStr(); sIn.value = state.customRange ? state.customRange.start : minStr; }
    if (eIn) { eIn.min = minStr; eIn.max = todayDateStr(); eIn.value = state.customRange ? state.customRange.end : todayDateStr(); }
    pop.hidden = false;
  }

  function closeCustomRangePopover() {
    var pop = document.querySelector('[data-role="custom-range-popover"]');
    if (pop) pop.hidden = true;
  }

  async function applyCustomRange() {
    var pop = document.querySelector('[data-role="custom-range-popover"]');
    if (!pop) return;
    var sIn = pop.querySelector('[data-role="custom-start"]');
    var eIn = pop.querySelector('[data-role="custom-end"]');
    var start = sIn && sIn.value;
    var end = eIn && eIn.value;
    if (!start || !end) {
      if (global.WXToast) global.WXToast('请选择起止日期', 'error');
      return;
    }
    if (end < start) {
      if (global.WXToast) global.WXToast('结束日期不能早于开始日期', 'error');
      return;
    }
    try {
      var res = await global.WXApi.authJson('/api/stats?start=' + encodeURIComponent(start) + '&end=' + encodeURIComponent(end));
      if (res.data && res.data.code === 0 && res.data.data) {
        state.customRange = { start: start, end: end };
        state.customStats = res.data.data;
        renderStatGrid();
        renderCustomRangeButton();
        renderTokenBreakdownCard();
        closeCustomRangePopover();
      } else {
        if (global.WXToast) global.WXToast((res.data && res.data.msg) || '查询失败', 'error');
      }
    } catch (e) {
      if (e && e.isAuth) return;
      if (global.WXToast) global.WXToast(e.message || '网络错误', 'error');
    }
  }

  function clearCustomRange() {
    state.customRange = null;
    state.customStats = null;
    renderStatGrid();
    renderCustomRangeButton();
    renderTokenBreakdownCard();
    closeCustomRangePopover();
  }

  /* ---------- per-token breakdown table ---------- */

  function renderTokenBreakdownCard() {
    var body = document.querySelector('[data-role="breakdown-body"]');
    if (!body) return;
    var s = state.stats;
    if (!s || !Array.isArray(s.by_token) || s.by_token.length === 0) {
      body.innerHTML = '<div class="empty"><div class="empty__title">尚未配置 token</div><div class="empty__desc">在配置页添加 token 后此处会显示每个 token 的调用明细</div></div>';
      return;
    }
    var byLabel = {};
    (s.by_token || []).forEach(function (t) { byLabel[t.label] = t; });
    var customMap = {};
    if (state.customStats && Array.isArray(state.customStats.by_token)) {
      state.customStats.by_token.forEach(function (t) { customMap[t.label] = t.count; });
    }
    var hasCustom = !!state.customStats;

    var head = '<tr>' +
      '<th>Token</th>' +
      '<th class="num">今日</th>' +
      '<th class="num">本周</th>' +
      '<th class="num">本月</th>' +
      '<th class="num">总计</th>' +
      (hasCustom ? '<th class="num col-custom">自定义</th>' : '') +
      '</tr>';

    var bodyRows = (s.by_token || []).map(function (t) {
      return '<tr>' +
        '<td title="' + escapeHtml(t.label) + '">' + escapeHtml(t.label) + '</td>' +
        '<td class="num">' + Number(t.today || 0).toLocaleString() + '</td>' +
        '<td class="num">' + Number(t.week || 0).toLocaleString() + '</td>' +
        '<td class="num">' + Number(t.month || 0).toLocaleString() + '</td>' +
        '<td class="num">' + Number(t.total || 0).toLocaleString() + '</td>' +
        (hasCustom ? '<td class="num col-custom">' + Number(customMap[t.label] || 0).toLocaleString() + '</td>' : '') +
        '</tr>';
    }).join('');

    body.innerHTML = '<table class="token-breakdown-table">' + head + bodyRows + '</table>';
  }

  /* ---------- charts: trend + heatmap ---------- */

  function renderChartControls() {
    var sel = document.querySelector('[data-role="chart-token"]');
    if (!sel) return;
    var opts = ['<option value="">全部</option>'];
    state.tokens.forEach(function (t) {
      var sel2 = (state.chartToken === t.label) ? ' selected' : '';
      opts.push('<option value="' + escapeHtml(t.label) + '"' + sel2 + '>' + escapeHtml(t.label) + '</option>');
    });
    sel.innerHTML = opts.join('');
  }

  function renderChartsCard() {
    var trendHost = document.querySelector('[data-role="trend-chart"]');
    var heatHost = document.querySelector('[data-role="heatmap"]');
    if (!trendHost || !heatHost) return;

    var series = (state.daily && Array.isArray(state.daily.series)) ? state.daily.series : [];
    var max = 0;
    series.forEach(function (d) { if (d.count > max) max = d.count; });
    if (max === 0) max = 1;

    trendHost.innerHTML = renderTrendSvg(series, max);
    heatHost.innerHTML = renderHeatmapGrid(series, max);

    // bind hover for tooltip
    bindChartTooltips();
  }

  function renderTrendSvg(series, maxValue) {
    if (!series || series.length === 0) {
      return '<svg class="trend-chart" viewBox="0 0 600 200" preserveAspectRatio="none">' +
        '<text x="300" y="100" text-anchor="middle" class="trend-chart__label">暂无数据</text>' +
      '</svg>';
    }
    var w = 600, h = 200, padL = 30, padR = 10, padT = 10, padB = 20;
    var innerW = w - padL - padR;
    var innerH = h - padT - padB;
    var n = series.length;
    var xStep = n > 1 ? innerW / (n - 1) : 0;

    var pts = series.map(function (d, i) {
      var x = padL + (n > 1 ? i * xStep : innerW / 2);
      var y = padT + innerH - (d.count / maxValue) * innerH;
      return { x: x, y: y, d: d };
    });

    var linePts = pts.map(function (p) { return p.x.toFixed(1) + ',' + p.y.toFixed(1); }).join(' ');
    // Area path: line then down to baseline
    var areaD = 'M ' + pts[0].x.toFixed(1) + ',' + (padT + innerH).toFixed(1) +
      ' L ' + linePts.replace(/ /g, ' L ') +
      ' L ' + pts[pts.length - 1].x.toFixed(1) + ',' + (padT + innerH).toFixed(1) + ' Z';

    // X ticks: 5 evenly spaced indices (first, last, 3 in between)
    var tickIdx = [];
    if (n <= 5) {
      for (var i = 0; i < n; i++) tickIdx.push(i);
    } else {
      for (var k = 0; k < 5; k++) tickIdx.push(Math.round((k * (n - 1)) / 4));
    }
    var xLabels = tickIdx.map(function (i) {
      var p = pts[i];
      var d = series[i].date.slice(5); // MM-DD
      return '<text x="' + p.x.toFixed(1) + '" y="' + (h - 4) + '" text-anchor="middle" class="trend-chart__label">' + escapeHtml(d) + '</text>';
    }).join('');

    // Y labels: 0, max/2, max
    var yLabels = [
      { v: 0, y: padT + innerH },
      { v: Math.round(maxValue / 2), y: padT + innerH / 2 },
      { v: maxValue, y: padT }
    ].map(function (yl) {
      return '<text x="' + (padL - 4) + '" y="' + (yl.y + 3) + '" text-anchor="end" class="trend-chart__label">' + yl.v + '</text>';
    }).join('');

    var dots = pts.map(function (p, i) {
      return '<circle class="trend-chart__dot" data-idx="' + i + '" cx="' + p.x.toFixed(1) + '" cy="' + p.y.toFixed(1) + '" r="3"></circle>';
    }).join('');

    return '<svg class="trend-chart" viewBox="0 0 ' + w + ' ' + h + '" preserveAspectRatio="none">' +
      '<line class="trend-chart__axis" x1="' + padL + '" y1="' + (padT + innerH) + '" x2="' + (w - padR) + '" y2="' + (padT + innerH) + '"></line>' +
      '<path class="trend-chart__area" d="' + areaD + '"></path>' +
      '<polyline class="trend-chart__line" points="' + linePts + '"></polyline>' +
      yLabels + xLabels + dots +
    '</svg>';
  }

  function renderHeatmapGrid(series, maxValue) {
    if (!series || series.length === 0) {
      return '<div class="empty" style="padding:var(--s-3)"><div class="empty__title" style="font-size:var(--t-sm)">暂无数据</div></div>';
    }
    return series.map(function (d) {
      var level = 0;
      if (d.count > 0 && maxValue > 0) {
        var pct = d.count / maxValue;
        if (pct > 0.75) level = 4;
        else if (pct > 0.50) level = 3;
        else if (pct > 0.25) level = 2;
        else level = 1;
      }
      return '<div class="heatmap__cell" data-level="' + level + '" data-date="' + escapeHtml(d.date) + '" data-count="' + d.count + '"></div>';
    }).join('');
  }

  function ensureTooltip() {
    var t = document.getElementById('chartTooltip');
    if (!t) {
      t = document.createElement('div');
      t.id = 'chartTooltip';
      t.className = 'chart-tooltip';
      t.hidden = true;
      document.body.appendChild(t);
    }
    return t;
  }

  function bindChartTooltips() {
    var tooltip = ensureTooltip();
    function show(html, x, y) {
      tooltip.innerHTML = html;
      tooltip.hidden = false;
      // position so the tooltip is above-right of the cursor, clamped to viewport
      var tw = tooltip.offsetWidth;
      var th = tooltip.offsetHeight;
      var nx = x + 12;
      var ny = y - th - 12;
      if (nx + tw > global.innerWidth - 8) nx = global.innerWidth - 8 - tw;
      if (ny < 8) ny = y + 12;
      tooltip.style.left = nx + 'px';
      tooltip.style.top = ny + 'px';
    }
    function hide() { tooltip.hidden = true; }
    document.querySelectorAll('.trend-chart__dot').forEach(function (dot) {
      dot.addEventListener('mouseenter', function (e) {
        var idx = Number(dot.getAttribute('data-idx'));
        if (!state.daily || !state.daily.series[idx]) return;
        var d = state.daily.series[idx];
        show(escapeHtml(d.date) + ' · ' + d.count + ' 次', e.clientX, e.clientY);
        dot.classList.add('is-active');
      });
      dot.addEventListener('mousemove', function (e) {
        var idx = Number(dot.getAttribute('data-idx'));
        if (!state.daily || !state.daily.series[idx]) return;
        var d = state.daily.series[idx];
        show(escapeHtml(d.date) + ' · ' + d.count + ' 次', e.clientX, e.clientY);
      });
      dot.addEventListener('mouseleave', function () {
        hide();
        dot.classList.remove('is-active');
      });
    });
    document.querySelectorAll('.heatmap__cell').forEach(function (cell) {
      cell.addEventListener('mouseenter', function (e) {
        var date = cell.getAttribute('data-date') || '';
        var count = cell.getAttribute('data-count') || '0';
        show(escapeHtml(date) + ' · ' + count + ' 次', e.clientX, e.clientY);
      });
      cell.addEventListener('mousemove', function (e) {
        var date = cell.getAttribute('data-date') || '';
        var count = cell.getAttribute('data-count') || '0';
        show(escapeHtml(date) + ' · ' + count + ' 次', e.clientX, e.clientY);
      });
      cell.addEventListener('mouseleave', hide);
    });
  }

  async function loadChartData() {
    try {
      var q = '/api/stats/daily?token=' + encodeURIComponent(state.chartToken);
      var res = await global.WXApi.authJson(q);
      if (res.data && res.data.code === 0 && res.data.data) {
        state.daily = res.data.data;
        renderChartsCard();
      }
    } catch (e) {
      if (e && e.isAuth) return;
      // non-fatal; leave the previous chart in place
    }
  }

  function scheduleChartRefresh() {
    if (state.chartDebounceTimer) clearTimeout(state.chartDebounceTimer);
    state.chartDebounceTimer = setTimeout(function () {
      state.chartDebounceTimer = null;
      loadChartData();
    }, CHART_DEBOUNCE_MS);
  }

  /* ---------- token list + config reload ---------- */

  async function loadTokensAndCount() {
    try {
      var res = await global.WXApi.authJson('/api/config');
      if (res.data && res.data.code === 0 && res.data.data) {
        state.tokens = res.data.data.tokens || [];
        renderStatGrid();
        renderChartControls();
        renderTokenBreakdownCard();
      }
    } catch (e) { /* leave as-is */ }
  }

  /* ---------- subscriptions ---------- */

  function bindEvents() {
    if (!global.WXEvents) return;
    state.unsubscribers.push(global.WXEvents.subscribe('system.snapshot', function (frame) {
      if (frame && frame.stats) {
        state.stats = frame.stats;
        renderStatGrid();
        renderTokenBreakdownCard();
      }
    }));
    state.unsubscribers.push(global.WXEvents.subscribe('config.changed', function () {
      loadTokensAndCount();
    }));
    state.unsubscribers.push(global.WXEvents.subscribe('log.new', function (frame) {
      if (!frame || !frame.log) return;
      recent.unshift(frame.log);
      if (recent.length > RECENT_SIZE) recent.length = RECENT_SIZE;
      renderRecent(document.getElementById('pageContent'));
      scheduleChartRefresh();
    }));
  }

  function cleanup() {
    state.unsubscribers.forEach(function (u) { try { u(); } catch (e) { /* ignore */ } });
    state.unsubscribers = [];
    if (state.chartDebounceTimer) { clearTimeout(state.chartDebounceTimer); state.chartDebounceTimer = null; }
  }

  /* ---------- boot ---------- */

  function render(slot) {
    cleanup();
    slot.innerHTML =
      '<div class="section-title">概览</div>' +

      '<div class="stat-grid" data-role="stat-grid">' +
        '<div class="stat"><div class="stat__label">加载中…</div></div>' +
      '</div>' +

      '<div class="section-title">' +
        '<span>Token 调用明细</span>' +
        '<span class="section-title__actions">' +
          '<span class="custom-range-wrap" data-role="custom-range-wrap">' +
            '<button type="button" class="custom-range-btn" data-role="custom-range-btn">自定义区间</button>' +
            '<div class="custom-range-popover" data-role="custom-range-popover" hidden>' +
              '<div class="custom-range-popover__row"><label>起</label>' +
                '<input type="date" class="input" data-role="custom-start"></div>' +
              '<div class="custom-range-popover__row"><label>止</label>' +
                '<input type="date" class="input" data-role="custom-end"></div>' +
              '<div class="custom-range-popover__actions">' +
                '<button type="button" class="btn btn--secondary btn--sm" data-role="custom-clear">清空</button>' +
                '<button type="button" class="btn btn--primary btn--sm" data-role="custom-apply">应用</button>' +
              '</div>' +
            '</div>' +
          '</span>' +
        '</span>' +
      '</div>' +
      '<div class="card" data-role="breakdown-card">' +
        '<div data-role="breakdown-body">' +
          '<div class="result-msg">加载中…</div>' +
        '</div>' +
      '</div>' +

      '<div class="section-title">调用趋势与热力</div>' +
      '<div class="card charts-card" data-role="charts-card">' +
        '<div class="charts-card__head">' +
          '<div class="charts-card__title">近 ' + ((state.stats && state.stats.retention_days) || 60) + ' 天</div>' +
          '<div class="charts-card__controls">' +
            '<select data-role="chart-token" aria-label="选择 token"></select>' +
            '<button type="button" class="btn btn--secondary btn--sm" data-role="chart-refresh">刷新</button>' +
          '</div>' +
        '</div>' +
        '<div data-role="trend-chart"></div>' +
        '<div class="heatmap" data-role="heatmap"></div>' +
        '<div class="heatmap__legend">' +
          '<span>少</span>' +
          '<div class="heatmap__legend-cells">' +
            '<div class="heatmap__legend-cell" style="background:var(--surface-2)"></div>' +
            '<div class="heatmap__legend-cell" style="background:color-mix(in srgb, var(--primary) 25%, var(--surface-2))"></div>' +
            '<div class="heatmap__legend-cell" style="background:color-mix(in srgb, var(--primary) 50%, var(--surface-2))"></div>' +
            '<div class="heatmap__legend-cell" style="background:color-mix(in srgb, var(--primary) 75%, var(--surface-2))"></div>' +
            '<div class="heatmap__legend-cell" style="background:var(--primary)"></div>' +
          '</div>' +
          '<span>多</span>' +
        '</div>' +
      '</div>' +

      '<div class="section-title">' +
        '<span>最近请求</span>' +
        '<span class="section-title__actions">' +
          '<button class="btn btn--secondary" data-role="recent-refresh">刷新</button> ' +
          '<a class="link-btn" data-route="/history">查看全部 →</a>' +
        '</span>' +
      '</div>' +
      '<div class="card" data-role="recent-card">' +
        '<div data-role="recent-body">' +
          '<div class="result-msg">加载中…</div>' +
        '</div>' +
      '</div>';

    // custom range popover handlers
    var rangeBtn = slot.querySelector('[data-role="custom-range-btn"]');
    if (rangeBtn) rangeBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      if (state.customRange) {
        clearCustomRange();
        return;
      }
      var pop = slot.querySelector('[data-role="custom-range-popover"]');
      if (!pop) return;
      if (pop.hidden) openCustomRangePopover(); else closeCustomRangePopover();
    });
    var applyBtn = slot.querySelector('[data-role="custom-apply"]');
    if (applyBtn) applyBtn.addEventListener('click', applyCustomRange);
    var clearBtn = slot.querySelector('[data-role="custom-clear"]');
    if (clearBtn) clearBtn.addEventListener('click', clearCustomRange);
    document.addEventListener('click', function dismiss(e) {
      var pop = slot.querySelector('[data-role="custom-range-popover"]');
      var wrap = slot.querySelector('[data-role="custom-range-wrap"]');
      if (!pop || pop.hidden) return;
      if (wrap && wrap.contains(e.target)) return;
      pop.hidden = true;
    });

    // chart controls
    var chartSel = slot.querySelector('[data-role="chart-token"]');
    if (chartSel) chartSel.addEventListener('change', function (e) {
      state.chartToken = e.currentTarget.value || '';
      loadChartData();
    });
    var chartRefresh = slot.querySelector('[data-role="chart-refresh"]');
    if (chartRefresh) chartRefresh.addEventListener('click', loadChartData);

    // recent handlers
    var refresh = slot.querySelector('[data-role="recent-refresh"]');
    if (refresh) refresh.addEventListener('click', function () { loadRecent(slot); });
    var jump = slot.querySelector('a[data-route="/history"]');
    if (jump) jump.addEventListener('click', function (e) {
      e.preventDefault();
      if (global.WXRouter && global.WXRouter.navigate) global.WXRouter.navigate('/history');
    });

    // boot
    loadTokensAndCount();
    loadRecent(slot);
    loadChartData();
    bindEvents();
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.dashboard = { render: render };
})(window);
```

- [ ] **Step 2: Spot-check the file for syntax issues**

Run: `node -e "require('./web/static/js/pages/dashboard.js')"` (this will fail because of the `window` reference at the bottom — that's expected; we just want a parse check). Use:

```bash
node --check web/static/js/pages/dashboard.js
```

Expected: exits 0, no syntax error.

- [ ] **Step 3: Commit**

```bash
git add web/static/js/pages/dashboard.js
git commit -m "feat(dashboard): add 6 stat cards, breakdown table, custom range, trend/heatmap"
```

---

## Task 16: Full smoke test (manual, in browser)

**Files:** none (verification only)

- [ ] **Step 1: Build and start the server**

Run:

```bash
go build -o dist/wx_web_api .
./dist/wx_web_api -pwd 1 -port 13335
```

Expected: server listens on :13335, log shows `wx_web_api starting on :13335`.

- [ ] **Step 2: Verify the legacy retention clamp**

Open [dist/wx_web_api.json](dist/wx_web_api.json); the file currently has `"history_retention_days": 33`. That's in range, so it should not be changed. If you want to exercise the clamp, edit it to `0`, restart, and confirm it's rewritten to `60`. Restore to a sensible value (1..60) before continuing.

- [ ] **Step 3: Open the admin UI and walk through the dashboard**

In a browser, log in at `http://127.0.0.1:13335`. On the dashboard, confirm:

1. The sidebar shows 5 entries (概览 / 配置 / 解析测试 / 解析历史 / 系统信息); `/users` is gone.
2. The top of the dashboard shows 6 stat cards with real numbers (or "—" when no data). If a custom range is active, a 7th "自定义成功" card appears with a primary-color left border.
3. The "Token 调用明细" card shows a table; if you add a token via the 配置 page, a row appears here. (If there is only one configured token, you should still see one row.)
4. Click "自定义区间" → the popover opens with date inputs bounded by `retention` days. Pick a range and click "应用" → a 7th stat card and a 5th table column appear. Click the active button to clear.
5. The "调用趋势与热力" card shows an SVG line chart and a heatmap; changing the token `<select>` re-fetches; the "刷新" button re-fetches immediately.
6. Send a parse request via the 解析测试 page; the "最近请求" card prepends a new row, and after ~3s the chart re-renders (no full page reload).
7. On the 配置 page, the retention input now has `min=1 max=60` and the hint reads "1~60 天".

- [ ] **Step 4: Run all Go tests one more time**

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit any leftover edits (should be none)**

```bash
git status
```

Expected: clean working tree (or only `dist/wx_web_api.db-shm` / `dist/wx_web_api.db-wal` if you ran the server — these are WAL-mode SQLite files; do not commit them).

---

## Self-Review

**1. Spec coverage**

| Spec section | Covered by |
|---|---|
| 范围.1 remove `/users` | Tasks 10, 11 |
| 范围.2.a 6 cards + optional 7th | Task 15 (`renderStatGrid`) |
| 范围.2.b per-token breakdown table + optional 5th column | Task 15 (`renderTokenBreakdownCard`) |
| 范围.2.c charts card with token selector + refresh | Task 15 (`renderChartsCard` + `renderChartControls`) |
| 范围.2.d 保留 "最近请求" | Task 15 (`renderRecent` + `loadRecent`) |
| 范围.3.a extend `system.snapshot.stats` | Task 7 |
| 范围.3.b 2 new REST endpoints | Tasks 8, 9 |
| 范围.4 retention 1..60 | Tasks 5, 6, 13 |
| 数据流: WS for cards, REST for custom + charts | Task 15 (`bindEvents` + REST calls) |
| 数据语义: status=0, local time | Tasks 1, 2, 3 (helpers + SQL) |
| 后端: 6 storage methods | Tasks 2, 3, 4 |
| 后端: extended ReqStats + collectSnapshot | Task 7 |
| 后端: stats.go GetStats + GetStatsDaily | Task 8 |
| 路由: 2 new routes | Task 9 |
| 前端: cleanup, chart controls, debounce | Task 15 (`cleanup`, `scheduleChartRefresh`, `bindEvents`) |
| 前端: CSS grid auto-fit | Task 12 |
| 前端: token breakdown table, popover, charts CSS | Task 14 |
| 配置页: retention input bounds | Task 13 |
| 测试: 7 cases | Tasks 2, 3, 4 (counts, by-token, latency, daily) |
| 兼容性: 老 system.snapshot 老字段不删 | Task 7 (kept Total/Today/Errors) |
| 兼容性: 老 /users 路由 fallback | Already handled by router.js (line 59) |

**2. Placeholder scan:** No "TBD" / "TODO" / "implement later" markers. Every step shows full code or full commands.

**3. Type / name consistency:**
- `TokenStat` (broadcaster.go) ↔ `storage.DailyCount` (storage.go) ↔ `byTokenOut` (stats.go) — three distinct shapes, used in three different layers. The internal `byTokenOut` in `stats.go` is defined inside `GetStats` to disambiguate from `TokenStat.Total` (which carries 4 numbers) — the REST response only needs a single `count`. Names are intentionally distinct.
- `chartToken` (dashboard.js) is always a string: `''` for "all", else the token label. Matches the server's `token` query param treatment.
- `state.customRange` is `{ start, end }`; `state.customStats` is the full REST data. Names match the renderers.
- `renderTrendSvg` / `renderHeatmapGrid` produce HTML strings inserted via `innerHTML`; CSS class names match Task 14.

**4. Spec requirement that needed a small spec-side clarification (caught during review):**
- The "总计" stat card's note text uses `s.retention_days` (Task 7) — added by extending `ReqStats`. The renderer in Task 15 reads `state.stats.retention_days`. Consistent.

Plan covers all 17 spec items; no gaps found.
