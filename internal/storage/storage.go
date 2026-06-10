package storage

import (
	"database/sql"
	"encoding/json"
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
	// Migrate older DBs that predate client_ip. CREATE TABLE IF NOT EXISTS won't
	// add the column to an existing table, so we probe via PRAGMA table_info and
	// ALTER if absent. Safe to run repeatedly: the probe returns the column once
	// migration has run.
	if err := ensureClientIPColumn(db); err != nil {
		return fmt.Errorf("migrate client_ip: %w", err)
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

// ensureClientIPColumn adds the client_ip column to an existing request_log
// table if it doesn't already exist. No-op on fresh databases (CREATE TABLE
// already included it).
func ensureClientIPColumn(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(request_log)")
	if err != nil {
		return fmt.Errorf("pragma table_info: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		if name == "client_ip" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows table_info: %w", err)
	}
	if _, err := db.Exec(addColumnClientIP); err != nil {
		return fmt.Errorf("alter add client_ip: %w", err)
	}
	return nil
}

func (s *Storage) LogRequest(r *RequestLog) error {
	if r.Source == "" {
		r.Source = "external"
	}
	res, err := s.db.Exec(
		`INSERT INTO request_log (ts, token_label, kind, source, client_ip, request, status, latency_ms, msg, result_data)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Ts, r.TokenLabel, r.Kind, r.Source, r.ClientIP, string(r.Request), r.Status, r.LatencyMs, r.Msg, nullableJSON(r.Result),
	)
	if err != nil {
		return fmt.Errorf("log request: %w", err)
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
	listSQL := "SELECT id, ts, token_label, kind, source, client_ip, request, status, latency_ms, msg, result_data FROM request_log" +
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
			r         RequestLog
			reqStr    string
			resultStr sql.NullString
		)
		if err := rows.Scan(&r.ID, &r.Ts, &r.TokenLabel, &r.Kind, &r.Source, &r.ClientIP,
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
		return 0, fmt.Errorf("delete by ids: %w", err)
	}
	return res.RowsAffected()
}

func (s *Storage) DeleteAll() (int64, error) {
	res, err := s.db.Exec("DELETE FROM request_log")
	if err != nil {
		return 0, fmt.Errorf("delete all: %w", err)
	}
	return res.RowsAffected()
}

func (s *Storage) PurgeOlderThan(cutoffMs int64) (int64, error) {
	res, err := s.db.Exec("DELETE FROM request_log WHERE ts < ?", cutoffMs)
	if err != nil {
		return 0, fmt.Errorf("purge older than: %w", err)
	}
	return res.RowsAffected()
}

func (s *Storage) Count() (int64, error) {
	var n int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM request_log").Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}
	return n, nil
}

func (s *Storage) CountSince(sinceMs int64) (int64, error) {
	var n int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM request_log WHERE ts >= ?", sinceMs).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count since: %w", err)
	}
	return n, nil
}

func (s *Storage) CountErrors() (int64, error) {
	var n int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM request_log WHERE status != 0").Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count errors: %w", err)
	}
	return n, nil
}
