package storage

const createRequestLogTable = `
CREATE TABLE IF NOT EXISTS request_log (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  ts            INTEGER NOT NULL,
  token_label   TEXT    NOT NULL,
  token_value   TEXT    NOT NULL DEFAULT '',
  kind          TEXT    NOT NULL,
  source        TEXT    NOT NULL,
  client_ip     TEXT    NOT NULL DEFAULT '',
  request       TEXT    NOT NULL,
  status        INTEGER NOT NULL,
  latency_ms    INTEGER NOT NULL,
  msg           TEXT    NOT NULL DEFAULT '',
  result_data   TEXT
);
`

// addColumnClientIP is issued for pre-existing databases that were created
// before client_ip was part of the schema. ALTER TABLE ADD COLUMN with a
// NOT NULL DEFAULT '' is safe in SQLite and backfills existing rows to ''.
const addColumnClientIP = `ALTER TABLE request_log ADD COLUMN client_ip TEXT NOT NULL DEFAULT ''`

// addColumnTokenValue is issued for pre-existing databases that predate
// the masked token_value column. Backfilled to ''.
const addColumnTokenValue = `ALTER TABLE request_log ADD COLUMN token_value TEXT NOT NULL DEFAULT ''`

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
