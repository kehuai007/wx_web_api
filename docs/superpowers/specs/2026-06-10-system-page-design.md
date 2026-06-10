# 系统信息页面 设计稿

**日期**: 2026-06-10
**状态**: 待用户复核
**范围**: 单个子项目——实现 `/system` 页面（管理后台的"系统信息"入口）
**前置依赖**: 解析测试已完成（无新依赖）

## 背景与目标

`web/static/js/pages/system.js` 当前是占位空页面（"该功能将在下个版本上线"）。该页面是管理员查看服务运行状态的入口：

- 服务启停时间（uptime）
- Go runtime 信息（版本、OS/Arch、goroutine 数、内存使用）
- 版本信息（build tag、编译时间、git SHA）
- 配置摘要（监听端口、API base URL、token 数、配置文件路径、DB 路径/大小）
- 健康度（请求总数、今日调用、错误率——**待 history 实现后填实数**，pprof 链接）

数据通过 **WebSocket 推送**（`/ws/system`）实时刷新，每 2 秒一帧。首次进入页面时同时拉一次 `GET /api/system` 拿静态字段（端口、配置路径、build tag、DB 路径等不会变的值），再通过 WebSocket 推变化的字段（uptime、goroutine、内存、错误率等）。

**不在本 spec**：
- 日志查看 / 实时 tail（独立 feature）
- 远程重启 / 关闭服务
- 配置修改（独立页面 `/settings`）
- 慢请求 / 错误请求的具体记录（这是 history 的活）

## 架构

### 改动文件清单

| 文件 | 改动 |
|---|---|
| `internal/handler/system.go` | **新建**：`SystemHandler` 暴露 `GetSystem`（一次拉静态字段）和 `HandleSystemWS`（WebSocket 升级 + 推 snapshot） |
| `internal/handler/broadcaster.go` | **新建**：进程内的 `systemHub`，管理连接 + 每 2s 收集 runtime 统计广播 |
| `main.go` | 加 `r.GET("/api/system", h.SessionAuth(), systemHandler.GetSystem)` + `r.GET("/ws/system", h.SessionAuth(), systemHandler.HandleSystemWS)`；启动时 `go broadcaster.Start(ctx)` |
| `web/static/js/pages/system.js` | 从空占位重写为完整页面：4 段式 Card（运行时 / 版本 / 配置 / 健康度），WebSocket client，自动重连 |
| `web/static/css/pages.css` | 追加 system 页专用样式（kv 表格、状态指示灯、连接状态徽章） |
| `go.mod` / `go.sum` | 引入 `github.com/gorilla/websocket` |

### 不改动的文件

- 所有 `internal/handler/handler.go`（TokenAuth / SessionAuth / Login 维持原样）
- `internal/handler/settings.go`（不变）
- `web/index.html`（已经引用 `pages/system.js?v=1`）
- `web/static/js/router.js` / `api.js` / `auth.js` / `store.js` / `app.js`
- `web/static/js/pages/dashboard.js` / `test.js` / `history.js` / `users.js` / `settings.js`
- `dist/wx_web_api.db`（读，但**不写**——本 spec 不动 schema；如果 history 决定用 sqlite，到时候再统一定义）

## 页面布局

桌面（≥ 768px）：4 段式 Card 堆叠，2x2 网格在第一段（运行时），其余 3 段单列。移动端：单列堆叠。

```
┌─ 卡片 1：运行时 ────────────────────────────┐
│  Go version:  go1.25.6                       │
│  GOOS / GOARCH:  windows / amd64             │
│  Goroutine 数: 12   🟢 正常                  │
│  内存 Alloc:    3.2 MB                        │
│  内存 HeapSys:  6.8 MB                        │
│  内存 Sys:      12.4 MB                       │
│  连接状态: 🟢 已连接 (ws://...  2s 前)        │
└──────────────────────────────────────────────┘

┌─ 卡片 2：版本 ────────────────────────────────┐
│  Build tag:   dev                              │
│  编译时间:    2026-06-10 15:30:12 (UTC)         │
│  Git SHA:     8947005                          │
└──────────────────────────────────────────────┘

┌─ 卡片 3：配置 ────────────────────────────────┐
│  监听端口:    13335                            │
│  API base:    http://127.0.0.1:2022            │
│  Token 数:    2                                │
│  配置文件:    C:\...\dist\wx_web_api.json       │
│  DB 路径:     C:\...\dist\wx_web_api.db        │
│  DB 大小:     12 KB                            │
└──────────────────────────────────────────────┘

┌─ 卡片 4：健康度 ──────────────────────────────┐
│  请求总数:   1234                              │
│  今日调用:   78                                │
│  错误率:     2.1% (26 / 1234)                  │
│  ────                                         │
│  性能分析:   /debug/pprof/  [打开]             │
│  ────                                         │
│  备注: 请求统计依赖 history log;history 未启用  │
│         时显示 "—"                              │
└──────────────────────────────────────────────┘
```

## 数据流

### 1. 静态字段（一次性 GET）

`GET /api/system` 返回 `SimpleResponse{Code: 0, Data: SystemData{...}}`：

```json
{
  "code": 0,
  "data": {
    "build_tag": "dev",
    "build_time": "2026-06-10T15:30:12Z",
    "git_sha": "8947005",
    "go_version": "go1.25.6",
    "goos": "windows",
    "goarch": "amd64",
    "config_path": "C:\\...\\dist\\wx_web_api.json",
    "db_path": "C:\\...\\dist\\wx_web_api.db",
    "db_size": 12288,
    "port": 13335,
    "api_base_url": "http://127.0.0.1:2022",
    "token_count": 2
  }
}
```

`api_base_url` 和 `token_count` 来自 `/api/config`（前端在拿到 system 响应后再请求一次 `/api/config` 拿 token 数；或者后端聚合，但合并请求会让 system endpoint 依赖 settings 内部，**反而麻烦**——前端拿两次更干净）。**修订：后端从 `config.Get()` 读，避免前端两次请求。**

`db_size` 是文件 stat。

### 2. 动态字段（WebSocket 推送）

WebSocket 协议：

- URL: `/ws/system`（带 `Authorization` header 走 SessionAuth 升级）
- 服务端每 2 秒发一帧 `SystemSnapshot`：
  ```json
  {
    "type": "snapshot",
    "ts": 1718023812,
    "uptime_seconds": 3612,
    "goroutines": 12,
    "mem": { "alloc": 3355443, "heap_sys": 7340032, "sys": 13002342 },
    "stats": { "total": 1234, "today": 78, "errors": 26 }   // history 未启用时为 null
  }
  ```
- 客户端断开时服务端清理连接。
- 客户端断开后重连：指数退避（1s, 2s, 4s, ...，最大 30s），不弹全局 toast（WS 断连在 admin 工具里是正常事件，不算 error）。

### 3. WebSocket 鉴权

WebSocket 升级走标准 HTTP，浏览器通过 `Authorization` header 携带 session token。但浏览器 WS API 不支持自定义 header。两种方案：

1. **session token 走 query string**（`?token=<session_token>`）：本项目现有 `/api/*` 路由的 SessionAuth 已经支持 query string（handler.go:44），复用。`new WebSocket(url + '?token=' + encodeURIComponent(token))`。
2. **首次握手时 cookie 携带**：项目没有 cookie 鉴权，跳过。

选 #1。注意：**`/ws/system` 也走 SessionAuth**，跟 `/api/system` 保持一致。

### 4. 健康度统计的依赖

`stats` 字段（请求总数 / 今日 / 错误率）依赖请求日志表。**History 没实现前**：

- 服务端不读 log（没有这张表）
- 推送帧里 `stats: null`
- 前端显示 `—`，并加一行注释："待 history 启用后填实数"

**History 实现后**：
- 服务端聚合查询 `SELECT COUNT(*), SUM(CASE WHEN success=0 THEN 1 ELSE 0 END) FROM request_log WHERE ts >= <今日0点>`
- 推送帧里 `stats: {total, today, errors}`
- 前端用真实数据

## 后端实现要点

### `internal/handler/system.go`

```go
type SystemData struct {
    BuildTag   string `json:"build_tag"`
    BuildTime  string `json:"build_time"`     // 来自 binary mtime 或编译期注入
    GitSHA     string `json:"git_sha"`        // 编译期注入
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

type SystemSnapshot struct {
    Type          string  `json:"type"`     // "snapshot"
    Ts            int64   `json:"ts"`
    UptimeSeconds int64   `json:"uptime_seconds"`
    Goroutines    int     `json:"goroutines"`
    Mem           MemStats `json:"mem"`
    Stats         *ReqStats `json:"stats"` // null when history not enabled
}

type MemStats struct {
    Alloc   uint64 `json:"alloc"`
    HeapSys uint64 `json:"heap_sys"`
    Sys     uint64 `json:"sys"`
}

type ReqStats struct {
    Total  int `json:"total"`
    Today  int `json:"today"`
    Errors int `json:"errors"`
}

func (h *Handler) GetSystem(c *gin.Context) {
    cfg := config.Get()
    data := SystemData{
        BuildTag:   buildTag,  // from main.go
        BuildTime:  buildTime, // injected at compile time
        GitSHA:     gitSHA,    // injected at compile time
        GoVersion:  runtime.Version(),
        GOOS:       runtime.GOOS,
        GOARCH:     runtime.GOARCH,
        ConfigPath: config.ExeDir + "/wx_web_api.json",  // (already exists)
        DBPath:     config.ExeDir + "/wx_web_api.db",
        Port:       cfg.Port,
        ApiBaseUrl: cfg.ApiBaseUrl,
        TokenCount: len(cfg.Tokens),
    }
    if info, err := os.Stat(data.DBPath); err == nil {
        data.DBSize = info.Size()
    } else {
        data.DBSize = 0
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func (h *Handler) HandleSystemWS(c *gin.Context) {
    // Token check already done by SessionAuth middleware
    conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil { return }
    systemHub.register(conn)
    defer systemHub.unregister(conn)
    
    // Send initial snapshot immediately
    snap := collectSnapshot()
    conn.WriteJSON(snap)
    
    // Block until client disconnects
    for {
        if _, _, err := conn.NextReader(); err != nil {
            return
        }
    }
}
```

**`buildTag` / `buildTime` / `gitSHA` 怎么注入**：

- `buildTag` 已经存在（`main.go:21`），但目前是 const。改成 `var`，编译时 `-ldflags "-X main.buildTag=..."` 覆盖。
- `buildTime`：`var buildTime = time.Now().UTC().Format(time.RFC3339)` 放在 main.go 顶部，但**只在 build 时跑一次**——Go 编译期不会重新跑 main，所以这是 build time。
- `gitSHA`：用 `git rev-parse --short HEAD` 注入：`go build -ldflags "-X main.gitSHA=$(git rev-parse --short HEAD)"`。
- 不引入 build script——`build.bat` / `Makefile` 这些是后话；现在 main.go 里给个默认值（"dev" / "unknown" / "unknown"），生产 build 时手动 ldflags。

### `internal/handler/broadcaster.go`

```go
type systemHub struct {
    mu      sync.RWMutex
    clients map[*websocket.Conn]time.Time // conn -> connected at
}

var SystemHub = &systemHub{clients: make(map[*websocket.Conn]time.Time)}

func (h *systemHub) register(c *websocket.Conn) { ... }
func (h *systemHub) unregister(c *websocket.Conn) { ... }

func (h *systemHub) Start(ctx context.Context) {
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            snap := collectSnapshot()
            h.mu.RLock()
            for c := range h.clients {
                c.WriteJSON(snap)
            }
            h.mu.RUnlock()
        }
    }
}
```

**`collectSnapshot()`** 收集：
- `uptime_seconds = time.Since(processStart).Seconds()`
- `goroutines = runtime.NumGoroutine()`
- `mem = runtime.ReadMemStats(...)` → alloc / heap_sys / sys
- `stats`: history 启用后用 sql 查询；目前为 `nil`

`processStart` 是 `var processStart = time.Now()` 放在 main.go。

### 路由

```go
r.GET("/api/system", h.SessionAuth(), h.GetSystem)
r.GET("/ws/system", h.SessionAuth(), h.HandleSystemWS)

// in main()
go SystemHub.Start(context.Background())
```

**`upgrader`** 是 `gorilla/websocket.Upgrader{ CheckOrigin: func(r *http.Request) bool { return true } }`（admin 工具，不严格 CORS）。

## 前端实现要点

### `web/static/js/pages/system.js`

```js
(function (global) {
  'use strict';
  
  var state = {
    staticLoaded: false,  // GET /api/system 完成
    ws: null,
    reconnectAttempt: 0,
    lastSnapshotTs: 0
  };
  
  async function render(slot) {
    slot.innerHTML = renderSkeleton();  // 4 张卡 + "加载中..."
    await loadStatic(slot);
    connectWS(slot);
  }
  
  async function loadStatic(slot) { ... }   // GET /api/system
  function connectWS(slot) { ... }           // WebSocket + 指数退避重连
  function applyStatic(slot, data) { ... }   // 渲染 build_tag/git_sha/port 等
  function applySnapshot(slot, snap) { ... } // 渲染 runtime / uptime / goroutines
  function renderConnectionBadge(slot, status, lastTs) { ... }  // "🟢 已连接 (2s 前)"
  
  global.WXPages = global.WXPages || {};
  global.WXPages.system = { render: render };
})(window);
```

**WebSocket URL 构造**：
```js
var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
var url = proto + '//' + location.host + '/ws/system?token=' + encodeURIComponent(localStorage.getItem('wx_token'));
```

**重连退避**：
```js
var delay = Math.min(30000, 1000 * Math.pow(2, state.reconnectAttempt));
state.reconnectAttempt++;
setTimeout(connectWS, delay);
```

**pprof 链接**：
- 静态展示 `/debug/pprof/`，点击 `window.open`。
- 当前 binary 不一定启用了 pprof（main.go 没注册）——**所以 pprof 链接目前指向 `/debug/pprof/`，但打开可能 404**。这是已知限制，本 spec 范围内不动；要启用 pprof 是独立 feature。
- 折中：链接**仍然显示**（"打开"按钮），但加 tooltip 提示 "需要服务端启用 pprof 路由"。后续 issue 处理。

## UI 细节

### kv 表格

每个 card 用 `<dl>` 语义化：
```html
<dl class="kv">
  <dt>Go version</dt><dd>go1.25.6</dd>
  <dt>Goroutine 数</dt><dd>12 <span class="badge badge--ok">正常</span></dd>
</dl>
```

CSS：`.kv { display: grid; grid-template-columns: 140px 1fr; gap: var(--s-2) var(--s-4); }`。
`.kv dt { color: var(--text-muted); }` `.kv dd { color: var(--text); font-family: var(--font-mono); }`。

### 连接状态徽章

```html
<span class="conn-badge conn-badge--ok">🟢 已连接 (2s 前)</span>
<span class="conn-badge conn-badge--err">🔴 已断开 (重连中...)</span>
```

3 状态：ok / err / connecting。

### 内存显示

后端返回的是 bytes。前端用 `formatBytes(n)`：`n < 1024 → N B`；`n < 1MB → N.N KB`；`n < 1GB → N.N MB`；`n >= 1GB → N.N GB`。

## 错误处理

| 场景 | 表现 |
|---|---|
| `GET /api/system` 失败 | Card 显示 "加载失败" + 重试按钮（手点） |
| WebSocket 不可达 | 显示 "🔴 已断开 (重连中...)"，自动重连 |
| WebSocket 鉴权失败（401） | 不重连（重连也只会再 401），显示 "🔴 鉴权失败" 提示重新登录 |
| pprof 链接 404 | 打开新 tab 显示 Gin 404 页面（已知） |

## 对外契约

- **新增** `GET /api/system`（SessionAuth，admin）
- **新增** `GET /ws/system`（SessionAuth via query string，admin WebSocket）
- **新依赖** `github.com/gorilla/websocket`
- **不变**所有现有路由

## 实施步骤概要

1. **go.mod**：加 `gorilla/websocket` 依赖
2. **main.go**：buildTime/gitSHA 注入；processStart 全局；注册路由；启动 broadcaster
3. **internal/handler/system.go**：SystemData 定义 + GetSystem handler + HandleSystemWS
4. **internal/handler/broadcaster.go**：hub + Start goroutine + collectSnapshot()
5. **pages.css**：追加 `.kv` / `.conn-badge` / system card 样式
6. **system.js**：render + loadStatic + connectWS + 重连退避
7. **冒烟**：手动刷新看 runtime 数字每 2s 变；停止 binary 看徽章变红；启动 binary 看自动重连

## 后续路线图（不在本 spec）

- pprof 路由启用（独立 feature）
- 实时日志 tail
- 请求统计从 history 表聚合（等 history 实现后回来填）
- 配置热重载（不在 system 页展示，settings 改完即可）
