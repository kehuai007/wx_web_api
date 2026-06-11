# Token 调用统计 + 下线"用户/角色"页 设计说明

**Date:** 2026-06-11
**Status:** approved, awaiting implementation plan

## 背景

- `/users`（"用户/角色"）页目前只是一个 placeholder（[users.js:1-13](web/static/js/pages/users.js)），承诺"下个版本上线"但无任何后续，需要彻底拆除。
- 概览页（[dashboard.js:147](web/static/js/pages/dashboard.js)）"今日调用"和"平均耗时"卡片是硬编码 `0` / `–`，需要替换成真实数据。
- 用户希望看到按 token 分组的调用次数统计，跨"今日 / 本周 / 本月 / 总计"四个固定区间，并允许临时指定自定义区间。
- 数据上限：最长保留 2 个月（60 天）的请求日志。

## 范围

### 包含
1. 移除 `/users` 路由、页面、侧边栏入口、`<script>` 引用、页面模块文件。
2. 概览页改造：
   - 顶部固定 6 张数字卡：`Token 数 / 今日成功 / 本周成功 / 本月成功 / 总计 / 平均耗时`，可追加 1 张"自定义"卡。
   - 新增"Token 调用明细"表格卡：行 = 当前 `cfg.Tokens`，列 = `今日 / 本周 / 本月 / 总计`，可追加"自定义"列。
   - 保留现有"最近请求"卡。
3. 后端 SQLite 查询新方法 + `system.snapshot` 帧字段扩展 + 新增 `GET /api/stats` REST 端点（仅自定义区间用）。
4. `history_retention_days` 配置项上限改为 60 天、下限改为 1 天、不再允许 0=永久；现有 0 / >60 的配置在 `Init()` 时 clamp 为 60。

### 不包含
- 不修改解析测试、解析历史、系统信息页（这些页面与本次需求无关）。
- 配置页只动一处：retention 输入框的 `min/max/hint`（如已有控件），不重做布局。
- 不做导出（CSV / 图表）。
- 不做趋势图（每天的折线图）。
- 不做"已删除 token 的历史数据"展示（按用户决策："仅现有配置的 token"）。
- 不做多用户/多角色（恰恰因为"用户/角色"页要被拆掉）。

## 数据语义

| 维度 | SQL 谓词 |
|---|---|
| **算谁** | `status = 0`（成功调用），不区分 `kind`（`url` / `finder` / `auth`） |
| **今日** | `ts >= 今天 00:00:00 (local)` |
| **本周** | `ts >= 本周一 00:00:00 (local)` — 周日时为同周的周一（6 天前），不是下周一 |
| **本月** | `ts >= 本月 1 日 00:00:00 (local)` |
| **总计** | 不限 `ts`；实际等于"近 N 天"，N = `cfg.HistoryRetentionDays`（最长 60） |
| **自定义** | `ts >= startMs AND ts < endMs+1day` — 闭区间起、含止 |
| **平均耗时** | `AVG(latency_ms) WHERE status=0 AND ts >= 今天 00:00:00`；返回整数毫秒；无数据时返回 `0` |

时区固定用 `time.Now().Local()`，与 [log.go:48-52](internal/storage/log.go#L48-L52) 的 `StartOfTodayMs` 保持一致。

## 架构

### 数据流

| 显示项 | 来源 | 更新频率 |
|---|---|---|
| 5 张固定数字卡（今/周/月/总/平均） | WS `system.snapshot.stats.*`（每 2s 推送） | 实时 |
| Token 数卡 | 已有 `GET /api/config`；订阅 `config.changed` 事件 | 配置变化时 |
| Token 调用明细表 4 列 | WS `system.snapshot.stats.by_token`（每 2s 推送） | 实时 |
| 自定义卡 + 自定义列 | REST `GET /api/stats?start=&end=` | 用户点"应用"时一次 |
| 最近请求 | 沿用现有 `loadRecent()` + `log.new` WS 事件 | 已实现 |

### 后端

**`internal/storage/log.go` 新增工具：**
```go
// StartOfWeekMs 返回本周一 00:00 (local) 的 unix ms。
// 周日返回的是同一周的周一（即 6 天前），不是下周一。
func StartOfWeekMs() int64

// StartOfMonthMs 返回本月 1 日 00:00 (local) 的 unix ms。
func StartOfMonthMs() int64
```

**`internal/storage/storage.go` 新增 5 个方法：**
```go
// 全局成功调用数（status=0 且 ts >= sinceMs）。sinceMs == 0 表示不限下界。
func (s *Storage) CountSuccessSince(sinceMs int64) (int64, error)

// 全局成功调用数（status=0 且 startMs <= ts < endMs）。
func (s *Storage) CountSuccessBetween(startMs, endMs int64) (int64, error)

// 按 token 分组（status=0 且 ts >= sinceMs 且 token_label IN labels）。
// labels 为空时返回空 map。结果 map 仅含查到的 label，未调用的 label 不出现。
func (s *Storage) CountSuccessByTokenSince(sinceMs int64, labels []string) (map[string]int64, error)

// 按 token 分组（自定义闭区间）。语义同 CountSuccessBetween + IN labels。
func (s *Storage) CountSuccessByTokenBetween(startMs, endMs int64, labels []string) (map[string]int64, error)

// 今日成功调用的平均耗时（int ms）。无数据时返回 0。
func (s *Storage) AvgLatencyTodayMs() (int64, error)
```

SQL 复用已有索引：`idx_token_ts (token_label, ts)`、`idx_kind_status_ts (kind, status, ts)`、`idx_ts (ts)`。

**`internal/handler/broadcaster.go` 扩展 `ReqStats`：**
```go
type TokenStat struct {
  Label string `json:"label"`
  Today int64  `json:"today"`
  Week  int64  `json:"week"`
  Month int64  `json:"month"`
  Total int64  `json:"total"`
}

type ReqStats struct {
  // 已有字段，保持不变（向后兼容）
  Total  int64 `json:"total"`
  Today  int64 `json:"today"`
  Errors int64 `json:"errors"`

  // 新增（全部 WHERE status=0）
  SuccessToday    int64       `json:"success_today"`
  SuccessWeek     int64       `json:"success_week"`
  SuccessMonth    int64       `json:"success_month"`
  SuccessTotal    int64       `json:"success_total"`
  AvgLatencyToday int64       `json:"avg_latency_today_ms"`
  RetentionDays   int         `json:"retention_days"`   // 用于"总计(近 N 天)"卡的副文案
  ByToken         []TokenStat `json:"by_token"`         // 按 cfg.Tokens 顺序，labels 取自当前配置
}
```

`collectSnapshot()` 中：
1. 从 `config.Get().Tokens` 拿当前 token 列表，提取 `Label` 数组。
2. 查询：
   - 全局 4 次 `CountSuccessSince`：参数分别为 `StartOfTodayMs / StartOfWeekMs / StartOfMonthMs / 0`（0 = 总计无下界）
   - 按 token 分组 4 次 `CountSuccessByTokenSince(sinceMs, labels)`：参数同上
   - 1 次 `AvgLatencyTodayMs`
   - 合计 9 条 SQL，每 2s 一次
3. 把每个 token 的 4 个 map 结果合并成 `[]TokenStat`，顺序与 `cfg.Tokens` 一致；某个 label 在某个 map 里不存在则视作 0。
4. 配置未初始化或 token 列表为空时 `ByToken` 为 `nil`，前端按空数组处理。

**`internal/handler/stats.go`（新文件）：**
```go
func (h *Handler) GetStats(c *gin.Context) {
  // 解析 start / end query 参数（YYYY-MM-DD，local 时区）
  // start 必填、end 必填；end >= start
  // start 不能早于 (today - retention_days)，否则返回 400
  // end 不能晚于 today
  // 返回:
  // { code: 0, data: { range: { start, end },
  //   success_total: N,
  //   by_token: [{ label, count }, ...] } }
}
```

路由注册在 `main.go`，挂在 `/api/*` 的 `SessionAuth` 组下（与 `/api/system`、`/api/config` 同组）。

**`internal/handler/settings.go` `UpdateConfig` 验证：**
```go
if req.HistoryRetentionDays != nil {
    if *req.HistoryRetentionDays < 1 || *req.HistoryRetentionDays > 60 {
        c.JSON(http.StatusOK, model.SimpleResponse{
            Code: 1, Msg: "history_retention_days 必须在 1~60 之间",
        })
        return
    }
    cfg.HistoryRetentionDays = *req.HistoryRetentionDays
}
```

**`internal/config/config.go` `Init` clamp 老配置：**
读完配置后，如果 `HistoryRetentionDays == 0` 或 `> 60`，clamp 为 `60` 并标记 `retentionChanged = true`，让现有的 backfill 写回逻辑把修正值持久化到磁盘。

### 前端

**删除：**
- `web/static/js/pages/users.js` — 整文件 `git rm`
- `web/static/js/router.js:19` — 从 `ROUTES` 删 `/users` 一行
- `web/static/js/router.js:142` — 从 `NAV_ICONS` 删 `users` 一行
- `web/index.html:72` — 删 `<script src="/static/js/pages/users.js?v=1"></script>`

**改写 `web/static/js/pages/dashboard.js`：**
- 模块级 state：`{ stats, customRange, customStats, unsubscribers, tokens }`
- `renderStatGrid()` 渲染 6（或 7）张数字卡；从 `state.stats` 读
- `renderTokenBreakdownCard()` 渲染表格卡：表头依 `state.customStats` 决定是否含"自定义"列；行用 `state.tokens.map(...)`，找不到对应 label 时显示 `0`
- `renderCustomRangeButton()` + `renderCustomRangePopover()`：右上角按钮 → 点击打开 popover；popover 含两个 `<input type="date">` + 应用/清空按钮；起的 `min = today - retention_days`，止的 `max = today`
- 事件：
  - 订阅 `system.snapshot` → 更新 `state.stats` → 重渲数字卡和表格（不重渲整页避免闪烁）
  - 订阅 `config.changed` → 重拉 `/api/config` 取最新 `tokens`
  - 应用自定义区间 → REST 调 `/api/stats` → 写 `state.customStats` → 重渲
  - 清空自定义 → `state.customStats = null` → 重渲（移除卡和列）
- cleanup 时取消所有 WS 订阅

**改 `web/static/css/components.css:148`：**
```css
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: var(--s-4);
}
/* 删除原 @media (max-width: 768px) { .stat-grid { grid-template-columns: 1fr; } } */
```

**追加 `web/static/css/pages.css`：**
- `.token-breakdown-table`：宽度 100%、`border-collapse: collapse`、单元格 padding、数字列右对齐 + `font-variant-numeric: tabular-nums`
- `.custom-range-btn`：右上角按钮样式
- `.custom-range-popover`：浮层定位、白底带阴影、日期输入 + 按钮
- `.stat--custom` / `.col-custom`：自定义卡 / 列的高亮（左边框 `var(--primary)`）

**配置页（`settings.js`）小改：**
- 如果 retention 输入框有数值控件，设 `min=1 max=60`；hint 文字写"1~60 天"。

## 文件清单

| 类型 | 文件 | 改动 |
|---|---|---|
| 删除 | `web/static/js/pages/users.js` | `git rm` 整文件 |
| 修改 | `web/static/js/router.js` | 删 ROUTES `/users` + NAV_ICONS `users` |
| 修改 | `web/index.html` | 删 `users.js` 的 `<script>` |
| 改写 | `web/static/js/pages/dashboard.js` | 6 卡 + 表格 + 自定义区间 popover 逻辑 |
| 修改 | `web/static/css/components.css` | `.stat-grid` 改 auto-fit + 删 768px media |
| 追加 | `web/static/css/pages.css` | 表格、popover、自定义高亮样式 |
| 修改 | `web/static/js/pages/settings.js` | retention 输入 min/max/hint（如有） |
| 新增 | `internal/storage/log.go` | StartOfWeekMs / StartOfMonthMs |
| 修改 | `internal/storage/storage.go` | 5 个新方法 |
| 新增测试 | `internal/storage/storage_test.go` | 6 个用例（见下） |
| 修改 | `internal/handler/broadcaster.go` | `ReqStats` 扩字段 + `collectSnapshot` 重算 |
| 新增 | `internal/handler/stats.go` | `GetStats` 处理器 |
| 修改 | `main.go` | 注册 `/api/stats` 路由 |
| 修改 | `internal/handler/settings.go` | retention 1~60 校验 |
| 修改 | `internal/config/config.go` | Init 时 clamp 老配置 |

## 测试

`internal/storage/storage_test.go` 新增用例：

1. `TestCountSuccessSince_ExcludesNonZeroStatus` — 插 status=0 / 1 / 401 各一条，验证只数 0
2. `TestCountSuccessByTokenSince_GroupsCorrectly` — 多个 token、多个 status，验证 GROUP BY 结果正确
3. `TestCountSuccessByTokenSince_FiltersToLabels` — 一个 token 在日志里、不在 labels 列表里 → 不出现在结果
4. `TestCountSuccessByTokenSince_EmptyLabelsReturnsEmptyMap` — 短路：空 labels 不发 SQL
5. `TestCountSuccessBetween_InclusiveStart_ExclusiveEnd` — 边界：`ts == startMs` 算入，`ts == endMs` 不算
6. `TestAvgLatencyTodayMs_NoDataReturnsZero` — 没有今日记录返回 0；混合 status 时仅 status=0 参与平均

跨日 / 跨周 / 跨月时间边界不单独写用例（用 `StartOfWeekMs` / `StartOfMonthMs` 工具函数生成，工具本身用 `time.Date` 构造可被 unit test 验证，但工具的测试不在本 spec 范围 — 实现时如果时间逻辑复杂可加）。

不写 handler 集成测试（项目当前只有 `events_test.go` 一个 handler 测试）。

## 兼容性 / Breaking Changes

1. **retention 不再允许 0**：现有 0 配置会被 `Init()` clamp 为 60，无声 backfill 到磁盘。> 60 的配置同样 clamp 为 60。运行时无影响（旧值之前也只是用作"无 retention"，新值变成"60 天 retention"，会触发 retention loop 删除老数据 — 但 60 天的数据在多数环境下不会立即被裁掉）。
2. `system.snapshot.stats` 增加字段、不删字段 — 老前端忽略新字段，正常工作。
3. `/users` 路由消失 — 访问 `/users` 自动 fallback 到 `/dashboard`（[router.js:59](web/static/js/router.js#L59) 已有逻辑）。bookmark `/users` 的人会无感跳到首页。

## 性能

- WS 推送频率 2s 不变。每次 9 条 SQL（4 全局 + 4 分组 + 1 AVG），所有谓词命中索引；按 1k 行 / 天估算，60 天 = 60k 行，每条 COUNT/AVG 在 SQLite WAL 模式下 < 1ms。
- 自定义区间 REST 单次最差扫 60 天 = 60k 行，同 SQL plan，单次 < 10ms。

## 不在本 spec

- 趋势折线图 / 每天调用数柱状图
- CSV / JSON 导出
- 按 `kind` 拆分（url vs finder）的统计
- 已删除 token 的历史数据展示
- "永久"保留选项的恢复（如有需要单独提）

## 实现交付方式

实现阶段使用 superpowers:writing-plans 写实施计划；执行时按用户标准（直接 commit 到 main、`Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`）。
