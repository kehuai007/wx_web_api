# Token Expiration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-token expiration dates to `wx_web_api` config, enforce them in `TokenAuth()` middleware in real time, and expose management UI in the existing settings page with quick-preset buttons and status badges.

**Architecture:** Migrate config `tokens: [string]` to `tokens: [{value, expires_at}]` (object array) at startup with one-time on-disk migration. `Handler` drops the cached `validTokens` map; `TokenAuth()` reads `config.Get()` per request, applies `isExpired` with a `sync.Map` date cache. Login flow gets a separate `sessionTokens` map (admin sessions are not subject to expiry). Settings UI reworks each row to include a date input, 5 preset buttons, and a status badge.

**Tech Stack:** Go 1.25 + Gin (server), vanilla HTML/CSS/JS + History API (client). No new dependencies. No test framework introduced — verification via manual smoke tests + curl (per Phase 1 precedent and spec §"单元 / 集成测试").

**Branch policy:** Implementer commits directly to `main` (per user's Phase 1 standing instruction). All 29 Phase 1 commits are on `main`; this plan continues on the same branch.

---

## File Structure

Files created or modified by this plan:

| File | Responsibility |
|---|---|
| `internal/config/config.go` | `Token` struct, config `Tokens []Token`, startup migration of legacy string-array tokens |
| `internal/handler/handler.go` | `Handler` struct refactor (drop `validTokens` cache, add `sessionTokens` + `dateCache`); `TokenAuth` reads config live + applies `isExpired`; `Login` writes to `sessionTokens`; `SessionAuth` reads only `sessionTokens` |
| `internal/handler/settings.go` | `GetConfig`/`UpdateConfig` adapt to `{value, expires_at}` shape; validate value non-empty + date format |
| `web/static/js/pages/settings.js` | New row layout (date input + 5 preset buttons + status badge); dirty/copy/toggle/remove/save/cancel all work on object array |
| `web/static/css/pages.css` | Styles for badge, preset button group, date input, multi-row token item layout |

Files explicitly **not** modified (per spec §"不改动的文件"):
- `main.go`
- `web/index.html`
- `web/static/js/router.js`, `auth.js`, `api.js`, `store.js`, `app.js`
- `web/static/js/pages/dashboard.js`, `test.js`, `history.js`, `users.js`, `system.js`
- `internal/handler/handler.go::ParseWxURL`, `ParseFinderFeedByObjectID`, `GetChallenge`
- `dist/wx_web_api.json` (only auto-mutated by `config.Init` on startup)

---

## Task 1: Config schema + startup migration

**Files:**
- Modify: `internal/config/config.go` (full file rewrite)

- [ ] **Step 1: Rewrite `internal/config/config.go` with `Token` struct + migration logic**

Replace the entire file with the following content:

```go
package config

import (
    "encoding/json"
    "log"
    "os"
    "path/filepath"
    "sync"
)

type Token struct {
    Value     string `json:"value"`
    ExpiresAt string `json:"expires_at"` // yyyy-MM-dd; empty = permanent
}

type Config struct {
    ApiBaseUrl string  `json:"api_base_url"`
    Tokens     []Token `json:"tokens"`
    Port       int     `json:"port"`
}

type Manager struct {
    path   string
    config *Config
    mu     sync.RWMutex
}

var defaultManager *Manager

var ExeDir string

func Init(exePath string, binName string) error {
    ExeDir = filepath.Dir(exePath)
    cfgPath := filepath.Join(ExeDir, binName+".json")
    m := &Manager{path: cfgPath, config: &Config{
        ApiBaseUrl: "http://127.0.0.1:2022",
        Tokens:     []Token{},
        Port:       13335,
    }}

    if data, err := os.ReadFile(cfgPath); err == nil {
        if migrated, n := migrateTokens(data); migrated {
            if err := os.WriteFile(cfgPath, data, 0644); err != nil {
                log.Printf("config: failed to write migrated config: %v", err)
            } else {
                log.Printf("config: migrated %d tokens to new format", n)
            }
        }
        if err := json.Unmarshal(data, m.config); err != nil {
            log.Printf("config: failed to parse %s, using defaults: %v", cfgPath, err)
        }
    }

    defaultManager = m
    return nil
}

// migrateTokens inspects the raw JSON bytes; if the "tokens" field contains any
// legacy string entries, rewrites them in place to {value, expires_at:""} objects
// and returns (true, n) where n is the number of legacy entries converted.
// If the file is already in the new format, returns (false, 0) and the bytes
// are untouched.
func migrateTokens(data []byte) (bool, int) {
    var probe struct {
        Tokens json.RawMessage `json:"tokens"`
    }
    if err := json.Unmarshal(data, &probe); err != nil || len(probe.Tokens) == 0 {
        return false, 0
    }
    var raws []json.RawMessage
    if err := json.Unmarshal(probe.Tokens, &raws); err != nil {
        return false, 0
    }
    legacyCount := 0
    out := make([]json.RawMessage, 0, len(raws))
    for _, raw := range raws {
        var s string
        if err := json.Unmarshal(raw, &s); err == nil {
            tok := Token{Value: s, ExpiresAt: ""}
            b, _ := json.Marshal(tok)
            out = append(out, b)
            legacyCount++
        } else {
            out = append(out, raw) // already an object, keep as-is
        }
    }
    if legacyCount == 0 {
        return false, 0
    }
    // Detect mixed file (some legacy + some object) and warn.
    if legacyCount < len(raws) {
        log.Printf("config: WARNING detected mixed legacy+object token entries; normalizing all to object form")
    }
    newTokens, _ := json.Marshal(out)
    // Re-serialize the whole file with the new tokens array.
    var generic map[string]json.RawMessage
    if err := json.Unmarshal(data, &generic); err != nil {
        return false, 0
    }
    generic["tokens"] = newTokens
    rewritten, err := json.MarshalIndent(generic, "", "  ")
    if err != nil {
        return false, 0
    }
    copy(data, rewritten)
    // copy may have left trailing bytes; truncate
    if len(data) > len(rewritten) {
        for i := range data[len(rewritten):] {
            data[len(rewritten)+i] = 0
        }
    }
    return true, legacyCount
}
```

Note: The `copy(data, rewritten)` + trailing-zero approach mutates the caller's slice in place. Callers must always pass a fresh slice (which `os.ReadFile` provides).

- [ ] **Step 2: Build to verify it compiles**

Run: `cd c:/Users/Admin/src/wx_web_api && go build ./...`
Expected: success, no errors. (Other files still reference `cfg.Tokens` as `[]string`; that's OK — they will fail to compile until Tasks 2-3 are done. If build fails because handler.go or settings.go reference the old type, that's expected. Note the failure and proceed.)

- [ ] **Step 3: Manual smoke test of migration (old → new)**

Stop any running server first.

Edit `dist/wx_web_api.json` to legacy format:

```json
{
  "api_base_url": "http://127.0.0.1:2022",
  "tokens": ["smoke-test-legacy-1", "smoke-test-legacy-2"],
  "port": 13399
}
```

Build and start the server:

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
cd dist && ./wx_web_api.exe -port 13399
```

Watch the startup log. Expected: line containing `config: migrated 2 tokens to new format`.

In a separate terminal, read the config back:

```bash
cat dist/wx_web_api.json
```

Expected: file now contains the object-array shape, e.g.:

```json
{
  "api_base_url": "http://127.0.0.1:2022",
  "tokens": [
    { "value": "smoke-test-legacy-1", "expires_at": "" },
    { "value": "smoke-test-legacy-2", "expires_at": "" }
  ],
  "port": 13399
}
```

Restart the server (Ctrl-C, then re-run). Expected: **no** migration log line on second startup.

Stop the server (Ctrl-C).

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): migrate tokens to {value, expires_at} object array

Adds Token struct, drops []string, and migrates legacy string-array
configs in place at startup. Mixed files (legacy+object) are
normalized with a warning log.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 2: Refactor `Handler` — TokenAuth with live expiry, separate session tokens

**Files:**
- Modify: `internal/handler/handler.go` (struct + `New` + `TokenAuth` + `Login` + `SessionAuth` + add `isExpired`)

- [ ] **Step 1: Replace the entire `internal/handler/handler.go` with the new implementation**

```go
package handler

import (
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "net/http"
    "strings"
    "sync"
    "time"
    "wx_web_api/internal/config"
    "wx_web_api/internal/model"
    "wx_web_api/internal/service"

    "github.com/gin-gonic/gin"
)

type Handler struct {
    parser        *service.ParserService
    pwd           string
    sessionTokens map[string]bool
    dateCache     sync.Map // string (yyyy-MM-dd) -> time.Time
}

func New(pwd string) *Handler {
    return &Handler{
        parser:        service.NewParserService(),
        pwd:           pwd,
        sessionTokens: make(map[string]bool),
    }
}

// TokenAuth middleware for external API routes.
// Reads cfg.Tokens live so admin updates take effect without restart.
func (h *Handler) TokenAuth() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")
        if token != "" {
            token = strings.TrimPrefix(token, "Bearer ")
        }
        if token == "" {
            token = c.Query("token")
        }
        if token == "" {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
            return
        }
        cfg := config.Get()
        matched := false
        var matchedTok *config.Token
        for i := range cfg.Tokens {
            if cfg.Tokens[i].Value == token {
                matched = true
                matchedTok = &cfg.Tokens[i]
                break
            }
        }
        if !matched {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
            return
        }
        if h.isExpired(matchedTok.ExpiresAt) {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "token expired"})
            return
        }
        c.Next()
    }
}

// SessionAuth middleware for web UI routes (admin sessions).
// Session tokens are NOT subject to expiry — they live in h.sessionTokens
// only and are dropped on process restart.
func (h *Handler) SessionAuth() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")
        if token != "" {
            token = strings.TrimPrefix(token, "Bearer ")
        }
        if token == "" {
            token = c.Query("token")
        }
        if token == "" || !h.sessionTokens[token] {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "unauthorized"})
            return
        }
        c.Next()
    }
}

func (h *Handler) GetChallenge(c *gin.Context) {
    b := make([]byte, 16)
    rand.Read(b)
    challenge := hex.EncodeToString(b)
    c.JSON(http.StatusOK, gin.H{"code": 0, "challenge": challenge})
}

func (h *Handler) Login(c *gin.Context) {
    var req struct {
        Pwd       string `json:"pwd"`
        Challenge string `json:"challenge"`
        Response  string `json:"response"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid request"})
        return
    }
    if req.Pwd == "" || req.Challenge == "" || req.Response == "" {
        c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "missing parameters"})
        return
    }
    expected := simpleHash(h.pwd + req.Challenge)
    if expected != req.Response {
        c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid password"})
        return
    }
    token := generateToken()
    h.sessionTokens[token] = true
    c.JSON(http.StatusOK, gin.H{"code": 0, "token": token})
}

func (h *Handler) ParseWxURL(c *gin.Context) {
    var req model.WxParseRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "url is required"})
        return
    }
    data, err := h.parser.Parse(req.URL)
    if err != nil {
        c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: err.Error()})
        return
    }
    c.JSON(http.StatusOK, model.WxParseResponse{Code: 0, Msg: "success", Data: data})
}

func (h *Handler) ParseFinderFeedByObjectID(c *gin.Context) {
    var req model.FinderFeedRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "objectId and objectNonceId are required"})
        return
    }
    if req.ObjectID == "" || req.ObjectNonceID == "" {
        c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "objectId and objectNonceId are required"})
        return
    }
    data, err := h.parser.ParseFinderFeedByObjectID(req.ObjectID, req.ObjectNonceID)
    if err != nil {
        c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: err.Error()})
        return
    }
    c.JSON(http.StatusOK, model.WxParseResponse{Code: 0, Msg: "success", Data: data})
}

// isExpired: includes the expiry day itself. expires_at=2026-06-08 means
// the token is valid all day on 6/8, and rejects starting at 6/9 00:00:00.
// Malformed dates are treated as permanent (defensive — do not block requests
// just because someone hand-edited a config).
func (h *Handler) isExpired(expiresAt string) bool {
    if expiresAt == "" {
        return false
    }
    cached, ok := h.dateCache.Load(expiresAt)
    var parsed time.Time
    if ok {
        parsed = cached.(time.Time)
    } else {
        var err error
        parsed, err = time.Parse("2006-01-02", expiresAt)
        if err != nil {
            return false
        }
        h.dateCache.Store(expiresAt, parsed)
    }
    parsedDate := parsed.Add(24 * time.Hour) // start of day after expiry
    return !time.Now().Before(parsedDate)
}

func simpleHash(data string) string {
    h := uint64(0)
    primes := []uint64{31, 37, 41, 43, 47, 53, 59, 61, 67, 71, 73, 79}
    for i, c := range data {
        h += uint64(c) * primes[(i+1)%12]
    }
    return fmt.Sprintf("%016x", h)
}

func generateToken() string {
    b := make([]byte, 32)
    rand.Read(b)
    return hex.EncodeToString(b)
}
```

Key changes vs prior version:
- `validTokens` field is gone; `sessionTokens` replaces it
- `IsValidToken` method is removed (callers in `settings.go` need updating in next task)
- `TokenAuth` reads `config.Get()` and applies `isExpired`
- `SessionAuth` only checks `h.sessionTokens`, no config lookup
- `Login` writes to `h.sessionTokens`

- [ ] **Step 2: Build to verify**

Run: `cd c:/Users/Admin/src/wx_web_api && go build ./...`
Expected: failure. `internal/handler/settings.go` still uses the old `[]string` type via `ConfigData` / `UpdateConfigRequest`. That's the next task's problem. Note the failure (file and line) and continue.

- [ ] **Step 3: Manual smoke test of `TokenAuth` expiry behavior**

First make sure `dist/wx_web_api.json` has the migrated object form (Task 1 Step 3 left it that way). Set a token with `expires_at: 2099-12-31` (future, permanent-feeling) for the smoke test.

Edit `dist/wx_web_api.json`:

```json
{
  "api_base_url": "http://127.0.0.1:2022",
  "tokens": [
    {"value": "smoke-future-2099", "expires_at": "2099-12-31"},
    {"value": "smoke-expired-2020", "expires_at": "2020-01-01"}
  ],
  "port": 13399
}
```

Build and start:

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe . 2>&1 | head -20
```

(If build fails on `settings.go` references, that's expected; the next task fixes it. Skip the smoke test and proceed to Task 3 — but if you can build (i.e., you have a stub `settings.go`), run the test.)

If build succeeds, run:

```bash
cd dist && ./wx_web_api.exe -port 13399
```

Test 1 — future-dated token: in a new terminal

```bash
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:13399/wx -H "Authorization: smoke-future-2099" -H "Content-Type: application/json" -d '{"url":"https://example.com"}'
```

Expected: `200` (parser may fail internally with code 1, but that's in the JSON body, not HTTP status; HTTP status is 200 since Gin returns 200 for app-level errors).

Test 2 — past-dated token:

```bash
curl -s -X POST http://127.0.0.1:13399/wx -H "Authorization: smoke-expired-2020" -H "Content-Type: application/json" -d '{"url":"https://example.com"}'
```

Expected JSON: `{"code":401,"msg":"token expired"}`.

Test 3 — unknown token:

```bash
curl -s -X POST http://127.0.0.1:13399/wx -H "Authorization: never-existed" -H "Content-Type: application/json" -d '{"url":"https://example.com"}'
```

Expected JSON: `{"code":401,"msg":"unauthorized"}`.

Stop the server (Ctrl-C).

- [ ] **Step 4: Commit**

```bash
git add internal/handler/handler.go
git commit -m "feat(handler): live token expiry check, separate session tokens

TokenAuth reads config.Get() per request, applies isExpired with a
sync.Map date cache. Login now writes to sessionTokens; SessionAuth
checks only sessionTokens (admin sessions are not subject to expiry).
Old validTokens cache and IsValidToken method removed.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 3: Settings API — adapt to new object-array shape

**Files:**
- Modify: `internal/handler/settings.go` (full file rewrite)

- [ ] **Step 1: Replace `internal/handler/settings.go`**

```go
package handler

import (
    "net/http"
    "strings"
    "time"
    "wx_web_api/internal/config"
    "wx_web_api/internal/model"

    "github.com/gin-gonic/gin"
)

type SettingsHandler struct{}

func NewSettingsHandler() *SettingsHandler {
    return &SettingsHandler{}
}

type ConfigData struct {
    ApiBaseUrl string         `json:"api_base_url"`
    Tokens     []config.Token `json:"tokens"`
}

func (h *SettingsHandler) GetConfig(c *gin.Context) {
    cfg := config.Get()
    c.JSON(http.StatusOK, gin.H{
        "code": 0,
        "data": ConfigData{
            ApiBaseUrl: cfg.ApiBaseUrl,
            Tokens:     cfg.Tokens,
        },
    })
}

type UpdateConfigRequest struct {
    ApiBaseUrl string         `json:"api_base_url"`
    Tokens     []config.Token `json:"tokens"`
}

func (h *SettingsHandler) UpdateConfig(c *gin.Context) {
    var req UpdateConfigRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid request"})
        return
    }
    cfg := config.Get()

    if req.ApiBaseUrl != "" {
        cfg.ApiBaseUrl = req.ApiBaseUrl
    }
    if req.Tokens != nil {
        // Validate each token
        normalized := make([]config.Token, 0, len(req.Tokens))
        seen := make(map[string]bool, len(req.Tokens))
        for i, t := range req.Tokens {
            value := strings.TrimSpace(t.Value)
            if value == "" {
                c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid tokens: index " + itoa(i) + " has empty value"})
                return
            }
            if seen[value] {
                c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid tokens: duplicate value at index " + itoa(i)})
                return
            }
            seen[value] = true
            expires := strings.TrimSpace(t.ExpiresAt)
            if expires != "" {
                if _, err := time.Parse("2006-01-02", expires); err != nil {
                    c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: "invalid tokens: index " + itoa(i) + " expires_at must be yyyy-MM-dd or empty"})
                    return
                }
            }
            normalized = append(normalized, config.Token{Value: value, ExpiresAt: expires})
        }
        cfg.Tokens = normalized
    }

    if err := config.Save(cfg); err != nil {
        c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: err.Error()})
        return
    }
    c.JSON(http.StatusOK, model.SimpleResponse{Code: 0, Msg: "success"})
}

func itoa(n int) string {
    if n == 0 {
        return "0"
    }
    neg := n < 0
    if neg {
        n = -n
    }
    var buf [20]byte
    i := len(buf)
    for n > 0 {
        i--
        buf[i] = byte('0' + n%10)
        n /= 10
    }
    if neg {
        i--
        buf[i] = '-'
    }
    return string(buf[i:])
}
```

Note: The `itoa` helper avoids importing `strconv` just for two-call use. If `strconv` is already imported elsewhere, feel free to use `strconv.Itoa` and drop the helper.

- [ ] **Step 2: Build to verify**

Run: `cd c:/Users/Admin/src/wx_web_api && go build ./...`
Expected: success. No compilation errors.

- [ ] **Step 3: Manual smoke test of settings API**

Start the server (using the same config from Task 2):

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
cd dist && ./wx_web_api.exe -port 13399
```

Test 1 — `GET /api/config` returns object array. (Need a session token first.)

In a new terminal, log in (use admin pwd; default is "1" per `main.go`):

```bash
# Get challenge
CHALLENGE=$(curl -s http://127.0.0.1:13399/api/login/challenge | grep -oE '"challenge":"[^"]+"' | cut -d'"' -f4)
# Simple hash: simpleHash("1" + challenge) — we can't easily compute this from bash.
# Use Python:
RESPONSE=$(python -c "p='1'+'$CHALLENGE';h=0;primes=[31,37,41,43,47,53,59,61,67,71,73,79]
for i,c in enumerate(p):
    h += ord(c) * primes[(i+1)%12]
print(f'{h:016x}')")
TOKEN=$(curl -s -X POST http://127.0.0.1:13399/api/login -H "Content-Type: application/json" -d "{\"pwd\":\"1\",\"challenge\":\"$CHALLENGE\",\"response\":\"$RESPONSE\"}" | grep -oE '"token":"[^"]+"' | cut -d'"' -f4)
echo "Session token: $TOKEN"
```

If the response is empty or you get an error, double-check the Python hash matches the Go `simpleHash` exactly (the formula is the same, just verify the casing — hex format is 16 lowercase chars).

Test 2 — GET config:

```bash
curl -s -H "Authorization: $TOKEN" http://127.0.0.1:13399/api/config
```

Expected: JSON with `data.tokens` being an array of `{value, expires_at}` objects, not strings.

Test 3 — PUT config (set a near-future expiry):

```bash
TOMORROW=$(date -d "tomorrow" +%Y-%m-%d)
curl -s -X PUT -H "Authorization: $TOKEN" -H "Content-Type: application/json" \
  -d "{\"api_base_url\":\"http://127.0.0.1:2022\",\"tokens\":[{\"value\":\"new-tok\",\"expires_at\":\"$TOMORROW\"}]}" \
  http://127.0.0.1:13399/api/config
```

Expected: `{"code":0,"msg":"success"}`.

Test 4 — verify file updated:

```bash
cat dist/wx_web_api.json
```

Expected: contains `new-tok` with `expires_at` = tomorrow.

Test 5 — verify `TokenAuth` sees the change immediately (hot-reload):

```bash
curl -s -X POST http://127.0.0.1:13399/wx -H "Authorization: new-tok" -H "Content-Type: application/json" -d '{"url":"https://example.com"}'
```

Expected: HTTP 200 (parser may return code 1 in body, but the auth path passed — token is still valid because tomorrow is in the future).

Test 6 — invalid date format rejected:

```bash
curl -s -X PUT -H "Authorization: $TOKEN" -H "Content-Type: application/json" \
  -d '{"api_base_url":"http://127.0.0.1:2022","tokens":[{"value":"bad-date","expires_at":"06/08/2026"}]}' \
  http://127.0.0.1:13399/api/config
```

Expected: `{"code":1,"msg":"invalid tokens: index 0 expires_at must be yyyy-MM-dd or empty"}` (or similar; the prefix must contain "invalid tokens").

Test 7 — empty value rejected:

```bash
curl -s -X PUT -H "Authorization: $TOKEN" -H "Content-Type: application/json" \
  -d '{"api_base_url":"http://127.0.0.1:2022","tokens":[{"value":"","expires_at":""}]}' \
  http://127.0.0.1:13399/api/config
```

Expected: `{"code":1,"msg":"invalid tokens: index 0 has empty value"}` (or similar).

Test 8 — duplicate value rejected:

```bash
curl -s -X PUT -H "Authorization: $TOKEN" -H "Content-Type: application/json" \
  -d '{"api_base_url":"http://127.0.0.1:2022","tokens":[{"value":"a","expires_at":""},{"value":"a","expires_at":"2027-01-01"}]}' \
  http://127.0.0.1:13399/api/config
```

Expected: `{"code":1,"msg":"invalid tokens: duplicate value at index 1"}` (or similar).

Stop the server (Ctrl-C).

- [ ] **Step 4: Commit**

```bash
git add internal/handler/settings.go
git commit -m "feat(handler/settings): adapt to {value, expires_at} tokens

GetConfig returns the new object-array shape. UpdateConfig validates
each token (non-empty value, valid yyyy-MM-dd expires_at, no
duplicates) and persists atomically.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 4: Settings UI — full rewrite for object array + date/presets/badges

**Files:**
- Modify: `web/static/js/pages/settings.js` (full file rewrite)

- [ ] **Step 1: Replace `web/static/js/pages/settings.js` with the new implementation**

```javascript
/* Settings page — full implementation with token expiration.
 * Loads /api/config, renders form, supports add/remove/copy/reveal tokens
 * and per-row expiration date (manual + 5 quick presets + status badge),
 * dirty tracking, save/cancel, beforeunload prompt.
 */

(function (global) {
  'use strict';

  var original = { apiBaseUrl: '', tokens: [] };
  var current  = { apiBaseUrl: '', tokens: [] };
  var tokenRevealed = {};
  var beforeUnloadBound = false;

  var PRESETS = [
    { label: '7天',  days: 7 },
    { label: '30天', days: 30 },
    { label: '90天', days: 90 },
    { label: '1年',  days: 365 }
  ];

  function escapeHtml(s) {
    var div = document.createElement('div');
    div.textContent = s == null ? '' : String(s);
    return div.innerHTML;
  }

  function todayStr() {
    var d = new Date();
    var pad = function (n) { return n < 10 ? '0' + n : '' + n; };
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  }

  function presetDate(days) {
    var d = new Date();
    d.setDate(d.getDate() + days);
    var pad = function (n) { return n < 10 ? '0' + n : '' + n; };
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  }

  function parseDate(s) {
    if (!s) return null;
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s);
    if (!m) return null;
    var d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
    if (isNaN(d.getTime())) return null;
    return d;
  }

  function daysUntil(dateStr) {
    var d = parseDate(dateStr);
    if (!d) return null;
    var now = new Date();
    var todayMid = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    var diffMs = d.getTime() - todayMid.getTime();
    return Math.round(diffMs / 86400000);
  }

  function mask(token) {
    if (!token) return '';
    if (token.length <= 12) return token.slice(0, 4) + '••••••••';
    return token.slice(0, 8) + '••••••••';
  }

  function isDirty() {
    if (original.apiBaseUrl !== current.apiBaseUrl) return true;
    if (original.tokens.length !== current.tokens.length) return true;
    for (var i = 0; i < original.tokens.length; i++) {
      if (original.tokens[i].value !== current.tokens[i].value) return true;
      if ((original.tokens[i].expires_at || '') !== (current.tokens[i].expires_at || '')) return true;
    }
    return false;
  }

  function updateSaveButton() {
    var btn = document.getElementById('settingsSaveBtn');
    if (!btn) return;
    btn.disabled = !isDirty();
  }

  function badgeHtmlFor(dateStr) {
    var d = daysUntil(dateStr);
    if (d == null) return '';
    if (d < 0)  return '<span class="badge badge--danger">已过期</span>';
    if (d === 0) return '<span class="badge badge--danger">今天过期</span>';
    if (d <= 7) return '<span class="badge badge--warning">' + d + ' 天后过期</span>';
    return '';
  }

  function renderTokens() {
    var slot = document.getElementById('settingsTokenList');
    var countEl = document.getElementById('settingsTokenCount');
    if (!slot) return;
    if (!current.tokens.length) {
      slot.innerHTML = '<div class="empty"><div class="empty__title">暂无 token</div>' +
                       '<div class="empty__desc">在下方输入框中添加第一个 token</div></div>';
      if (countEl) countEl.textContent = '共 0 个 token';
      return;
    }
    slot.innerHTML = current.tokens.map(function (t, i) {
      var revealed = !!tokenRevealed[i];
      var valueHtml = revealed
        ? '<span class="token-item__value">' + escapeHtml(t.value) + '</span>'
        : '<span class="token-item__value token-item__value--masked">' + escapeHtml(mask(t.value)) + '</span>';
      var expires = t.expires_at || '';
      var presetsHtml = PRESETS.map(function (p) {
        var active = expires && expires === presetDate(p.days);
        return '<button type="button" class="btn btn--sm btn--preset' + (active ? ' btn--active' : '') +
               '" data-action="preset" data-idx="' + i + '" data-days="' + p.days + '">' + p.label + '</button>';
      }).join('');
      var permanentActive = !expires ? ' btn--active' : '';
      var badge = badgeHtmlFor(expires);
      return '<div class="token-item token-item--with-expiry" data-idx="' + i + '">' +
               '<div class="token-item__row">' +
                 valueHtml +
                 '<button type="button" class="btn btn--ghost btn--sm" data-action="copy" data-idx="' + i + '">复制</button>' +
                 '<button type="button" class="btn btn--ghost btn--sm" data-action="toggle" data-idx="' + i + '">' + (revealed ? '隐藏' : '显示') + '</button>' +
                 '<button type="button" class="btn btn--danger btn--sm" data-action="remove" data-idx="' + i + '">删除</button>' +
               '</div>' +
               '<div class="token-item__row token-item__row--expiry">' +
                 '<label class="token-item__expiry-label">过期日期</label>' +
                 '<input type="date" class="input input--date" data-action="expiry" data-idx="' + i + '" value="' + escapeHtml(expires) + '" placeholder="yyyy-MM-dd">' +
                 '<div class="token-item__presets">' + presetsHtml +
                   '<button type="button" class="btn btn--sm btn--preset' + permanentActive + '" data-action="preset-permanent" data-idx="' + i + '">永久</button>' +
                 '</div>' +
               '</div>' +
               (badge ? '<div class="token-item__badge">' + badge + '</div>' : '') +
             '</div>';
    }).join('');
    if (countEl) countEl.textContent = '共 ' + current.tokens.length + ' 个 token';
  }

  function addToken(raw) {
    var token = String(raw || '').trim();
    if (!token) {
      if (global.WXToast) global.WXToast('Token 不能为空', 'error');
      return false;
    }
    for (var i = 0; i < current.tokens.length; i++) {
      if (current.tokens[i].value === token) {
        if (global.WXToast) global.WXToast('Token 已存在', 'error');
        return false;
      }
    }
    current.tokens.push({ value: token, expires_at: '' });
    renderTokens();
    updateSaveButton();
    return true;
  }

  function removeToken(idx) {
    if (idx < 0 || idx >= current.tokens.length) return;
    current.tokens.splice(idx, 1);
    delete tokenRevealed[idx];
    var next = {};
    Object.keys(tokenRevealed).forEach(function (k) {
      var n = Number(k);
      if (n < idx) next[n] = tokenRevealed[k];
      else if (n > idx) next[n - 1] = tokenRevealed[k];
    });
    tokenRevealed = next;
    renderTokens();
    updateSaveButton();
  }

  function copyToken(idx) {
    var t = current.tokens[idx];
    if (!t) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(t.value).then(function () {
        if (global.WXToast) global.WXToast('已复制', 'success');
      }, function () {
        fallbackCopy(t.value);
      });
    } else {
      fallbackCopy(t.value);
    }
  }
  function fallbackCopy(text) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); } catch (e) { /* ignore */ }
    document.body.removeChild(ta);
    if (global.WXToast) global.WXToast('已复制', 'success');
  }

  function setExpiry(idx, value) {
    if (idx < 0 || idx >= current.tokens.length) return;
    current.tokens[idx].expires_at = value || '';
    renderTokens();
    updateSaveButton();
  }

  async function save() {
    var apiBaseUrl = (document.getElementById('settingsApiBaseUrl').value || '').trim() ||
                     'http://127.0.0.1:2022';
    current.apiBaseUrl = apiBaseUrl;
    var tokensToSend = current.tokens.map(function (t) {
      return { value: t.value, expires_at: t.expires_at || '' };
    });

    var btn = document.getElementById('settingsSaveBtn');
    if (btn) { btn.disabled = true; btn.classList.add('is-loading'); btn.textContent = '保存中…'; }

    try {
      var res = await global.WXApi.authJson('/api/config', {
        method: 'PUT',
        body: JSON.stringify({ api_base_url: apiBaseUrl, tokens: tokensToSend })
      });
      if (res.data && res.data.code === 0) {
        original = {
          apiBaseUrl: apiBaseUrl,
          tokens: tokensToSend.map(function (t) { return { value: t.value, expires_at: t.expires_at }; })
        };
        if (global.WXToast) global.WXToast('保存成功', 'success');
      } else {
        if (global.WXToast) global.WXToast((res.data && res.data.msg) || '保存失败', 'error');
      }
    } catch (e) {
      if (global.WXToast) global.WXToast(e.message || '网络错误', 'error');
    } finally {
      if (btn) { btn.disabled = false; btn.classList.remove('is-loading'); btn.textContent = '保存配置'; }
      updateSaveButton();
    }
  }

  function cancel() {
    current = {
      apiBaseUrl: original.apiBaseUrl,
      tokens: original.tokens.map(function (t) { return { value: t.value, expires_at: t.expires_at }; })
    };
    var input = document.getElementById('settingsApiBaseUrl');
    if (input) input.value = original.apiBaseUrl;
    tokenRevealed = {};
    renderTokens();
    updateSaveButton();
  }

  function bindBeforeUnload() {
    if (beforeUnloadBound) return;
    beforeUnloadBound = true;
    global.addEventListener('beforeunload', function (e) {
      if (isDirty()) {
        e.preventDefault();
        e.returnValue = '';
      }
    });
  }

  async function load() {
    var res = await global.WXApi.authJson('/api/config');
    if (res.data && res.data.code === 0 && res.data.data) {
      var toks = Array.isArray(res.data.data.tokens) ? res.data.data.tokens : [];
      original = {
        apiBaseUrl: res.data.data.api_base_url || 'http://127.0.0.1:2022',
        tokens: toks.map(function (t) { return { value: t.value, expires_at: t.expires_at || '' }; })
      };
      current = {
        apiBaseUrl: original.apiBaseUrl,
        tokens: original.tokens.map(function (t) { return { value: t.value, expires_at: t.expires_at }; })
      };
      var input = document.getElementById('settingsApiBaseUrl');
      if (input) input.value = current.apiBaseUrl;
      renderTokens();
      updateSaveButton();
    }
  }

  function render(slot) {
    tokenRevealed = {};
    slot.innerHTML =
      '<div class="card">' +
        '<div class="card__title">后端 API</div>' +
        '<div class="form-group">' +
          '<label class="form-label" for="settingsApiBaseUrl">后端 API 地址</label>' +
          '<input type="text" class="input" id="settingsApiBaseUrl" placeholder="http://127.0.0.1:2022">' +
          '<div class="form-helper">调用微信解析后端的地址（内部 127.0.0.1:2022 服务）</div>' +
        '</div>' +
      '</div>' +

      '<div class="card">' +
        '<div class="card__title">认证 Token</div>' +
        '<div class="form-group">' +
          '<label class="form-label" for="settingsNewToken">新增 Token</label>' +
          '<div class="input-row">' +
            '<input type="text" class="input" id="settingsNewToken" placeholder="输入新 token 后回车或点添加">' +
            '<button type="button" class="btn btn--primary" id="settingsAddTokenBtn">添加</button>' +
          '</div>' +
        '</div>' +
        '<div id="settingsTokenList" class="token-list"></div>' +
        '<div class="token-item__count" id="settingsTokenCount"></div>' +
      '</div>' +

      '<div class="settings-actions">' +
        '<button type="button" class="btn btn--secondary" id="settingsCancelBtn">取消</button>' +
        '<button type="button" class="btn btn--primary" id="settingsSaveBtn">保存配置</button>' +
      '</div>';

    var list = document.getElementById('settingsTokenList');

    list.addEventListener('click', function (e) {
      var btn = e.target.closest('button[data-action]');
      if (!btn) return;
      var idx = Number(btn.getAttribute('data-idx'));
      var action = btn.getAttribute('data-action');
      if (action === 'remove') removeToken(idx);
      else if (action === 'copy') copyToken(idx);
      else if (action === 'toggle') {
        tokenRevealed[idx] = !tokenRevealed[idx];
        renderTokens();
      } else if (action === 'preset') {
        var days = Number(btn.getAttribute('data-days'));
        setExpiry(idx, presetDate(days));
      } else if (action === 'preset-permanent') {
        setExpiry(idx, '');
      }
    });

    list.addEventListener('change', function (e) {
      var input = e.target.closest('input[data-action="expiry"]');
      if (!input) return;
      var idx = Number(input.getAttribute('data-idx'));
      setExpiry(idx, input.value);
    });

    document.getElementById('settingsAddTokenBtn').addEventListener('click', function () {
      var input = document.getElementById('settingsNewToken');
      if (addToken(input.value)) input.value = '';
    });
    document.getElementById('settingsNewToken').addEventListener('keyup', function (e) {
      if (e.key === 'Enter') {
        var input = e.currentTarget;
        if (addToken(input.value)) input.value = '';
      }
    });
    document.getElementById('settingsApiBaseUrl').addEventListener('input', function (e) {
      current.apiBaseUrl = e.currentTarget.value.trim();
      updateSaveButton();
    });
    document.getElementById('settingsCancelBtn').addEventListener('click', cancel);
    document.getElementById('settingsSaveBtn').addEventListener('click', save);

    bindBeforeUnload();
    load();
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.settings = { render: render };
})(window);
```

- [ ] **Step 2: Verify it parses (no syntax error)**

Open the page in Chrome DevTools console (after running the server). Navigate to `/settings`. If there is a JS error, the toast/styling/buttons won't render. Expected: page renders with current tokens listed.

This step is verified as part of the full smoke pass in Task 6 — at this point, only the syntax check matters. The page may visually break (no CSS for new classes yet) until Task 5 lands; that's expected.

- [ ] **Step 3: Commit**

```bash
git add web/static/js/pages/settings.js
git commit -m "feat(ui/settings): per-row token expiration with presets and badges

Each token row now includes a date input, 5 quick presets (7d/30d/90d/1y/permanent),
and a status badge (expired / expires today / N days remaining). Dirty tracking,
copy/reveal/remove/save/cancel/beforeunload all preserved with the new object-array
data shape.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 5: Settings UI styles — badge, presets, date input, multi-row layout

**Files:**
- Modify: `web/static/css/pages.css` (append new rules; do not touch existing ones)

- [ ] **Step 1: Append new CSS rules to `web/static/css/pages.css`**

Open `web/static/css/pages.css`. Add the following to the **end** of the file (after the existing rules; do not touch what's there):

```css
/* Token item with expiration (Phase 2: token expiration) */

.token-item--with-expiry {
  flex-direction: column;
  align-items: stretch;
  gap: var(--s-2);
}
.token-item__row {
  display: flex;
  align-items: center;
  gap: var(--s-3);
  flex-wrap: wrap;
}
.token-item__row--expiry {
  gap: var(--s-2);
  align-items: center;
}
.token-item__expiry-label {
  font-size: var(--t-sm);
  color: var(--text-muted);
  white-space: nowrap;
  flex-shrink: 0;
}
.input--date {
  width: 160px;
  flex-shrink: 0;
}
.token-item__presets {
  display: flex;
  gap: var(--s-1);
  flex-wrap: wrap;
}
.btn--preset {
  background: var(--surface);
  border: 1px solid var(--border);
  color: var(--text-muted);
  padding: var(--s-1) var(--s-2);
  font-size: var(--t-xs);
}
.btn--preset:hover {
  background: var(--surface-2);
  color: var(--text);
  border-color: var(--border-strong);
}
.btn--preset.btn--active {
  background: var(--gradient-primary);
  color: #fff;
  border-color: transparent;
  box-shadow: var(--shadow-glow);
}
.token-item__badge {
  display: flex;
  gap: var(--s-2);
  align-items: center;
}
.badge {
  display: inline-flex;
  align-items: center;
  padding: 2px var(--s-2);
  border-radius: 999px;
  font-size: var(--t-xs);
  font-weight: 500;
  line-height: 1.4;
}
.badge--warning {
  background: rgba(245, 158, 11, 0.15);
  color: var(--warning);
  border: 1px solid rgba(245, 158, 11, 0.4);
}
.badge--danger {
  background: rgba(239, 68, 68, 0.15);
  color: var(--danger);
  border: 1px solid rgba(239, 68, 68, 0.4);
}

@media (max-width: 640px) {
  .token-item__row { flex-direction: column; align-items: stretch; }
  .input--date { width: 100%; }
  .token-item__presets { justify-content: flex-start; }
}
```

- [ ] **Step 2: Verify styles load (in the browser, after running the server)**

After Task 6's smoke pass, the layout should look right. For now, the file just needs to be syntactically valid CSS. If the file has any unbalanced braces, all subsequent CSS may be dropped by the browser. Open the page in Chrome DevTools and check the Elements panel — the `.token-item` elements should pick up the new `--with-expiry` modifier styles.

This is verified as part of the full smoke pass in Task 6.

- [ ] **Step 3: Commit**

```bash
git add web/static/css/pages.css
git commit -m "feat(ui): styles for token expiration row, presets, badges

Adds .token-item--with-expiry (multi-row), preset button group with
active state (gradient highlight), warning/danger badges for expiry
status, and responsive collapse to single column under 640px.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 6: Full smoke pass — verify all 12 cases from spec

**Files:** none modified; this is verification only.

Run the server (use the existing build):

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
cd dist && ./wx_web_api.exe -port 13399
```

- [ ] **Step 1: Spec smoke #1 — startup migration (already verified in Task 1 Step 3; re-verify)**

If you kept the migrated config from Task 3, no migration log should appear on this startup. If you've reset the config, repeat Task 1's smoke procedure.

- [ ] **Step 2: Spec smoke #2 — permanent token works**

Set a token with `expires_at: ""`:

```bash
curl -s -X PUT -H "Authorization: $TOKEN" -H "Content-Type: application/json" \
  -d '{"api_base_url":"http://127.0.0.1:2022","tokens":[{"value":"perm-test","expires_at":""}]}' \
  http://127.0.0.1:13399/api/config

curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:13399/wx \
  -H "Authorization: perm-test" -H "Content-Type: application/json" -d '{"url":"https://example.com"}'
```

Expected: PUT returns `{"code":0,"msg":"success"}`; subsequent `POST /wx` returns HTTP 200.

- [ ] **Step 3: Spec smoke #3 — today-expiry: valid now, rejected tomorrow**

```bash
TODAY=$(date +%Y-%m-%d)
curl -s -X PUT -H "Authorization: $TOKEN" -H "Content-Type: application/json" \
  -d "{\"api_base_url\":\"http://127.0.0.1:2022\",\"tokens\":[{\"value\":\"today-tok\",\"expires_at\":\"$TODAY\"}]}" \
  http://127.0.0.1:13399/api/config

# Now (still today): should pass
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:13399/wx \
  -H "Authorization: today-tok" -H "Content-Type: application/json" -d '{"url":"https://example.com"}'
```

Expected: 200 today. To verify the "rejected tomorrow" half without waiting: edit `dist/wx_web_api.json` to set `today-tok`'s `expires_at` to yesterday's date, then call `/wx`:

```bash
YESTERDAY=$(date -d "yesterday" +%Y-%m-%d)
# Use sed or Python to replace
python -c "
import json
p = 'c:/Users/Admin/src/wx_web_api/dist/wx_web_api.json'
d = json.load(open(p))
for t in d['tokens']:
    if t['value'] == 'today-tok':
        t['expires_at'] = '$YESTERDAY'
json.dump(d, open(p, 'w'), indent=2)
"
# Hot-reload check (no restart)
curl -s -X POST http://127.0.0.1:13399/wx -H "Authorization: today-tok" -H "Content-Type: application/json" -d '{"url":"https://example.com"}'
```

Expected: `{"code":401,"msg":"token expired"}` (and importantly, the change took effect without a restart — this is the "hot reload" property of `config.Get()`).

- [ ] **Step 4: Spec smoke #4 — tomorrow-expiry, valid today**

```bash
TOMORROW=$(date -d "tomorrow" +%Y-%m-%d)
curl -s -X PUT -H "Authorization: $TOKEN" -H "Content-Type: application/json" \
  -d "{\"api_base_url\":\"http://127.0.0.1:2022\",\"tokens\":[{\"value\":\"tomorrow-tok\",\"expires_at\":\"$TOMORROW\"}]}" \
  http://127.0.0.1:13399/api/config

curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:13399/wx \
  -H "Authorization: tomorrow-tok" -H "Content-Type: application/json" -d '{"url":"https://example.com"}'
```

Expected: 200 (tomorrow is still in the future).

- [ ] **Step 5: Spec smoke #5 — malformed date is treated as permanent (defensive)**

```bash
# Hand-edit config to set a malformed date
python -c "
import json
p = 'c:/Users/Admin/src/wx_web_api/dist/wx_web_api.json'
d = json.load(open(p))
d['tokens'] = [{'value': 'malformed-tok', 'expires_at': '06/08/2026'}]
json.dump(d, open(p, 'w'), indent=2)
"

# Without restarting:
curl -s -X POST http://127.0.0.1:13399/wx -H "Authorization: malformed-tok" -H "Content-Type: application/json" -d '{"url":"https://example.com"}'
```

Expected: HTTP 200 (auth path treated malformed as permanent, parser may return code 1 in body, that's fine).

- [ ] **Step 6: Spec smoke #6 — hot-reload of expiry**

Already implicitly covered in Step 3. Confirm the mechanism: `TokenAuth` reads `config.Get()` on every call, so a config save (or even direct file edit followed by saving through the API) takes effect immediately. If you want a more explicit test, set a future expiry, then PUT to make it past, then immediately call `/wx` — should get 401 without restart.

- [ ] **Step 7: Spec smoke #7 — admin session unaffected by token expiry**

```bash
# Set a token with past expiry so it would be 401 to TokenAuth
python -c "
import json
p = 'c:/Users/Admin/src/wx_web_api/dist/wx_web_api.json'
d = json.load(open(p))
d['tokens'] = [{'value': 'old', 'expires_at': '2020-01-01'}]
json.dump(d, open(p, 'w'), indent=2)
"
# /api/config (which uses SessionAuth = sessionTokens) should still respond
curl -s -H "Authorization: $TOKEN" http://127.0.0.1:13399/api/config
```

Expected: 200 with `data.tokens` containing `old` (admin session token is independent of the token list).

- [ ] **Step 8: Spec smoke #8 — Settings page UI (Chrome DevTools)**

This is a manual visual + interaction pass. Use Chrome DevTools to load `http://127.0.0.1:13399/settings`. Walk through:

1. **Add token**: type a value, click "添加" or press Enter → new row appears with empty expiry.
2. **Set date manually**: click the date input, pick a date far in the future → save button enables; status badge does NOT appear (>7 days).
3. **Use preset**: pick 7 days → input shows 7 days from now, "7天" button is highlighted with gradient.
4. **Use "永久" preset**: input clears, "永久" button highlighted; status badge does not appear.
5. **Set near-future**: pick a date 3 days from now → orange badge "3 天后过期" appears below the row.
6. **Set today**: orange badge changes to red "今天过期".
7. **Set past date**: red "已过期" badge appears.
8. **Copy token**: click 复制 → toast "已复制", clipboard has the value.
9. **Show/Hide**: click 显示 → value reveals; click 隐藏 → masked again.
10. **Remove**: click 删除 → row vanishes, count decrements.
11. **Duplicate detection**: add a token with value "abc", try to add another "abc" → toast "Token 已存在".
12. **Cancel**: change something (add row, edit field), click 取消 → all changes revert, save button disabled.
13. **Save**: change something, click 保存 → toast "保存成功"; refresh page (F5) → changes persist (no migration log on this startup either).
14. **Beforeunload**: edit a field, try to navigate away or close tab → browser prompts "Leave site?" dialog.
15. **Mobile**: DevTools → toggle device toolbar (iPhone 12) → rows collapse to single column, date input full width, presets wrap.

If any of these fail, file a follow-up commit fixing the specific issue.

- [ ] **Step 9: Spec smoke #9 — duplicate value rejected at save**

In the settings UI, add two tokens with the same value (e.g. "dup" — first add succeeds, second add is rejected client-side with toast). To test the **server-side** duplicate check, manually construct a request:

```bash
curl -s -X PUT -H "Authorization: $TOKEN" -H "Content-Type: application/json" \
  -d '{"api_base_url":"http://127.0.0.1:2022","tokens":[{"value":"dup","expires_at":""},{"value":"dup","expires_at":"2027-01-01"}]}' \
  http://127.0.0.1:13399/api/config
```

Expected: `{"code":1,"msg":"invalid tokens: ..."}` containing "duplicate".

- [ ] **Step 10: Spec smoke #10 — empty value rejected at add**

In settings UI, click "添加" with empty input → toast "Token 不能为空"; no row added.

- [ ] **Step 11: Spec smoke #11 — empty tokens array works**

```bash
curl -s -X PUT -H "Authorization: $TOKEN" -H "Content-Type: application/json" \
  -d '{"api_base_url":"http://127.0.0.1:2022","tokens":[]}' \
  http://127.0.0.1:13399/api/config

# All prior tokens should now be 401
curl -s -X POST http://127.0.0.1:13399/wx -H "Authorization: perm-test" -H "Content-Type: application/json" -d '{"url":"https://example.com"}'
```

Expected: PUT returns success; `/wx` returns `{"code":401,"msg":"unauthorized"}`.

- [ ] **Step 12: Spec smoke #12 — Dashboard token count**

Navigate to `/dashboard`. The "配置的 Token 数" card should reflect `cfg.Tokens.length` (now the count of objects, which equals the number of configured tokens).

Currently `dist/wx_web_api.json` has empty `tokens` (from Step 11). Add a couple of tokens:

```bash
curl -s -X PUT -H "Authorization: $TOKEN" -H "Content-Type: application/json" \
  -d '{"api_base_url":"http://127.0.0.1:2022","tokens":[{"value":"a","expires_at":""},{"value":"b","expires_at":"2026-12-31"}]}' \
  http://127.0.0.1:13399/api/config
```

Then refresh the dashboard. Expected: "配置的 Token 数" shows `2`.

- [ ] **Step 13: Stop server, commit any leftover changes**

```bash
# Stop the server (Ctrl-C in its terminal)
git status
```

If any code/config was changed during smoke (e.g., to recover from a finding), commit it. If everything is green, no commit needed; the previous 5 tasks' commits are the final state.

- [ ] **Step 14: Final summary commit (optional)**

If you want a clean marker commit to identify the end of the feature on `main`:

```bash
git commit --allow-empty -m "feat(token-expiration): feature complete

Implements the spec at docs/superpowers/specs/2026-06-08-token-expiration-design.md.
Smoke tests 1-12 verified. External POST /wx and /wx/finder contract
unchanged; only 401 msg text now distinguishes 'token expired' from
'unauthorized'.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Self-Review

**1. Spec coverage** — mapping each spec section to a task:

- §背景与目标 (4 bullets) → Tasks 1, 2, 3, 4, 5 (one each)
- §数据模型 (Config + JSON + 启动时迁移) → Task 1
- §服务端 TokenAuth 校验顺序 + isExpired + 缓存 → Task 2
- §实时读 config.Get() 理由 + Handler 重构 → Task 2
- §登录流程 sessionTokens 分离 → Task 2
- §dateCache 失效分析 → Task 2 (decided not to invalidate)
- §管理接口 GET/PUT shape → Task 3
- §UpdateConfig 校验规则 → Task 3
- §设置页 UI 行结构 → Task 4
- §预设按钮行为 → Task 4
- §过期状态徽章 → Task 4
- §快速算法 presetDate → Task 4
- §dirty/save/cancel 适配 → Task 4
- §新增 token 不弹日期选择器 → Task 4 (default empty, add row)
- §对外契约表 → no code, just maintained
- §冒烟清单 1-12 → Task 6

All covered.

**2. Placeholder scan** — searched for "TBD", "TODO", "implement later", "fill in", "appropriate error handling", "add validation", "handle edge cases", "similar to Task N" (the latter I deliberately re-used for the `simpleHash` Python snippet — that one is fully self-contained in the task, so OK). No actual placeholders.

**3. Type consistency** —

- `config.Token` defined Task 1; used in handler (Task 2) and settings (Task 3) and JS (Task 4) — all match.
- `Handler.sessionTokens map[string]bool` defined Task 2; used in `Login` and `SessionAuth` (same task) — consistent.
- `Handler.dateCache sync.Map` defined Task 2; used in `isExpired` (same task) — consistent.
- JS shape `current.tokens = [{value, expires_at}]` defined Task 4; used in `addToken`, `removeToken`, `setExpiry`, `save`, `load`, `renderTokens`, `isDirty` (all same task) — consistent.
- Preset `days` values: `[7, 30, 90, 365]` defined once in `PRESETS` array, used in render + click handler — consistent.
- `escapeHtml`, `parseDate`, `daysUntil`, `presetDate`, `mask`, `badgeHtmlFor` all defined Task 4 and used within Task 4 — consistent.

**4. Execution order** — Tasks 1, 2 must precede Task 3 (build must succeed for smoke). Tasks 4, 5, 6 are sequenced. Build is verified at the end of Tasks 1, 3 (the two tasks that change types). Task 2's build is intentionally broken (waits for Task 3) — this is acknowledged in the task.

No issues found. Plan is ready.
