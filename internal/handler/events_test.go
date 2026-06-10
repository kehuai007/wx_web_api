package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wx_web_api/internal/storage"

	"github.com/gorilla/websocket"
)

func newTestHub() *eventsHub {
	return &eventsHub{
		clients:  make(map[*eventClient]time.Time),
		logCh:    make(chan storage.RequestLog, 256),
		logDelCh: make(chan []int64, 64),
		configCh: make(chan struct{}, 16),
	}
}

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

func dialTestHub(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

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

	time.Sleep(50 * time.Millisecond)
	hub.mu.RLock()
	n := len(hub.clients)
	hub.mu.RUnlock()
	if n != 2 {
		t.Fatalf("expected 2 clients registered, got %d", n)
	}

	c1.Close()
	time.Sleep(100 * time.Millisecond)
	hub.mu.RLock()
	n = len(hub.clients)
	hub.mu.RUnlock()
	if n != 1 {
		t.Fatalf("expected 1 client after c1 close, got %d", n)
	}
}

func TestEventsHub_PublishLog_BroadcastsToAllClients(t *testing.T) {
	hub := newTestHub()
	// 抑制 system.snapshot 干扰:把 ticker 调到 1h,本测试期间不会触发
	old := systemTickerInterval
	systemTickerInterval = time.Hour
	defer func() { systemTickerInterval = old }()

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

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func TestEventsHub_PublishLogDeleted_BroadcastsIDs(t *testing.T) {
	hub := newTestHub()
	// 抑制 system.snapshot 干扰
	old := systemTickerInterval
	systemTickerInterval = time.Hour
	defer func() { systemTickerInterval = old }()

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
	// 抑制 system.snapshot 干扰
	old := systemTickerInterval
	systemTickerInterval = time.Hour
	defer func() { systemTickerInterval = old }()

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

func TestEventsHub_PublishConfigChanged_Broadcasts(t *testing.T) {
	hub := newTestHub()
	// 抑制 system.snapshot 干扰
	old := systemTickerInterval
	systemTickerInterval = time.Hour
	defer func() { systemTickerInterval = old }()

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

func TestHandleEventsWS_SendsSnapshotOnHello(t *testing.T) {
	hub := newTestHub()
	// 本测试不调 hub.Start(只用 register + 直接读 hello),只验证 hello 触发首帧逻辑。

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
