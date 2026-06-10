# System Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the placeholder `系统信息` page with a live system-info dashboard. Backend exposes static config (build tag, ports, paths, runtime) via `GET /api/system` and pushes dynamic stats (uptime, goroutines, memory, request counts) every 2s via `GET /ws/system`. Frontend renders 4 cards (runtime / version / config / health) with auto-reconnect.

**Architecture:** New `internal/buildinfo` package holds the link-time-overridable build vars (BuildTag, BuildTime, GitSHA). New `internal/handler/system.go` provides `GetSystem` (one-shot, reads from config + filesystem) and `HandleSystemWS` (gorilla/websocket upgrader, registers with hub, blocks reading). New `internal/handler/broadcaster.go` owns a `systemHub` (map of `*websocket.Conn`) and a single 2s ticker goroutine that fans out `SystemSnapshot` JSON to all connected clients. WS auth via `?token=<session>` query param (browser WebSocket API cannot send custom headers; the existing `SessionAuth` middleware already supports both header and query). Frontend connects, applies `SystemData` to static fields once, applies `SystemSnapshot` to dynamic fields on every frame, auto-reconnects with exponential backoff.

**Tech Stack:** Go 1.25.6 + Gin v1.12.0 + `github.com/gorilla/websocket` (new dep). Vanilla JS frontend (no build step). `embed.FS` rebuild required after web asset changes.

**Branch policy:** Implementer commits directly to `main` (user's standing instruction). All commits include `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/buildinfo/buildinfo.go` | **Create.** Package-level vars `BuildTag`, `BuildTime`, `GitSHA` — overridable via `-ldflags "-X wx_web_api/internal/buildinfo.BuildTag=..."` at link time. Defaults: `"dev"`, `"unknown"`, `"unknown"`. |
| `internal/handler/system.go` | **Create.** `SystemData` struct (one-shot response shape), `MemStats`/`ReqStats`/`SystemSnapshot` structs (push frame), `GetSystem` handler, `HandleSystemWS` handler, `upgrader` (gorilla/websocket), `processStart` time. |
| `internal/handler/broadcaster.go` | **Create.** `systemHub` (client map + RWMutex), `SystemHub` package-level instance, `Start(ctx)` ticker loop, `collectSnapshot()` helper, `register`/`unregister` methods. |
| `main.go` | **Modify.** Add `internal/buildinfo` import; change `var buildTag = "dev"` to use `buildinfo.BuildTag`; add `go SystemHub.Start(ctx)` after `config.Init`; register two new routes: `r.GET("/api/system", h.SessionAuth(), h.GetSystem)` and `r.GET("/ws/system", h.SessionAuth(), h.HandleSystemWS)`. |
| `web/static/css/pages.css` | **Modify (append).** `.kv` definition-list grid, `.conn-badge` and its `--ok`/`--err`/`--connecting` modifiers, system-card layout. |
| `web/static/js/pages/system.js` | **Modify (full rewrite).** 4-card render, `loadStatic` (GET /api/system), `connectWS` (open + exponential-backoff reconnect), `applyStatic` (one-shot DOM update), `applySnapshot` (per-frame DOM update for runtime/mem fields), `formatBytes` helper, `formatDuration` helper. |

Files **not** modified:
- `internal/handler/handler.go` (no changes to `TokenAuth`, `SessionAuth`, `Login`, `ParseWxURL`, `ParseFinderFeedByObjectID`, `isExpired`)
- `internal/handler/settings.go` (no changes to `GetConfig`/`UpdateConfig`)
- `web/index.html` (`pages/system.js?v=1` already wired)
- `web/static/js/router.js` / `auth.js` / `api.js` / `store.js` / `app.js`
- `web/static/js/pages/dashboard.js` / `test.js` / `history.js` / `users.js` / `settings.js`
- `web/embed.go` (wildcard already covers new file)
- `dist/wx_web_api.db` (read only — for size, not content; no schema changes)

---

## Design decisions

1. **WS auth via query string**, not cookie or custom header. The browser WebSocket API does not allow custom headers, and the project has no cookie auth. The existing `SessionAuth` middleware (handler.go:75) already reads `?token=` as a fallback to `Authorization`, so this is a zero-cost reuse.
2. **Per-connection goroutine for writes** inside the broadcaster's tick loop. A slow client must not block other clients. The tick loop is single-threaded (it owns the snapshot), but each `WriteJSON` runs in `go func() { ... }` per client.
3. **Static endpoint reads from in-process state**, not from a fresh disk read every call. `cfg := config.Get()` (already in-memory), `os.Stat(dbPath)` is the only filesystem hit per call. This makes the static endpoint trivially safe to spam from the frontend (we don't, but the contract allows it).
4. **`processStart` is a package-level var in `internal/handler`**, set in an `init()` at package load time. Simpler than threading it through main, and is correct (handler package is loaded exactly once before any handler is called).
5. **`Stats` field is `*ReqStats`** (pointer, nil when history is unimplemented). Avoids the "magic zero value looks like real data" trap. Frontend checks for null and shows `—` plus an explanatory note.
6. **Frontend reconnect is silent** (no toast). Admin tool WS drops are normal (e.g., user opens the page, server restarts, user comes back). The connection badge is the only signal.
7. **pprof link shown but not implemented**. `main.go` does not currently register `/debug/pprof/`. The link is rendered in the UI as `/debug/pprof/`, but opening it returns 404 from gin. This is documented as out-of-scope for this spec; users see the link with a tooltip "需要服务端启用 pprof 路由".
8. **`buildTime` is captured at package init**, not at request time. Go's `init()` runs once at process start, so `time.Now()` in `init()` gives the process start time. This is what the user wants ("compiled time"). For finer control, `-ldflags "-X"` can override.

---

## Task 1: Add gorilla/websocket dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum` (auto-generated by `go get`)

- [ ] **Step 1: `go get github.com/gorilla/websocket`**

Run from the project root:
```bash
cd c:/Users/Admin/src/wx_web_api
go get github.com/gorilla/websocket@latest
```
Expected: `go.mod` and `go.sum` are updated. The `go.mod` `require` block gains `github.com/gorilla/websocket vX.Y.Z`.

- [ ] **Step 2: Verify the build still passes (sanity check)**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o /tmp/wx_web_api_sanity_check.exe .
```
Expected: build succeeds with no output. The binary is not used — just a sanity check that the existing code still compiles with the new dep in `go.sum`.

Delete the sanity binary: `rm /tmp/wx_web_api_sanity_check.exe` (or on Windows, `del` it; on bash under Git for Windows, the file may be locked — just leave it; this is a sanity check only).

- [ ] **Step 3: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add go.mod go.sum
git commit -m "chore: add gorilla/websocket dependency"
```

---

## Task 2: Build-info package + static endpoint

**Files:**
- Create: `internal/buildinfo/buildinfo.go`
- Create: `internal/handler/system.go` (partial — only the data structs and `GetSystem`; the WS handler is in Task 3)
- Modify: `main.go` (3 changes: import buildinfo, change `var buildTag` to use buildinfo, register `GET /api/system` route)

- [ ] **Step 1: Create `internal/buildinfo/buildinfo.go`**

Create the file with this content:

```go
// Package buildinfo holds compile-time-overridable build metadata.
// Default values are placeholders for development builds. For release builds,
// set at link time via:
//
//   go build -ldflags "\
//     -X wx_web_api/internal/buildinfo.BuildTag=v1.2.3 \
//     -X wx_web_api/internal/buildinfo.BuildTime=2026-06-10T15:30:00Z \
//     -X wx_web_api/internal/buildinfo.GitSHA=8947005" \
//     -o dist/wx_web_api.exe .
package buildinfo

var (
	BuildTag  = "dev"
	BuildTime = "unknown"
	GitSHA    = "unknown"
)
```

- [ ] **Step 2: Create `internal/handler/system.go` (data structs + GetSystem only)**

Create the file with this content (the WS handler at the bottom is included here as a stub so the file compiles after Task 2, but the body is filled in Task 3):

```go
package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"wx_web_api/internal/buildinfo"
	"wx_web_api/internal/config"

	"github.com/gin-gonic/gin"
)

// processStart is captured at package init and is the source of truth for
// uptime. A package-level var (not a const-time expression) is required so that
// the value reflects the moment the binary started, not the moment the source
// was compiled.
var processStart = time.Now() //nolint:gochecknoglobals

// SystemData is the one-shot response of GET /api/system. It contains values
// that do not change at runtime: build metadata, configured ports, paths, file
// sizes. Values that *do* change (uptime, goroutines, memory) are sent via the
// SystemSnapshot WebSocket push instead.
type SystemData struct {
	BuildTag   string `json:"build_tag"`
	BuildTime  string `json:"build_time"`
	GitSHA     string `json:"git_sha"`
	GoVersion  string `json:"go_version"`
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	ConfigPath string `json:"config_path"`
	DBPath     string `json:"db_path"`
	DBSize     int64  `json:"db_size"`
	Port       int    `json:"port"`
	ApiBaseUrl string `json:"api_base_url"`
	TokenCount int    `json:"token_count"`
}

// GetSystem handles GET /api/system. Returns the static SystemData snapshot.
// Frontend fetches this on page render; live values come via /ws/system.
func (h *Handler) GetSystem(c *gin.Context) {
	cfg := config.Get()
	dbPath := filepath.Join(config.ExeDir, "wx_web_api.db")
	var dbSize int64
	if info, err := os.Stat(dbPath); err == nil {
		dbSize = info.Size()
	}
	data := SystemData{
		BuildTag:   buildinfo.BuildTag,
		BuildTime:  buildinfo.BuildTime,
		GitSHA:     buildinfo.GitSHA,
		GoVersion:  runtime.Version(),
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		ConfigPath: filepath.Join(config.ExeDir, "wx_web_api.json"),
		DBPath:     dbPath,
		DBSize:     dbSize,
		Port:       cfg.Port,
		ApiBaseUrl: cfg.ApiBaseUrl,
		TokenCount: len(cfg.Tokens),
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}
```

Note: the `time` import is required for the `time.Now()` at package level. Add it to the import block.

Also add the stub for HandleSystemWS at the bottom of the same file (will be filled in by Task 3):

```go
// HandleSystemWS is implemented in broadcaster.go (Task 3). Stub here to
// keep the file compiling if main.go references it before Task 3 lands.
// REMOVE THIS STUB WHEN TASK 3 LANDS.
func (h *Handler) HandleSystemWS(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"code": 1, "msg": "ws not yet wired"})
}
```

- [ ] **Step 3: Modify `main.go` to use buildinfo and register the new route**

Edit `main.go` with the following changes (apply as a single Edit operation, or as multiple targeted Edits):

1. Add `"wx_web_api/internal/buildinfo"` to the import block.

2. Replace the line `var buildTag = "dev"` (currently at main.go:21) with:
   ```go
   var buildTag = buildinfo.BuildTag
   ```
   This keeps the existing `log.Printf` (line 137) which references `buildTag` working. The `buildinfo` package's own `BuildTag` defaults to `"dev"`, and can be overridden via `-ldflags`.

3. Add the following two route registrations after the existing `/api/config` block (after main.go line 113):
   ```go
   // System info routes (session-authenticated)
   r.GET("/api/system", h.SessionAuth(), h.GetSystem)
   r.GET("/ws/system", h.SessionAuth(), h.HandleSystemWS)
   ```
   The `/ws/system` route references `h.HandleSystemWS` which is a stub at this point. It returns 501. Task 3 replaces the stub body with a real WebSocket upgrader. The route registration is correct in both tasks.

- [ ] **Step 4: Verify the build**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
```
Expected: build succeeds. The binary at `dist/wx_web_api.exe` is now updated.

- [ ] **Step 5: Smoke-test the new endpoint**

Start the server in the background and verify `GET /api/system` returns the expected shape:

```bash
cd c:/Users/Admin/src/wx_web_api
./dist/wx_web_api.exe -pwd 1 &
SERVER_PID=$!
sleep 1
# Get a session token
CHALLENGE=$(curl -s http://127.0.0.1:13335/api/login/challenge | python -c "import json,sys; print(json.load(sys.stdin)['challenge'])")
# The admin password is "1". The simpleHash is computed in the handler. For this smoke test,
# we use a known-good token. The session token format is "Bearer <hex>" — see handler.go.
# Easier: just hit /api/system with the Authorization header set to a valid session token
# that we obtain via the login flow. Run the same flow as the frontend:
LOGIN_RESP=$(curl -s -X POST http://127.0.0.1:13335/api/login -H "Content-Type: application/json" -d "{\"pwd\":\"1\",\"challenge\":\"$CHALLENGE\",\"response\":\"$(python -c "
import sys
pwd='1'
challenge='$CHALLENGE'
primes=[31,37,41,43,47,53,59,61,67,71,73,79]
h=0
for i,c in enumerate(pwd+challenge):
    h += ord(c) * primes[(i+1)%12]
print(format(h & 0xFFFFFFFF, '016x'))
")\"}")
SESSION_TOKEN=$(echo $LOGIN_RESP | python -c "import json,sys; print(json.load(sys.stdin)['token'])")
echo "Session token: $SESSION_TOKEN"
echo "--- GET /api/system ---"
curl -s -H "Authorization: $SESSION_TOKEN" http://127.0.0.1:13335/api/system | python -m json.tool
kill $SERVER_PID 2>/dev/null
```

Expected response (values will vary):
```json
{
    "code": 0,
    "data": {
        "build_tag": "dev",
        "build_time": "unknown",
        "git_sha": "unknown",
        "go_version": "go1.25.6",
        "goos": "windows",
        "goarch": "amd64",
        "config_path": "C:\\...\\dist\\wx_web_api.json",
        "db_path": "C:\\...\\dist\\wx_web_api.db",
        "db_size": 0,
        "port": 13335,
        "api_base_url": "http://127.0.0.1:2022",
        "token_count": 2
    }
}
```

- [ ] **Step 6: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add internal/buildinfo/buildinfo.go internal/handler/system.go main.go
git commit -m "feat(handler/system): add static GET /api/system endpoint"
```

---

## Task 3: WebSocket broadcaster + handler

**Files:**
- Create: `internal/handler/broadcaster.go`
- Modify: `internal/handler/system.go` (replace the `HandleSystemWS` stub with the real upgrader)

- [ ] **Step 1: Create `internal/handler/broadcaster.go`**

Create the file with this content:

```go
package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MemStats is the JSON-friendly subset of runtime.MemStats the system page
// needs. Three fields (Alloc, HeapSys, Sys) give an at-a-glance view of live
// heap pressure without dragging the full struct across the wire.
type MemStats struct {
	Alloc   uint64 `json:"alloc"`
	HeapSys uint64 `json:"heap_sys"`
	Sys     uint64 `json:"sys"`
}

// ReqStats is the aggregate of all /wx and /wx/finder calls in this process.
// Stats is null (not a zero-value object) when the request log is not yet
// implemented — the frontend distinguishes "no data" from "zero data" by
// checking for null.
type ReqStats struct {
	Total  int `json:"total"`
	Today  int `json:"today"`
	Errors int `json:"errors"`
}

// SystemSnapshot is one WebSocket push frame. The frontend applies it on top
// of the static SystemData fetched from GET /api/system.
type SystemSnapshot struct {
	Type          string    `json:"type"` // always "snapshot"
	Ts            int64     `json:"ts"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	Goroutines    int       `json:"goroutines"`
	Mem           MemStats  `json:"mem"`
	Stats         *ReqStats `json:"stats"`
}

// upgrader is shared by all HandleSystemWS invocations. CheckOrigin returns
// true unconditionally — this is an admin tool behind a session login, and
// the WS endpoint sits behind SessionAuth middleware, so origin checks add
// friction without security value.
var upgrader = websocket.Upgrader{ //nolint:gochecknoglobals
	CheckOrigin: func(r *http.Request) bool { return true },
}

// systemHub is the in-process registry of active /ws/system connections.
// A single ticker goroutine fans out SystemSnapshot to every connected client.
// Per-client writes run in their own goroutine so a slow client cannot block
// other clients' updates.
type systemHub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]time.Time
}

// SystemHub is the package-level singleton started by main.go.
var SystemHub = &systemHub{clients: make(map[*websocket.Conn]time.Time)} //nolint:gochecknoglobals

func (h *systemHub) register(c *websocket.Conn) {
	h.mu.Lock()
	h.clients[c] = time.Now()
	h.mu.Unlock()
}

func (h *systemHub) unregister(c *websocket.Conn) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		_ = c.Close()
	}
	h.mu.Unlock()
}

// Start runs the broadcaster loop until ctx is cancelled. The loop ticks
// every 2 seconds, marshals one SystemSnapshot, and fans it out to all
// connected clients. Per-client writes run in goroutines with a 5s deadline;
// a client that fails to ack within the deadline is unregistered.
func (h *systemHub) Start(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := collectSnapshot()
			data, err := json.Marshal(snap)
			if err != nil {
				log.Printf("broadcaster: marshal error: %v", err)
				continue
			}
			h.mu.RLock()
			for c := range h.clients {
				go func(conn *websocket.Conn, payload []byte) {
					_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
						h.unregister(conn)
					}
				}(c, data)
			}
			h.mu.RUnlock()
		}
	}
}

// collectSnapshot builds the SystemSnapshot for the current moment. Stats
// returns nil until history is implemented; the frontend shows "—" for that
// field and an explanatory note.
func collectSnapshot() SystemSnapshot {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return SystemSnapshot{
		Type:          "snapshot",
		Ts:            time.Now().Unix(),
		UptimeSeconds: int64(time.Since(processStart).Seconds()),
		Goroutines:    runtime.NumGoroutine(),
		Mem: MemStats{
			Alloc:   m.Alloc,
			HeapSys: m.HeapSys,
			Sys:     m.Sys,
		},
		Stats: nil,
	}
}
```

- [ ] **Step 2: Replace the `HandleSystemWS` stub in `internal/handler/system.go`**

In `internal/handler/system.go`, find the `HandleSystemWS` stub at the bottom (it returns 501) and replace it with:

```go
// HandleSystemWS upgrades the HTTP connection to a WebSocket and registers
// the connection with SystemHub. The first frame sent is an immediate
// SystemSnapshot so the client does not have to wait up to 2 seconds for
// the first tick. After that, the goroutine blocks reading from the
// connection; any read error (which on a WebSocket means the client has
// disconnected) triggers cleanup via deferred unregister.
func (h *Handler) HandleSystemWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}
	SystemHub.register(conn)
	defer SystemHub.unregister(conn)

	// Send initial snapshot immediately so the client sees data on first
	// frame, not after the next ticker fire.
	if err := conn.WriteJSON(collectSnapshot()); err != nil {
		return
	}

	// Block reading from the connection. We do not consume any client
	// messages — this is a server-push channel — but reading is the only
	// way to detect client disconnect on a WebSocket. NextReader returns
	// an error when the client closes; we then unregister and return.
	for {
		if _, _, err := conn.NextReader(); err != nil {
			return
		}
	}
}
```

Also add `"log"` to the imports of `internal/handler/system.go` (the `log.Printf` needs it).

- [ ] **Step 3: Modify `main.go` to start the broadcaster**

In `main.go`, immediately after `h := handler.New(effectivePwd)` (around line 66), add:

```go
go handler.SystemHub.Start(context.Background())
```

And add `"context"` to main.go's imports.

- [ ] **Step 4: Verify the build**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
```
Expected: build succeeds.

- [ ] **Step 5: Smoke-test the WebSocket endpoint**

Start the server in the background, get a session token, and verify the WS endpoint accepts a connection and pushes a snapshot.

```bash
cd c:/Users/Admin/src/wx_web_api
./dist/wx_web_api.exe -pwd 1 &
SERVER_PID=$!
sleep 1
# Get a session token (same flow as Task 2 step 5)
CHALLENGE=$(curl -s http://127.0.0.1:13335/api/login/challenge | python -c "import json,sys; print(json.load(sys.stdin)['challenge'])")
LOGIN_RESP=$(curl -s -X POST http://127.0.0.1:13335/api/login -H "Content-Type: application/json" -d "{\"pwd\":\"1\",\"challenge\":\"$CHALLENGE\",\"response\":\"$(python -c "
import sys
pwd='1'
challenge='$CHALLENGE'
primes=[31,37,41,43,47,53,59,61,67,71,73,79]
h=0
for i,c in enumerate(pwd+challenge):
    h += ord(c) * primes[(i+1)%12]
print(format(h & 0xFFFFFFFF, '016x'))
")\"}")
SESSION_TOKEN=$(echo $LOGIN_RESP | python -c "import json,sys; print(json.load(sys.stdin)['token'])")

# Use Python's websocket-client (install if needed) to connect and read 2 frames
pip install websocket-client >/dev/null 2>&1
python <<EOF
import websocket
import json
url = "ws://127.0.0.1:13335/ws/system?token=$SESSION_TOKEN"
ws = websocket.create_connection(url, timeout=5)
for i in range(2):
    frame = ws.recv()
    print(f"frame {i+1}:")
    print(json.dumps(json.loads(frame), indent=2))
ws.close()
EOF
kill $SERVER_PID 2>/dev/null
```

Expected: 2 frames are received (the initial snapshot and the next tick ~2s later). Each frame has the shape:
```json
{
  "type": "snapshot",
  "ts": 1718023812,
  "uptime_seconds": 5,
  "goroutines": 8,
  "mem": { "alloc": 3355443, "heap_sys": 7340032, "sys": 13002342 },
  "stats": null
}
```

If the connection is rejected with 401, the session token was wrong. If the connection is rejected with 404, the route is not registered. If frames are not received within 5s, the broadcaster ticker is not running.

- [ ] **Step 6: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add internal/handler/system.go internal/handler/broadcaster.go main.go
git commit -m "feat(handler/system): add /ws/system WebSocket push with broadcaster"
```

---

## Task 4: CSS for system page

**Files:**
- Modify: `web/static/css/pages.css` (append to end of file, do not modify any existing rules)

- [ ] **Step 1: Append the system-page CSS block**

Append the following content to the end of `web/static/css/pages.css` (do not change any existing rules above):

```css
/* ============================================================
 * System page (系统信息) — Phase 3
 * ============================================================ */

/* Definition-list kv grid used by every system card. */
.kv {
  display: grid;
  grid-template-columns: 160px 1fr;
  gap: var(--s-2) var(--s-4);
  margin: 0;
}
.kv dt {
  color: var(--text-muted);
  font-size: var(--t-sm);
  font-weight: 500;
  padding-top: 2px;
}
.kv dd {
  color: var(--text);
  font-size: var(--t-sm);
  font-family: var(--font-mono);
  word-break: break-all;
  margin: 0;
  display: flex;
  align-items: center;
  gap: var(--s-2);
  flex-wrap: wrap;
}
.kv dd .kv__value--muted {
  color: var(--text-faint);
}
.kv dd .kv__sub {
  color: var(--text-faint);
  font-size: var(--t-xs);
  font-family: var(--font-sans);
}

/* Connection badge (live indicator at the top of the runtime card) */
.conn-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--s-1);
  padding: 2px var(--s-2);
  border-radius: var(--r-full);
  font-size: var(--t-xs);
  font-weight: 500;
  background: var(--surface-2);
  color: var(--text-muted);
  white-space: nowrap;
}
.conn-badge__dot {
  width: 8px;
  height: 8px;
  border-radius: var(--r-full);
  display: inline-block;
}
.conn-badge--ok .conn-badge__dot {
  background: var(--success);
  box-shadow: 0 0 0 0 rgba(34, 197, 94, 0.5);
  animation: pulse 2s ease-in-out infinite;
}
.conn-badge--ok { color: var(--success); }
.conn-badge--connecting { color: var(--warning); }
.conn-badge--connecting .conn-badge__dot {
  background: var(--warning);
  animation: pulse 1s ease-in-out infinite;
}
.conn-badge--err { color: var(--danger); }
.conn-badge--err .conn-badge__dot { background: var(--danger); }
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

/* Health度 section divider */
.health-note {
  margin-top: var(--s-3);
  padding-top: var(--s-3);
  border-top: 1px dashed var(--border);
  font-size: var(--t-xs);
  color: var(--text-faint);
}
.health-note a {
  color: var(--primary);
  text-decoration: none;
}
.health-note a:hover { text-decoration: underline; }

/* External link button (used for pprof) */
.link-btn {
  display: inline-flex;
  align-items: center;
  gap: var(--s-1);
  background: transparent;
  border: 1px solid var(--border);
  color: var(--primary);
  padding: 2px var(--s-2);
  font-size: var(--t-xs);
  border-radius: var(--r-sm);
  cursor: pointer;
  text-decoration: none;
  font-family: var(--font-mono);
  transition: border-color var(--dur-fast) var(--ease);
}
.link-btn:hover { border-color: var(--primary); }

/* Mobile: stack kv columns */
@media (max-width: 640px) {
  .kv { grid-template-columns: 1fr; gap: var(--s-1) 0; }
  .kv dt { padding-top: var(--s-2); }
  .kv dt:first-child { padding-top: 0; }
}
```

- [ ] **Step 2: Verify CSS parses**

```bash
cd c:/Users/Admin/src/wx_web_api
python -c "data=open('web/static/css/pages.css',encoding='utf-8').read(); print('lines:', len(data.splitlines()))"
```
Expected: a line count > 407 (the size after Task 1 of the previous feature; current size should be larger). No errors.

- [ ] **Step 3: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add web/static/css/pages.css
git commit -m "feat(ui/system): add styles for system page (kv grid, conn badge)"
```

---

## Task 5: `system.js` — full implementation

**Files:**
- Modify: `web/static/js/pages/system.js` (full replacement of the 14-line stub)

- [ ] **Step 1: Replace `system.js` with the full implementation**

Replace the entire file `web/static/js/pages/system.js` with the following content:

```js
/* System page (系统信息) — admin-only runtime / version / config / health view.
 * Loads /api/system for static values, opens a WebSocket to /ws/system for
 * live updates every 2s, auto-reconnects with exponential backoff.
 *
 * WebSocket auth: the browser WS API cannot send custom headers, so the
 * session token is passed via the URL query string. The server's SessionAuth
 * middleware already accepts both Authorization header and ?token= query.
 */

(function (global) {
  'use strict';

  var RECONNECT_BASE_MS = 1000;
  var RECONNECT_MAX_MS = 30000;
  var STALE_THRESHOLD_MS = 6000; // > 3 missed ticks => consider connection stale

  var state = {
    staticLoaded: false,
    ws: null,
    reconnectAttempt: 0,
    reconnectTimer: null,
    lastSnapshotTs: 0,
    lastFrameAt: 0,
    connectionStatus: 'connecting' // 'ok' | 'connecting' | 'err'
  };

  function escapeHtml(s) {
    var div = document.createElement('div');
    div.textContent = s == null ? '' : String(s);
    return div.innerHTML;
  }

  function pad2(n) { return n < 10 ? '0' + n : '' + n; }

  function formatBytes(n) {
    if (n == null) return '—';
    if (n < 1024) return n + ' B';
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
    if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB';
    return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB';
  }

  function formatDuration(seconds) {
    if (seconds == null) return '—';
    var d = Math.floor(seconds / 86400);
    var h = Math.floor((seconds % 86400) / 3600);
    var m = Math.floor((seconds % 3600) / 60);
    var s = Math.floor(seconds % 60);
    var parts = [];
    if (d > 0) parts.push(d + 'd');
    parts.push(pad2(h) + ':' + pad2(m) + ':' + pad2(s));
    return parts.join(' ');
  }

  function ago(ms) {
    if (!ms) return '—';
    var diff = Math.max(0, Date.now() - ms);
    if (diff < 1000) return '刚刚';
    if (diff < 60000) return Math.floor(diff / 1000) + 's 前';
    if (diff < 3600000) return Math.floor(diff / 60000) + 'm 前';
    return Math.floor(diff / 3600000) + 'h 前';
  }

  /* ---------- skeleton ---------- */

  function renderSkeleton() {
    return '<div class="card">' +
             '<div class="card__title">运行时</div>' +
             '<dl class="kv" id="sysRuntimeKv"></dl>' +
           '</div>' +
           '<div class="card">' +
             '<div class="card__title">版本</div>' +
             '<dl class="kv" id="sysVersionKv"></dl>' +
           '</div>' +
           '<div class="card">' +
             '<div class="card__title">配置</div>' +
             '<dl class="kv" id="sysConfigKv"></dl>' +
           '</div>' +
           '<div class="card">' +
             '<div class="card__title">健康度</div>' +
             '<dl class="kv" id="sysHealthKv"></dl>' +
             '<div class="health-note">' +
               '性能分析（pprof）: ' +
               '<a href="/debug/pprof/" target="_blank" rel="noopener noreferrer" class="link-btn">/debug/pprof/ ↗</a>' +
               ' <span class="kv__sub">（需要服务端启用 pprof 路由）</span>' +
             '</div>' +
           '</div>';
  }

  function placeholderRow(label, value) {
    return '<dt>' + escapeHtml(label) + '</dt><dd class="kv__value--muted">' + escapeHtml(value) + '</dd>';
  }

  function realRow(label, value, sub) {
    var html = escapeHtml(value);
    if (sub) html += ' <span class="kv__sub">' + escapeHtml(sub) + '</span>';
    return '<dt>' + escapeHtml(label) + '</dt><dd>' + html + '</dd>';
  }

  function connectionBadge() {
    var label;
    if (state.connectionStatus === 'ok') {
      label = '🟢 已连接 (' + ago(state.lastFrameAt) + ')';
    } else if (state.connectionStatus === 'connecting') {
      label = '🟡 连接中...';
    } else {
      label = '🔴 已断开 (重连中...)';
    }
    return '<dt>连接状态</dt><dd>' +
             '<span class="conn-badge conn-badge--' + state.connectionStatus + '">' +
               '<span class="conn-badge__dot"></span>' + label +
             '</span>' +
           '</dd>';
  }

  /* ---------- static fetch ---------- */

  function applyStatic(slot, d) {
    var rt = slot.querySelector('#sysRuntimeKv');
    var ver = slot.querySelector('#sysVersionKv');
    var cfg = slot.querySelector('#sysConfigKv');

    rt.innerHTML =
      connectionBadge() +
      placeholderRow('Go version', d.go_version || '—') +
      placeholderRow('GOOS / GOARCH', (d.goos || '—') + ' / ' + (d.goarch || '—')) +
      placeholderRow('Goroutine 数', '—') +
      placeholderRow('内存 Alloc', '—') +
      placeholderRow('内存 HeapSys', '—') +
      placeholderRow('内存 Sys', '—') +
      placeholderRow('启动时长', '—');

    ver.innerHTML =
      realRow('Build tag', d.build_tag || '—') +
      realRow('编译时间', d.build_time || '—') +
      realRow('Git SHA', d.git_sha || '—');

    cfg.innerHTML =
      realRow('监听端口', String(d.port)) +
      realRow('API base URL', d.api_base_url || '—') +
      realRow('Token 数', String(d.token_count)) +
      realRow('配置文件', d.config_path || '—') +
      realRow('DB 路径', d.db_path || '—') +
      realRow('DB 大小', d.db_size > 0 ? formatBytes(d.db_size) : '—');
  }

  async function loadStatic(slot) {
    try {
      var res = await global.WXApi.authJson('/api/system');
      if (res.data && res.data.code === 0 && res.data.data) {
        applyStatic(slot, res.data.data);
        state.staticLoaded = true;
      } else {
        renderStaticError(slot, (res.data && res.data.msg) || '未知错误');
      }
    } catch (e) {
      renderStaticError(slot, e.message || '网络错误');
    }
  }

  function renderStaticError(slot, msg) {
    var cards = slot.querySelectorAll('.card');
    if (cards[0]) {
      cards[0].insertAdjacentHTML('beforeend',
        '<div class="result-msg">' + escapeHtml('静态字段加载失败: ' + msg) + '</div>');
    }
  }

  /* ---------- snapshot apply ---------- */

  function applySnapshot(slot, snap) {
    state.lastFrameAt = Date.now();
    state.lastSnapshotTs = snap.ts || 0;
    if (state.connectionStatus !== 'ok') {
      state.connectionStatus = 'ok';
      updateConnectionBadge(slot);
    }
    var rt = slot.querySelector('#sysRuntimeKv');
    if (!rt) return;
    // Replace the placeholder rows (everything after the connection badge)
    var rows = [];
    rows.push(connectionBadge());
    rows.push(realRow('Go version', slot.dataset.goVersion || '—'));
    rows.push(realRow('GOOS / GOARCH', slot.dataset.goOsArch || '—'));
    rows.push(realRow('Goroutine 数', String(snap.goroutines != null ? snap.goroutines : '—')));
    rows.push(realRow('内存 Alloc', formatBytes(snap.mem && snap.mem.alloc)));
    rows.push(realRow('内存 HeapSys', formatBytes(snap.mem && snap.mem.heap_sys)));
    rows.push(realRow('内存 Sys', formatBytes(snap.mem && snap.mem.sys)));
    rows.push(realRow('启动时长', formatDuration(snap.uptime_seconds)));
    rt.innerHTML = rows.join('');

    var hk = slot.querySelector('#sysHealthKv');
    if (hk) {
      var healthRows = [];
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
      hk.innerHTML = healthRows.join('');
    }
  }

  function updateConnectionBadge(slot) {
    var rt = slot.querySelector('#sysRuntimeKv');
    if (!rt) return;
    // Replace only the first row (connection badge)
    var firstDt = rt.querySelector('dt');
    if (!firstDt) return;
    var firstRow = connectionBadge();
    var tmp = document.createElement('div');
    tmp.innerHTML = firstRow;
    var firstDd = rt.querySelector('dd');
    if (firstDd) {
      firstDd.outerHTML = tmp.querySelector('dd').outerHTML;
    }
  }

  /* ---------- websocket ---------- */

  function buildWsUrl() {
    var proto = global.location.protocol === 'https:' ? 'wss:' : 'ws:';
    var token = '';
    try { token = localStorage.getItem('wx_token') || ''; } catch (e) { token = ''; }
    return proto + '//' + global.location.host + '/ws/system?token=' + encodeURIComponent(token);
  }

  function connectWS(slot) {
    if (state.ws) {
      try { state.ws.close(); } catch (e) { /* ignore */ }
    }
    if (state.reconnectTimer) {
      clearTimeout(state.reconnectTimer);
      state.reconnectTimer = null;
    }
    state.connectionStatus = 'connecting';
    updateConnectionBadge(slot);

    var url = buildWsUrl();
    var ws;
    try {
      ws = new WebSocket(url);
    } catch (e) {
      scheduleReconnect(slot);
      return;
    }
    state.ws = ws;

    ws.onopen = function () {
      state.reconnectAttempt = 0;
      // first snapshot will set state.connectionStatus = 'ok' via applySnapshot
    };
    ws.onmessage = function (e) {
      try {
        var snap = JSON.parse(e.data);
        if (snap && snap.type === 'snapshot') applySnapshot(slot, snap);
      } catch (err) {
        if (global.console) console.error('system: bad frame', err);
      }
    };
    ws.onerror = function () {
      state.connectionStatus = 'err';
      updateConnectionBadge(slot);
    };
    ws.onclose = function () {
      state.connectionStatus = 'err';
      updateConnectionBadge(slot);
      scheduleReconnect(slot);
    };
  }

  function scheduleReconnect(slot) {
    if (state.reconnectTimer) return;
    var delay = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * Math.pow(2, state.reconnectAttempt));
    state.reconnectAttempt++;
    state.reconnectTimer = setTimeout(function () {
      state.reconnectTimer = null;
      connectWS(slot);
    }, delay);
  }

  /* ---------- stale-frame watchdog ---------- */

  function startWatchdog(slot) {
    setInterval(function () {
      if (state.connectionStatus === 'ok' && state.lastFrameAt > 0) {
        var staleFor = Date.now() - state.lastFrameAt;
        if (staleFor > STALE_THRESHOLD_MS) {
          // Server missed > 3 ticks; treat as disconnect and force reconnect
          if (state.ws) {
            try { state.ws.close(); } catch (e) { /* ignore */ }
          }
        }
      }
    }, 2000);
  }

  /* ---------- boot ---------- */

  function render(slot) {
    slot.innerHTML = renderSkeleton();
    loadStatic(slot);
    connectWS(slot);
    startWatchdog(slot);
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.system = { render: render };
})(window);
```

- [ ] **Step 2: Verify the file replaced**

```bash
cd c:/Users/Admin/src/wx_web_api
wc -l web/static/js/pages/system.js
```
Expected: ~290 lines (the file is large because of the 4-card render skeleton and the various helper functions).

- [ ] **Step 3: Rebuild the Go binary**

```bash
cd c:/Users/Admin/src/wx_web_api
go build -o dist/wx_web_api.exe .
```
Expected: build succeeds. The new `system.js` is now embedded.

- [ ] **Step 4: Commit**

```bash
cd c:/Users/Admin/src/wx_web_api
git add web/static/js/pages/system.js
git commit -m "feat(ui/system): implement system page (cards, WS client, reconnect)"
```

---

## Task 6: Visual smoke test + bug fixes

**Files:** none initially — fix anything the smoke test reveals by editing `test.js` and/or `pages.css`.

- [ ] **Step 1: Walk through the spec scenarios**

Start `dist/wx_web_api.exe` and walk through each of these scenarios. For each, report ✅/❌ and a one-line note. Take screenshots at major milestones.

| # | Scenario | Expected | Verified? |
|---|---|---|---|
| 1 | Load `/system` while logged in | 4 cards render with "—" or default values for runtime fields; static fields populated (build_tag=dev, go_version=go1.25.6, port=13335, etc.); connection badge shows "🟡 连接中..." then "🟢 已连接 (刚刚)" within ~100ms | ☐ |
| 2 | Wait 10 seconds on `/system` | Goroutine 数, 内存 fields, 启动时长 all update at least 3 times; connection badge says "(Ns 前)" with N growing then resetting each tick | ☐ |
| 3 | Stop the server (kill the binary) | Within ~6s the connection badge changes to "🔴 已断开 (重连中...)"; the watchdog forces reconnect attempts; runtime fields stop updating | ☐ |
| 4 | Restart the server | Connection badge transitions to "🟡 连接中..." then "🟢 已连接"; runtime fields start updating again | ☐ |
| 5 | Logout and back in (token changes) | New token is used in the WS URL on next reconnect; if you reload the page mid-session, the new token is picked up | ☐ |
| 6 | Open `/system` in two browser tabs | Both tabs receive updates; both badges show "🟢 已连接"; restarting the server causes both to reconnect | ☐ |
| 7 | Resize to mobile (≤ 640px) | `.kv` collapses to single column; badges wrap; health-note still readable | ☐ |
| 8 | Click the pprof link | Opens `/debug/pprof/` in a new tab; currently 404 (expected, documented as out-of-scope); tooltip "需要服务端启用 pprof 路由" visible | ☐ |
| 9 | Verify Token Count and API base URL | Token 数 matches `cfg.Tokens.length`; API base URL matches `cfg.ApiBaseUrl` | ☐ |
| 10 | Verify DB size | If `dist/wx_web_api.db` exists, DB 大小 shows non-zero (e.g., "12.0 KB"); if missing, shows "—" | ☐ |
| 11 | Build the binary with ldflags overrides | `go build -ldflags "-X wx_web_api/internal/buildinfo.BuildTag=v1.2.3 -X wx_web_api/internal/buildinfo.GitSHA=abc1234" -o dist/wx_web_api.exe .` — verify the page shows the new values | ☐ |
| 12 | Run with invalid session token (e.g., clear `localStorage['wx_token']` in DevTools, then reload) | Login overlay appears; `/api/system` never resolves (401 from SessionAuth); `/ws/system` connection is rejected; no console errors | ☐ |

- [ ] **Step 2: Fix any failures found**

For each row that fails, edit `test.js` or `pages.css` to match the spec. No new design choices — this is a defect-fix pass. Commit each fix individually.

- [ ] **Step 3: Commit any fix(es)**

If fixes were needed:
```bash
cd c:/Users/Admin/src/wx_web_api
git add web/static/js/pages/system.js web/static/css/pages.css
git commit -m "fix(ui/system): address smoke test findings (see notes)"
```

If no fixes were needed, skip this step.

---

## Self-review

**Spec coverage:**
- §"改动文件清单" → Tasks 1 (go.mod), 2 (buildinfo, system.go partial, main.go partial), 3 (broadcaster.go, system.go completion, main.go), 4 (pages.css), 5 (system.js). 6 (smoke test).
- §"页面布局" → 4 cards in `renderSkeleton()` (运行时 / 版本 / 配置 / 健康度).
- §"数据流" → `loadStatic` (one-shot GET), `connectWS` + `applySnapshot` (push), reconnect with exponential backoff.
- §"WebSocket 鉴权" → query string token via `buildWsUrl()`.
- §"健康度统计的依赖" → `snap.stats == null` branch shows "—" + explanation.
- §"后端实现要点" → `SystemData` struct, `GetSystem` handler, `HandleSystemWS` handler, `upgrader`, `processStart` (set via `var ... = time.Now()` at package init in `system.go`).
- §"internal/handler/broadcaster.go" → `systemHub` + `Start` + `collectSnapshot` + per-client goroutine writes.
- §"前端实现要点" → `render` + `loadStatic` + `connectWS` + exponential backoff.
- §"kv 表格" → `<dl class="kv">` markup, `.kv` CSS.
- §"连接状态徽章" → 3 states (ok / err / connecting) via `.conn-badge` + modifier classes.
- §"内存显示" → `formatBytes` helper.
- §"错误处理" → 5 rows covered: GET failure → "加载失败" message, WS unreachable → reconnect, WS 401 → silent (we can't distinguish from `onerror`), pprof 404 → known limitation, pprof tooltip → in the health-note.

**Placeholder scan:** No "TBD" / "TODO" / "implement later" in the code blocks.

**Type consistency:**
- `SystemData` (Go) ↔ applied as keys in `applyStatic` JS (build_tag, build_time, git_sha, go_version, goos, goarch, config_path, db_path, db_size, port, api_base_url, token_count) — match.
- `MemStats` (Go) ↔ `snap.mem.{alloc, heap_sys, sys}` in JS — match.
- `SystemSnapshot` (Go) ↔ `{type, ts, uptime_seconds, goroutines, mem, stats}` in JS — match.
- `ReqStats` (Go) ↔ `{total, today, errors}` in JS — match.
- The `processStart` var is in `internal/handler/system.go` (not `broadcaster.go` as the original spec said). This was an arbitrary choice during writing; both work. The plan uses the `system.go` location. If the spec reviewer prefers `broadcaster.go`, swap the line — it's mechanical.

**Gaps:**
- The spec mentions pprof as a "static" link with a "known limitation" note. The plan implements exactly that (renders the link with a tooltip; doesn't add a server-side `/debug/pprof/` route).
- The spec says the broadcaster is a "single goroutine" — the plan uses per-client write goroutines inside the tick loop, which is the standard pattern. The spec's "single goroutine" is satisfied (the tick loop is one goroutine); per-client writes are auxiliary.
- The "stale-frame watchdog" in `startWatchdog` is not in the spec but is a sensible defensive measure (a WS that thinks it's open but receives no frames for 6s is treated as dead). Documented in code comments.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-10-system-page.md`. Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints for review.

Which approach?
