package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

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

// HandleSystemWS is implemented in broadcaster.go (Task 3). Stub here to
// keep the file compiling if main.go references it before Task 3 lands.
// REMOVE THIS STUB WHEN TASK 3 LANDS.
func (h *Handler) HandleSystemWS(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"code": 1, "msg": "ws not yet wired"})
}
