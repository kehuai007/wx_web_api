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
