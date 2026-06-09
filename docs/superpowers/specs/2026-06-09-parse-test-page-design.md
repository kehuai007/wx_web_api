# 解析测试 设计稿

**日期**: 2026-06-09
**状态**: 待用户复核
**范围**: 单个子项目——实现 `/test` 页面（管理后台的"解析测试"入口）
**前置依赖**: Phase 1（管理 UI 框架）、Token 有效期（已完成）

## 背景与目标

Phase 1 的 `/test` 路由存在，但 `web/static/js/pages/test.js` 是占位空页面。该页面是管理员调试 `/wx` 和 `/wx/finder` 接口的工作台：

- 粘贴微信分享链接（`/wx`）或 objectId + objectNonceId（`/wx/finder`），发起一次实际调用
- 字段化展示返回结果（author / title / cover_url / video_url / decode_key / media_type），cover 是图片时缩略图预览
- 显示调用耗时
- 可折叠的"请求/响应详情"区块，给调试用
- 一次会话内的最近测试列表，点击可回填表单
- 鉴权用 `cfg.Tokens` 里的一个 token（管理后台自己的 session token 不能用于外部 API）

**不在本 spec**：
- 解析历史（持久化、跨会话、跨页面的请求日志）—— 单独 feature
- 解析测试的 token 持久化或分享——admin 想用别的 token 直接去配置页加
- 多用户隔离——Phase 2 才有用户/角色
- 自动重试 / 批量测试

## 架构

### 改动文件清单

| 文件 | 改动 |
|---|---|
| `web/static/js/pages/test.js` | 从空占位重写为完整页面：3 个 Card（输入 / 结果 / 历史），调用 `WXApi.authFetch` 发请求；拉取 `/api/config` 拿 token 列表 |
| `web/static/css/pages.css` | 追加测试页专用样式：Tab 切换、表单布局、字段展示、缩略图、可折叠 details、history 列表 |
| `web/static/css/components.css` | 视需要追加 `.tabs`、`.tab` 通用组件（若已存在则复用） |
| `internal/handler/handler.go` | **不改**（路由、模型、解析服务都现成） |
| `internal/handler/settings.go` | **不改**（token 列表通过 `GET /api/config` 已暴露） |
| 后端路由 | **不改**（`POST /wx` 和 `POST /wx/finder` 已存在并用 `TokenAuth`） |

### 不改动的文件

- `main.go`、所有 `internal/*`（Go）代码、模型、配置
- `web/index.html`、`web/static/js/router.js` / `auth.js` / `api.js` / `store.js` / `app.js`
- `web/static/js/pages/dashboard.js` / `users.js` / `system.js` / `history.js` / `settings.js`
- 外部 `POST /wx` 和 `POST /wx/finder` 的请求/响应 schema

## 页面布局

桌面（≥ 768px）和移动都用单列 Card 堆叠，参照 `settings.js` 的三段式：

```
┌─ 卡片 1：输入 ─────────────────────────────────────┐
│ 鉴权 Token: [选择 token ▼]                          │
│ ─────────────────────────────────────────────────── │
│ Tab:  [ URL ]  [ objectId ]                          │
│ ─────────────────────────────────────────────────── │
│ [URL 输入框                              ]  [测试]   │
│ 或                                                  │
│ objectId:    [                              ]      │
│ objectNonceId:[                             ]      │
└────────────────────────────────────────────────────┘

┌─ 卡片 2：结果（仅在有结果后显示） ──────────────────┐
│ 状态: ✅ 成功 / ❌ 失败   耗时: 234 ms   [清空]      │
│ ─────────────────────────────────────────────────── │
│ Author:       张三                                  │
│ Title:        文章标题                                │
│ Cover:        [缩略图]  https://...                  │
│ Video URL:    https://...  [复制]                    │
│ Decode Key:   abc123...                              │
│ Media Type:   2 (视频)                               │
│ ─────────────────────────────────────────────────── │
│ ▶ 请求/响应详情  (折叠)                              │
└────────────────────────────────────────────────────┘

┌─ 卡片 3：本次会话历史（仅内存） ────────────────────┐
│ 共 3 条 · [清空]                                     │
│ • 14:23  ✅ 2026-06-09  https://mp.weixin.qq.com/... │
│   [回填]                                            │
│ • 14:20  ❌ objectId:abc  invalid nonce              │
│   [回填]                                            │
└────────────────────────────────────────────────────┘
```

`cfg.Tokens` 为空时：
- Token 下拉显示 "(无 token，请先在配置页添加)"
- 提交按钮禁用，提示 toast "无可用 token"

## 数据流

### 1. 页面加载

`render(slot)` 流程：
1. 渲染 3 个 Card 骨架（输入区 disabled 状态，显示 "加载中..."）
2. `await WXApi.authJson('/api/config')` → 拿 `cfg.Tokens`
3. 渲染 Token 下拉选项
4. 启用表单

### 2. 提交

点击"测试"按钮：
1. 取选中的 token 作为 `Authorization: <token>`
2. 读当前 Tab 决定 endpoint：
   - **URL Tab**: `POST /wx` with `{ url: <input value> }`
   - **objectId Tab**: `POST /wx/finder` with `{ objectId, objectNonceId, ... }`（只发有值的字段）
3. 记 `t0 = performance.now()`
4. 调 `WXApi.authFetch('/wx', { method: 'POST', body: JSON.stringify(...) })`
5. 记 `t1 = performance.now()`，计算 `elapsed = t1 - t0`
6. 解析响应（`res.json()`）
7. 处理结果（成功 / 失败 / 401 / 网络错误）
8. 追加到 history 列表
9. 重新渲染结果卡

### 3. 鉴权失败（401）

`/wx` 的 `TokenAuth` 中间件要求 Authorization 在 `cfg.Tokens` 里。如果管理员选的 token 已过期或被删除：

- 响应 401 `{"code":401,"msg":"token expired"}` 或 `"unauthorized"`
- 结果卡显示 ❌ 失败，`msg` 字段原样展示
- 不弹全局 toast（结果卡是预期的展示区）

### 4. 网络错误

`fetch` reject（断网、CORS、超时）：

- 结果卡显示 ❌ 失败，`msg: "网络错误"`
- 不区分 timeout vs DNS（暂时够用）

## UI 细节

### Token 选择器

- 标签 "鉴权 Token"
- 下拉显示 `cfg.Tokens` 列表
- 显示格式：`{value 头 8 字符}{过期信息}`，例如 `wx-web-a… · 永久` / `tmp-tok… · 2026-12-31`
- 列表为空时显示提示文字并禁用提交

### Tab 切换

- 顶部两个 Tab：`URL` / `objectId`
- 用 `btn--secondary` 风格（未激活）+ 主题色下划线（激活）
- 切换时不保留另一个 Tab 的输入（避免误用）
- 当前结果卡和 history 记录都打 endpoint 标签，便于识别

### 结果字段展示

`WxParseData` 的 6 个字段一一列出：

| 字段 | 渲染方式 |
|---|---|
| `author` | 文本，可复制 |
| `title` | 文本，可复制 |
| `cover_url` | 缩略图（`img` 标签，`max-width: 200px`），下方 url 文本可复制 |
| `video_url` | 文本 + 复制按钮（无视频预览） |
| `decode_key` | 单行文本 + 复制 |
| `media_type` | 数字 + 已知映射表（`1=图片` `2=视频` `4=文章` `0=未知`），未知值原样显示 |

`data` 为 null（错误响应）：不展示字段，只展示 `msg`。

错误状态：
- `code === 0` → ✅ 成功（绿色）
- `code !== 0` → ❌ 失败（红色），`msg` 高亮展示

### 耗时

调用成功后展示 `耗时 {elapsed} ms`，保留 0 位小数（不到 1ms 显示 `<1ms`）。

### 可折叠"请求/响应详情"

默认折叠。展开后展示：
- 请求：`{ method, url, headers, body }`（token 字段值替换为 `***`）
- 响应：`{ status, body }`

格式用 `<pre>` 块 + JSON.stringify(..., null, 2)。不高亮（不引外部库）。

### History 列表

内存数组 `[{ ts, endpoint, ok, request, response, elapsed }, ...]`，最多 20 条，超出从最早删。

每行：
- 时间戳 `HH:MM:SS`（本地时区）
- 状态图标
- endpoint 标签 (`/wx` 或 `/wx/finder`)
- 入参摘要（URL 短 / objectId 短）
- `[回填]` 按钮：把 request 填回表单，token 选择和 endpoint 切到对应 Tab

无 history 时整卡片不显示（不显示占位），节省空间。

### Tab 样式

文本按钮组：当前 Tab 加底部 2px 主题色下划线 + 文本加粗；非当前 Tab 仅文本，hover 时 `text-muted → text` 渐变。

## 与其他页面的关系

| 页面 | 关系 |
|---|---|
| 配置 (`/settings`) | token 在那里增删，test 页读同一份 |
| 解析历史 (`/history`) | 独立 feature，本页 history 是页面内会话级，不写库；`/history` 是持久的全局视图 |
| 概览 (`/dashboard`) | dashboard 的"最近请求"区块可由 `/history` 数据驱动，与本 feature 独立 |
| 登录 (`/`) | 仍是 admin 登录页，本页用 session token 调 `/api/config` 拿 token 列表 |

## 对外契约

**`POST /wx` 和 `POST /wx/finder` 都不变**（请求/响应 schema、错误码、鉴权）。本 feature 只增加**前端页面**，不引入新接口，不改后端。

唯一的"契约"是隐含的：管理员调用测试页时，所选 token 必须存在于 `cfg.Tokens` 中——这跟生产环境调用方完全一致，没有任何"测试特权"。

## 错误处理矩阵

| 场景 | 表现 |
|---|---|
| 管理员没选 token | 提交按钮禁用，hover 提示 "请先选择 token" |
| `cfg.Tokens` 为空 | 下拉显示空提示，提交按钮禁用 |
| URL Tab 提交空 URL | toast "请输入 URL"，不调 API |
| objectId Tab 提交缺字段 | toast 提示哪个字段空 |
| 401 (token expired / unauthorized) | 结果卡 ❌，`msg` 展示 |
| 502 / 500 等后端错误 | 结果卡 ❌，后端返回的 `msg` 展示 |
| 网络错误 | 结果卡 ❌，固定 `msg: "网络错误"` |
| 解析 JSON 失败 | 结果卡 ❌，`msg: "响应不是合法 JSON"` |

## 实施步骤概要

1. **CSS 增量**：在 `pages.css` 追加测试页专用样式（.tabs / .field / .field-label / .field-value / .result-status / .history-item 等）。如 `components.css` 没有通用 `.tabs` 组件，顺手加上。
2. **`test.js` 重写**：render 函数三段式；fetch 走 `WXApi.authFetch`；latency 用 `performance.now()`；history 数组内存持有。
3. **冒烟**：手动点一遍——选 token、URL Tab 提交、objectId Tab 提交、故意 401 一次、看 history 回填、折叠/展开 details。
4. **提交**：按 subagent-driven 流程 implementer + 两次 review。

## 后续路线图（不在本 spec）

- 解析历史（持久化、跨会话、跨页面的请求日志）
- 解析测试页：批量 URL 测试、循环测试
- 解析测试结果导出（JSON / CSV）
- 测试页 token 选择器支持"自填入"（粘贴任意 token 而不仅限 cfg.Tokens 里的）
