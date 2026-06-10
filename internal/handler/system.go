package handler

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"wx_web_api/internal/buildinfo"
	"wx_web_api/internal/config"

	"github.com/gin-gonic/gin"
)

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
	if err := conn.WriteJSON(SystemHub.collectSnapshot(h.storage)); err != nil {
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
