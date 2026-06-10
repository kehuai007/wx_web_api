# 解析历史 设计稿

**日期**: 2026-06-11
**状态**: 待用户复核
**范围**: 单个子项目——实现 `/history` 页面 + 配套的请求日志层（SQLite + `request_log` 表）
**前置依赖**: 解析测试（已上线）、系统信息（已上线）、Token 有效期（已上线）

## 背景与目标

`web/static/js/pages/history.js` 是占位空页面（"该功能将在下个版本上线"）。同时，整个 wx_web_api 进程里**没有任何请求日志记录**：

- 代码库中没有 `database/sql` / `sqlite` / `request_log` 的任何引用
- `internal/handler/handler.go` 的 `ParseWxURL` / `ParseFinderFeedByObjectID` 只负责解析,不落盘
- 但 `dist/wx_web_api.db` / `.db-shm` / `.db-wal` 三个文件已存在,说明 SQLite 路径和 WAL 模式已经预期好,只差 schema

本期要一次性把**日志层**和**`/history` 页面**都补上,并联动更新系统信息页(把"下期接入真实数据"占位的 `stats` 字段填实)、解析测试页(加 `X-Wx-Source` header 标记调用来源)、配置页(加 token label 输入框 + 历史保留天数)。

做完之后,概览页(dashboard)的"最近请求"区块也能直接消费这个数据源——dashboard 真实数据作为本期的下一个小迭代。

**不在本 spec**:
- 用户/角色体系(独立 feature,继续延后)
- 慢请求/错误请求的告警通知
- 解析结果导出(CSV/JSON 下载)
- dashboard 真实数据接入(下一个 spec)
- 日志远程导出 / 上报
- pprof 路由启用(独立 feature)

## 架构

### 改动文件清单

| 文件 | 改动 |
|---|---|
| `go.mod` / `go.sum` | 加 `modernc.org/sqlite` v1.34.5(纯 Go,无 CGO) |
| `internal/storage/storage.go` | **新建**:`Storage` 类型封装 `*sql.DB`,暴露 `Init()` / `Close()` / `LogRequest()` / `QueryHistory()` / `DeleteByIDs()` / `DeleteAll()` / `PurgeOlderThan()` / `Count()` / `CountSince()` / `CountErrors()` |
| `internal/storage/schema.go` | **新建**:`CREATE TABLE` / `CREATE INDEX` 常量 |
| `internal/storage/log.go` | **新建**:`RequestLog` / `RequestLogResult` 结构体 + 入参/出参的 SQLite 行映射 |
| `internal/handler/handler.go` | `Handler` 加 `storage *storage.Storage` 字段;`New` 签名扩展为 `New(pwd, storage)`;`ParseWxURL` / `ParseFinderFeedByObjectID` 里加延迟日志写入;`TokenAuth` 中间件认证成功后把 `token_label` 和 `source` 塞 gin context |
| `internal/handler/system.go` | `broadcaster.collectSnapshot()` 增加 `stats` 字段,真实从 storage 聚合 |
| `internal/handler/broadcaster.go` | `Start(ctx)` 增加 `RunRetentionLoop(ctx, storage)`,每天 03:00 跑一次 `PurgeOlderThan` |
| `internal/handler/settings.go` | 暴露 `history_retention_days`;启动时做 token label backfill 和 retention days 默认值注入 |
| `internal/config/config.go` | `Token` 加 `Label string`;`Config` 加 `HistoryRetentionDays int`;新增 `LoadRawJson()` 工具给 settings handler 探字段存在性 |
| `main.go` | 启动时 `storage.Init(exeDir+"/wx_web_api.db")`;`h := handler.New(effectivePwd, storage)`;`go SystemHub.Start(ctx)` 改成由 `Start` 内部派生 retention goroutine |
| `web/static/js/pages/history.js` | 完整重写:filter bar + 列表 + 行展开 + 分页 + 单条/批量/全清删除 |
| `web/static/js/pages/settings.js` | token 行加 `label` 输入框;配置块底部加"历史保留天数"输入框 |
| `web/static/js/pages/test.js` | 调 `/wx` 和 `/wx/finder` 时加 `X-Wx-Source: admin_test` header |
| `web/static/css/pages.css` | 追加 history 列表/分页/过滤栏/行展开/badges 样式 |

### 不改动的文件

- `internal/service/parser.go`、`internal/model/response.go`(解析服务不变)
- `web/index.html`(`history.js?v=1` 已引用)
- `web/static/js/router.js` / `api.js` / `auth.js` / `store.js` / `app.js`
- `web/static/js/pages/dashboard.js` / `system.js` / `users.js`(users 继续是 stub)
- `internal/handler/broadcaster.go` 之外的所有 handler

## 存储层

### 1. SQLite driver 选择

采用 `modernc.org/sqlite` v1.34.5,**不**用 `mattn/go-sqlite3`:

- 纯 Go,无 CGO —— `build.bat` 不需要 mingw-w64 / MSVC 工具链
- 当前项目 0 CGO 依赖,加 CGO 是一次系统级变更
- admin 工具的 QPS 顶天几十,modernc 性能足够
- 编出来的 binary 体积更小(无 SQLite C 源码)

### 2. DB 文件路径

`config.ExeDir + "/wx_web_api.db"`(也就是 `dist/wx_web_api.db`)。

### 3. schema

```sql
CREATE TABLE IF NOT EXISTS request_log (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  ts            INTEGER NOT NULL,         -- unix milliseconds
  token_label   TEXT    NOT NULL,         -- cfg.Tokens[].label(显示用,不是 token 值)
  kind          TEXT    NOT NULL,         -- 'url' | 'finder'
  source        TEXT    NOT NULL,         -- 'external' | 'admin_test'
  request       TEXT    NOT NULL,         -- 入参 JSON 字符串
  status        INTEGER NOT NULL,         -- 0 成功 / 1 业务错 / 401 鉴权失败
  latency_ms    INTEGER NOT NULL,         -- handler 端到端耗时
  msg           TEXT    NOT NULL DEFAULT '', -- 业务错误信息
  result_data   TEXT                       -- 成功时存 WxParseData JSON;失败为 NULL
);

CREATE INDEX IF NOT EXISTS idx_request_log_ts
  ON request_log(ts DESC);

CREATE INDEX IF NOT EXISTS idx_request_log_token_ts
  ON request_log(token_label, ts DESC);

CREATE INDEX IF NOT EXISTS idx_request_log_kind_status_ts
  ON request_log(kind, status, ts DESC);
```

**字段解释**:
- `token_label`: 用 token 的可读 label 区分(详见"配置改动"一节),不存 token 值——避免 DB 文件泄露时所有 token 一起被拿走
- `kind`: 入参形态。`url` 对应 `POST /wx`,`finder` 对应 `POST /wx/finder`
- `source`: 区分外部生产调用 vs admin 测试页调试调用。`external`(default)/ `admin_test`
- `status`: 0 成功;1 业务错误(解析失败);401 鉴权失败(单独用 HTTP 状态码值,方便过滤)
- `result_data`: 成功时存完整 `WxParseData` 的 JSON,UI 展开行能回看结果。失败时 NULL

**索引设计**:
- `ts DESC`: 主排序键,所有列表/计数都按时间倒序
- `(token_label, ts DESC)`: 按 token 过滤的常见查询
- `(kind, status, ts DESC)`: 组合过滤

### 4. 初始化流程

```
main() 启动:
  1. config.Init(exePath, binName)         # 现有
  2. storage.Init(exeDir+"/wx_web_api.db")
     - sql.Open("sqlite", path)
     - PRAGMA journal_mode=WAL             # 已存在 .db-wal 表明预期
     - PRAGMA synchronous=NORMAL
     - PRAGMA foreign_keys=ON
     - CREATE TABLE IF NOT EXISTS ...
     - CREATE INDEX IF NOT EXISTS ...
     - 启动时若 request_log 表空 → 不需要 backfill(表自增)
  3. handler.New(pwd, storage)
  4. go SystemHub.Start(ctx)                # 内部启动 RunRetentionLoop
```

`Storage.Init` 失败(例如目录不可写)→ main 进程 panic,`log.Fatal` 退出。**DB 不可用等于 service 不可用**,不留"静默降级"路径。

### 5. Storage API

```go
// internal/storage/log.go
type RequestLog struct {
    ID          int64           `json:"id"`
    Ts          int64           `json:"ts"`
    TokenLabel  string          `json:"token_label"`
    Kind        string          `json:"kind"`        // 'url' | 'finder'
    Source      string          `json:"source"`      // 'external' | 'admin_test'
    Request     json.RawMessage `json:"request"`
    Status      int             `json:"status"`      // 0 / 1 / 401
    LatencyMs   int64           `json:"latency_ms"`
    Msg         string          `json:"msg"`
    Result      json.RawMessage `json:"result,omitempty"` // nil if failed
}

type HistoryQuery struct {
    Range   string // 'today' | '7d' | '30d' | 'all'
    Kind    string // 'url' | 'finder' | 'all'
    Status  string // 'ok' | 'err' | 'auth_err' | 'all'
    Token   string // token_label 精确匹配, 'all' 不加约束
    Q       string // 文本搜索 (LIKE '%q%' on request column)
    Page    int    // 1-based
    Size    int    // default 50
}

type HistoryPage struct {
    Total int          `json:"total"`
    Page  int          `json:"page"`
    Size  int          `json:"size"`
    Items []RequestLog `json:"items"`
}

// internal/storage/storage.go
type Storage struct { db *sql.DB }

func (s *Storage) Init(path string) error
func (s *Storage) Close() error

// 单条写入
func (s *Storage) LogRequest(r *RequestLog) error

// 列表查询(支持 Range/Kind/Status/Token/Q/Page/Size)
func (s *Storage) QueryHistory(q HistoryQuery) (*HistoryPage, error)

// 删除: id 列表 / 全部 / 早于 ts
func (s *Storage) DeleteByIDs(ids []int64) (int64, error)
func (s *Storage) DeleteAll() (int64, error)
func (s *Storage) PurgeOlderThan(cutoffMs int64) (int64, error)

// 统计(system 页用)
func (s *Storage) Count() (int64, error)
func (s *Storage) CountSince(sinceMs int64) (int64, error)
func (s *Storage) CountErrors() (int64, error)
```

`QueryHistory` 实现:
1. 根据 `Range` 算出 `ts` 下界
2. 根据 `Status` 把 UI 名映射成 DB 值(`ok→0, err→1, auth_err→401, all→不过滤`)
3. WHERE 子句动态拼接(参数化,**绝不**用 fmt.Sprintf 拼值)
4. `SELECT COUNT(*) FROM request_log WHERE ...` 拿 total
5. `SELECT * FROM request_log WHERE ... ORDER BY ts DESC LIMIT ? OFFSET ?` 拿当前页
6. 反序列化 `request` / `result_data` 列的 JSON 字符串为 `json.RawMessage` 返给调用方

**返回时 `request` / `result` 始终是 JSON object**——handler 层把它们重新 marshal 给前端,前端拿到的就是干净的 object,不用做 "string 包裹" 的额外处理。

### 6. Retention goroutine

```go
// internal/handler/broadcaster.go (新增)
func (h *systemHub) RunRetentionLoop(ctx context.Context, s *storage.Storage) {
    // 第一次跑:启动后 60s 立即清一次(避免老数据 + retention=1 的首次配置立刻生效)
    timer := time.NewTimer(60 * time.Second)
    defer timer.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-timer.C:
            h.runRetentionOnce(s)
            // 下次跑:次日 03:00
            now := time.Now()
            next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
            if !next.After(now) { next = next.Add(24 * time.Hour) }
            timer.Reset(time.Until(next))
        }
    }
}

func (h *systemHub) runRetentionOnce(s *storage.Storage) {
    cfg := config.Get()
    if cfg.HistoryRetentionDays <= 0 { return } // 0 = 永久
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
```

`Start(ctx)` 内部:
```go
func (h *systemHub) Start(ctx context.Context, s *storage.Storage) {
    // 现有 ticker 逻辑
    go h.RunRetentionLoop(ctx, s)
    // ...
}
```

## 日志写入路径

**关键**: 日志写入发生在 handler 内,**与 response 写入并行**(不阻塞 response)。Go handler 用 `defer` 计时 + 写入:

```go
// internal/handler/handler.go
func (h *Handler) ParseWxURL(c *gin.Context) {
    t0 := time.Now()
    label := c.GetString("token_label")
    source := c.GetString("source")  // 'external' | 'admin_test'
    
    var req model.WxParseRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        h.writeLog(c, t0, label, source, "url", 1, err.Error(), nil, gin.H{"url": ""})
        c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: "url is required"})
        return
    }
    
    data, err := h.parser.Parse(req.URL)
    if err != nil {
        h.writeLog(c, t0, label, source, "url", 1, err.Error(), nil,
                   gin.H{"url": req.URL})
        c.JSON(http.StatusOK, model.WxParseResponse{Code: 1, Msg: err.Error()})
        return
    }
    h.writeLog(c, t0, label, source, "url", 0, "", data,
               gin.H{"url": req.URL})
    c.JSON(http.StatusOK, model.WxParseResponse{Code: 0, Msg: "success", Data: data})
}

func (h *Handler) writeLog(c *gin.Context, t0 time.Time, label, source, kind string, status int, msg string, result interface{}, request interface{}) {
    go func() {  // 异步, 不阻塞 response
        latency := time.Since(t0).Milliseconds()
        r, _ := json.Marshal(request)
        var resBytes []byte
        if result != nil {
            resBytes, _ = json.Marshal(result)
        }
        rec := &storage.RequestLog{
            Ts: time.Now().UnixMilli(), TokenLabel: label, Kind: kind, Source: source,
            Request: r, Status: status, LatencyMs: latency, Msg: msg,
        }
        if resBytes != nil { rec.Result = resBytes }
        if err := h.storage.LogRequest(rec); err != nil {
            log.Printf("[storage] LogRequest failed: %v", err)
        }
    }()
}
```

**为什么异步 (goroutine)**:
- 解析完成,response 立刻发出 → 调用方等的是 wx 上游,不是日志落盘
- 日志是观察性数据,**晚 1-10ms 不影响业务**
- 如果同步写入 + SQLite WAL commit 慢(罕见),会拖累 /wx P99 延迟

**鉴权失败 (401) 也要记**:
- `TokenAuth` 中间件 Abort 后,handler 不跑,我们在中间件内手动写一条 status=401 的记录
- 复用同样的 `writeLog` 路径,`kind='auth'`,`request` 存 `{"path": "POST /wx"}` / `{"path": "POST /wx/finder"}` 让 admin 看到当时在试哪个 endpoint
- `token_label` 取自当时匹配的 token(若 expired);完全没匹配上的未知 token 写 `label=""`

```go
// TokenAuth 中间件末尾(放 c.AbortWithStatusJSON 之前)
matched := ... // 现有匹配逻辑
matchedTok := &cfg.Tokens[i]
defer func() {
    var reqObj gin.H
    if c.FullPath() != "" { reqObj = gin.H{"path": c.Request.Method + " " + c.FullPath()} }
    h.writeLog(c, t0, matchedTok.Label, "external", "auth", 401,
               "unauthorized", nil, reqObj)
}()
```

`auth` 作为 kind 是个特殊值,UI 列表展示时单独映射显示(避免被算入"URL"或"finder"统计);`status=auth_err` 过滤器也能筛到这些行。

**Source 来源**:
- `TokenAuth` 中间件读 `X-Wx-Source` header,合法值 `admin_test` → 记 `admin_test`,其他/缺省 → `external`
- admin_test 只来自 `web/static/js/pages/test.js`,外部生产调用方不会加这个 header

## 配置改动

### `internal/config/config.go`

```go
type Token struct {
    Value     string `json:"value"`
    Label     string `json:"label"`        // 新增
    ExpiresAt string `json:"expires_at"`
}

type Config struct {
    ApiBaseUrl           string  `json:"api_base_url"`
    Tokens               []Token `json:"tokens"`
    Port                 int     `json:"port"`
    HistoryRetentionDays int     `json:"history_retention_days"` // 新增, 0 = 永久
}
```

### 启动时 backfill

在 `config.Init` 末尾加:

```go
// 1. Token label backfill
needsWrite := false
for i := range m.config.Tokens {
    if m.config.Tokens[i].Label == "" {
        v := m.config.Tokens[i].Value
        if len(v) > 8 { v = v[:8] }
        m.config.Tokens[i].Label = v + "..."
        needsWrite = true
    }
}

// 2. Retention days 默认值
//    通过 LoadRawJson() 探一下原文件里 history_retention_days 字段是否存在
//    不存在 → 注入 30(只有当需要写时才一并写)
rawCfg, _ := m.loadRawJson()
if _, ok := rawCfg["history_retention_days"]; !ok {
    if m.config.HistoryRetentionDays == 0 {
        m.config.HistoryRetentionDays = 30
        needsWrite = true
    }
}

if needsWrite {
    data, _ := json.MarshalIndent(m.config, "", "  ")
    os.WriteFile(m.path, data, 0644)
}
```

**语义**:
- 老 token 缺 label → 自动用前 8 字符做 label(写文件持久化)
- 老配置缺 retention_days → 注入 30(写文件持久化)
- admin 显式把 retention_days 改成 0 → 文件有 `history_retention_days: 0`,启动时不再注入 30,保留"永久"语义

`loadRawJson()` 工具:
```go
func (m *Manager) loadRawJson() (map[string]json.RawMessage, error) {
    data, err := os.ReadFile(m.path)
    if err != nil { return nil, err }
    var raw map[string]json.RawMessage
    err = json.Unmarshal(data, &raw)
    return raw, err
}
```

## API 改动

### 新增: `GET /api/history`(SessionAuth, admin)

**请求**:
```
GET /api/history?range=today|7d|30d|all
              &kind=url|finder|all
              &status=ok|err|auth_err|all
              &token=<label>|all
              &q=<text>
              &page=1
              &size=50
```

| 参数 | 合法值 | 默认 | 行为 |
|---|---|---|---|
| `range` | `today` / `7d` / `30d` / `all` | `today` | today=`ts >= startOfTodayLocalMs`;7d/30d=同样 N 天前;all=不过滤 |
| `kind` | `url` / `finder` / `all` | `all` | 严格相等 |
| `status` | `ok` / `err` / `auth_err` / `all` | `all` | ok→0;err→1;auth_err→401;all=不过滤 |
| `token` | `<label>` / `all` | `all` | 严格相等;空字符串=all |
| `q` | 任意文本 | `""` | `LIKE '%' || ? || '%'` 搜 `request` 列 |
| `page` | 1+ | `1` | 1-based |
| `size` | 1-200 | `50` | 服务端 clamp 到 [1, 200] |

**响应**:
```json
{
  "code": 0,
  "data": {
    "total": 1234,
    "page": 1,
    "size": 50,
    "items": [
      {
        "id": 5678,
        "ts": 1718023812345,
        "token_label": "marketing-bot",
        "kind": "url",
        "source": "external",
        "request": { "url": "https://mp.weixin.qq.com/s/..." },
        "status": 0,
        "latency_ms": 234,
        "msg": "",
        "result": { "author": "张三", "title": "...", "cover_url": "...", "video_url": "...", "decode_key": "...", "media_type": 2 }
      }
    ]
  }
}
```

`request` / `result` 始终是 object(后端把存储的 JSON 字符串 parse 回来再返)。

**错误**:
- `400`: `size > 200` / `page < 1` → `{code: 1, msg: "invalid size"}`
- `500`: storage 出错 → `{code: 1, msg: "query failed: <err>"}`

### 新增: `DELETE /api/history`(SessionAuth, admin)

| query | 行为 |
|---|---|
| `?id=1,2,3` | 删指定 id(逗号分隔);`data: {deleted: N}` |
| `?all=1` | 删全部(全清);`data: {deleted: N}` |
| 两者都缺 | `400: "id or all required"` |

**为什么都用 DELETE**:
- 语义统一,前端只处理一种 method
- batch 和 all 的区别只是 query 参数

### 改动: `internal/handler/system.go`

`broadcaster.collectSnapshot()` 增强:

```go
type ReqStats struct {
    Total  int64 `json:"total"`
    Today  int64 `json:"today"`
    Errors int64 `json:"errors"`
}

type SystemSnapshot struct {
    Type          string   `json:"type"`
    Ts            int64    `json:"ts"`
    UptimeSeconds int64    `json:"uptime_seconds"`
    Goroutines    int      `json:"goroutines"`
    Mem           MemStats `json:"mem"`
    Stats         *ReqStats `json:"stats"`  // 现在有真实数据了
}
```

聚合 query(每 2s 一次,3 条 SELECT,各走 idx_request_log_ts):
```go
func collectStats(s *storage.Storage) *ReqStats {
    total, _ := s.Count()
    today, _ := s.CountSince(startOfTodayMs())
    errors, _ := s.CountErrors()
    return &ReqStats{Total: total, Today: today, Errors: errors}
}
```

`startOfTodayMs()`: `time.Now()` 取本地零点,转 unix ms。**和 history 的 `range=today` 语义保持完全一致**——同一个零点,否则系统页显示的"今日调用"和 history "今天" 过滤器对不上,会困惑 admin。

## 前端改动

### 1. `web/static/js/pages/settings.js`

**改动 1: Token 行加 Label 输入框**

每行 token 现有: `[value] [expires_at]` → 改为 `[value] [label] [expires_at]`。

- `label` 输入框 placeholder: "可选,默认取前 8 字符"
- 提交 `PUT /api/config` 时把 label 一并带上
- 加载配置时如果 label 是 backfill 的默认值(前 8 字符 + "..."),输入框显示这个值,admin 可改

**改动 2: 配置块底部加"历史保留天数"**

```
┌─ 卡片 2：配置 ──────────────────────────────┐
│  监听端口: 13335                              │
│  API base: ...                                │
│  ...                                          │
│  Token 数: 2                                  │
│  配置文件: ...                                │
│  DB 路径:   ...                               │
│  DB 大小:   1.2 MB                            │
│  ────                                         │
│  历史保留天数: [ 30 ] (0 = 永久)              │
│  当前已记录: 1,234 条                         │
└──────────────────────────────────────────────┘
```

"当前已记录" 用 `GET /api/history?size=1&page=1` 的 `data.total` 字段,或加一个轻量 `GET /api/history/count` 接口。**采用前者**(省一个接口,history 接口已经存在)。

### 2. `web/static/js/pages/test.js`

调 `/wx` 和 `/wx/finder` 时:
```js
headers: Object.assign({ 'X-Wx-Source': 'admin_test' }, callerHeaders)
```

只改一处,handler `TokenAuth` 中间件读这个 header。

### 3. `web/static/js/pages/history.js` (重写)

#### 桌面布局

```
┌─ 卡片 1：过滤栏 ──────────────────────────────────────┐
│  时间: [今天 ▼]  类型: [全部 ▼]  状态: [全部 ▼]          │
│  Token: [全部 ▼]   搜索: [                  ]   [清空]   │
│  共 1,234 条 · 第 1/25 页                              │
└──────────────────────────────────────────────────────┘

┌─ 卡片 2：列表 ─────────────────────────────────────────┐
│  ☑ │ 时间            │ 类型  │ Token            │ 状态  │ 耗时  │ 来源     │ 摘要                  │ 操作 │
│  ☐ │ 14:23:01        │ url   │ marketing-bot   │  ✅   │ 234ms │ external │ mp.weixin.qq.com/s/... │ ⋯    │
│  ...                                                    │
│                                                          │
│  [批量删除]   [<上一页]  1 2 3 ... 25  [下一页>]   [清空] │
└──────────────────────────────────────────────────────┘

行点击展开:
┌─ 展开区 ─────────────────────────────────────────────┐
│ 入参: { "url": "https://mp.weixin.qq.com/s/..." }     │
│ 业务消息: (无)                                         │
│ 解析结果:                                              │
│   Author:    张三                  [复制]              │
│   Title:     文章标题               [复制]              │
│   Cover:     [缩略图] + URL                            │
│   Video URL: https://...  [复制]                       │
│   Decode Key: abc123...           [复制]               │
│   Media Type: 2 (视频)             [复制]               │
│ 原始 JSON:                                             │
│   { ... }                                              │
│ [关闭]                                                 │
└──────────────────────────────────────────────────────┘
```

#### 移动端 (< 640px)

表格 → 卡片式列表:每行一张小卡,显示 `时间 | 状态 | 耗时 | 类型 | token_label | 来源`,点击展开。

#### 过滤器交互

- 时间下拉: `今天 / 近 7 天 / 近 30 天 / 全部`(默认"今天")
- 类型下拉: `全部 / URL / finder`(默认"全部")
- 状态下拉: `全部 / 成功 / 业务错 / 鉴权失败`(默认"全部")
- Token 下拉: 从 `GET /api/config` 拿 cfg.Tokens,渲染成 `<label> · <前8字符>`(没有 label 用前 8 字符值);`全部` 是第一个选项
- 搜索: debounce 300ms,`q` 参数走 `LIKE '%q%'` 搜 request 列
- **任一过滤器变化**:触发重新拉取,page 重置为 1
- **page 变化**:单独触发拉取,不重置过滤器

#### 分页

- 默认 50/页(写死 50,不开放给用户)
- 顶部统计 `共 N 条 · 第 X/Y 页` 实时更新
- 底部 `[<上一页] [1] [2] [3] ... [25] [下一页>]` —— 省略号折叠(当前页 ±2 之外用 `…` 占位)
- 总数 0 但过滤器 active: `无匹配记录 · [清空过滤]`
- 总数 0 且无过滤器: `暂无请求记录,试试在解析测试页发一次请求`

#### 行操作

- **复选框**: 多选 → 顶栏 `[批量删除]`,二次确认
- **`⋯` 按钮**: 单条删除(confirm 弹窗)
- **行本身点击**: 折叠/展开结果详情(点击复选框或 ⋯ 不触发折叠)
- **展开区 `[关闭]`**: 折叠
- **复制按钮**: 行内每个字段一个,复用 test 页的 `.copy-btn` 样式

#### 删除 UX

| 操作 | 流程 |
|---|---|
| 单条 | 行内 `⋯` → `confirm("确定删除此条记录?")` → `DELETE /api/history?id=X` → 成功后从列表移除(不重载) |
| 批量 | 勾选 1+ 行后 `[批量删除]` 亮起 → `confirm("确定删除 N 条?")` → `DELETE /api/history?id=1,2,3,...` → 成功后批量移除 |
| 全清 | 分页区 `[清空]` 按钮 → `confirm("确定删除全部 N 条历史?此操作不可撤销")` → `DELETE /api/history?all=1` → 成功后 reload |

#### 错误处理

| 场景 | 表现 |
|---|---|
| `GET /api/history` 401 | 已有 `authJson` 处理 → 弹登录 |
| `GET /api/history` 500/网络 | 列表区显示 `加载失败 [重试]` |
| `DELETE` 失败 | toast `删除失败:{msg}`,勾选保留,行不消失 |
| 后端崩再恢复 | 列表底部 `[重新加载]` |

#### 性能

- 列表接口只返当前页 + total;展开时**不再**单独拉详情——展开区用的就是列表里已有的字段
- 50 条/页 × ~1KB = 50KB 响应,OK
- 过滤器变化时取消正在 in-flight 的 fetch(`AbortController`),避免竞态覆盖

#### 状态管理

复用 system.js 的清理模式:
```js
var state = {
  filter: { range:'today', kind:'all', status:'all', token:'all', q:'', page:1 },
  abortCtrl: null,
  historyData: null,  // {total, page, size, items: []}
  expandedIds: new Set(),
  selectedIds: new Set(),
};
function render(slot) {
  // 清掉 state 里的 fetch / timers
  state.abortCtrl && state.abortCtrl.abort();
  slot.innerHTML = renderSkeleton();
  load();
}
```

### 4. `web/static/css/pages.css`

追加 ~150 行:

```css
.history-filter { display: grid; grid-template-columns: ...; gap: var(--s-3); }
.history-table { display: flex; flex-direction: column; gap: 0; }
.history-row { ... }
.history-row--expanded { ... }
.history-row__detail { ... }
.history-pagination { ... }
.badge--ok { ... }
.badge--err { ... }
.badge--auth_err { ... }
.badge--source-external { ... }
.badge--source-admin_test { ... }
@media (max-width: 640px) { .history-table { display: block; } ... }
```

复用 test 页已有:
- `.result-fields` / `.field` / `.field-label` / `.field-value`
- `.copy-btn`
- `.result-msg`(错误提示)
- `.empty`(空状态)

## 错误处理

| 场景 | 表现 |
|---|---|
| `GET /api/system` / `/api/history` 401 | 弹登录(已有) |
| DB 不可用(目录权限、磁盘满) | main 进程 panic,启动失败 —— 不可降级 |
| 异步日志写入失败 | `log.Printf("[storage] LogRequest failed: %v", err)`,不影响 response |
| `PurgeOlderThan` 失败 | 日志打印,下次 03:00 再试 |
| `history_retention_days=0` 但有 100 万条数据 | 不清理,DB 会持续增长 —— 这是 admin 显式选择 |
| DELETE 不存在的 id | 不报错,`data.deleted=0`,200 OK |

## 对外契约

**新增**:
- `GET /api/history`(SessionAuth, admin)
- `DELETE /api/history`(SessionAuth, admin)

**改动**:
- `GET /api/config` 响应增加 `history_retention_days` 字段;`tokens[].label` 字段
- `PUT /api/config` 请求/响应增加同上
- `POST /wx` / `POST /wx/finder` 接受 `X-Wx-Source: admin_test` header(可选)
- WebSocket `/ws/system` 推送的 `snapshot.stats` 字段从 null 变成 `{total, today, errors}` 真实值

**新依赖**:
- `modernc.org/sqlite` v1.34.5

**不变**:
- `/wx` 和 `/wx/finder` 的请求/响应 schema 和错误码
- 鉴权中间件行为(除了写日志,业务逻辑零变化)
- 所有现有路由的 path / method / auth 要求

## 实施步骤概要

按依赖关系分 8 步,subagent-driven 推进,每步独立 review:

1. **go.mod + storage 包骨架** —— 加 modernc.org/sqlite,新建 internal/storage/{storage,schema,log}.go,Init 跑通(空表创建成功)
2. **配置改动** —— Token.Label / Config.HistoryRetentionDays;config.Init 加 backfill;settings handler 透传新字段
3. **日志写入路径** —— TokenAuth + ParseWxURL + ParseFinderFeedByObjectID 接入 storage.LogRequest(异步);TokenAuth 在 Abort 时也写 status=401
4. **GET /api/history + DELETE /api/history** —— handler 层 + storage.QueryHistory / DeleteByIDs / DeleteAll
5. **系统页 stats 字段** —— broadcaster.collectSnapshot 真实化;前端 /api/system 拿到 stats 后填健康度卡
6. **settings.js 改动** —— token label 输入框;历史保留天数;当前已记录条数
7. **test.js header 改动** —— X-Wx-Source: admin_test
8. **/history 页面** —— history.js 重写(过滤/列表/展开/分页/删除);pages.css 追加样式

冒烟: 启动 binary → /test 页发一次 /wx 请求 → /history 看到记录 → 改 token label → /history 列表显示新 label → /settings 改 retention=1 → 等下次 03:00(或临时调低阈值)看 purge 跑 → DB 体积下降

## 后续路线图(不在本 spec)

- dashboard "最近请求"区块接入 /api/history(下一期)
- 解析结果导出(CSV / JSON)
- 慢请求/错误请求告警
- 用户/角色体系(继续延后)
- pprof 路由启用
