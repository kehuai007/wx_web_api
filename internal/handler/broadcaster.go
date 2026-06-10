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
