# Token 有效期 设计稿

**日期**: 2026-06-08
**状态**: 待用户复核
**范围**: 单个子项目——为管理后台配置的 token 增加"过期日期"能力
**前置依赖**: Phase 1（管理 UI 框架，2026-06-04 spec）已完成

## 背景与目标

Phase 1 的 `tokens` 字段是 `string[]`：每个字符串就是一个长期有效的 token，没有生命周期管理能力。本次新增：

- 每个 token 可附带"过期日期"（`yyyy-MM-dd`，可空 = 永久）
- 服务端在 `TokenAuth()` 中**实时**校验过期（无需重启）
- 管理后台可设置、查看、清除过期时间，含"7 天 / 30 天 / 90 天 / 1 年 / 永久"快捷预设
- 配置自动迁移：旧 `string[]` 启动时一次性转成新对象数组

**不在本 spec**：
- 多用户/角色（Phase 2，单独立项）
- Token 调用次数配额（Phase 3）
- 解析历史（Phase 4）
- 统计面板真实数据（Phase 5）
- 解析测试 / 系统信息页面

## 架构

### 改动文件清单

| 文件 | 改动 |
|---|---|
| `internal/config/config.go` | `Token` 新增 `ExpiresAt`；`Tokens` 类型由 `[]string` 改 `[]Token`；启动时一次性格式迁移 |
| `internal/handler/handler.go` | 删除 `validTokens map` 缓存；`TokenAuth()` 改为读 `config.Get()`，过期时返回 `msg:"token expired"`；新增日期解析 cache（`sync.Map`） |
| `internal/handler/settings.go` | `GetConfig` / `UpdateConfigRequest` / `UpdateConfig` 适配新对象结构；过滤掉空 `Value` 项 |
| `web/static/js/pages/settings.js` | token 行增加日期 input + 5 个预设按钮 + 过期状态徽章；`isDirty`/`save`/`cancel` 适配对象结构 |
| `web/static/css/pages.css` | 徽章、预设按钮、日期 input 排版 |
| `dist/wx_web_api.json` | 启动时迁移：旧 `string[]` → 新对象数组，写回 |

### 不改动的文件

- `main.go`：路由注册、NoRoute fallback、CORS 全部不变
- `web/index.html`：app shell 不变
- `web/static/js/router.js`、`auth.js`、`api.js`、`store.js`、`app.js`：框架代码不变
- `internal/handler/handler.go` 中的 `ParseWxURL`、`ParseFinderFeedByObjectID`：不变
- `internal/handler/handler.go` 中的 `GetChallenge`：不变
- `internal/handler/handler.go` 中的 `Login` / `SessionAuth`：会**改动**——把"会话 token"与"外部调用方 token"分开存储，详见 §"登录流程的 token 存储"
- `web/static/js/pages/dashboard.js`、`test.js`、`history.js`、`users.js`、`system.js`：不变
- 外部 `POST /wx` 与 `POST /wx/finder` 的请求/响应 schema：不变

## 数据模型

### Config 结构（Go）

```go
package config

type Token struct {
    Value     string `json:"value"`
    ExpiresAt string `json:"expires_at"` // yyyy-MM-dd；空字符串 = 永久
}

type Config struct {
    ApiBaseUrl string   `json:"api_base_url"`
    Tokens     []Token  `json:"tokens"`
    Port       int      `json:"port"`
}
```

### JSON 形状

```json
{
  "api_base_url": "http://127.0.0.1:2022",
  "tokens": [
    {"value": "wx-web-api-secret-token-2025", "expires_at": ""},
    {"value": "tmp-token-2026-summer",         "expires_at": "2026-09-01"}
  ],
  "port": 13335
}
```

### 启动时迁移

`config.Init()` 中，读取 `wx_web_api.json` 后做一次 `unmarshal` 到一个中间结构 `rawTokens []json.RawMessage`，逐项探测：

- 若 `rawTokens[i]` 解析为 `string` → 视为旧条目，构造 `Token{Value: s, ExpiresAt: ""}`
- 若 `rawTokens[i]` 解析为 `map[string]interface{}{"value":..., "expires_at":...}` → 直接采用

完成转换后**立即**用新 schema 写回磁盘（仅当有任何旧条目存在时），并在 stdout 打 `log.Printf("config: migrated %d tokens to new format", n)`。已经迁移过的 config 下次启动 no-op（不再写回）。

如果同一文件内既有 `string` 又有 `object` 形式（理论上不应出现，但读到的就是事实），视为脏数据：按"先 string 后 object"的顺序全部展开为对象，**写回**。出现混合时在日志里打 warning，提示用户复核。

## 服务端 `TokenAuth()` 行为

### 校验顺序

```go
func (h *Handler) TokenAuth() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")
        if token != "" {
            token = strings.TrimPrefix(token, "Bearer ")
        }
        if token == "" {
            token = c.Query("token")
        }
        if token == "" {
            c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "unauthorized"})
            return
        }
        cfg := config.Get()
        matched := false
        var matchedTok *config.Token
        for i := range cfg.Tokens {
            if cfg.Tokens[i].Value == token {
                matched = true
                matchedTok = &cfg.Tokens[i]
                break
            }
        }
        if !matched {
            c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "unauthorized"})
            return
        }
        if h.isExpired(matchedTok.ExpiresAt) {
            c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "token expired"})
            return
        }
        c.Next()
    }
}
```

`isExpired` 处理：

```go
func (h *Handler) isExpired(expiresAt string) bool {
    if expiresAt == "" {
        return false
    }
    // date cache: sync.Map[string]bool, key = expiresAt, value = parsed time.Time
    // invalidate on config save (see below)
    now := time.Now()
    t, ok := h.dateCache.Load(expiresAt)
    if !ok {
        parsed, err := time.Parse("2006-01-02", expiresAt)
        if err != nil {
            return false // malformed → treat as permanent, log once
        }
        h.dateCache.Store(expiresAt, parsed)
        t = parsed
    }
    parsed := t.(time.Time)
    // 包含到期当天：expiresAt=2026-06-08，6/8 全天可用，6/9 0 点起失效
    // parsedDate 是到期日次日 0:00；now >= parsedDate 即过期
    parsedDate := parsed.Add(24 * time.Hour)
    return !now.Before(parsedDate)
}
```

### 为什么改成实时读 `config.Get()`

`Handler` 启动时构建的 `validTokens map[string]bool` 在 admin 改 config 后不会自动刷新——要么写一个 `Refresh()` 在 settings 写完时调用，要么每次校验时 `config.Get()`。后者更简单且 config 读 RLock 开销可忽略。`Handler` 结构体重构：

```go
type Handler struct {
    parser    *service.ParserService
    pwd       string
    dateCache sync.Map // string → time.Time，缓存解析过的日期
}
```

`New(pwd)` 不再读 cfg.Tokens 构造 map（cfg 仍在 init 后才存在，这里仍读一次用于登录 token 校验；`Login` 路径用 `validTokens` 校验 `Authorization`，但登录成功后写入 map 的逻辑需要保留——下面单列）。

实际上 `New(pwd)` 仍然需要一个会话 token 集合（用于 `Login` 写入 + `SessionAuth` 读出）——重命名为 `sessionTokens` 避免与 cfg.Tokens 混淆。`validTokens` 这个名字废弃：

```go
type Handler struct {
    parser       *service.ParserService
    pwd          string
    sessionTokens map[string]bool  // 管理后台登录生成的会话 token
    dateCache    sync.Map          // expiresAt 字符串 → 解析后的 time.Time
}

func New(pwd string) *Handler {
    return &Handler{
        parser:       service.NewParserService(),
        pwd:          pwd,
        sessionTokens: make(map[string]bool),
    }
}
```

`Login` 改成写入 `h.sessionTokens`；`SessionAuth` 只读 `h.sessionTokens`（**不读 cfg.Tokens、不查过期**）。`IsValidToken` 方法删除。

### 登录流程的 token 存储

`Login` 生成的会话 token（`generateToken()`）是**无过期**的——它是浏览器管理后台的会话凭据，与外部 API 的"调用方 token"是两套：

- 外部调用方 token：在 `cfg.Tokens`，受 `expires_at` 控制
- 管理后台会话 token：登录时生成，存在 `Handler` 内部集合，**不写 config**

为了让 `SessionAuth()` 也能正确响应"会话不存在"（区分未登录 vs token 不对），它需要同时检查：(a) cfg.Tokens 里有没有，(b) `Handler` 内部 session map 里有没有。但**会话 token 不受过期日期约束**（会话 token 是一次性登录生成，由前端 localStorage 持有；admin 想强制下线就重启进程）。

实现：
- `SessionAuth()` 先查内部 session map（hash 后的 token），命中即通过
- 否则查 `cfg.Tokens`，按 `TokenAuth()` 同款逻辑（含过期）
- 都未命中 → 401 `unauthorized`

**或者**简化方案：登录 token 走另一条中间件 `SessionAuth()` 不查 `TokenAuth()` 的过期——只让 `TokenAuth()` 关心 cfg.Tokens 的过期。这保持职责清晰。**采用简化方案**：`SessionAuth()` 只校验内部 session map + cfg.Tokens 存在性，**不查过期**（因为 admin 自己的登录不会被自己配置的过期干掉；如果一个 admin 同时也是"外部调用方 token 的持有者"，那他的浏览器会持有两套：管理后台会话 + 调用方 token；前者无过期，后者按业务过期）。

### `dateCache` 失效

`dateCache` 存 `expiresAt → time.Time`。当 admin 在 settings 保存新 config 时，旧 token 还在 map（key 是 `expiresAt` 字符串）但 cfg 改了——但 cfg 改了的话旧 key 已不存在（不同 expiresAt 就是不同 key），**新 key 进来后会被重新解析**。唯一问题是：如果 admin 把某 token 从"2026-12-31"改成空字符串（永久），旧 `2026-12-31` 还在 cache 中，浪费一点点内存但不构成正确性问题（cache 只查不写新值，命中即返回旧解析结果，但既然 cfg.Tokens 里这个 token 的 expires_at 已经是空，isExpired 根本不会查 cache——`expiresAt == ""` 直接 return false）。**所以不需要主动清 cache**。

如果想洁癖：`config.Save` 后清空 `dateCache`（handler 持有 `*Handler`，`Save` 路径回调清理）。**不做洁癖**——内存泄漏上限是 ~O(distinct expires_at strings ever used)，对于"几十个 token 改几次"的实际使用量无意义。

## 管理接口

### `GET /api/config`

响应：

```json
{
  "code": 0,
  "data": {
    "api_base_url": "http://127.0.0.1:2022",
    "tokens": [
      {"value": "abc", "expires_at": ""},
      {"value": "def", "expires_at": "2026-12-31"}
    ]
  }
}
```

### `PUT /api/config`

请求：

```json
{
  "api_base_url": "http://127.0.0.1:2022",
  "tokens": [
    {"value": "abc", "expires_at": ""},
    {"value": "def", "expires_at": "2026-09-01"}
  ]
}
```

服务端 `UpdateConfig`：

1. 解析请求体 → 校验每项 `value` 非空（trim 后）、`expires_at` 为空或符合 `yyyy-MM-DD` 格式
2. 任一不合法 → 整体拒绝 `{code:1, msg:"invalid tokens: <index reason>"}`
3. 校验通过 → 过滤空 value、过滤完全重复（同一 value 不管 expires_at）→ 写回 cfg
4. 写回磁盘

## 设置页 UI

### 行结构

每个 token 项（原 `.token-item`）改为：

```
[token 值（可显示/隐藏）]  [复制] [显示/隐藏]
[过期：<date input>   7天 | 30天 | 90天 | 1年 | 永久]   [删除]
[过期状态徽章：今天过期 / X 天后过期 / 已过期]
```

桌面端用 grid 排版成两行（值 + 按钮一行，日期 + 预设 + 删除一行），移动端退化为单列。徽章独占一行或紧贴日期 input 后方。

### 预设按钮行为

- 当前 input 解析后的日期与"今天 + N 天"匹配 → 该预设按钮高亮（`btn--active` 变体）
- input 为空 → 仅"永久"按钮高亮
- 都不匹配 → 全部不高亮
- 点预设 → 计算目标日期 `yyyy-MM-DD` 填入 input；点"永久"→ 清空 input

### 过期状态徽章

- `expires_at` 为空 → 不显示
- 解析失败 → 不显示（不阻拦保存）
- 已过今天 → 红色徽章 `已过期`
- 就是今天 → 红色徽章 `今天过期`
- 1~7 天内 → 橙色徽章 `X 天后过期`
- > 7 天 → 不显示

徽章为只读展示，**不阻止保存**（admin 可能就是要保存一个已过期的 token 来观察系统行为；或者干脆"先看到要过期再决定"）。

### 快速算法

```js
function presetDate(days) {
  var d = new Date();
  d.setDate(d.getDate() + days);
  return d.toISOString().slice(0, 10); // yyyy-MM-dd
}
```

### dirty / save / cancel 适配

- `original` / `current` 改为 `[]Token`（或 `{apiBaseUrl, tokens: [Token]}`）
- `isDirty()` 用 JSON 序列化后字符串比较；或自定义深比较（`apiBaseUrl` 字符串 + `tokens` 数组逐项 `value`/`expires_at` 比对）
- `mask` / `copy` / `toggle` / `remove` 操作对象仍是 `current.tokens`，API 不变
- 保存：`PUT /api/config` body 是 `{api_base_url, tokens}` 数组
- 取消：恢复 `current = clone(original)`

### 设置页"新增 token"输入

新增 token 时 `expires_at` 默认为空（永久）；admin 添加后可在行内再设过期。**新增时不弹日期选择器**——保持"先加后配"的两步流。

## 对外契约

| 路径 | 本次是否改 | 变化 |
|---|---|---|
| `POST /wx` | 行为 | 401 时 `msg` 文案可能为 `unauthorized` 或 `token expired`；状态码、headers、其他响应 schema 不变 |
| `POST /wx/finder` | 行为 | 同上 |
| `GET /api/login/challenge` | 否 | — |
| `POST /api/login` | 否 | — |
| `GET /api/config` | schema | `tokens` 字段从 `string[]` 变为 `object[]`（`{value, expires_at}`） |
| `PUT /api/config` | schema | 请求体 `tokens` 同上 |

外部客户端（`POST /wx`、`POST /wx/finder` 的调用方）**不需要任何改动**：它们提交 `Authorization` 头，收到的响应还是 `{code, msg, data?}` 形状，仅在过期时 `msg` 不再是固定 `"unauthorized"` 而是 `"token expired"`。这是一个**响应内容微调**，不构成契约破坏。

## 测试

### 单元 / 集成测试（Go）

> 当前仓库没有 `_test.go` 文件（Phase 1 没有引入）。本次**仍不引入**测试框架——按用户之前在 Phase 1 时的取舍（"本阶段不做"），单元/E2E 自动化属后续工作。功能正确性通过下方冒烟清单验证。

### 冒烟清单（验收）

1. **迁移路径**：手工编辑 `dist/wx_web_api.json` 为 `{"tokens":["abc","def"]}`（旧格式）→ 启动 → 日志出现 `migrated 2 tokens to new format` → 重新读文件确认变为对象数组 → 再启动日志不再出现迁移提示
2. **永久有效**：不设过期 → `curl -H "Authorization: <token>" -d '{"url":"..."}' http://127.0.0.1:13335/wx` → 200
3. **今天过期**：设 `expires_at: 2026-06-08` → 当天调 `POST /wx` 200，模拟次日（修改系统日期或修改 cfg 文件后调）→ 401 `token expired`
4. **明天过期**：设 `expires_at` 为明日 → 当天 OK、次日 401
5. **过期日期格式错误（防御性）**：手工编辑 config 把某 token 的 `expires_at` 写成 `"06/08/2026"`（不是 yyyy-MM-dd）→ 启动不报错（`config.Init` 不会拒绝这种 value）→ 调 `POST /wx` → 200（`isExpired` 解析失败视为永久，详见代码注释）→ 后台 settings 页显示该行不渲染徽章（前端也解析失败，不显示状态）
6. **热更新生效**：admin 在 settings 把某 token `expires_at` 从 `2026-12-31` 改到 `2025-01-01` → 不重启进程 → 调 `POST /wx` 立即 401
7. **管理后台会话不受影响**：admin 用 admin pwd 登录后，admin 自己的会话 token 不受 cfg.Tokens 过期影响
8. **设置页 UI 验收**：
   - 加 token → 出现新行，expires_at 为空
   - 设日期 / 点预设 / 取消 / 重新设
   - 输入框与预设高亮联动（输入 `7 天后` 的日期 → "7天"按钮高亮）
   - 删除 → 行消失、徽章消失、计数 -1
   - 复制 / 显示 / 隐藏 行为保留
   - 取消 → 还原成 original
   - 保存 → 后端 200、刷新页面看到新值
   - 关页面（dirty 状态）→ beforeunload 提示
9. **重复 token**：`value: "abc" expires_at:""` 与 `value:"abc" expires_at:"2026-12-31"` 同时提交 → 设置页输入"abc"时已存在提示
10. **空 value**：`{"value":"", "expires_at":""}` → 设置页 `addToken` 拦截
11. **空数组**：`tokens: []` → 保存成功、`POST /wx` 全部 401
12. **Dashboard 统计**：dashboard 读 `tokens.length`——新结构是对象数组，仍正确反映 token 总数

## 实施步骤概要

1. **`config.go`**：新增 `Token` 类型；改 `Tokens` 字段；写 `migrateConfig` 私有函数；`Init` 末尾调用
2. **`handler.go`**：删 `validTokens` 字段；改 `New`；`TokenAuth` 改实时查 `config.Get()` + `isExpired` 缓存；`Login` 改存 `sessionTokens` 集合（与 cfg.Tokens 分离）；`SessionAuth` 仅查 session map（不查过期）
3. **`settings.go`**：`GetConfig` / `UpdateConfig` 适配新结构；`UpdateConfig` 校验 value 非空 + expires_at 格式
4. **`settings.js`**：行结构调整；预设按钮 + 日期 input 联动；过期徽章渲染；`isDirty` 改深比较
5. **`pages.css`**：徽章、预设按钮组、日期 input 排版
6. **冒烟 1-12 全过**

## 后续路线图（不在本 spec）

- 解析测试页（`/test`）
- 解析历史（持久化 + 列表/详情）
- 用户/角色（多用户管理）
- 系统信息（后端状态/版本/配置诊断）
- Dashboard 真实数据（调用次数/错误率/平均耗时）

本 spec 仅涵盖 Token 有效期。
