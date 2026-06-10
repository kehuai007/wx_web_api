# WebSocket 实时事件推送 设计稿

**日期**: 2026-06-10
**状态**: 待用户复核
**范围**: 单个子项目——把现有的"页面必须手动刷新"改为后端主动推送(`log.new` / `log.deleted` / `config.changed` / `system.snapshot`)
**前置依赖**: system 页 / history 页 / settings 页 均已存在并稳定

## 背景与目标

管理后台的几个高频页面目前都是一次性 fetch + 用户手动点"刷新":

- **dashboard**:最近 10 条请求,要看新条目必须点"刷新"或重新进页。
- **history**:详细历史,必须切页或重新进页才能看到新解析。
- **settings**:另一个会话改了 token/保留天数,本会话完全无感,容易出现误保存覆盖。
- **system**:`/ws/system` 已有 2s 推送但走独立端点,无法和其他事件共享连接。

目标:统一一个 `/ws/events` 端点,后端通过它主动推上述四类事件,前端各页订阅感兴趣的事件类型,实现"开了页面就一直在更新,不必点刷新"。

**不在本 spec**:
- 不引入 SSE(项目已用 gorilla/websocket,沿用更一致)
- 不动 `gorilla/websocket` 版本(`v1.5.3` 已有)
- 不改 schema、不动 `/api/*` 一次性 GET 路径(WS 是增量,旧路径全部保留作兜底)
- 不做"WS 断开时自动 polling 兜底"(断线只影响实时性,业务数据仍有手动刷新按钮)
- 不做连接状态可视化徽章在 topbar 的全局指示(system 页已有的连接徽章够用)

## 架构

### 改动文件清单

| 文件 | 改动 |
|---|---|
| `internal/handler/broadcaster.go` | **重写**:`systemHub` 升格为 `EventsHub`,内部按事件类型分 fanout 协程,`PublishLog` / `PublishLogDeleted` / `PublishConfigChanged` 三个公开方法 |
| `internal/handler/system.go` | **重写**:`HandleSystemWS` 拆为 `HandleEventsWS`,从 `EventsHub` 拉 snapshot 推第一帧,后续事件由 EventsHub 自驱 |
| `internal/handler/handler.go` | `writeLog` 末尾 `defer EventsHub.PublishLog(*rec)`,在 DB 写完之后 publish |
| `internal/handler/history.go` | `DeleteHistory` 三个成功路径(单删/批删/全清)末尾 `EventsHub.PublishLogDeleted(ids)` |
| `internal/handler/settings.go` | `UpdateConfig` `config.Save` 成功之后 `EventsHub.PublishConfigChanged()` |
| `main.go` | `go handler.SystemHub.Start(...)` 改为 `go handler.EventsHub.Start(...)`;`r.GET("/ws/system", ...)` 改为 `r.GET("/ws/events", ...)` |
| `web/static/js/events.js` | **新建**:全局唯一 WebSocket 客户端,`subscribe(type, handler)` API + 指数退避重连 + 鉴权失效跳登录 |
| `web/static/js/app.js` | `showApp()` 末尾 `WXEvents.start()`;`logout()` 调 `WXEvents.stop()` |
| `web/static/js/pages/system.js` | 删除自己的 WS 客户端(整段 `connectWS` / `scheduleReconnect` / `probeAuth` / `startWatchdog`),改为 `WXEvents.subscribe('system.snapshot', ...)`;保留 `applySnapshot` / `applyStatic` / 渲染逻辑 |
| `web/static/js/pages/dashboard.js` | 新增 `bindEvents(slot)`:订阅 `log.new` 直接插顶部,订阅 `config.changed` 静默重拉 token 数 |
| `web/static/js/pages/history.js` | 新增 `applyLogNew` / `applyLogDeleted` / `logMatchesFilter` 三个纯函数;订阅两类事件接入;新增 `state.unreadCount` 与"有新 N 条"提示 |
| `web/static/js/pages/settings.js` | 新增顶部 `<div data-role="config-stale">` 提示条;订阅 `config.changed`,脏表单时弹条二次确认,干净表单静默重拉 |
| `web/index.html` | 脚本顺序中 `auth.js` 之后插入 `<script src="/static/js/events.js"></script>` |
| `internal/handler/events_test.go` | **新建**:EventsHub 单元测试(广播给所有 client、缓冲满不阻塞、client 关闭自动清理) |

### 不改动的文件

- `internal/handler/handler.go` 中的 `TokenAuth` / `SessionAuth` / `Login` / `GetChallenge` 不动
- `internal/storage/*` 不动
- `internal/config/*` 不动
- `internal/buildinfo/*` 不动
- `web/static/js/auth.js` / `api.js` / `store.js` / `router.js` 不动
- `web/static/css/*` 不动
- `go.mod` / `go.sum` 不动(已含 `gorilla/websocket v1.5.3`)
- `dist/wx_web_api.json` 配置文件不动(WS 推送是纯网络层,不影响 schema)

## 后端设计

### EventsHub 内部结构

```go
// eventsHub 是进程内唯一的 WS 事件多路复用器。拥有:
//   - 一个客户端注册表
//   - 三个事件类型的入队 channel(log / logDeleted / config)
//   - 三个 fanout 协程(每个类型一个)
//   - system 快照的 2s ticker
type eventsHub struct {
    mu      sync.RWMutex
    clients map[*eventClient]time.Time
    logCh     chan storage.RequestLog
    logDelCh  chan []int64
    configCh  chan struct{}
    // systemTicker 由 Start 内部启动
    storage *storage.Storage
}

type eventClient struct {
    conn    *websocket.Conn
    writeCh chan []byte  // 每 client 一条写队列
    closeFn func()       // 触发 unregister + 关闭
}
```

**入队缓冲**:
- `logCh`:256(覆盖突发请求日志,缓冲满丢弃,记日志)
- `logDelCh`:64
- `configCh`:16

**Publish 接口**(对外):

```go
// PublishLog 在 writeLog 内部 DB 写完后调用。失败仅记日志,绝不阻塞调用方。
func (h *EventsHub) PublishLog(r storage.RequestLog) {
    select {
    case h.logCh <- r:
    default:
        log.Printf("[events] logCh full, drop log id=%d", r.ID)
    }
}

func (h *EventsHub) PublishLogDeleted(ids []int64) {
    if len(ids) == 0 { return }
    select {
    case h.logDelCh <- ids:
    default:
        log.Printf("[events] logDelCh full, drop %d ids", len(ids))
    }
}

func (h *EventsHub) PublishConfigChanged() {
    select {
    case h.configCh <- struct{}{}:
    default:
        log.Printf("[events] configCh full, drop config.changed")
    }
}
```

**三个 fanout 协程**:

```go
func (h *EventsHub) Start(ctx context.Context, s *storage.Storage) {
    h.storage = s
    go h.systemTickerLoop(ctx)   // 2s tick,生成 system.snapshot
    go h.logFanoutLoop(ctx)      // 从 logCh 读,广播 log.new
    go h.logDelFanoutLoop(ctx)   // 从 logDelCh 读,广播 log.deleted
    go h.configFanoutLoop(ctx)   // 从 configCh 读,广播 config.changed
}
```

每条 fanout 在收到事件后,持 RLock 遍历 clients,把 frame 推到每个 client 的 `writeCh`(`select { case c.writeCh <- frame: default: log.Printf("client slow, drop"); c.closeFn() }`)。**慢客户端检测**:某 client 写队列满 64 即视为慢,主动 close 触发 unregister——这与现有 `systemHub` 行为一致(per-client write goroutine,慢客户端被踢)。

**Per-client write goroutine**:每注册一个 client,启动一个 goroutine:

```go
go func(c *eventClient) {
    for frame := range c.writeCh {
        if err := c.conn.WriteMessage(websocket.TextMessage, frame); err != nil {
            return
        }
    }
}(c)
```

写协程退出 = 连接挂了,deferred `unregister` 兜底清理。

**system.snapshot 帧生成**:从现有 `collectSnapshot(s)` 抽出来,直接复用代码,只改 frame wrapper:

```go
{
    "type": "system.snapshot",
    "ts": ...,
    "uptime_seconds": ...,
    "goroutines": ...,
    "mem": {...},
    "stats": {...}
}
```

### Wire 协议

所有帧均为 JSON `TextMessage`,统一带 `type` 字段:

```json
// 服务端推,system 页消费(替代现有 /ws/system)
{ "type": "system.snapshot", "ts": 1749576000, "uptime_seconds": ..., "goroutines": 42, "mem": {"alloc": 123, "heap_sys": 456, "sys": 789}, "stats": {"total": 100, "today": 5, "errors": 2} }

// 服务端推,history 页 / dashboard 页消费
{ "type": "log.new", "log": { /* 完整 storage.RequestLog,含 result */ } }

// 服务端推,history 页消费
//   - ids 为非空数组:前端从 state.data.items 删这些 id,total -= ids.length
//   - ids 为 null / 空数组:前端清空 state.data.items 并 total=0(全清信号)
{ "type": "log.deleted", "ids": [12, 34, 56] }
{ "type": "log.deleted", "ids": null }

// 服务端推,settings 页 / dashboard 页 / history 页消费
{ "type": "config.changed", "ts": 1749576000 }

// 客户端发(可选,用于请求"立即推一帧 system.snapshot"作为首帧)
{ "type": "client.hello" }
```

`client.hello` 设计:客户端在 `onopen` 后立即发,服务端收到后立刻推一条 `system.snapshot`,避免 history / dashboard 页面订阅时缺失首帧。**不携带 token**(已通过 upgrade 时的 query 鉴权)。

### Hook 点

| 位置 | 改动 |
|---|---|
| `handler.handler.go::writeLog`(末尾) | `defer EventsHub.PublishLog(*rec)`。注意:writeLog 本身是 `go func`,从 goroutine 内 publish 是安全的;**defer 写在 `go func` 内部** |
| `handler.history.go::DeleteHistory`(三处成功路径) | 单删:`EventsHub.PublishLogDeleted([]int64{id})`;批删:`EventsHub.PublishLogDeleted(ids)`;全清:`EventsHub.PublishLogDeleted(nil)`(用 `nil` 表示清空所有) |
| `handler.settings.go::UpdateConfig`(`config.Save` 成功后) | `EventsHub.PublishConfigChanged()`。在 c.JSON 200 之前调,确保 publish 在响应到达客户端之前进入 fanout |
| `main.go` | `r.GET("/ws/system", h.SessionAuth(), h.HandleSystemWS)` 改为 `r.GET("/ws/events", h.SessionAuth(), h.HandleEventsWS)`;`SystemHub` 符号全部替换为 `EventsHub` |

`log.new` payload 包含完整 `storage.RequestLog`(`id` / `ts` / `token_label` / `kind` / `source` / `client_ip` / `request` / `status` / `latency_ms` / `msg` / `result`)。带宽权衡:解析 result 可能数百 KB;突发场景下行可能大,但缓冲满会丢帧,业务 `/wx` 路径不阻塞。`request` 是 POST body 摘要(短),`result` 是完整解析输出(可能大,接受这点代价以避免另开单条 GET API)。

## 前端设计

### `web/static/js/events.js`(新文件)

**核心导出**:`window.WXEvents`,提供:
- `start()`:建立 WS 连接,鉴权失败由内部调用 `WXAuth.handle401()`。
- `stop()`:主动断开(登出 / 鉴权失效后调用)。
- `subscribe(type, handler)`:返回 unsubscribe 句柄(也可存下来用 `unsubscribe`)。
- `unsubscribe(type, handler)`。
- `connectionStatus` getter:`'connecting' | 'ok' | 'err' | 'auth_err'`。
- `onStatusChange(handler)`:订阅状态变化;返回 unsubscribe 句柄(供 system 页的连接徽章使用)。
- 内部:每个 type 一个 `Set<handler>`,`onmessage` 解析后 fanout 给对应集合。

**重连**:
- `RECONNECT_BASE_MS = 1000`
- `RECONNECT_MAX_MS = 30000`
- 指数退避,网络恢复时 `reconnectAttempt` 重置为 0
- watchdog:每 2s 检查,若 `connectionStatus === 'ok'` 且 `Date.now() - lastFrameAt > 6000ms`,主动 close 触发重连

**鉴权探测**:沿用 `system.js` 的 `probeAuth` 模式——断开后用 `WXApi.authJson('/api/system')` 探测一次,`isAuth` 错误则标记 `auth_err` 并跳登录。

**首帧触发**:onopen 后发 `client.hello`,服务端立刻推 `system.snapshot`;之后 system.snapshot 按 2s tick 自然到达。`log.new` / `config.changed` 是事件驱动,无需 hello。

**加载顺序**:`events.js` 必须早于 `app.js`、晚于 `auth.js`(要用 `WXAuth`)。在 `index.html` 脚本顺序中:
```html
<script src="/static/js/auth.js"></script>
<script src="/static/js/events.js"></script>   <!-- 新增 -->
<script src="/static/js/api.js"></script>
<script src="/static/js/store.js"></script>
<script src="/static/js/router.js"></script>
<script src="/static/js/pages/..."></script>   <!-- 已有 -->
<script src="/static/js/app.js"></script>
```

### 页面接入规则

| 页面 | 订阅类型 | 行为 |
|---|---|---|
| `dashboard.js` | `log.new` | `state.recent.unshift`;`length > 10` 时 `pop()`;重渲染 recent 列表 |
| `dashboard.js` | `config.changed` | 静默 `loadTokenCount(slot)` |
| `history.js` | `log.new` | 见下方"智能插入"规则 |
| `history.js` | `log.deleted` | 从 `state.data.items` 删对应 id;`state.data.total -= ids.length`;`renderList()` |
| `history.js` | `config.changed` | 重拉 token 下拉(`loadTokenLabels` + `populateTokenDropdown`) |
| `settings.js` | `config.changed` | 脏表单 → 弹"配置已更新,重新加载?/忽略"条;干净表单 → 静默重拉 `/api/config` |
| `system.js` | `system.snapshot` | 复用现有 `applySnapshot` 逻辑 |
| `system.js` | `connectionStatus` 变化 | 调 `updateConnectionBadge` |

**`history.js` 智能插入规则**:

```
state.unreadHintVisible = false  // 布尔,只控制提示显隐,非累加计数

function applyLogNew(slot, log):
    // 全清事件不是 log.new 的事,这里只处理单条新行
    if state.page > 1:
        // 不在第 1 页,只更新计数 + 提示
        state.data.total += 1
        state.unreadHintVisible = true
        renderUnreadHint(slot, text='有 1 条新记录,点此查看第 1 页')
        return
    if logMatchesFilter(log, state.filter):
        state.data.items.unshift(log)
        state.data.total += 1
        if state.data.items.length > state.size:
            state.data.items.length = state.size  // 截断
        renderList(slot)
    else:
        state.data.total += 1
        state.unreadHintVisible = true
        renderUnreadHint(slot, text='有 1 条新记录不符合当前筛选,点此清空筛选查看')

function applyLogDeleted(slot, ids):
    if !ids || ids.length === 0:
        // 全清信号
        state.data.items = []
        state.data.total = 0
        state.unreadHintVisible = false
        renderList(slot)
        return
    // 单删 / 批删
    const set = new Set(ids)
    state.data.items = state.data.items.filter(function(it){ return !set.has(it.id) })
    state.data.total = Math.max(0, state.data.total - ids.length)
    renderList(slot)
```

`logMatchesFilter(log, filter)`:

| filter 字段 | 判定 |
|---|---|
| `range` | `today` / `7d` / `30d` / `all` 同 `HistoryQuery.tsLowerBoundPtr` 的 JS 版 |
| `kind` | `log.kind === filter.kind` 或 `all` |
| `status` | `0/1/401` ↔ `ok/err/auth_err` 映射,或 `all` |
| `token` | `log.token_label === filter.token` 或 `all` |
| `q` | `JSON.stringify(log.request).includes(filter.q)`(近似后端 `request LIKE %q%`) |

**`settings.js` 二次确认条**:

```html
<div class="card" data-role="config-stale" hidden>
  <div class="card__title">配置已被其他会话更新</div>
  <div class="kv__sub">当前表单有未保存的修改,刷新将丢失这些修改。</div>
  <button class="btn btn--primary" data-role="config-stale-reload">重新加载</button>
  <button class="btn btn--secondary" data-role="config-stale-ignore">忽略</button>
</div>
```

- `dirty` 判定:表单内任何 input/select 内容与上次拉取快照不一致(初始化时拉一次保存 baseline,input 事件标 dirty)。
- "重新加载"→ `WXApi.authJson('/api/config')` → 用响应重置表单 + 重设 baseline + 隐藏条。
- "忽略"→ 仅隐藏条;表单 dirty 状态保持。
- **自身保存后的免提示窗口**:`PUT /api/config` 收到 200 响应时,记 `selfSaveAt = Date.now()` 并设置 `ignoreConfigChangedUntil = selfSaveAt + 2000`。`config.changed` 事件回调先检查 `Date.now() < ignoreConfigChangedUntil`,若在窗口内则直接 return(不 reload、不弹条)。2s 窗口足以覆盖 publish → fanout → client receive 整条链路上所有本地时延。

### `system.js` 改动细节

**删除**:
- `RECONNECT_BASE_MS` / `RECONNECT_MAX_MS` / `STALE_THRESHOLD_MS` 常量
- `state.ws` / `state.reconnectAttempt` / `state.reconnectTimer` / `state.watchdogTimer` 字段
- `buildWsUrl` / `connectWS` / `probeAuth` / `handleDisconnect` / `scheduleReconnect` / `startWatchdog` 函数
- `connectionBadge` 中"连接中/已断开/鉴权失败" 的本地状态(改用 `WXEvents.connectionStatus`)

**保留**:
- `escapeHtml` / `formatBytes` / `formatDuration` / `ago` / `pad2` / `placeholderRow` / `realRow` / `renderSkeleton` / `applyStatic` / `loadStatic` / `renderStaticError` / `applySnapshot` / `updateConnectionBadge`

**新增**:
- `state.subscriptions = []`,render 末尾 `WXEvents.subscribe(...)` 拿句柄 push 进去
- `state.cleanup = function() { state.subscriptions.forEach(unsubscribe); state.subscriptions = []; }`
- render 开头先 `state.cleanup()` 避免上一次 render 残留
- 替换 `connectionBadge` 的本地 `state.connectionStatus` 为 `WXEvents.connectionStatus`(每次 render 拉一次,变化时通过订阅触发 `updateConnectionBadge`)

## 测试

### Go 单元测试(`internal/handler/events_test.go`)

| 用例 | 验证点 |
|---|---|
| `TestEventsHub_PublishLog_BroadcastsToAllClients` | 两个 client 都收到 `log.new` 帧 |
| `TestEventsHub_PublishLog_NonBlockingWhenFull` | 缓冲满时 publish 不阻塞(用超时验证) |
| `TestEventsHub_PublishConfigChanged_Broadcasts` | 三个 client 都收到 `config.changed` |
| `TestEventsHub_ClientUnregister_StopsReceiving` | 关闭一个 client 后不再收帧,其他仍正常 |
| `TestEventsHub_SystemSnapshot_FiresOnTicker` | 2s 内收到至少一帧 `system.snapshot` |
| `TestEventsHub_WireShape` | 解析 JSON 后 type 字段、payload 字段与 spec 一致 |

用 `httptest.NewServer` + `gorilla/websocket.Dialer` 起真实 WS,不引第三方 mock。`storage.Storage` 用最小内存实现或 sqlite `:memory:`。

### 前端手测 checklist

- [ ] 登录后 dashboard "最近请求"在别人发请求后 1s 内顶部出现新行
- [ ] 切到 history 页,新行按 filter 智能插入
- [ ] history 第 3 页时来新行,顶部出现"有新 1 条"提示,total 增 1,本页不变化
- [ ] history 全清后,`log.deleted` 触发列表清空,total 归 0
- [ ] settings 脏表单时另一会话保存 → 弹条;点"重新加载"覆盖;点"忽略"仅关条
- [ ] settings 干净表单时另一会话保存 → 静默重拉无提示
- [ ] system 页数值、内存曲线、uptime 滚动与改造前一致
- [ ] 服务端重启后,所有 WS 客户端 30s 内自动重连
- [ ] 主动 close 服务端,客户端连接徽章变红,30s 后重连成功
- [ ] token 过期刷新页面 → 401 跳登录,events.js 不在登录态下持续重连
- [ ] settings 自身保存后 2s 内不弹自身推送导致的二次确认条

### 风险与缓解

| 风险 | 缓解 |
|---|---|
| Goroutine 泄漏(client disconnect 未 unregister) | 写协程退出时调 closeFn → unregister 链式清理;`defer unregister` 兜底 |
| `WriteMessage` 并发调用 panic | 每 client 一条写 goroutine,外部推 `writeCh`,序列化写 |
| `publish` 阻塞业务路径 | 所有 Publish 走 `select { case ch <- x: default: log... }`,缓冲满丢弃 |
| `log.new` payload 过大(result 字段) | 接受带宽代价;突发场景由缓冲满丢帧保护;前端不依赖推送必达(手动刷新兜底) |
| 鉴权过期导致 WS 不断重连轰炸服务端 | probeAuth 401 → 标 `auth_err` + 跳登录,不再重连;等待用户重新登录后由 showApp 触发 start |
| 多页同时订阅同一事件 | events.js 内部 Set,handler 多次注册互不影响;router 切换 render 时旧 handler 必须 unsubscribe(在 render 开头 cleanup) |
| `client.hello` 服务端拒收 | 协议兼容:不发送 hello 时,system.snapshot 仍按 2s tick 到达,只是首帧延迟;前端可容忍 |

### 回滚策略

- 全部改动走一个 feature commit + 一个 revert,无需 feature flag。
- 旧的 `/api/history` / `/api/config` / `/api/system` 一次性 GET 全部保留,WS 挂了用户仍能手动刷新。
- 旧的 `/ws/system` 端点删除;若有外部监控依赖它,回滚 commit 即可恢复。
- 部署后第一周观察 `[events]` 日志中的 `drop` 频率,持续 > 1/s 考虑扩大 channel 缓冲。
