package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

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
