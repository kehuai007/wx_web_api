package main

import (
    "embed"
    "flag"
    "fmt"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "wx_web_api/internal/buildinfo"
    "wx_web_api/internal/config"
    "wx_web_api/internal/handler"

    "github.com/gin-gonic/gin"
)

//go:embed web
var webAssets embed.FS

var buildTag = buildinfo.BuildTag

func getFileContent(name string) ([]byte, error) {
    name = strings.TrimPrefix(name, "/")
    if name == "" {
        name = "index.html"
    }
    path := "web/" + name
    return webAssets.ReadFile(path)
}

func getContentType(name string) string {
    name = strings.ToLower(name)
    if strings.HasSuffix(name, ".html") { return "text/html; charset=utf-8" }
    if strings.HasSuffix(name, ".css") { return "text/css; charset=utf-8" }
    if strings.HasSuffix(name, ".js") { return "application/javascript; charset=utf-8" }
    if strings.HasSuffix(name, ".json") { return "application/json" }
    if strings.HasSuffix(name, ".png") { return "image/png" }
    if strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") { return "image/jpeg" }
    if strings.HasSuffix(name, ".gif") { return "image/gif" }
    if strings.HasSuffix(name, ".svg") { return "image/svg+xml" }
    if strings.HasSuffix(name, ".ico") { return "image/x-icon" }
    if strings.HasSuffix(name, ".woff") { return "font/woff" }
    if strings.HasSuffix(name, ".woff2") { return "font/woff2" }
    return "application/octet-stream"
}

func main() {
    exePath, _ := os.Executable()
    binName := strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))

    pwd := flag.String("pwd", "1", "admin password for web UI")
    port := flag.Int("port", 0, "HTTP listen port (overrides config file)")
    flag.Parse()

    config.Init(exePath, binName)
    cfg := config.Get()

    // CLI flags override config file; pwd always from flag (not config)
    effectivePwd := *pwd
    effectivePort := cfg.Port
    if *port != 0 {
        effectivePort = *port
    }

    h := handler.New(effectivePwd)
    settingsHandler := handler.NewSettingsHandler()

    gin.SetMode(gin.ReleaseMode)
    r := gin.Default()

    // CORS
    r.Use(func(c *gin.Context) {
        c.Header("Access-Control-Allow-Origin", "*")
        c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
        c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
        if c.Request.Method == "OPTIONS" {
            c.AbortWithStatus(204)
            return
        }
        c.Next()
    })

    // Serve web UI
    r.GET("/", func(c *gin.Context) {
        content, err := getFileContent("/index.html")
        if err != nil {
            c.String(500, "Internal error")
            return
        }
        c.Data(200, "text/html; charset=utf-8", content)
    })

    r.GET("/static/*filepath", func(c *gin.Context) {
        fp := c.Param("filepath")
        content, err := getFileContent("static" + fp)
        if err != nil {
            c.String(404, "Not found")
            return
        }
        c.Data(200, getContentType(fp), content)
    })

    // Web UI auth routes (session-based)
    r.GET("/api/login/challenge", h.GetChallenge)
    r.POST("/api/login", h.Login)

    // Config routes (session-authenticated)
    cfgGroup := r.Group("/api/config", h.SessionAuth())
    {
        cfgGroup.GET("", settingsHandler.GetConfig)
        cfgGroup.PUT("", settingsHandler.UpdateConfig)
    }

    // System info routes (session-authenticated)
    r.GET("/api/system", h.SessionAuth(), h.GetSystem)
    r.GET("/ws/system", h.SessionAuth(), h.HandleSystemWS)

    // External API: POST /wx (token-authenticated)
    r.POST("/wx", h.TokenAuth(), h.ParseWxURL)

    // External API: POST /wx/finder (token-authenticated)
    r.POST("/wx/finder", h.TokenAuth(), h.ParseFinderFeedByObjectID)

    // SPA fallback: any unknown GET returns the shell so client-side routing can take over.
    // Does not affect POST /wx, POST /wx/finder, or any /api/* route (they're registered above
    // with exact paths and are matched first).
    r.NoRoute(func(c *gin.Context) {
        if c.Request.Method != http.MethodGet {
            c.AbortWithStatus(404)
            return
        }
        content, err := getFileContent("/index.html")
        if err != nil {
            c.String(500, "Internal error")
            return
        }
        c.Data(200, "text/html; charset=utf-8", content)
    })

    log.Printf("wx_web_api starting on :%d (build: %s)", effectivePort, buildTag)
    if err := r.Run(fmt.Sprintf(":%d", effectivePort)); err != nil {
        log.Fatal(err)
    }
}