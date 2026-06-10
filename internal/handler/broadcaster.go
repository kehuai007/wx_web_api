package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"wx_web_api/internal/config"
	"wx_web_api/internal/storage"

	"github.com/gorilla/websocket"
)

// MemStats 是 system.snapshot 中 mem 字段的 JSON 形态。
type MemStats struct {
	Alloc   uint64 `json:"alloc"`
	HeapSys uint64 `json:"heap_sys"`
	Sys     uint64 `json:"sys"`
}

// ReqStats 是 system.snapshot 中 stats 字段的 JSON 形态。
// Stats 为 nil 时表示 request_log 尚未就绪(前端应显示 "—")。
type ReqStats struct {
	Total  int64 `json:"total"`
	Today  int64 `json:"today"`
	Errors int64 `json:"errors"`
}

// SystemSnapshot 是 system.snapshot 帧的 wire shape。
type SystemSnapshot struct {
	Type          string    `json:"type"`
	Ts            int64     `json:"ts"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	Goroutines    int       `json:"goroutines"`
	Mem           MemStats  `json:"mem"`
	Stats         *ReqStats `json:"stats"`
}

// upgrader 是 process 内共享的 gorilla websocket upgrader。
// CheckOrigin 全开,因为是管理工具 + 已有 SessionAuth。
var upgrader = websocket.Upgrader{ //nolint:gochecknoglobals
	CheckOrigin: func(r *http.Request) bool { return true },
}

// processStart 在 package init 时记录,用于 uptime 计算。
var processStart = time.Now() //nolint:gochecknoglobals

// systemTickerInterval 可被测试覆盖为更小值,加速 systemTickerLoop 的测试。
// 生产保持 2s。
var systemTickerInterval = 2 * time.Second //nolint:gochecknoglobals

// eventClient 是单个 WS 连接的运行时表示。
//   - conn:    gorilla websocket 连接,写操作只在 runClientWriter 中发生
//   - writeCh: 容量 64 的帧缓冲;fanout 在缓冲满时主动 close 客户端
//   - done:    关闭时由 unregister 关闭,通知 runClientWriter 退出
//   - closeFn: 由 register 设置,捕获 c + h,用于 fanout 慢客户端检测时调 unregister
type eventClient struct {
	conn    *websocket.Conn
	writeCh chan []byte
	done    chan struct{}
	closeFn func()
}

// eventsHub 是进程内唯一的 WS 事件多路复用器。
// 拥有:
//   - clients 客户端注册表
//   - logCh / logDelCh / configCh 三个事件类型的入队通道
//   - storage 引用(供 system.snapshot 使用)
// Start 启动四个后台 fanout 协程,生命周期跟随 ctx。
type eventsHub struct {
	mu       sync.RWMutex
	clients  map[*eventClient]time.Time
	logCh    chan storage.RequestLog
	logDelCh chan []int64
	configCh chan struct{}
	storage  *storage.Storage
}

// EventsHub 是 main.go 启动时使用的包级单例。
var EventsHub = &eventsHub{ //nolint:gochecknoglobals
	clients:  make(map[*eventClient]time.Time),
	logCh:    make(chan storage.RequestLog, 256),
	logDelCh: make(chan []int64, 64),
	configCh: make(chan struct{}, 16),
}

// register 把 conn 加入 hub,启动其写协程,返回 *eventClient。
// 调用方负责在 conn 关闭时调 unregister(通常用 defer)。
func (h *eventsHub) register(conn *websocket.Conn) *eventClient {
	c := &eventClient{
		conn:    conn,
		writeCh: make(chan []byte, 64),
		done:    make(chan struct{}),
	}
	c.closeFn = func() { h.unregister(c) }
	h.mu.Lock()
	h.clients[c] = time.Now()
	h.mu.Unlock()
	go h.runClientWriter(c)
	return c
}

// unregister 幂等:首次调用会从 map 删除并关闭 conn + done 通道;之后调用 no-op。
func (h *eventsHub) unregister(c *eventClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	_ = c.conn.Close()
	close(c.done)
}

// runClientWriter 是单 client 的写协程:从 writeCh 读帧 → 写 conn。
// done 关闭时退出。
func (h *eventsHub) runClientWriter(c *eventClient) {
	for {
		select {
		case frame, ok := <-c.writeCh:
			if !ok {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, frame); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

// fanout 把 frame 推给所有注册的 client。慢客户端(writeCh 满)会被主动踢出。
// fanout 必须在四类事件协程内调用——不是 public API。
func (h *eventsHub) fanout(frame []byte) {
	h.mu.RLock()
	clients := make([]*eventClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		select {
		case c.writeCh <- frame:
		default:
			log.Printf("[events] client writeCh full, closing client")
			if c.closeFn != nil {
				c.closeFn()
			}
		}
	}
}

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
	if h.storage != nil {
		total, _ := h.storage.Count()
		since, _ := h.storage.CountSince(storage.StartOfTodayMs())
		errs, _ := h.storage.CountErrors()
		snap.Stats = &ReqStats{Total: total, Today: since, Errors: errs}
	}
	return snap
}

// Snapshot 是 HandleEventsWS 在收到 client.hello 时调用的 public 接口。
// 返回当前 SystemSnapshot(结构体),由调用方自己 WriteJSON。
func (h *eventsHub) Snapshot() SystemSnapshot {
	return h.collectSnapshot()
}

// PublishLog 在 writeLog 内部(DB 写完)调用,缓冲满丢弃,绝不阻塞业务路径。
func (h *eventsHub) PublishLog(r storage.RequestLog) {
	select {
	case h.logCh <- r:
	default:
		log.Printf("[events] logCh full, drop log id=%d", r.ID)
	}
}

// PublishLogDeleted 在 DeleteHistory 成功后调用。
// ids == nil  → 全清信号(JSON 编码为 null,前端据此清空列表)
// ids == []int64{} → no-op(显式传空 slice 不发)
// ids 非空     → 正常发送
func (h *eventsHub) PublishLogDeleted(ids []int64) {
	if ids != nil && len(ids) == 0 {
		return
	}
	select {
	case h.logDelCh <- ids:
	default:
		log.Printf("[events] logDelCh full, drop %d ids", len(ids))
	}
}

// PublishConfigChanged 在 UpdateConfig 保存成功后调用。
func (h *eventsHub) PublishConfigChanged() {
	select {
	case h.configCh <- struct{}{}:
	default:
		log.Printf("[events] configCh full, drop config.changed")
	}
}

// Start 启动所有 fanout 协程,生命周期跟随 ctx。
// 包含 4 个协程:
//   - systemTickerLoop:   周期触发,广播 system.snapshot
//   - logFanoutLoop:      从 logCh 读,广播 log.new
//   - logDelFanoutLoop:   从 logDelCh 读,广播 log.deleted
//   - configFanoutLoop:   从 configCh 读,广播 config.changed
// 另起一个 runRetentionLoop 协程(从旧 systemHub 继承,清理老数据)。
func (h *eventsHub) Start(ctx context.Context, s *storage.Storage) {
	h.storage = s
	go h.systemTickerLoop(ctx)
	go h.logFanoutLoop(ctx)
	go h.logDelFanoutLoop(ctx)
	go h.configFanoutLoop(ctx)
	go h.runRetentionLoop(ctx)
}

// systemTickerLoop 每 systemTickerInterval 推一帧 system.snapshot。
func (h *eventsHub) systemTickerLoop(ctx context.Context) {
	ticker := time.NewTicker(systemTickerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := h.collectSnapshot()
			data, err := json.Marshal(snap)
			if err != nil {
				log.Printf("[events] marshal snapshot: %v", err)
				continue
			}
			h.fanout(data)
		}
	}
}

// logFanoutLoop 把 RequestLog 包装成 log.new 帧广播。
func (h *eventsHub) logFanoutLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-h.logCh:
			frame := struct {
				Type string             `json:"type"`
				Log  storage.RequestLog `json:"log"`
			}{Type: "log.new", Log: r}
			data, err := json.Marshal(frame)
			if err != nil {
				log.Printf("[events] marshal log.new: %v", err)
				continue
			}
			h.fanout(data)
		}
	}
}

// logDelFanoutLoop 把 ids 包装成 log.deleted 帧广播。
// ids 为 nil 时 JSON 编码为 null,前端据此清空列表。
func (h *eventsHub) logDelFanoutLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ids := <-h.logDelCh:
			data, err := json.Marshal(map[string]any{
				"type": "log.deleted",
				"ids":  ids,
			})
			if err != nil {
				log.Printf("[events] marshal log.deleted: %v", err)
				continue
			}
			h.fanout(data)
		}
	}
}

// configFanoutLoop 推 config.changed 帧(无 payload,仅 type + ts)。
func (h *eventsHub) configFanoutLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.configCh:
			data, err := json.Marshal(map[string]any{
				"type": "config.changed",
				"ts":   time.Now().Unix(),
			})
			if err != nil {
				log.Printf("[events] marshal config.changed: %v", err)
				continue
			}
			h.fanout(data)
		}
	}
}

// runRetentionLoop 沿用旧 systemHub:每天 03:00 清理早于 retention 天的记录。
// 首次运行延迟 60s 让配置刚改完能立即见效。
func (h *eventsHub) runRetentionLoop(ctx context.Context) {
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			h.runRetentionOnce()
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			timer.Reset(time.Until(next))
		}
	}
}

func (h *eventsHub) runRetentionOnce() {
	if h.storage == nil {
		return
	}
	cfg := config.Get()
	if cfg.HistoryRetentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(cfg.HistoryRetentionDays) * 24 * time.Hour).UnixMilli()
	n, err := h.storage.PurgeOlderThan(cutoff)
	if err != nil {
		log.Printf("[retention] purge failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("[retention] purged %d records older than %d days", n, cfg.HistoryRetentionDays)
	}
}
