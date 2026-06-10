package handler

import (
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
