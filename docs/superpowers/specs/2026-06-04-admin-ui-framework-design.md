# Admin UI 框架 设计稿

**日期**: 2026-06-04
**状态**: 已批准，待实施
**子项目**: 完整运营后台拆分后的第 1 阶段（共 5 阶段）

## 背景与目标

`wx_web_api` 当前管理 UI 是单 HTML + 原生 JS，功能仅"登录 + 配置（API 地址、Tokens）"，视觉简陋、没有导航/多页、没有响应式、配置修改不能热加载。本次目标是搭建一个**可扩展的运营后台 UI 框架**，作为后续 4 个子项目（多用户鉴权、Token 配额、解析历史、统计面板）的承载基座。

**本阶段交付**：
- 完整的视觉与布局框架
- 1 个完整页面（配置）+ 5 个占位页（统一 EmptyState 风格）
- 前端 401 拦截与路由守卫
- 不改任何对外 API 契约

**本阶段不做**（后续子项目处理）：多用户/角色、调用次数统计与配额、解析历史存储、后端日志面板、暗色以外的主题、单元/E2E 自动化。

## 架构

### 前端目录

```
web/
├── embed.go                      # 不变
├── index.html                    # 唯一外壳：登录蒙层 + 应用骨架
└── static/
    ├── css/
    │   ├── tokens.css            # 设计 token（CSS 变量）
    │   ├── base.css              # reset + 全局基础
    │   ├── layout.css            # sidebar / topbar / main 栅格
    │   ├── components.css        # 按钮 / 卡片 / 表单 / 表格 / 标签 / Toast / Modal
    │   └── pages.css             # 配置页等页面级样式
    └── js/
        ├── app.js                # 启动入口
        ├── router.js             # History API 路由表
        ├── auth.js               # 登录 / token 存储 / 401 拦截
        ├── api.js                # fetch 包装（注入 Authorization）
        ├── store.js              # 极简状态：侧边栏折叠、当前用户
        └── pages/
            ├── dashboard.js
            ├── settings.js
            ├── test.js
            ├── history.js
            ├── users.js
            └── system.js
```

### 后端改动

仅一处：在 `main.go` 已注册路由**之后**增加 `NoRoute` fallback，未知 URL 返回 `index.html` 让前端路由接管。**不影响任何已注册的精确路由**（`POST /wx`、`POST /wx/finder`、`/api/*` 等均优先匹配）。

```go
r.NoRoute(func(c *gin.Context) {
    content, err := getFileContent("/index.html")
    if err != nil {
        c.String(500, "Internal error")
        return
    }
    c.Data(200, "text/html; charset=utf-8", content)
})
```

> 复用 `main.go` 已有的 `getFileContent` 包装；不引入新函数。

### 路由方式

客户端 History API SPA。一个 `index.html` 外壳，JS 监听 `popstate` + `pushState` 切换。无刷新、URL 干净、与设计风格（渐变动效）契合。

## 设计系统

### Tokens（CSS 变量，深色为主）

```css
:root {
  /* 表面 */
  --bg:        #0a0a0f;
  --surface:   #14141d;
  --surface-2: #1c1c28;
  --border:    #2a2a3a;

  /* 文字 */
  --text:        #e5e7eb;
  --text-muted:  #9ca3af;
  --text-faint:  #6b7280;

  /* 状态 */
  --primary: #22d3ee;
  --accent:  #a855f7;
  --success: #22c55e;
  --warning: #f59e0b;
  --danger:  #ef4444;

  /* 渐变 */
  --gradient-primary: linear-gradient(135deg, #22d3ee 0%, #a855f7 100%);

  /* 圆角 / 间距 / 字号 / 阴影 / 动效 */
  --r-sm: 6px;  --r-md: 10px;  --r-lg: 14px;  --r-xl: 20px;
  --s-1: 4px;   --s-2: 8px;   --s-3: 12px;   --s-4: 16px;
  --s-5: 24px;  --s-6: 32px;  --s-7: 48px;
  --t-xs: 12px; --t-sm: 13px; --t-base: 14px; --t-lg: 16px;
  --t-xl: 20px; --t-2xl: 24px; --t-3xl: 32px;
  --shadow-sm: 0 1px 2px rgba(0,0,0,.4);
  --shadow-md: 0 4px 12px rgba(0,0,0,.4), 0 0 0 1px rgba(255,255,255,.04);
  --shadow-lg: 0 12px 32px rgba(0,0,0,.5), 0 0 0 1px rgba(255,255,255,.06);
  --ease: cubic-bezier(.4, 0, .2, 1);
  --dur-fast: 120ms; --dur-base: 200ms; --dur-slow: 360ms;
}

@media (max-width: 768px) {
  :root { --sidebar-w: 0px; }   /* 移动端：侧边栏变抽屉 */
}
```

### 核心组件

- **Button**: `btn` 基类 + 变体（`primary` 渐变 / `secondary` 描边 / `ghost` 透明 / `danger` 红）+ 尺寸（`sm` / `md`）。hover 微上浮 + 阴影增强。
- **Card**: 14px 圆角、`--surface` 背景、柔阴影。可选 `card-glow`（青/紫渐变描边）。
- **Form**: 统一输入框、focus 渐变描边、label/helper 排版。
- **Table**: 行 hover、斑马纹可选。
- **Badge**: 状态色小标签。
- **Toast**: 右下角堆叠、3s 自动消失、渐变边框。
- **Modal**: 中央浮层、背景 backdrop blur。
- **EmptyState**: 图标 + 标题 + 副标题 + CTA（占位页用）。
- **Skeleton**: 骨架屏。

### 关键交互

- 路由切换：内容区 200ms `opacity` 渐显
- 按钮 hover：`translateY(-1px)` + 阴影增强
- 侧边栏折叠：240px ↔ 64px，200ms 过渡
- 移动端：侧边栏变抽屉、汉堡按钮触发

## 页面

### `/dashboard` 概览（半完整 + mock 占位）

3 张统计卡片（今日调用 / 错误数 / 平均耗时）+ 最近请求表。卡片数据：当前阶段从 `GET /api/config` 拿到的 `tokens.length` 填充"今日调用"卡片，其他 2 张填 0；表格为静态 mock 5 行；所有数据卡片右下角附小字"下期接入真实数据"。下期接 `/api/stats` 后只换数据源，UI 不动。

### `/settings` 配置（完整实现）

- API 地址：单行输入 + helper 文字
- Token 列表：默认遮罩为 `0xab••••••••`，hover 显示完整，"复制" / "删除"按钮，空态用 EmptyState
- 重复 token 提示、非空校验（与现状后端行为一致）
- 保存按钮 loading 态、成功 Toast
- dirty 检测：未修改禁用保存
- 取消按钮：恢复保存前值
- `beforeunload` 提示未保存修改

### `/test` `/history` `/users` `/system`（4 个占位页）

统一 EmptyState 模板，附"将在 Phase X 上线"小字区分阶段。

### 侧边栏

```
⌂  概览        /dashboard
⚙  配置        /settings
⚡  解析测试    /test
☷  解析历史    /history
☻  用户/角色   /users
ℹ  系统信息    /system
─────────────────
                  (折叠按钮)
```

### 顶栏

```
[≡ 折叠]  页面标题  ·  ·  ·       [🌙 主题]  [用户 ▾]
```

主题切换按钮本期占位、不实装。

### 登录蒙层

- 居中卡片 + 渐变 logo
- 挑战-响应流程保留
- 登录成功后跳到 `intendedRoute`（`localStorage` 存）

## 鉴权

### Token 存储

`localStorage` 存 `wx_token`，与现状一致。`auth.js` 启动检查：未登录 → 登录蒙层；已登录 → 应用骨架。

### 登录流程

```
启动 → 有 token？
        ├── 否 → 登录蒙层，记录 intendedRoute
        └── 是 → 应用骨架，路由到 intendedRoute 或 /dashboard
                ↓
用户在登录页输入密码 → /api/login/challenge
                ↓
simpleHash(pwd + challenge) → /api/login
                ↓
拿到 token → 存 localStorage → 跳 intendedRoute
```

### `api.js` 401 拦截

```js
async function authFetch(url, options = {}) {
  const token = localStorage.getItem('wx_token');
  const headers = { 'Content-Type': 'application/json', ...options.headers };
  if (token) headers['Authorization'] = token;

  const res = await fetch(url, { ...options, headers });
  if (res.status === 401) {
    handle401();
    throw new Error('unauthorized');
  }
  return res;
}

function handle401() {
  localStorage.removeItem('wx_token');
  localStorage.setItem('intendedRoute', location.pathname);
  showLoginScreen();
  toast('登录已过期，请重新登录', 'warning');
}
```

> 401 处理是新增行为，但**只在客户端拦截**，不修改任何后端 handler 或 API 契约。

### 路由守卫

```js
function navigate(path) {
  if (!isLoggedIn() && path !== '/login') {
    localStorage.setItem('intendedRoute', path);
    showLoginScreen();
    return;
  }
  history.pushState({}, '', path);
  render(path);
}

window.addEventListener('popstate', () => render(location.pathname));
document.addEventListener('click', e => {
  const a = e.target.closest('[data-route]');
  if (a) {
    e.preventDefault();
    navigate(a.getAttribute('data-route'));
  }
});
```

### 对外 API 契约（约束验证）

| 路径 | 本期是否改 handler | 入参 | 出参 |
|---|---|---|---|
| `GET /api/login/challenge` | ❌ | — | `{code, challenge}` |
| `POST /api/login` | ❌ | `{pwd, challenge, response}` | `{code, token}` / `{code, msg}` |
| `GET /api/config` | ❌ | — | `{code, data:{api_base_url, tokens}}` |
| `PUT /api/config` | ❌ | `{api_base_url, tokens}` | `{code, msg}` |
| `POST /wx` | ❌ | `{url}` | `{code, msg, data?}` |
| `POST /wx/finder` | ❌ | `{objectId, objectNonceId, ...}` | `{code, msg, data?}` |

后续子项目在 handler **内部** 加鉴权升级、配额统计等，**不改入参出参**。

## 测试

### 冒烟清单（本次验收）

1. 启动：`go build && ./wx_web_api -port 13335` 正常启动，浏览器访问 `http://127.0.0.1:13335/` 显示登录蒙层
2. 登录：错误密码 Toast 失败；正确密码进入应用跳 `/dashboard`
3. 路由：侧边栏 6 项切换无刷新、URL 变化、标题更新、内容渐显；前进/后退不丢滚动位置；直接访问 `/settings` 已登录直接渲染、未登录先登录再回
4. 配置页：API 地址修改保存刷新保留；Token 增/删/复制/重复校验；取消恢复；`beforeunload` 提示
5. 401 场景：改坏 token 触发 401 → 跳登录 + Toast → 登录回原页
6. 响应式：DevTools 切 iPhone 尺寸 → 侧边栏变抽屉、汉堡可开、内容单列、表格横向滚动
7. fallback：直接访问 `/history` 返回 `index.html`，前端路由接管
8. 构建：`go build -ldflags "-s -w"` 通过；产物内嵌资源可独立运行

### 边界场景

| 场景 | 处理 |
|---|---|
| 网络断开 | API 调用 catch → Toast "网络错误"、按钮恢复可点 |
| API 返回非 200 但 `code != 0` | 业务错误，Toast 显示后端 msg |
| 登录蒙层覆盖下轮询 | 401 拦截后强制跳登录，不轮询 |
| localStorage 被禁用 | 启动 `try/catch`，token 退化为内存变量，提示用户 |
| `history.pushState` 不支持 | 退化到 hash 路由（仅 fallback 路径） |
| 启动时配置 JSON 损坏 | 保留现有行为：解析失败用默认配置 |
| 多次连续 401 | `handle401` 幂等，重复触发不重复写 `intendedRoute` |

## 实施步骤（概要）

1. 重组 `web/` 目录到新结构
2. 编写 `tokens.css` `base.css` `layout.css` `components.css` `pages.css`
3. 重写 `index.html`（外壳 + 登录蒙层）
4. 编写 `app.js` `router.js` `auth.js` `api.js` `store.js`
5. 编写 6 个 page 模块
6. 改 `main.go` 加 `NoRoute` fallback
7. 启动 + DevTools 跑冒烟清单
8. 调整至全部通过

## 后续路线图（不在本 spec）

- Phase 2: 多用户/角色（SQLite + 鉴权 handler 内部升级）
- Phase 3: Token 配额管理（handler 内部 + 数据库）
- Phase 4: 解析历史（数据库 + 列表页 + 详情）
- Phase 5: 统计 Dashboard（聚合接口 + 图表）

本 spec 是 Phase 1。
