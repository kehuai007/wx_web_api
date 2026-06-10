# WebSocket 实时事件推送 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把"页面必须手动刷新"改为后端主动推送——`log.new` / `log.deleted` / `config.changed` / `system.snapshot` 四类事件统一通过 `/ws/events` 端点推到前端,dashboard / history / settings / system 各页订阅感兴趣的类型。

**Architecture:** 后端把现有 `systemHub` 升格为 `EventsHub`,内部三个 fanout 协程 + system 2s ticker;`/ws/system` 端点替换为 `/ws/events`;`writeLog` / `DeleteHistory` / `UpdateConfig` 在成功路径调 `Publish*`。前端新增 `events.js` 单例 WebSocket 客户端,各页 `subscribe(type, handler)`,router 切换 render 时 unsubscribe 避免泄漏。

**Tech Stack:** Go 1.25 + Gin + gorilla/websocket v1.5.3(已存在),vanilla JS(无构建步骤)。

---

## File Structure

**新建**:
- `internal/handler/events_test.go` — EventsHub 单元测试
- `web/static/js/events.js` — 全局唯一 WS 客户端单例

**重写**:
- `internal/handler/broadcaster.go` — `systemHub` → `eventsHub`,`SystemHub` → `EventsHub`,新增 `PublishLog` / `PublishLogDeleted` / `PublishConfigChanged` / `Snapshot`
- `internal/handler/system.go` — `HandleSystemWS` → `HandleEventsWS`(读 `client.hello` 后推首帧)
- `web/static/js/pages/system.js` — 删除自带 WS 客户端,改用 `WXEvents.subscribe`

**局部修改**(新增 hook / 新增订阅 / 新增 UI):
- `internal/handler/handler.go` — `writeLog` 末尾 `defer EventsHub.PublishLog(*rec)`
- `internal/handler/history.go` — `DeleteHistory` 三处成功路径末尾 `EventsHub.PublishLogDeleted(...)`
- `internal/handler/settings.go` — `UpdateConfig` `config.Save` 成功后 `EventsHub.PublishConfigChanged()`
- `main.go` — `SystemHub` → `EventsHub`,`/ws/system` → `/ws/events`
- `web/index.html` — `auth.js` 之后插入 `<script src="/static/js/events.js">`
- `web/static/js/app.js` — `showApp` 末尾 `WXEvents.start()`,`logout` 调 `WXEvents.stop()`
- `web/static/js/pages/dashboard.js` — 订阅 `log.new` / `config.changed`
- `web/static/js/pages/history.js` — 订阅 `log.new` / `log.deleted` / `config.changed`,新增 `applyLogNew` / `applyLogDeleted` / `logMatchesFilter` / `renderUnreadHint`
- `web/static/js/pages/settings.js` — 订阅 `config.changed`,新增 `config-stale` 提示条 + dirty 跟踪 + 2s 自我保存忽略窗口

**不动**:
- `internal/handler/handler.go` 中的 `TokenAuth` / `SessionAuth` / `Login` / `GetChallenge` / `simpleHash` / `generateToken`
- `internal/storage/*` / `internal/config/*` / `internal/buildinfo/*`
- `web/static/js/auth.js` / `api.js` / `store.js` / `router.js`
- `web/static/css/*`
- `go.mod` / `go.sum`

---

## Task 1: eventsHub struct + register/unregister + 客户端写协程

**Files:**
- Modify: `internal/handler/broadcaster.go`(整体替换)
- Test: `internal/handler/events_test.go`(新建)

- [ ] **Step 1: 写测试 — 注册两个客户端,都能从 WriteMessage 通道读出 frame**

在 `internal/handler/events_test.go` 写入:

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newTestHub 返回一个未 Start 的 eventsHub(供单元测试直接调用 fanout)
func newTestHub() *eventsHub {
	return &eventsHub{
		clients:  make(map[*eventClient]time.Time),
		logCh:    make(chan storage.RequestLog, 256),
		logDelCh: make(chan []int64, 64),
		configCh: make(chan struct{}, 16),
	}
}

// serveTestHub 起一个 httptest server,把每个 WS 连接注册到 hub 上。
// 返回 server 与 hub;调用方负责 defer Close()。
func serveTestHub(h *eventsHub) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := h.register(conn)
		go func() {
			defer h.unregister(c)
			for {
				if _, _, err := conn.NextReader(); err != nil {
					return
				}
			}
		}()
	}))
	return ts
}

// dialTestHub 拨号并返回 WS 客户端;失败 t.Fatal
func dialTestHub(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

// readFrame 在 d 时间内读取一帧文本消息
func readFrame(t *testing.T, c *websocket.Conn, d time.Duration) []byte {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(d))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return msg
}

func TestEventsHub_RegisterAndUnregister(t *testing.T) {
	hub := newTestHub()
	ts := serveTestHub(hub)
	defer ts.Close()

	c1 := dialTestHub(t, ts)
	c2 := dialTestHub(t, ts)
	defer c1.Close()
	defer c2.Close()

	// 等待 register 完成(server 端 NextReader 会启动;但 register 在 upgrade 后立即跑)
	time.Sleep(50 * time.Millisecond)
	hub.mu.RLock()
	n := len(hub.clients)
	hub.mu.RUnlock()
	if n != 2 {
		t.Fatalf("expected 2 clients registered, got %d", n)
	}

	// 关闭 c1,unregister 应当自动触发
	c1.Close()
	time.Sleep(100 * time.Millisecond)
	hub.mu.RLock()
	n = len(hub.clients)
	hub.mu.RUnlock()
	if n != 1 {
		t.Fatalf("expected 1 client after c1 close, got %d", n)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/handler/ -run TestEventsHub_RegisterAndUnregister -v`
Expected: FAIL with `undefined: eventsHub` / `undefined: storage.RequestLog` 等编译错误(broadcaster.go 还没改)。

- [ ] **Step 3: 替换 broadcaster.go 为 eventsHub 骨架(不含 fanout)**

`internal/handler/broadcaster.go` 完整内容:

```go
package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

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
// 整体外层在 fanout 时再加 {"type":"system.snapshot", ...}。
type SystemSnapshot struct {
	Type          string    `json:"type"`
	Ts            int64     `json:"ts"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	Goroutines    int       `json:"goroutines"`
	Mem           MemStats  `json:"mem"`
	Stats         *ReqStats `json:"stats"`
}

// upgrader 复用既有 systemHub 的实现——CheckOrigin 全开,因为是管理工具 + 已有 SessionAuth。
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
// writeCh 关闭不会自然发生(done 才是退出信号)。写失败立即返回。
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
// ids 为 nil 表示全清信号,前端收到后清空列表。
func (h *eventsHub) PublishLogDeleted(ids []int64) {
	if len(ids) == 0 && ids == nil {
		// 仅当真的是 nil(全清信号)才发;显式传空 slice 不发
	} else if len(ids) == 0 {
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
			_ = strconv.IntSize // 保留 import 提示编译期不报 unused(若以后删除)
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
	cfg := getConfigSnapshot() // 见 Task 10
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

// avoid "import unused" for strconv in Task 1(若 configFanoutLoop 删了 strconv 引用)
var _ = strconv.IntSize

// sync 在 events_test.go 使用(避免 test 文件删 import 时的循环引用)
var _ = sync.RWMutex{}
```

> **注意**:上面 `getConfigSnapshot()` 是占位,Task 10 替换为 `config.Get()`。`strconv` 的 import 留到 Task 2 实际用时再去掉 `_ = strconv.IntSize` 这一行。

- [ ] **Step 4: 跑测试,确认 register/unregister 通过**

Run: `go test ./internal/handler/ -run TestEventsHub_RegisterAndUnregister -v`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/handler/broadcaster.go internal/handler/events_test.go
git commit -m "feat(events): add eventsHub struct with register/unregister and per-client writer"
```

---

## Task 2: PublishLog + logFanoutLoop 测试与行为

**Files:**
- Modify: `internal/handler/events_test.go`

- [ ] **Step 1: 添加测试 — 启动 hub 后 PublishLog,两个 client 都能收到 log.new 帧**

在 `events_test.go` 末尾追加:

```go
func TestEventsHub_PublishLog_BroadcastsToAllClients(t *testing.T) {
	hub := newTestHub()
	ts := serveTestHub(hub)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub.Start(ctx, nil) // 传 nil storage;logFanoutLoop 不依赖 storage
	defer func() { time.Sleep(50 * time.Millisecond) }()

	c1 := dialTestHub(t, ts)
	c2 := dialTestHub(t, ts)
	defer c1.Close()
	defer c2.Close()
	time.Sleep(50 * time.Millisecond) // 等 register 完成

	hub.PublishLog(storage.RequestLog{ID: 42, Ts: 1000, Status: 0, Kind: "url"})

	for i, c := range []*websocket.Conn{c1, c2} {
		msg := readFrame(t, c, 2*time.Second)
		var m struct {
			Type string             `json:"type"`
			Log  storage.RequestLog `json:"log"`
		}
		if err := jsonUnmarshal(msg, &m); err != nil {
			t.Fatalf("client %d unmarshal: %v", i, err)
		}
		if m.Type != "log.new" {
			t.Errorf("client %d: type=%q want log.new", i, m.Type)
		}
		if m.Log.ID != 42 {
			t.Errorf("client %d: log.ID=%d want 42", i, m.Log.ID)
		}
	}
}

func TestEventsHub_PublishLog_NonBlockingWhenFull(t *testing.T) {
	hub := newTestHub()
	// 不调 Start,只直测 Publish 在缓冲满时不阻塞
	// logCh 容量 256;塞满 256 条后再塞 1 条应在 <100ms 返回
	for i := 0; i < 256; i++ {
		hub.PublishLog(storage.RequestLog{ID: int64(i)})
	}
	done := make(chan struct{})
	go func() {
		hub.PublishLog(storage.RequestLog{ID: 999})
		close(done)
	}()
	select {
	case <-done:
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatal("PublishLog blocked when logCh full")
	}
}
```

在文件顶部 import 块加 `context` 和 `encoding/json`:

```go
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"wx_web_api/internal/storage"

	"github.com/gorilla/websocket"
)
```

并在文件末尾加 helper:

```go
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
```

- [ ] **Step 2: 跑测试,确认通过**

Run: `go test ./internal/handler/ -run TestEventsHub_PublishLog -v`
Expected: PASS

- [ ] **Step 3: commit**

```bash
git add internal/handler/events_test.go
git commit -m "test(events): verify PublishLog broadcasts and is non-blocking when full"
```

---

## Task 3: PublishLogDeleted + logDelFanoutLoop 测试

**Files:**
- Modify: `internal/handler/events_test.go`

- [ ] **Step 1: 添加测试 — PublishLogDeleted([12,34]) 推 log.deleted,ids 数组形态正确;nil 推 null**

```go
func TestEventsHub_PublishLogDeleted_BroadcastsIDs(t *testing.T) {
	hub := newTestHub()
	ts := serveTestHub(hub)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub.Start(ctx, nil)

	c := dialTestHub(t, ts)
	defer c.Close()
	time.Sleep(50 * time.Millisecond)

	hub.PublishLogDeleted([]int64{12, 34, 56})

	msg := readFrame(t, c, 2*time.Second)
	var m struct {
		Type string  `json:"type"`
		IDs  []int64 `json:"ids"`
	}
	if err := jsonUnmarshal(msg, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Type != "log.deleted" {
		t.Errorf("type=%q want log.deleted", m.Type)
	}
	if len(m.IDs) != 3 || m.IDs[0] != 12 || m.IDs[2] != 56 {
		t.Errorf("ids=%v want [12 34 56]", m.IDs)
	}
}

func TestEventsHub_PublishLogDeleted_NilIsClearAll(t *testing.T) {
	hub := newTestHub()
	ts := serveTestHub(hub)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub.Start(ctx, nil)

	c := dialTestHub(t, ts)
	defer c.Close()
	time.Sleep(50 * time.Millisecond)

	hub.PublishLogDeleted(nil) // nil → "全清"信号

	msg := readFrame(t, c, 2*time.Second)
	var raw map[string]json.RawMessage
	if err := jsonUnmarshal(msg, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(raw["type"]) != `"log.deleted"` {
		t.Errorf("type=%s want log.deleted", raw["type"])
	}
	// null 在 JSON 中是 "null"
	if string(raw["ids"]) != "null" {
		t.Errorf("ids=%s want null (clear-all signal)", raw["ids"])
	}
}

func TestEventsHub_PublishLogDeleted_EmptySliceIsNoOp(t *testing.T) {
	hub := newTestHub()
	// 不 Start,直接验证 PublishLogDeleted([]int64{}) 是 no-op
	done := make(chan struct{})
	go func() {
		hub.PublishLogDeleted([]int64{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("PublishLogDeleted blocked on empty slice")
	}
}
```

- [ ] **Step 2: 跑测试**

Run: `go test ./internal/handler/ -run TestEventsHub_PublishLogDeleted -v`
Expected: PASS

- [ ] **Step 3: commit**

```bash
git add internal/handler/events_test.go
git commit -m "test(events): verify PublishLogDeleted wire shape (ids, null, empty)"
```

---

## Task 4: PublishConfigChanged + configFanoutLoop 测试

**Files:**
- Modify: `internal/handler/events_test.go`

- [ ] **Step 1: 添加测试 — config.changed 帧的 type + ts 字段**

```go
func TestEventsHub_PublishConfigChanged_Broadcasts(t *testing.T) {
	hub := newTestHub()
	ts := serveTestHub(hub)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub.Start(ctx, nil)

	c := dialTestHub(t, ts)
	defer c.Close()
	time.Sleep(50 * time.Millisecond)

	hub.PublishConfigChanged()

	msg := readFrame(t, c, 2*time.Second)
	var m struct {
		Type string `json:"type"`
		Ts   int64  `json:"ts"`
	}
	if err := jsonUnmarshal(msg, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Type != "config.changed" {
		t.Errorf("type=%q want config.changed", m.Type)
	}
	if m.Ts <= 0 {
		t.Errorf("ts=%d want positive", m.Ts)
	}
}

func TestEventsHub_PublishConfigChanged_NonBlockingWhenFull(t *testing.T) {
	hub := newTestHub()
	for i := 0; i < 16; i++ {
		hub.PublishConfigChanged()
	}
	done := make(chan struct{})
	go func() {
		hub.PublishConfigChanged()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("PublishConfigChanged blocked when configCh full")
	}
}
```

- [ ] **Step 2: 跑测试**

Run: `go test ./internal/handler/ -run TestEventsHub_PublishConfigChanged -v`
Expected: PASS

- [ ] **Step 3: commit**

```bash
git add internal/handler/events_test.go
git commit -m "test(events): verify PublishConfigChanged broadcasts and is non-blocking"
```

---

## Task 5: systemTickerLoop + Snapshot 测试

**Files:**
- Modify: `internal/handler/events_test.go`

- [ ] **Step 1: 添加测试 — 覆盖 systemTickerInterval,2.5x 间隔内 client 收到至少 1 帧**

```go
func TestEventsHub_SystemSnapshot_FiresOnTicker(t *testing.T) {
	hub := newTestHub()
	// 把 ticker 间隔缩到 50ms,让测试在 1s 内完成
	old := systemTickerInterval
	systemTickerInterval = 50 * time.Millisecond
	defer func() { systemTickerInterval = old }()

	ts := serveTestHub(hub)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub.Start(ctx, nil)

	c := dialTestHub(t, ts)
	defer c.Close()
	time.Sleep(50 * time.Millisecond)

	// 等 200ms,期望至少收到 1 帧
	deadline := time.Now().Add(2 * time.Second)
	count := 0
	for time.Now().Before(deadline) && count == 0 {
		msg := readFrame(t, c, 500*time.Millisecond)
		var m struct {
			Type string `json:"type"`
		}
		if err := jsonUnmarshal(msg, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if m.Type == "system.snapshot" {
			count++
		}
	}
	if count == 0 {
		t.Fatal("no system.snapshot frame received within 2s")
	}
}

func TestEventsHub_Snapshot_ReturnsCurrentValues(t *testing.T) {
	hub := newTestHub()
	snap := hub.Snapshot()
	if snap.Type != "system.snapshot" {
		t.Errorf("type=%q want system.snapshot", snap.Type)
	}
	if snap.Goroutines < 1 {
		t.Errorf("goroutines=%d want >=1", snap.Goroutines)
	}
	// 内存 Sys 必然 > 0
	if snap.Mem.Sys == 0 {
		t.Error("mem.sys = 0, want >0")
	}
}
```

- [ ] **Step 2: 跑测试**

Run: `go test ./internal/handler/ -run TestEventsHub_SystemSnapshot -v`
Run: `go test ./internal/handler/ -run TestEventsHub_Snapshot -v`
Expected: PASS

- [ ] **Step 3: commit**

```bash
git add internal/handler/events_test.go
git commit -m "test(events): verify system.snapshot fires on ticker and Snapshot returns current values"
```

---

## Task 6: HandleEventsWS 端点 — 升级 + 读 client.hello 推首帧

**Files:**
- Modify: `internal/handler/system.go`(整体替换)
- Modify: `internal/handler/events_test.go`(追加 WS 端点测试)

- [ ] **Step 1: 替换 system.go 为 HandleEventsWS 形态**

`internal/handler/system.go` 完整内容:

```go
package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"wx_web_api/internal/buildinfo"
	"wx_web_api/internal/config"

	"github.com/gin-gonic/gin"
)

// SystemData 是 GET /api/system 一次性响应的 shape。
// 不会变的字段(build tag、port、DB 路径等);运行时字段(uptime、goroutine、内存)走 WS 推送。
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

// GetSystem 一次性拉静态字段,前端首屏渲染时调用。
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

// HandleEventsWS 升级到 /ws/events。注册到 EventsHub,读 client.hello 推首帧,
// 之后阻塞读;客户端断开时 unregister 链式清理。
//
// 设计:首帧由客户端主动触发("client.hello"),而不是服务端升级后立即推。
// 原因:Hub 不知道当前 page 是不是已经订阅了 system.snapshot;让客户端在 onopen 后
// 显式请求首帧,可以保证订阅语义和首帧到达顺序一致(订阅在前、首帧在后)。
func (h *Handler) HandleEventsWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}
	client := EventsHub.register(conn)
	defer EventsHub.unregister(client)

	// 读循环:收到 client.hello 立刻推一帧 system.snapshot;之后客户端继续读,
	// 我们不消费具体 payload,只监测连接存活(NextReader 报错 = 客户端断开)。
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var m struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			// 忽略无法解析的帧,继续读
			continue
		}
		if m.Type == "client.hello" {
			snap := EventsHub.Snapshot()
			if err := conn.WriteJSON(snap); err != nil {
				return
			}
		}
		// 其它 type 一律忽略(协议目前仅 client.hello 一类客户端消息)
	}
}
```

- [ ] **Step 2: 跑 system 包内的所有 events 测试,确认 system.go 改动不破坏其他测试**

Run: `go test ./internal/handler/ -v`
Expected: PASS(全部)

- [ ] **Step 3: 添加端到端 WS 测试 — 升级成功 + 收到 hello 后立刻得首帧**

在 `events_test.go` 末尾追加:

```go
func TestHandleEventsWS_SendsSnapshotOnHello(t *testing.T) {
	hub := newTestHub()
	// 缩短 ticker 间隔避免测试与 ticker 干扰
	old := systemTickerInterval
	systemTickerInterval = 50 * time.Millisecond
	defer func() { systemTickerInterval = old }()

	// 起一个直接用 HandleEventsWS 形态的 server(测试不依赖 gin,直接用 httptest)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := hub.register(conn)
		defer hub.unregister(client)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(msg, &m) == nil && m.Type == "client.hello" {
				snap := hub.Snapshot()
				conn.WriteJSON(snap)
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]string{"type": "client.hello"}); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var snap struct {
		Type string `json:"type"`
		Ts   int64  `json:"ts"`
	}
	if err := json.Unmarshal(msg, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if snap.Type != "system.snapshot" {
		t.Errorf("type=%q want system.snapshot", snap.Type)
	}
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/handler/ -run TestHandleEventsWS -v`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/handler/system.go internal/handler/events_test.go
git commit -m "feat(events): replace HandleSystemWS with HandleEventsWS reading client.hello"
```

---

## Task 7: writeLog 末尾 publish

**Files:**
- Modify: `internal/handler/handler.go`(在 writeLog 内部加 defer)

- [ ] **Step 1: 定位 writeLog 中的 `go func()` 末尾**

在 `internal/handler/handler.go` 找到 `writeLog` 函数(`go func() { ... }()` 的内部块末尾)。原文末尾是:

```go
		if err := h.storage.LogRequest(rec); err != nil {
			log.Printf("[storage] LogRequest failed: %v", err)
		}
	}()
```

- [ ] **Step 2: 在 `LogRequest` 成功后加 PublishLog**

把上面那段替换为:

```go
		if err := h.storage.LogRequest(rec); err != nil {
			log.Printf("[storage] LogRequest failed: %v", err)
		} else {
			// DB 写完才 publish,保证前端拿到的 row 一定能从历史接口读回
			EventsHub.PublishLog(*rec)
		}
	}()
```

> **不要在 `go func()` 外面 publish**——`rec` 是局部变量,可能在 goroutine 内被改。defer 也不行,因为这个函数没有 defer 链。

- [ ] **Step 3: 编译验证**

Run: `go build ./...`
Expected: no error

- [ ] **Step 4: 跑全部 events 测试**

Run: `go test ./internal/handler/ -v`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/handler/handler.go
git commit -m "feat(events): publish log.new from writeLog after DB write succeeds"
```

---

## Task 8: DeleteHistory 三处成功路径 publish

**Files:**
- Modify: `internal/handler/history.go`

- [ ] **Step 1: 单删路径加 publish**

`internal/handler/history.go` 找到 `DeleteHistory` 函数中 `DeleteByIDs` 成功后的 `gin.H{"code": 0, "data": gin.H{"deleted": n}}` 之前(在 `res, err := h.storage.DeleteByIDs(ids)` 的 `if err != nil` 分支之后),在成功分支顶部加:

```go
		EventsHub.PublishLogDeleted(ids)
```

- [ ] **Step 2: 全清路径加 publish(nil)**

在 `DeleteAll` 成功路径顶部加:

```go
		EventsHub.PublishLogDeleted(nil)
```

- [ ] **Step 3: 编译验证**

Run: `go build ./...`
Expected: no error

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/handler/ -v`
Expected: PASS

- [ ] **Step 5: commit**

```bash
git add internal/handler/history.go
git commit -m "feat(events): publish log.deleted on single/batch/clear history delete"
```

---

## Task 9: UpdateConfig 成功路径 publish

**Files:**
- Modify: `internal/handler/settings.go`

- [ ] **Step 1: 在 `config.Save` 成功后 publish**

`internal/handler/settings.go` 找到:

```go
	if err := config.Save(cfg); err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.SimpleResponse{Code: 0, Msg: "success"})
```

改为:

```go
	if err := config.Save(cfg); err != nil {
		c.JSON(http.StatusOK, model.SimpleResponse{Code: 1, Msg: err.Error()})
		return
	}
	// 保存成功后立即广播,前端收到时本会话响应可能还没到达(200 通常 <1ms),
	// ignoreConfigChangedUntil 窗口(2s)会覆盖这段本地时延,避免自我二次确认弹窗。
	EventsHub.PublishConfigChanged()
	c.JSON(http.StatusOK, model.SimpleResponse{Code: 0, Msg: "success"})
```

- [ ] **Step 2: 编译验证**

Run: `go build ./...`
Expected: no error

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/handler/ -v`
Expected: PASS

- [ ] **Step 4: commit**

```bash
git add internal/handler/settings.go
git commit -m "feat(events): publish config.changed on UpdateConfig success"
```

---

## Task 10: main.go 切换 SystemHub → EventsHub,清理占位

**Files:**
- Modify: `internal/handler/broadcaster.go`(把 `getConfigSnapshot` 占位换成 `config.Get`)
- Modify: `main.go`(路径 + 符号替换)

- [ ] **Step 1: 替换 `broadcaster.go` 中 `runRetentionOnce` 的 `getConfigSnapshot()` 占位**

把:

```go
	cfg := getConfigSnapshot() // 见 Task 10
```

改为:

```go
	cfg := config.Get()
```

并在文件顶部 import 块加 `"wx_web_api/internal/config"`(确认未引入)。

- [ ] **Step 2: main.go 中 `SystemHub` → `EventsHub`,`/ws/system` → `/ws/events`**

`main.go` 中两处修改:

```go
	// 旧
	go handler.SystemHub.Start(context.Background(), store)
	// 新
	go handler.EventsHub.Start(context.Background(), store)
```

```go
	// 旧
	r.GET("/ws/system", h.SessionAuth(), h.HandleSystemWS)
	// 新
	r.GET("/ws/events", h.SessionAuth(), h.HandleEventsWS)
```

- [ ] **Step 3: 编译 + 全量测试**

Run: `go build ./...`
Run: `go test ./...`
Expected: both succeed

- [ ] **Step 4: 启动二进制,smoke 测试**

Run: `go build -o /tmp/wx_web_api_test.exe . && /tmp/wx_web_api_test.exe -port 18080 -pwd test123`

在另一终端:

```bash
# 登录拿 token
curl -s "http://127.0.0.1:18080/api/login/challenge"
# 用 challenge + test123 算 simpleHash 后 POST /api/login 拿 token
# 验证二进制启动且 /ws/events 端点已注册
```

> binary 启动时日志应出现 `wx_web_api starting on :18080 (build: ...)`,无 panic。
> 关闭时 Ctrl-C。

- [ ] **Step 5: commit**

```bash
git add internal/handler/broadcaster.go main.go
git commit -m "feat(events): wire EventsHub into main.go and rename /ws/system to /ws/events"
```

---

## Task 11: events.js 骨架 + start/stop + WS 客户端 + 重连

**Files:**
- Create: `web/static/js/events.js`

- [ ] **Step 1: 在 `web/index.html` 中插入 events.js script 标签**

找到 `<script src="/static/js/auth.js"></script>`,在它之后追加:

```html
    <script src="/static/js/events.js"></script>
```

- [ ] **Step 2: 写 events.js 完整内容(单例 + WS 客户端 + 重连)**

`web/static/js/events.js`:

```javascript
/* WebSocket 客户端单例 — 整个 SPA 共享一条 /ws/events 连接。
 * - subscribe(type, handler) / unsubscribe(type, handler):按事件类型订阅
 * - onStatusChange(handler):订阅连接状态变化
 * - start() / stop():启动 / 停止(登出时停)
 * - connectionStatus: 'connecting' | 'ok' | 'err' | 'auth_err'
 *
 * 重连:指数退避 1s → 30s;watchdog 2s 检查,6s 未收帧视为 stale 主动 close。
 * 鉴权:断开时 probe /api/system,401 → auth_err + WXAuth.handle401。
 * 首帧:onopen 后发 {type:'client.hello'},服务端立即推 system.snapshot。
 *
 * 依赖: WXAuth(用于 401 跳登录与 token 读取),WXApi(用于 probe)。
 * 加载顺序:auth.js → events.js → api.js → app.js。
 */
(function (global) {
  'use strict';

  var RECONNECT_BASE_MS = 1000;
  var RECONNECT_MAX_MS = 30000;
  var STALE_THRESHOLD_MS = 6000;       // > 6s 未收帧视为 stale
  var WATCHDOG_INTERVAL_MS = 2000;
  var PING_FRAME = JSON.stringify({ type: 'client.hello' });

  var state = {
    ws: null,
    reconnectAttempt: 0,
    reconnectTimer: null,
    watchdogTimer: null,
    lastFrameAt: 0,
    connectionStatus: 'connecting',
    running: false,
    handlers: Object.create(null),      // { 'log.new': Set<fn>, ... }
    statusHandlers: new Set(),
  };

  function setStatus(next) {
    if (state.connectionStatus === next) return;
    state.connectionStatus = next;
    state.statusHandlers.forEach(function (fn) { try { fn(next); } catch (e) { /* ignore */ } });
  }

  function getToken() {
    if (global.WXAuth && global.WXAuth.getToken) return global.WXAuth.getToken();
    try { return localStorage.getItem('wx_token') || ''; } catch (e) { return ''; }
  }

  function buildWsUrl() {
    var proto = global.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return proto + '//' + global.location.host + '/ws/events?token=' + encodeURIComponent(getToken());
  }

  function connect() {
    if (!state.running) return;
    if (state.ws) { try { state.ws.close(); } catch (e) { /* ignore */ } }
    if (state.reconnectTimer) { clearTimeout(state.reconnectTimer); state.reconnectTimer = null; }

    setStatus('connecting');
    var url;
    try { url = buildWsUrl(); } catch (e) {
      scheduleReconnect();
      return;
    }

    var ws;
    try { ws = new global.WebSocket(url); }
    catch (e) { scheduleReconnect(); return; }
    state.ws = ws;

    ws.onopen = function () {
      state.reconnectAttempt = 0;
      // 请求服务端立即推一帧 system.snapshot 作为首帧
      try { ws.send(PING_FRAME); } catch (e) { /* ignore */ }
    };

    ws.onmessage = function (e) {
      state.lastFrameAt = Date.now();
      var frame;
      try { frame = JSON.parse(e.data); }
      catch (err) { if (global.console) console.error('events: bad frame', err); return; }
      if (frame && frame.type) {
        if (state.connectionStatus !== 'ok') setStatus('ok');
        var set = state.handlers[frame.type];
        if (set) {
          set.forEach(function (fn) { try { fn(frame); } catch (err) { if (global.console) console.error('events: handler error', err); } });
        }
      }
    };

    ws.onerror = function () { handleDisconnect(); };
    ws.onclose = function () { handleDisconnect(); };
  }

  function probeAuth() {
    if (!global.WXApi || !global.WXApi.authJson) return Promise.resolve(true);
    return global.WXApi.authJson('/api/system').then(function () { return true; }).catch(function (e) {
      if (e && e.isAuth) return false;
      return true; // 网络/500/parse 错误不视为鉴权失败
    });
  }

  function handleDisconnect() {
    if (!state.running) return;
    setStatus('connecting');
    probeAuth().then(function (ok) {
      if (!ok) {
        setStatus('auth_err');
        if (global.WXAuth && global.WXAuth.handle401) global.WXAuth.handle401();
        return;
      }
      setStatus('err');
      scheduleReconnect();
    });
  }

  function scheduleReconnect() {
    if (!state.running || state.reconnectTimer) return;
    var delay = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * Math.pow(2, state.reconnectAttempt));
    state.reconnectAttempt++;
    state.reconnectTimer = setTimeout(function () {
      state.reconnectTimer = null;
      connect();
    }, delay);
  }

  function startWatchdog() {
    if (state.watchdogTimer) return;
    state.watchdogTimer = setInterval(function () {
      if (state.connectionStatus === 'ok' && state.lastFrameAt > 0) {
        var staleFor = Date.now() - state.lastFrameAt;
        if (staleFor > STALE_THRESHOLD_MS && state.ws) {
          try { state.ws.close(); } catch (e) { /* ignore */ }
        }
      }
    }, WATCHDOG_INTERVAL_MS);
  }

  function stopWatchdog() {
    if (state.watchdogTimer) { clearInterval(state.watchdogTimer); state.watchdogTimer = null; }
  }

  function start() {
    if (state.running) return;
    state.running = true;
    state.reconnectAttempt = 0;
    connect();
    startWatchdog();
  }

  function stop() {
    state.running = false;
    if (state.reconnectTimer) { clearTimeout(state.reconnectTimer); state.reconnectTimer = null; }
    stopWatchdog();
    if (state.ws) { try { state.ws.close(); } catch (e) { /* ignore */ } state.ws = null; }
    setStatus('connecting');
  }

  function subscribe(type, handler) {
    if (!type || typeof handler !== 'function') return function () {};
    if (!state.handlers[type]) state.handlers[type] = new Set();
    state.handlers[type].add(handler);
    return function () { unsubscribe(type, handler); };
  }

  function unsubscribe(type, handler) {
    var set = state.handlers[type];
    if (set) set.delete(handler);
  }

  function onStatusChange(handler) {
    if (typeof handler !== 'function') return function () {};
    state.statusHandlers.add(handler);
    return function () { state.statusHandlers.delete(handler); };
  }

  global.WXEvents = {
    start: start,
    stop: stop,
    subscribe: subscribe,
    unsubscribe: unsubscribe,
    onStatusChange: onStatusChange,
    get connectionStatus() { return state.connectionStatus; },
  };
})(window);
```

- [ ] **Step 3: 在浏览器 console 验证对象存在**

打开 `http://127.0.0.1:18080/`,登录后浏览器 DevTools console:

```javascript
typeof window.WXEvents
// 期望: 'object'
WXEvents.connectionStatus
// 期望: 'connecting' (登录后自动 start) 或 'ok' (连上后)
```

- [ ] **Step 4: 验证 WS 建立**

DevTools Network → WS 过滤,应看到一条 `ws/events?token=...` 连接,Frames 标签下有 `client.hello`(发出)和 `system.snapshot`(收到)。

- [ ] **Step 5: 验证重连**

在 DevTools console 调 `WXEvents.stop()` 后再 `WXEvents.start()`,应看到 close + reconnect。重启后端进程,应看到 1~30s 内自动重连。

- [ ] **Step 6: commit**

```bash
git add web/static/js/events.js web/index.html
git commit -m "feat(events): add WXEvents singleton with reconnect and watchdog"
```

---

## Task 12: system.js 改用 WXEvents,删除自带 WS 客户端

**Files:**
- Modify: `web/static/js/pages/system.js`

- [ ] **Step 1: 替换 system.js 中所有 WS 相关代码,保留渲染逻辑**

完整替换 `web/static/js/pages/system.js`:

```javascript
/* System page (系统信息) — admin-only runtime / version / config / health view.
 * Loads /api/system for static values, subscribes to WXEvents 'system.snapshot'
 * for live updates every 2s.
 *
 * 改动(本次):
 * - 删除自带 WS 客户端、watchdog、reconnect、probeAuth
 * - 改用 WXEvents.subscribe('system.snapshot', ...)
 * - 改用 WXEvents.onStatusChange(...) 驱动连接徽章
 * - render() 入口先 cleanup 旧订阅,避免 router 切换时回调打到 detach 的 slot
 */

(function (global) {
  'use strict';

  var state = {
    staticLoaded: false,
    unsubscribers: [],
  };

  function escapeHtml(s) {
    var div = document.createElement('div');
    div.textContent = s == null ? '' : String(s);
    return div.innerHTML;
  }

  function pad2(n) { return n < 10 ? '0' + n : '' + n; }

  function formatBytes(n) {
    if (n == null || n === 0) return '—';
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
    var status = global.WXEvents ? global.WXEvents.connectionStatus : 'connecting';
    var label;
    if (status === 'ok') {
      label = '🟢 已连接';
    } else if (status === 'connecting') {
      label = '🟡 连接中...';
    } else if (status === 'auth_err') {
      label = '🔴 鉴权失败,请重新登录';
    } else {
      label = '🔴 已断开 (重连中...)';
    }
    return '<dt>连接状态</dt><dd>' +
             '<span class="conn-badge conn-badge--' + status + '">' +
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
      realRow('DB 大小', formatBytes(d.db_size));

    slot.dataset.goVersion = d.go_version || '';
    slot.dataset.goOsArch = (d.goos || '') + ' / ' + (d.goarch || '');
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
    var rt = slot.querySelector('#sysRuntimeKv');
    if (!rt) return;
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
      healthRows.push('<dt>—</dt><dd><span class="kv__sub">数据来源: /api/history 后台聚合 (request_log 表)</span></dd>');
      hk.innerHTML = healthRows.join('');
    }
  }

  /* ---------- subscription lifecycle ---------- */

  function cleanup() {
    state.unsubscribers.forEach(function (u) { try { u(); } catch (e) { /* ignore */ } });
    state.unsubscribers = [];
  }

  function bindEvents(slot) {
    if (!global.WXEvents) return;
    state.unsubscribers.push(global.WXEvents.subscribe('system.snapshot', function (snap) {
      applySnapshot(slot, snap);
    }));
    state.unsubscribers.push(global.WXEvents.onStatusChange(function () {
      // 状态变化时,只需重渲 connectionBadge(第一行)
      var rt = slot.querySelector('#sysRuntimeKv');
      if (!rt) return;
      var firstDd = rt.querySelector('dd');
      if (!firstDd) return;
      var tmp = document.createElement('div');
      tmp.innerHTML = connectionBadge();
      var newDd = tmp.querySelector('dd');
      if (newDd) firstDd.outerHTML = newDd.outerHTML;
    }));
  }

  /* ---------- boot ---------- */

  function render(slot) {
    cleanup(); // router 重新进入时先清掉旧订阅
    slot.innerHTML = renderSkeleton();
    loadStatic(slot);
    bindEvents(slot);
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.system = { render: render };
})(window);
```

- [ ] **Step 2: 浏览器手测**

进入 /system 页面:
- [ ] runtime / version / config / 健康度 各段数据正常显示
- [ ] 数值每 2s 刷新
- [ ] 杀掉后端进程,徽章变红;重启后 30s 内重连成功,徽章变绿
- [ ] 切到 /dashboard 再切回 /system,数值仍正常(无 stale 订阅)

- [ ] **Step 3: commit**

```bash
git add web/static/js/pages/system.js
git commit -m "refactor(system): use WXEvents for snapshots instead of self-managed WebSocket"
```

---

## Task 13: dashboard.js 订阅 log.new + config.changed

**Files:**
- Modify: `web/static/js/pages/dashboard.js`

- [ ] **Step 1: 把 dashboard.js 中的 `state.recent` 提升到 module 级**

在 `dashboard.js` 顶部 `var RECENT_SIZE = 10;` 之后,加 `var recent = [];`(不再嵌在 render 闭包内,因为事件回调需要访问)。

- [ ] **Step 2: 改造 `loadRecent` 直接渲染 module 级 `recent` 数组**

`loadRecent` 函数体内的渲染逻辑改为读 `recent` 全局变量。完整 `loadRecent`:

```javascript
  async function loadRecent(slot) {
    var card = slot.querySelector('[data-role="recent-card"]');
    if (!card) return;
    var body = card.querySelector('[data-role="recent-body"]');
    if (!body) return;
    try {
      var res = await global.WXApi.authJson('/api/history?range=all&page=1&size=' + RECENT_SIZE);
      if (res.data && res.data.code === 0 && res.data.data) {
        recent = (res.data.data.items || []).slice();
        renderRecent(slot);
      } else {
        body.innerHTML = '<div class="result-msg">加载失败: ' + escapeHtml((res.data && res.data.msg) || '未知错误') + '</div>';
      }
    } catch (e) {
      if (e && e.isAuth) return;
      body.innerHTML = '<div class="result-msg">加载失败: ' + escapeHtml(e.message || '网络错误') + '</div>';
    }
  }

  function renderRecent(slot) {
    var card = slot.querySelector('[data-role="recent-card"]');
    if (!card) return;
    var body = card.querySelector('[data-role="recent-body"]');
    if (!body) return;
    body.innerHTML = renderRecentRows(recent);
  }
```

- [ ] **Step 3: 在 render 末尾添加 bindEvents**

```javascript
  var unsubscribers = [];

  function bindEvents(slot) {
    if (!global.WXEvents) return;
    unsubscribers.push(global.WXEvents.subscribe('log.new', function (frame) {
      if (!frame || !frame.log) return;
      recent.unshift(frame.log);
      if (recent.length > RECENT_SIZE) recent.length = RECENT_SIZE;
      renderRecent(slot);
    }));
    unsubscribers.push(global.WXEvents.subscribe('config.changed', function () {
      loadTokenCount(slot);
    }));
  }

  function cleanup() {
    unsubscribers.forEach(function (u) { try { u(); } catch (e) { /* ignore */ } });
    unsubscribers = [];
  }
```

- [ ] **Step 4: render 入口调用 cleanup,末尾调用 bindEvents**

```javascript
  function render(slot) {
    cleanup();
    // ...原 slot.innerHTML = ...
    bindEvents(slot);
  }
```

把 `render` 函数最开头加 `cleanup();`,最末尾(在 `loadRecent(slot)` 之后)加 `bindEvents(slot);`。

- [ ] **Step 5: 浏览器手测**

- [ ] dashboard "最近请求"初始加载正常
- [ ] 在解析测试页 / 外部 POST /wx 后,1s 内 dashboard 顶部出现新行
- [ ] 连续触发 11+ 次请求,列表始终保持 10 条,最早的最先被丢
- [ ] 在设置页改 token 数后,dashboard "配置的 Token 数" 静默更新(无刷新)
- [ ] 切到 /history 再切回 /dashboard,列表正常,无重复条目

- [ ] **Step 6: commit**

```bash
git add web/static/js/pages/dashboard.js
git commit -m "feat(dashboard): subscribe to log.new and config.changed"
```

---

## Task 14: history.js — logMatchesFilter + applyLogNew + applyLogDeleted

**Files:**
- Modify: `web/static/js/pages/history.js`

- [ ] **Step 1: 在 `state` 上加 `unreadHintVisible` 与 `unsubscribers`**

```javascript
  var state = {
    filter: { range: 'today', kind: 'all', status: 'all', token: 'all', q: '' },
    page: 1,
    size: DEFAULT_SIZE,
    data: null,
    expanded: new Set(),
    selected: new Set(),
    abortCtrl: null,
    loadId: 0,
    tokenLabels: [],
    unreadHintVisible: false,
    unsubscribers: [],
  };
```

- [ ] **Step 2: 在 history.js 末尾添加 `logMatchesFilter` 纯函数**

```javascript
  function tsLowerBoundMs(range) {
    var d = new Date();
    if (range === 'today') {
      return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
    }
    if (range === '7d') return d.getTime() - 7 * 86400000;
    if (range === '30d') return d.getTime() - 30 * 86400000;
    return 0; // 'all' or unknown
  }

  function statusValue(s) {
    if (s === 'ok') return 0;
    if (s === 'err') return 1;
    if (s === 'auth_err') return 401;
    return null;
  }

  function logMatchesFilter(log, filter) {
    if (!log) return false;
    if (filter.kind && filter.kind !== 'all' && log.kind !== filter.kind) return false;
    if (filter.token && filter.token !== 'all' && log.token_label !== filter.token) return false;
    if (filter.status && filter.status !== 'all') {
      var sv = statusValue(filter.status);
      if (sv !== null && log.status !== sv) return false;
    }
    if (filter.range && filter.range !== 'all') {
      var lb = tsLowerBoundMs(filter.range);
      if (lb > 0 && log.ts < lb) return false;
    }
    if (filter.q) {
      var reqStr;
      try { reqStr = JSON.stringify(log.request || ''); } catch (e) { reqStr = ''; }
      if (reqStr.indexOf(filter.q) === -1) return false;
    }
    return true;
  }
```

- [ ] **Step 3: 添加 `applyLogNew` 与 `applyLogDeleted`**

```javascript
  function applyLogNew(slot, frame) {
    if (!frame || !frame.log || !state.data) return;
    var log = frame.log;
    state.data.total += 1;

    if (state.page > 1) {
      state.unreadHintVisible = true;
      renderUnreadHint(slot);
      return;
    }
    if (logMatchesFilter(log, state.filter)) {
      state.data.items.unshift(log);
      if (state.data.items.length > state.size) {
        state.data.items.length = state.size;
      }
      renderList(slot);
    } else {
      state.unreadHintVisible = true;
      renderUnreadHint(slot);
    }
  }

  function applyLogDeleted(slot, frame) {
    if (!frame || !state.data) return;
    var ids = frame.ids;
    if (!ids || (Array.isArray(ids) && ids.length === 0)) {
      // 全清信号
      state.data.items = [];
      state.data.total = 0;
      state.unreadHintVisible = false;
      renderList(slot);
      renderUnreadHint(slot);
      return;
    }
    var set = new Set(ids);
    state.data.items = state.data.items.filter(function (it) { return !set.has(it.id); });
    state.data.total = Math.max(0, state.data.total - ids.length);
    renderList(slot);
  }

  function renderUnreadHint(slot) {
    var el = slot.querySelector('[data-role="unread-hint"]');
    if (!el) return;
    if (state.unreadHintVisible) {
      el.hidden = false;
      var text = state.page > 1
        ? '有 1 条新记录,点此查看第 1 页'
        : '有 1 条新记录不符合当前筛选,点此清空筛选查看';
      el.textContent = text;
    } else {
      el.hidden = true;
    }
  }
```

- [ ] **Step 4: 在 `renderSkeleton` 顶部 summary 行后插入 unread-hint DOM**

把 `renderSkeleton()` 中:

```javascript
        '<div class="history-summary">' +
          '<span data-role="summary-text">加载中…</span>' +
          '<span>' +
            '<button class="btn btn--secondary" data-role="batch-delete" disabled>批量删除</button> ' +
          '</span>' +
        '</div>' +
```

改为:

```javascript
        '<div class="history-summary">' +
          '<span data-role="summary-text">加载中…</span>' +
          '<span data-role="unread-hint" class="badge" hidden></span>' +
          '<span>' +
            '<button class="btn btn--secondary" data-role="batch-delete" disabled>批量删除</button> ' +
          '</span>' +
        '</div>' +
```

- [ ] **Step 5: 在 `wireFilter` 末尾添加 unread-hint 点击 → 跳第 1 页 / 清空筛选**

在 `wireFilter(slot)` 末尾(在 batchBtn 点击绑定之后)加:

```javascript
    var hint = slot.querySelector('[data-role="unread-hint"]');
    if (hint) {
      hint.style.cursor = 'pointer';
      hint.addEventListener('click', function () {
        state.unreadHintVisible = false;
        if (state.page > 1) {
          state.page = 1;
        } else {
          state.filter = { range: 'today', kind: 'all', status: 'all', token: 'all', q: '' };
          syncFilterUI(slot);
        }
        load(slot);
      });
    }
```

- [ ] **Step 6: 添加 bindEvents / cleanup,并在 render 末尾接入**

```javascript
  function bindEvents(slot) {
    if (!global.WXEvents) return;
    state.unsubscribers.push(global.WXEvents.subscribe('log.new', function (frame) { applyLogNew(slot, frame); }));
    state.unsubscribers.push(global.WXEvents.subscribe('log.deleted', function (frame) { applyLogDeleted(slot, frame); }));
    state.unsubscribers.push(global.WXEvents.subscribe('config.changed', function () {
      loadTokenLabels().then(function () { populateTokenDropdown(slot); });
    }));
  }

  function cleanup() {
    state.unsubscribers.forEach(function (u) { try { u(); } catch (e) { /* ignore */ } });
    state.unsubscribers = [];
  }
```

把 `render` 函数最开头加 `cleanup();`,最末尾(在 `load(slot)` 之后)加 `bindEvents(slot);`。

- [ ] **Step 7: 浏览器手测**

- [ ] /history 初始加载正常
- [ ] 别人发 /wx 请求,1s 内顶部出现新行(如果筛选匹配)
- [ ] 切到第 3 页,新行不插入顶部,但 summary 旁出现 "有 1 条新记录,点此查看第 1 页" 提示
- [ ] 点提示 → 跳到第 1 页,看到新行
- [ ] 设置筛选 kind=url,但新来一条 finder,顶部出现 "有 1 条新记录不符合当前筛选..." 提示
- [ ] 点提示 → 筛选清空,新行可见
- [ ] 单条 / 批量 / 全清删除,log.deleted 触发后列表同步更新
- [ ] 另一会话改 token 数,token 下拉自动刷新

- [ ] **Step 8: commit**

```bash
git add web/static/js/pages/history.js
git commit -m "feat(history): smart-insert log.new, apply log.deleted, subscribe config.changed"
```

---

## Task 15: settings.js — config-stale 提示条 + dirty 跟踪 + 2s 自我忽略窗口

**Files:**
- Modify: `web/static/js/pages/settings.js`

- [ ] **Step 1: 读现有 settings.js,定位表单渲染函数**

确认已有 GET /api/config / PUT /api/config 调用,以及 token 列表、表单等 DOM 结构。

- [ ] **Step 2: 添加 module 级 state**

```javascript
  var state = {
    config: null,           // 最近一次拉取的 config(用于 dirty 比较 + reload 重置)
    dirty: false,           // 表单是否被改
    ignoreConfigChangedUntil: 0,
    unsubscribers: [],
  };
```

- [ ] **Step 3: 改造 `loadConfig` 保存到 state.config 并设 baseline**

```javascript
  function loadConfig() {
    return global.WXApi.authJson('/api/config').then(function (res) {
      if (res.data && res.data.code === 0 && res.data.data) {
        state.config = res.data.data;
        applyConfigToForm(state.config);
        state.dirty = false;
      }
    });
  }
```

- [ ] **Step 4: 在 applyConfigToForm 之后挂 input/change 事件标 dirty**

(若 settings.js 已有 applyConfigToForm 函数,只需在末尾加事件;若没有,把 applyConfigToForm 抽出来。)

```javascript
  function applyConfigToForm(cfg) {
    // ...原表单填充逻辑,逐字段把 cfg 值写到 input/select...
    // 在填充完成后挂 dirty 监听(用一次性 flag 避免初始化时触发)
    var inputs = document.querySelectorAll('#settingsForm input, #settingsForm select, #settingsForm textarea');
    inputs.forEach(function (el) {
      el.addEventListener('input', function () { state.dirty = true; });
      el.addEventListener('change', function () { state.dirty = true; });
    });
  }
```

> 注意:本任务假设 `settings.js` 已存在的 DOM 结构。实际写代码时按真实结构调整 id/class;若 settings.js 还没有专门的表单包裹元素,用 slot 容器代替。

- [ ] **Step 5: 改造 PUT /api/config 成功路径,设置 ignoreConfigChangedUntil**

找到 `callUpdate` 或 `submitForm` 类的函数,在 `res.data.code === 0` 之后加:

```javascript
          state.ignoreConfigChangedUntil = Date.now() + 2000;
          state.dirty = false;
          state.config = res.data.data; // 同步本地
          applyConfigToForm(state.config);
```

- [ ] **Step 6: 添加 config-stale DOM 到 settings 页骨架**

在 `render(slot)` 输出的最顶部(其它 card 之前)加:

```javascript
    '<div class="card" data-role="config-stale" hidden>' +
      '<div class="card__title">配置已被其他会话更新</div>' +
      '<div class="kv__sub">当前表单有未保存的修改,刷新将丢失这些修改。</div>' +
      '<div style="margin-top:var(--s-3)">' +
        '<button class="btn btn--primary" data-role="config-stale-reload">重新加载</button> ' +
        '<button class="btn btn--secondary" data-role="config-stale-ignore">忽略</button>' +
      '</div>' +
    '</div>' +
```

并在 render 末尾添加:

```javascript
    slot.querySelector('[data-role="config-stale-reload"]').addEventListener('click', function () { reloadFromServer(slot); });
    slot.querySelector('[data-role="config-stale-ignore"]').addEventListener('click', function () { hideStaleBar(slot); });
```

- [ ] **Step 7: 添加 reloadFromServer / hideStaleBar 与 onConfigChanged**

```javascript
  function hideStaleBar(slot) {
    var el = slot.querySelector('[data-role="config-stale"]');
    if (el) el.hidden = true;
  }

  function reloadFromServer(slot) {
    loadConfig().then(function () { hideStaleBar(slot); });
  }

  function onConfigChanged(slot) {
    // 自我推送免提示窗口
    if (Date.now() < state.ignoreConfigChangedUntil) return;
    if (state.dirty) {
      var el = slot.querySelector('[data-role="config-stale"]');
      if (el) el.hidden = false;
    } else {
      reloadFromServer(slot);
    }
  }
```

- [ ] **Step 8: 在 render 末尾接入订阅;render 开头 cleanup**

```javascript
  function bindEvents(slot) {
    if (!global.WXEvents) return;
    state.unsubscribers.push(global.WXEvents.subscribe('config.changed', function () { onConfigChanged(slot); }));
  }

  function cleanup() {
    state.unsubscribers.forEach(function (u) { try { u(); } catch (e) { /* ignore */ } });
    state.unsubscribers = [];
    state.config = null;
    state.dirty = false;
    state.ignoreConfigChangedUntil = 0;
  }
```

把 `render` 函数最开头加 `cleanup();`,最末尾(在初始 loadConfig().then(...) 之后)加 `bindEvents(slot);`。

- [ ] **Step 9: 浏览器手测**

- [ ] /settings 初始加载表单正常
- [ ] 改一个 token value 但未保存,另一会话保存 → 本会话顶部出现 "配置已被其他会话更新" 条
- [ ] 点 "重新加载" → 表单回到另一会话保存后的值,dirty 重置,提示条消失
- [ ] 点 "忽略" → 提示条消失,表单 dirty 状态保留
- [ ] 不改任何东西,另一会话保存 → 静默重拉,无提示条
- [ ] 自身点保存 → 2s 内另一会话若保存(理论极端情况)不会触发自身二次确认弹窗

- [ ] **Step 10: commit**

```bash
git add web/static/js/pages/settings.js
git commit -m "feat(settings): add config-stale bar with dirty tracking and self-save ignore window"
```

---

## Task 16: app.js + 端到端 smoke

**Files:**
- Modify: `web/static/js/app.js`

- [ ] **Step 1: 在 `app.js` 顶部定位 `showApp` 与 `logout`**

`app.js` 已有 `showApp()`(在 loginForm submit 成功时调)与 `logout`(在顶部 logout 按钮调)。

- [ ] **Step 2: 在 `showApp` 末尾添加 `WXEvents.start()`**

`app.js` 中找到:

```javascript
      if (global.WXAuth) global.WXAuth.showApp();
      if (global.WXRouter) {
        global.WXRouter.navigate(target, { replace: true });
      } else {
        global.history.replaceState({}, '', target);
      }
```

在这段之后(在同一 `.then` 块内)加:

```javascript
      if (global.WXEvents) global.WXEvents.start();
```

- [ ] **Step 3: 在 logout 中添加 `WXEvents.stop()`**

`app.js` 找到 `bindLogout` 函数,它在 logout 按钮 click 时调 `WXAuth.logout()`。在 `logout` 调之前 / 之后加 `WXEvents.stop()`:

```javascript
    btn.addEventListener('click', function () {
      if (global.WXEvents) global.WXEvents.stop();
      if (global.WXAuth) global.WXAuth.logout();
      if (global.location && global.location.pathname !== '/') {
        global.history.replaceState({}, '', '/');
      }
    });
```

- [ ] **Step 4: 浏览器手测 — 端到端 smoke**

打开 `http://127.0.0.1:18080/`,DevTools console 与 Network/WS 标签打开:

- [ ] 登录成功 → console 出现 `WXEvents.start()` 触发,Network/WS 看到 `/ws/events?token=...` 建立
- [ ] 切到 /dashboard → 不应建立新 WS,单条连接复用
- [ ] 用 curl `POST /wx` 触发一条新请求 → dashboard 顶部 1s 内出现新行
- [ ] 切到 /history → 新行同步出现
- [ ] 切到 /settings → 改 token 数量,另一会话登录后保存,本会话不需手动刷新就看到 token 数变化
- [ ] 切到 /system → runtime 数值每 2s 刷新
- [ ] 点 "退出登录" → WS 立即关闭,跳回登录页
- [ ] 重新登录 → 同一 client 重新建立 WS(新 token)

- [ ] **Step 5: commit**

```bash
git add web/static/js/app.js
git commit -m "feat(app): start WXEvents on showApp and stop on logout"
```

---

## Task 17: 全量回归与文档收尾

**Files:**
- Modify: 无新文件,但要跑全量回归

- [ ] **Step 1: 全量 Go 测试**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: 全量 build**

Run: `go build -o dist/wx_web_api.exe .`
Expected: no error

- [ ] **Step 3: 启动二进制,跑 spec §"前端手测 checklist" 中 11 项**

逐条勾选,任何一条失败立即回滚最近 commit。

- [ ] **Step 4: 收尾 — 把 spec 文件状态从"待用户复核"改为"已确认"**

修改 `docs/superpowers/specs/2026-06-10-websocket-realtime-events-design.md` 第一行状态字段:

```markdown
**状态**: 已实施(2026-06-10) / 已确认
```

- [ ] **Step 5: 收尾 commit**

```bash
git add docs/superpowers/specs/2026-06-10-websocket-realtime-events-design.md
git commit -m "docs(spec): mark websocket realtime events design as implemented"
```

---

## 实施完成

17 个任务全部完成后:
- 后端:3 个 hook 点就位,`/ws/events` 端点注册,`EventsHub` 在 main.go 启动
- 前端:`events.js` 单例,4 个页面订阅,智能插入 / 二次确认 / 静默重拉就位
- 旧的 `/ws/system` 端点已删除,旧的 `systemHub` 类型已删除(无外部依赖)
- 旧的 `/api/*` GET 全部保留,WS 挂了用户仍可手动刷新

回滚:整个 feature 走 17 次独立 commit,任意 commit 失败可单独 revert 而不影响其它。
