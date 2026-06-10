package handler

import (
	"context"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"wx_web_api/internal/config"
	"wx_web_api/internal/storage"

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
	Total  int64 `json:"total"`
	Today  int64 `json:"today"`
	Errors int64 `json:"errors"`
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

// collectSnapshot builds the SystemSnapshot for the current moment. Stats
// are pulled from storage if available; otherwise nil (the frontend shows
// "—" for that field and an explanatory note).
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
		// StartOfTodayMs lives in the storage package; reuse it so /api/system's
		// "today" and /api/history?range=today agree to the exact same instant.
		since, _ := s.CountSince(storage.StartOfTodayMs())
		errs, _ := s.CountErrors()
		snap.Stats = &ReqStats{Total: total, Today: since, Errors: errs}
	}
	return snap
}
