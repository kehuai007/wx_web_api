# Admin UI Framework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a complete admin UI framework — design system, layout shell, client-side router, 1 full page (settings) + 5 placeholder pages, 401 handling — without changing any external API contracts.

**Architecture:** Single `index.html` shell + client-side History API SPA. Vanilla JS modules (no framework, no build step) loaded via `<script>` tags. CSS via `<link>` tags. One `NoRoute` handler on the Go side falls back to `index.html` for unknown GET paths so the SPA routing can take over.

**Tech Stack:** Go 1.25, Gin, vanilla HTML/CSS/JS, Chrome DevTools for smoke testing.

**Spec:** `docs/superpowers/specs/2026-06-04-admin-ui-framework-design.md`

---

## File Map

**New files:**
- `web/static/css/tokens.css` — design tokens (CSS variables)
- `web/static/css/base.css` — reset, body, scrollbar
- `web/static/css/layout.css` — app grid, sidebar, topbar, main
- `web/static/css/components.css` — button, card, form, table, badge, toast, modal, empty-state
- `web/static/css/pages.css` — settings, dashboard, placeholder page specific styles
- `web/static/js/auth.js` — token storage, 401 handler, login flow
- `web/static/js/api.js` — authFetch wrapper
- `web/static/js/store.js` — sidebar collapsed state, current page meta
- `web/static/js/router.js` — route table, History API navigation
- `web/static/js/pages/dashboard.js` — overview page
- `web/static/js/pages/settings.js` — config page (full implementation)
- `web/static/js/pages/test.js` — placeholder
- `web/static/js/pages/history.js` — placeholder
- `web/static/js/pages/users.js` — placeholder
- `web/static/js/pages/system.js` — placeholder
- `web/static/js/app.js` — bootstrap

**Rewrite:**
- `web/index.html` — application shell + login overlay

**Delete:**
- `web/static/app.js` — superseded by the new module set

**Modify:**
- `main.go` — add `NoRoute` fallback handler

---

## Task 1: Create new directory structure

**Files:**
- Create: `web/static/css/`
- Create: `web/static/js/pages/`

- [ ] **Step 1: Create the directories**

Run from project root:

```bash
mkdir -p web/static/css web/static/js/pages
```

- [ ] **Step 2: Verify directories exist**

```bash
ls -la web/static/
```

Expected: `css/` and `js/pages/` listed.

- [ ] **Step 3: Commit**

```bash
git add web/static/css web/static/js/pages
git commit -m "chore: scaffold static/css and static/js/pages directories"
```

---

## Task 2: Design tokens (CSS variables)

**Files:**
- Create: `web/static/css/tokens.css`

- [ ] **Step 1: Write the tokens file**

Create `web/static/css/tokens.css` with the following content:

```css
/* Design tokens — single source of truth for colors, spacing, typography, motion */

:root {
  /* Surfaces */
  --bg:        #0a0a0f;
  --surface:   #14141d;
  --surface-2: #1c1c28;
  --surface-3: #232333;
  --border:    #2a2a3a;
  --border-strong: #3a3a4a;

  /* Text */
  --text:        #e5e7eb;
  --text-muted:  #9ca3af;
  --text-faint:  #6b7280;

  /* Status */
  --primary: #22d3ee;
  --accent:  #a855f7;
  --success: #22c55e;
  --warning: #f59e0b;
  --danger:  #ef4444;

  /* Gradients */
  --gradient-primary: linear-gradient(135deg, #22d3ee 0%, #a855f7 100%);
  --gradient-glow:    radial-gradient(circle at top right, rgba(168, 85, 247, 0.15), transparent 60%);

  /* Radius */
  --r-sm: 6px;
  --r-md: 10px;
  --r-lg: 14px;
  --r-xl: 20px;
  --r-full: 9999px;

  /* Spacing */
  --s-1: 4px;
  --s-2: 8px;
  --s-3: 12px;
  --s-4: 16px;
  --s-5: 24px;
  --s-6: 32px;
  --s-7: 48px;

  /* Typography */
  --font-sans: -apple-system, BlinkMacSystemFont, 'Inter', 'Segoe UI',
               'PingFang SC', 'Microsoft YaHei', sans-serif;
  --font-mono: 'JetBrains Mono', 'Fira Code', Consolas, monospace;
  --t-xs:   12px;
  --t-sm:   13px;
  --t-base: 14px;
  --t-lg:   16px;
  --t-xl:   20px;
  --t-2xl:  24px;
  --t-3xl:  32px;

  /* Shadows */
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.4);
  --shadow-md: 0 4px 12px rgba(0, 0, 0, 0.4), 0 0 0 1px rgba(255, 255, 255, 0.04);
  --shadow-lg: 0 12px 32px rgba(0, 0, 0, 0.5), 0 0 0 1px rgba(255, 255, 255, 0.06);
  --shadow-glow: 0 0 24px rgba(34, 211, 238, 0.2);

  /* Motion */
  --ease:      cubic-bezier(0.4, 0, 0.2, 1);
  --dur-fast:  120ms;
  --dur-base:  200ms;
  --dur-slow:  360ms;

  /* Layout dimensions */
  --sidebar-w:        240px;
  --sidebar-w-collapsed: 64px;
  --topbar-h:         56px;
  --content-max-w:    1200px;
}

@media (max-width: 768px) {
  :root {
    --sidebar-w: 0px;
    --topbar-h: 52px;
  }
}
```

- [ ] **Step 2: Verify the file parses**

There is no build step for CSS. Quick sanity check:

```bash
ls -la web/static/css/tokens.css
```

- [ ] **Step 3: Commit**

```bash
git add web/static/css/tokens.css
git commit -m "feat(ui): add design tokens (CSS variables)"
```

---

## Task 3: Base styles (reset, body, scrollbar)

**Files:**
- Create: `web/static/css/base.css`

- [ ] **Step 1: Write the base styles**

Create `web/static/css/base.css`:

```css
/* Reset + body + scrollbar */
*, *::before, *::after { box-sizing: border-box; }
* { margin: 0; padding: 0; }

html, body {
  height: 100%;
  font-family: var(--font-sans);
  font-size: var(--t-base);
  line-height: 1.5;
  color: var(--text);
  background: var(--bg);
  background-image: var(--gradient-glow);
  background-attachment: fixed;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  text-rendering: optimizeLegibility;
}

body {
  min-height: 100vh;
  overflow-x: hidden;
}

a { color: var(--primary); text-decoration: none; }
a:hover { text-decoration: underline; }

button { font: inherit; color: inherit; }

code, pre, kbd, samp {
  font-family: var(--font-mono);
  font-size: 0.95em;
}

input, button, select, textarea {
  font: inherit;
  color: inherit;
}

/* Scrollbar */
::-webkit-scrollbar { width: 10px; height: 10px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb {
  background: var(--surface-3);
  border-radius: var(--r-full);
  border: 2px solid var(--bg);
}
::-webkit-scrollbar-thumb:hover { background: var(--border-strong); }

/* Selection */
::selection {
  background: rgba(34, 211, 238, 0.3);
  color: var(--text);
}

/* Focus ring (visible only on keyboard) */
:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: 2px;
  border-radius: var(--r-sm);
}

/* Utility */
.sr-only {
  position: absolute;
  width: 1px; height: 1px;
  padding: 0; margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
```

- [ ] **Step 2: Commit**

```bash
git add web/static/css/base.css
git commit -m "feat(ui): add base styles (reset, body, scrollbar)"
```

---

## Task 4: Layout styles (sidebar, topbar, main grid)

**Files:**
- Create: `web/static/css/layout.css`

- [ ] **Step 1: Write the layout styles**

Create `web/static/css/layout.css`:

```css
/* Application shell layout: sidebar + main column with topbar */

.app {
  display: grid;
  grid-template-columns: var(--sidebar-w) 1fr;
  grid-template-rows: var(--topbar-h) 1fr;
  grid-template-areas:
    "sidebar topbar"
    "sidebar main";
  min-height: 100vh;
  transition: grid-template-columns var(--dur-base) var(--ease);
}

.app.collapsed {
  --sidebar-w: var(--sidebar-w-collapsed);
}

/* Sidebar */
.sidebar {
  grid-area: sidebar;
  background: var(--surface);
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  padding: var(--s-4) 0;
  position: sticky;
  top: 0;
  height: 100vh;
  overflow-y: auto;
  transition: padding var(--dur-base) var(--ease);
}

.sidebar__logo {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  padding: 0 var(--s-5) var(--s-5);
  border-bottom: 1px solid var(--border);
  margin-bottom: var(--s-4);
}
.sidebar__logo-mark {
  width: 28px; height: 28px;
  border-radius: var(--r-md);
  background: var(--gradient-primary);
  box-shadow: var(--shadow-glow);
  flex-shrink: 0;
}
.sidebar__logo-text {
  font-size: var(--t-lg);
  font-weight: 600;
  letter-spacing: -0.01em;
  white-space: nowrap;
  overflow: hidden;
  transition: opacity var(--dur-base) var(--ease);
}
.app.collapsed .sidebar__logo-text { opacity: 0; pointer-events: none; }

.nav { display: flex; flex-direction: column; gap: var(--s-1); padding: 0 var(--s-3); }

.nav__item {
  display: flex;
  align-items: center;
  gap: var(--s-3);
  padding: var(--s-3) var(--s-3);
  border-radius: var(--r-md);
  color: var(--text-muted);
  font-size: var(--t-base);
  font-weight: 500;
  cursor: pointer;
  transition: background var(--dur-fast) var(--ease), color var(--dur-fast) var(--ease);
  white-space: nowrap;
  overflow: hidden;
  text-decoration: none;
}
.nav__item:hover { background: var(--surface-2); color: var(--text); }
.nav__item.active {
  background: var(--surface-2);
  color: var(--text);
  box-shadow: inset 2px 0 0 var(--primary);
}
.nav__icon {
  width: 20px; height: 20px;
  flex-shrink: 0;
  stroke: currentColor;
  fill: none;
  stroke-width: 2;
}
.nav__label {
  overflow: hidden;
  transition: opacity var(--dur-base) var(--ease);
}
.app.collapsed .nav__label { opacity: 0; pointer-events: none; }
.app.collapsed .nav__item { justify-content: center; padding: var(--s-3); }

.sidebar__footer {
  margin-top: auto;
  padding: var(--s-3);
  border-top: 1px solid var(--border);
}

/* Topbar */
.topbar {
  grid-area: topbar;
  display: flex;
  align-items: center;
  gap: var(--s-3);
  padding: 0 var(--s-5);
  background: rgba(20, 20, 29, 0.7);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border-bottom: 1px solid var(--border);
  position: sticky;
  top: 0;
  z-index: 10;
}

.topbar__collapse {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px; height: 36px;
  background: transparent;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  cursor: pointer;
  color: var(--text-muted);
  transition: all var(--dur-fast) var(--ease);
}
.topbar__collapse:hover { color: var(--text); border-color: var(--border-strong); }

.topbar__title {
  font-size: var(--t-lg);
  font-weight: 600;
  letter-spacing: -0.01em;
  flex: 1;
}

.topbar__actions { display: flex; align-items: center; gap: var(--s-2); }

/* Main */
.main {
  grid-area: main;
  padding: var(--s-5) var(--s-6);
  max-width: 100%;
  overflow-x: hidden;
}
.main__inner {
  max-width: var(--content-max-w);
  margin: 0 auto;
  animation: fadeIn var(--dur-base) var(--ease);
}
@keyframes fadeIn {
  from { opacity: 0; transform: translateY(4px); }
  to   { opacity: 1; transform: translateY(0); }
}

/* Responsive: mobile drawer */
@media (max-width: 768px) {
  .app { grid-template-columns: 1fr; grid-template-areas: "topbar" "main"; }
  .sidebar {
    position: fixed;
    left: 0; top: 0;
    width: 280px;
    --sidebar-w: 280px;
    z-index: 100;
    transform: translateX(-100%);
    transition: transform var(--dur-base) var(--ease);
    box-shadow: var(--shadow-lg);
  }
  .sidebar.open { transform: translateX(0); }
  .app.collapsed .sidebar { transform: translateX(-100%); }
  .sidebar__logo-text, .nav__label { opacity: 1 !important; pointer-events: auto; }
  .main { padding: var(--s-4); }
  .topbar__title { font-size: var(--t-base); }
}

.backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(2px);
  z-index: 99;
  opacity: 0;
  pointer-events: none;
  transition: opacity var(--dur-base) var(--ease);
}
.backdrop.show { opacity: 1; pointer-events: auto; }
```

- [ ] **Step 2: Commit**

```bash
git add web/static/css/layout.css
git commit -m "feat(ui): add layout styles (sidebar, topbar, main grid)"
```

---

## Task 5: Component styles (button, card, form, table, toast, modal, empty-state)

**Files:**
- Create: `web/static/css/components.css`

- [ ] **Step 1: Write the components file**

Create `web/static/css/components.css`:

```css
/* Reusable UI components */

/* Button */
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--s-2);
  padding: var(--s-3) var(--s-4);
  border: 1px solid transparent;
  border-radius: var(--r-md);
  background: var(--surface-2);
  color: var(--text);
  font-size: var(--t-base);
  font-weight: 500;
  cursor: pointer;
  transition: transform var(--dur-fast) var(--ease),
              box-shadow var(--dur-fast) var(--ease),
              background var(--dur-fast) var(--ease),
              border-color var(--dur-fast) var(--ease);
  white-space: nowrap;
  user-select: none;
}
.btn:hover { transform: translateY(-1px); box-shadow: var(--shadow-md); }
.btn:active { transform: translateY(0); }
.btn:disabled, .btn.is-loading { opacity: 0.5; cursor: not-allowed; transform: none; }

.btn--primary {
  background: var(--gradient-primary);
  color: #0a0a0f;
  font-weight: 600;
}
.btn--primary:hover { box-shadow: var(--shadow-glow); }

.btn--secondary { background: transparent; border-color: var(--border); }
.btn--secondary:hover { border-color: var(--border-strong); background: var(--surface-2); }

.btn--ghost { background: transparent; }
.btn--ghost:hover { background: var(--surface-2); }

.btn--danger { background: var(--danger); color: #fff; }
.btn--danger:hover { background: #dc2626; }

.btn--sm { padding: var(--s-2) var(--s-3); font-size: var(--t-sm); }
.btn--icon { padding: var(--s-2); width: 32px; height: 32px; }

/* Card */
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  padding: var(--s-5);
  box-shadow: var(--shadow-md);
}
.card + .card { margin-top: var(--s-4); }
.card__title {
  font-size: var(--t-sm);
  font-weight: 500;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: var(--s-4);
}
.card__hint { color: var(--text-faint); font-size: var(--t-sm); margin-top: var(--s-2); }

/* Form */
.form-group { display: flex; flex-direction: column; gap: var(--s-2); margin-bottom: var(--s-4); }
.form-group:last-child { margin-bottom: 0; }
.form-label { font-size: var(--t-sm); color: var(--text-muted); }
.form-helper { font-size: var(--t-xs); color: var(--text-faint); }

.input {
  width: 100%;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: var(--s-3) var(--s-4);
  color: var(--text);
  font-size: var(--t-base);
  outline: none;
  transition: border-color var(--dur-fast) var(--ease),
              box-shadow var(--dur-fast) var(--ease);
}
.input:focus {
  border-color: var(--primary);
  box-shadow: 0 0 0 3px rgba(34, 211, 238, 0.15);
}
.input::placeholder { color: var(--text-faint); }

.input-row { display: flex; gap: var(--s-2); align-items: stretch; }
.input-row .input { flex: 1; }

/* Badge */
.badge {
  display: inline-flex;
  align-items: center;
  gap: var(--s-1);
  padding: 2px var(--s-2);
  border-radius: var(--r-full);
  font-size: var(--t-xs);
  font-weight: 500;
  background: var(--surface-2);
  color: var(--text-muted);
}
.badge--success { background: rgba(34, 197, 94, 0.15);  color: var(--success); }
.badge--warning { background: rgba(245, 158, 11, 0.15); color: var(--warning); }
.badge--danger  { background: rgba(239, 68, 68, 0.15);  color: var(--danger); }
.badge--info    { background: rgba(34, 211, 238, 0.15); color: var(--primary); }

/* Table */
.table-wrap { overflow-x: auto; border: 1px solid var(--border); border-radius: var(--r-lg); }
.table { width: 100%; border-collapse: collapse; font-size: var(--t-sm); }
.table th, .table td { padding: var(--s-3) var(--s-4); text-align: left; }
.table thead {
  background: var(--surface-2);
  color: var(--text-muted);
  font-size: var(--t-xs);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.table tbody tr { border-top: 1px solid var(--border); transition: background var(--dur-fast); }
.table tbody tr:hover { background: var(--surface-2); }

/* Stat card */
.stat {
  display: flex;
  flex-direction: column;
  gap: var(--s-2);
  padding: var(--s-5);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  position: relative;
  overflow: hidden;
}
.stat__label { font-size: var(--t-sm); color: var(--text-muted); }
.stat__value {
  font-size: var(--t-3xl);
  font-weight: 700;
  letter-spacing: -0.02em;
  font-variant-numeric: tabular-nums;
}
.stat__note { font-size: var(--t-xs); color: var(--text-faint); margin-top: var(--s-1); }
.stat-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--s-4); }
@media (max-width: 768px) { .stat-grid { grid-template-columns: 1fr; } }

/* Empty state */
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: var(--s-7) var(--s-4);
  gap: var(--s-3);
}
.empty__icon {
  width: 64px; height: 64px;
  border-radius: var(--r-full);
  background: var(--surface-2);
  display: flex; align-items: center; justify-content: center;
  color: var(--text-faint);
  margin-bottom: var(--s-2);
}
.empty__icon svg { width: 32px; height: 32px; }
.empty__title { font-size: var(--t-lg); font-weight: 600; }
.empty__desc  { color: var(--text-muted); font-size: var(--t-sm); max-width: 360px; line-height: 1.6; }

/* Toast */
.toast-stack {
  position: fixed;
  right: var(--s-5); bottom: var(--s-5);
  display: flex; flex-direction: column; gap: var(--s-2);
  z-index: 1000;
  pointer-events: none;
}
.toast {
  pointer-events: auto;
  min-width: 240px; max-width: 360px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-left: 3px solid var(--primary);
  border-radius: var(--r-md);
  padding: var(--s-3) var(--s-4);
  box-shadow: var(--shadow-lg);
  font-size: var(--t-sm);
  animation: slideIn var(--dur-base) var(--ease);
}
.toast--success { border-left-color: var(--success); }
.toast--error   { border-left-color: var(--danger); }
.toast--warning { border-left-color: var(--warning); }
@keyframes slideIn {
  from { opacity: 0; transform: translateX(20px); }
  to   { opacity: 1; transform: translateX(0); }
}
.toast.is-leaving { animation: slideOut var(--dur-base) var(--ease) forwards; }
@keyframes slideOut {
  to { opacity: 0; transform: translateX(20px); }
}

/* Modal */
.modal-backdrop {
  position: fixed; inset: 0;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(4px);
  z-index: 200;
  display: flex; align-items: center; justify-content: center;
  animation: fadeIn var(--dur-base) var(--ease);
}
.modal {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-lg);
  width: min(480px, 92vw);
  padding: var(--s-5);
}
.modal__title { font-size: var(--t-lg); font-weight: 600; margin-bottom: var(--s-3); }
.modal__body  { color: var(--text-muted); margin-bottom: var(--s-5); }
.modal__actions { display: flex; gap: var(--s-2); justify-content: flex-end; }
```

- [ ] **Step 2: Commit**

```bash
git add web/static/css/components.css
git commit -m "feat(ui): add component styles (button, card, form, table, toast, modal)"
```

---

## Task 6: Page-specific styles

**Files:**
- Create: `web/static/css/pages.css`

- [ ] **Step 1: Write the page styles**

Create `web/static/css/pages.css`:

```css
/* Page-specific styles */

/* Settings page */
.settings-actions {
  display: flex;
  gap: var(--s-3);
  justify-content: flex-end;
  margin-top: var(--s-5);
  padding-top: var(--s-5);
  border-top: 1px solid var(--border);
}

.token-list { display: flex; flex-direction: column; gap: var(--s-2); }
.token-item {
  display: flex;
  align-items: center;
  gap: var(--s-3);
  padding: var(--s-3) var(--s-4);
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  font-family: var(--font-mono);
  font-size: var(--t-sm);
}
.token-item__value { flex: 1; word-break: break-all; color: var(--success); }
.token-item__value--masked { color: var(--text-muted); user-select: none; }
.token-item__count {
  font-size: var(--t-sm);
  color: var(--text-muted);
  margin-top: var(--s-3);
}

/* Login overlay */
.login {
  position: fixed; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: var(--bg);
  background-image: var(--gradient-glow);
  z-index: 9999;
  animation: fadeIn var(--dur-base) var(--ease);
}
.login__card {
  width: min(360px, 92vw);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-xl);
  padding: var(--s-7) var(--s-6);
  box-shadow: var(--shadow-lg);
  text-align: center;
}
.login__logo {
  width: 56px; height: 56px;
  border-radius: var(--r-lg);
  background: var(--gradient-primary);
  box-shadow: var(--shadow-glow);
  margin: 0 auto var(--s-4);
}
.login__title { font-size: var(--t-xl); font-weight: 600; margin-bottom: var(--s-2); }
.login__subtitle { color: var(--text-muted); font-size: var(--t-sm); margin-bottom: var(--s-5); }
.login__form { display: flex; flex-direction: column; gap: var(--s-3); }
.login__error { color: var(--danger); font-size: var(--t-sm); min-height: 1.2em; margin-top: var(--s-2); }

/* Dashboard */
.dashboard-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--s-5);
}
.section-title {
  font-size: var(--t-lg);
  font-weight: 600;
  margin: var(--s-6) 0 var(--s-3);
}
.section-title:first-child { margin-top: 0; }
```

- [ ] **Step 2: Commit**

```bash
git add web/static/css/pages.css
git commit -m "feat(ui): add page-specific styles (settings, login, dashboard)"
```

---

## Task 7: HTML shell

**Files:**
- Rewrite: `web/index.html`
- Delete: `web/static/app.js`

- [ ] **Step 1: Rewrite `web/index.html`**

Replace the entire contents of `web/index.html` with:

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>wx_web_api 管理后台</title>
  <link rel="stylesheet" href="/static/css/tokens.css?v=1">
  <link rel="stylesheet" href="/static/css/base.css?v=1">
  <link rel="stylesheet" href="/static/css/layout.css?v=1">
  <link rel="stylesheet" href="/static/css/components.css?v=1">
  <link rel="stylesheet" href="/static/css/pages.css?v=1">
</head>
<body>
  <!-- Login overlay (shown when unauthenticated) -->
  <div id="loginRoot" class="login" hidden>
    <div class="login__card">
      <div class="login__logo"></div>
      <h1 class="login__title">wx_web_api</h1>
      <p class="login__subtitle">请输入访问密码</p>
      <form class="login__form" id="loginForm" autocomplete="off">
        <input type="password" class="input" id="loginPwd" placeholder="密码" required>
        <button type="submit" class="btn btn--primary">登录</button>
        <div class="login__error" id="loginError"></div>
      </form>
    </div>
  </div>

  <!-- Application shell (shown when authenticated) -->
  <div id="appRoot" class="app" hidden>
    <aside class="sidebar" id="sidebar">
      <div class="sidebar__logo">
        <div class="sidebar__logo-mark"></div>
        <span class="sidebar__logo-text">wx_admin</span>
      </div>
      <nav class="nav" id="nav"></nav>
    </aside>

    <header class="topbar">
      <button class="topbar__collapse" id="collapseBtn" aria-label="折叠侧边栏">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
          <path d="M3 12h18M3 6h18M3 18h18"/>
        </svg>
      </button>
      <h2 class="topbar__title" id="pageTitle">概览</h2>
      <div class="topbar__actions">
        <button class="btn btn--ghost btn--icon" id="logoutBtn" aria-label="退出登录" title="退出登录">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
            <path d="M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4M16 17l5-5-5-5M21 12H9"/>
          </svg>
        </button>
      </div>
    </header>

    <main class="main">
      <div class="main__inner" id="pageContent"></div>
    </main>
  </div>

  <div class="backdrop" id="backdrop"></div>
  <div class="toast-stack" id="toastStack" aria-live="polite"></div>

  <!-- JS modules: load order matters (no module system) -->
  <script src="/static/js/store.js?v=1"></script>
  <script src="/static/js/api.js?v=1"></script>
  <script src="/static/js/auth.js?v=1"></script>
  <script src="/static/js/router.js?v=1"></script>
  <script src="/static/js/pages/dashboard.js?v=1"></script>
  <script src="/static/js/pages/settings.js?v=1"></script>
  <script src="/static/js/pages/test.js?v=1"></script>
  <script src="/static/js/pages/history.js?v=1"></script>
  <script src="/static/js/pages/users.js?v=1"></script>
  <script src="/static/js/pages/system.js?v=1"></script>
  <script src="/static/js/app.js?v=1"></script>
</body>
</html>
```

- [ ] **Step 2: Delete the old `app.js`**

```bash
git rm web/static/app.js
```

(Or `rm web/static/app.js` if not tracked — confirm with `git status` first.)

- [ ] **Step 3: Verify the file**

```bash
ls -la web/static/
```

Expected: only the `css/` and `js/` directories, no standalone `app.js`.

- [ ] **Step 4: Commit**

```bash
git add web/index.html
git commit -m "feat(ui): rewrite index.html as app shell + login overlay"
```

---

## Task 8: Store module (sidebar collapsed, intended route)

**Files:**
- Create: `web/static/js/store.js`

- [ ] **Step 1: Write the store module**

Create `web/static/js/store.js`:

```js
/* Minimal state store. Persists only UI prefs (sidebar, intendedRoute).
 * Sensitive data (token) lives in localStorage via auth.js. */

(function (global) {
  'use strict';

  const KEY_COLLAPSED = 'wx_ui_collapsed';
  const KEY_INTENT    = 'wx_ui_intended_route';

  function safeGet(key) {
    try { return localStorage.getItem(key); } catch (e) { return null; }
  }
  function safeSet(key, value) {
    try { localStorage.setItem(key, value); } catch (e) { /* ignore */ }
  }
  function safeDel(key) {
    try { localStorage.removeItem(key); } catch (e) { /* ignore */ }
  }

  const Store = {
    isCollapsed() { return safeGet(KEY_COLLAPSED) === '1'; },
    setCollapsed(v) {
      if (v) safeSet(KEY_COLLAPSED, '1'); else safeDel(KEY_COLLAPSED);
      document.body.firstElementChild?.classList?.toggle?.('collapsed', v);
      document.getElementById('appRoot')?.classList.toggle('collapsed', v);
    },
    toggleCollapsed() { Store.setCollapsed(!Store.isCollapsed()); },

    getIntendedRoute() { return safeGet(KEY_INTENT) || '/dashboard'; },
    setIntendedRoute(p) { safeSet(KEY_INTENT, p); },
    clearIntendedRoute() { safeDel(KEY_INTENT); }
  };

  global.WXStore = Store;
})(window);
```

- [ ] **Step 2: Commit**

```bash
git add web/static/js/store.js
git commit -m "feat(ui): add store module (sidebar collapsed, intended route)"
```

---

## Task 9: API helper with 401 handling

**Files:**
- Create: `web/static/js/api.js`

- [ ] **Step 1: Write the API helper**

Create `web/static/js/api.js`:

```js
/* Fetch wrapper that injects Authorization header and handles 401.
 * Paired with auth.js — 401 triggers login redirect. */

(function (global) {
  'use strict';

  function getToken() {
    try { return localStorage.getItem('wx_token') || ''; } catch (e) { return ''; }
  }

  async function authFetch(url, options) {
    options = options || {};
    const token = getToken();
    const headers = Object.assign(
      { 'Content-Type': 'application/json' },
      options.headers || {}
    );
    if (token) headers['Authorization'] = token;

    let res;
    try {
      res = await fetch(url, Object.assign({}, options, { headers: headers }));
    } catch (e) {
      // network error
      throw new Error('网络错误');
    }

    if (res.status === 401) {
      if (global.WXAuth && global.WXAuth.handle401) {
        global.WXAuth.handle401();
      }
      throw new Error('未授权');
    }
    return res;
  }

  async function authJson(url, options) {
    const res = await authFetch(url, options);
    let data = null;
    try { data = await res.json(); } catch (e) { /* leave null */ }
    return { status: res.status, data: data };
  }

  global.WXApi = { authFetch, authJson, getToken };
})(window);
```

- [ ] **Step 2: Commit**

```bash
git add web/static/js/api.js
git commit -m "feat(ui): add api helper with 401 handling"
```

---

## Task 10: Auth module (login flow, 401, logout)

**Files:**
- Create: `web/static/js/auth.js`

- [ ] **Step 1: Write the auth module**

Create `web/static/js/auth.js`:

```js
/* Authentication module.
 * - Token storage in localStorage
 * - Challenge-response login (matches server simpleHash)
 * - 401 handler: clear token, show login overlay, preserve intended route
 * - Login overlay show/hide
 */

(function (global) {
  'use strict';

  const TOKEN_KEY = 'wx_token';

  function getToken() {
    try { return localStorage.getItem(TOKEN_KEY) || ''; } catch (e) { return ''; }
  }
  function setToken(t) {
    try { localStorage.setItem(TOKEN_KEY, t); } catch (e) { /* ignore */ }
  }
  function clearToken() {
    try { localStorage.removeItem(TOKEN_KEY); } catch (e) { /* ignore */ }
  }

  function isLoggedIn() { return !!getToken(); }

  /* simpleHash must match the algorithm in internal/handler/handler.go */
  function simpleHash(data) {
    var h = 0;
    var primes = [31, 37, 41, 43, 47, 53, 59, 61, 67, 71, 73, 79];
    for (var i = 0; i < data.length; i++) {
      h += data.charCodeAt(i) * primes[(i + 1) % 12];
    }
    return ('0000000000000000' + (h >>> 0).toString(16)).slice(-16);
  }

  function showLogin() {
    document.getElementById('loginRoot').hidden = false;
    document.getElementById('appRoot').hidden = true;
    setTimeout(function () {
      var pwd = document.getElementById('loginPwd');
      if (pwd) { pwd.value = ''; pwd.focus(); }
    }, 50);
  }

  function showApp() {
    document.getElementById('loginRoot').hidden = true;
    document.getElementById('appRoot').hidden = false;
  }

  function handle401() {
    var current = (global.location && global.location.pathname) || '/dashboard';
    if (global.WXStore) {
      global.WXStore.setIntendedRoute(current);
    }
    clearToken();
    showLogin();
    if (global.WXToast) {
      global.WXToast('登录已过期，请重新登录', 'warning');
    }
  }

  async function login(pwd) {
    var challengeRes = await fetch('/api/login/challenge');
    var challengeData = await challengeRes.json();
    if (challengeData.code !== 0) {
      throw new Error(challengeData.msg || '获取挑战失败');
    }
    var challenge = challengeData.challenge;
    var response  = simpleHash(pwd + challenge);

    var loginRes = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pwd: pwd, challenge: challenge, response: response })
    });
    var loginData = await loginRes.json();
    if (loginData.code !== 0) {
      throw new Error(loginData.msg || '登录失败');
    }
    setToken(loginData.token);
    return loginData.token;
  }

  function logout() {
    clearToken();
    if (global.WXStore) global.WXStore.clearIntendedRoute();
    showLogin();
  }

  global.WXAuth = {
    isLoggedIn: isLoggedIn,
    getToken: getToken,
    setToken: setToken,
    clearToken: clearToken,
    login: login,
    logout: logout,
    showLogin: showLogin,
    showApp: showApp,
    handle401: handle401,
    simpleHash: simpleHash
  };
})(window);
```

- [ ] **Step 2: Commit**

```bash
git add web/static/js/auth.js
git commit -m "feat(ui): add auth module (login, 401, logout)"
```

---

## Task 11: Toast helper (small, used by app.js and pages)

The toast helper is small enough to live in `app.js`. Skip this task.

---

## Task 12: Router (route table, History API, popstate, link delegation)

**Files:**
- Create: `web/static/js/router.js`

- [ ] **Step 1: Write the router module**

Create `web/static/js/router.js`:

```js
/* Client-side History API router.
 * - Single source of truth: ROUTES table
 * - Each route: { path, title, render(ctx) } where ctx = { params, query }
 * - Renders into #pageContent
 * - Updates <title> and #pageTitle
 * - Updates active state in #nav
 * - Intercepts clicks on [data-route] anchors
 * - popstate handler for back/forward
 */

(function (global) {
  'use strict';

  const ROUTES = [
    { path: '/dashboard', title: '概览',     page: 'dashboard' },
    { path: '/settings',  title: '配置',     page: 'settings'  },
    { path: '/test',      title: '解析测试', page: 'test'      },
    { path: '/history',   title: '解析历史', page: 'history'   },
    { path: '/users',     title: '用户/角色', page: 'users'    },
    { path: '/system',    title: '系统信息', page: 'system'    }
  ];

  const DEFAULT_ROUTE = '/dashboard';

  function getRouteMeta(path) {
    for (var i = 0; i < ROUTES.length; i++) {
      if (ROUTES[i].path === path) return ROUTES[i];
    }
    return null;
  }

  function parseQuery() {
    var q = {};
    var s = global.location.search.replace(/^\?/, '');
    if (!s) return q;
    s.split('&').forEach(function (pair) {
      if (!pair) return;
      var kv = pair.split('=');
      q[decodeURIComponent(kv[0])] = decodeURIComponent(kv[1] || '');
    });
    return q;
  }

  function renderActiveNav(path) {
    var links = document.querySelectorAll('#nav .nav__item');
    for (var i = 0; i < links.length; i++) {
      var a = links[i];
      if (a.getAttribute('data-route') === path) {
        a.classList.add('active');
      } else {
        a.classList.remove('active');
      }
    }
  }

  function render() {
    var path = global.location.pathname || DEFAULT_ROUTE;
    var meta = getRouteMeta(path);
    if (!meta) { path = DEFAULT_ROUTE; meta = getRouteMeta(path); }

    /* update topbar */
    var titleEl = document.getElementById('pageTitle');
    if (titleEl) titleEl.textContent = meta.title;
    document.title = meta.title + ' · wx_web_api';

    /* update nav highlight */
    renderActiveNav(path);

    /* render page */
    var slot = document.getElementById('pageContent');
    slot.innerHTML = '';
    var mod = global.WXPages && global.WXPages[meta.page];
    if (mod && typeof mod.render === 'function') {
      var ret = mod.render(slot, { path: path, query: parseQuery() });
      if (ret && typeof ret.then === 'function') {
        ret.catch(function (e) {
          if (global.WXToast) global.WXToast('页面加载失败', 'error');
          if (global.console) console.error(e);
        });
      }
    } else {
      slot.innerHTML = '<div class="empty"><div class="empty__title">页面模块缺失</div></div>';
    }

    /* close mobile drawer after navigation */
    var sb = document.getElementById('sidebar');
    if (sb) sb.classList.remove('open');
    var bd = document.getElementById('backdrop');
    if (bd) bd.classList.remove('show');

    /* scroll to top of content */
    var main = document.querySelector('.main');
    if (main) main.scrollTop = 0;
  }

  function navigate(path, opts) {
    opts = opts || {};
    if (!global.WXAuth || !global.WXAuth.isLoggedIn()) {
      if (global.WXStore) global.WXStore.setIntendedRoute(path);
      if (global.WXAuth) global.WXAuth.showLogin();
      return;
    }
    if (path === global.location.pathname && !opts.force) {
      render();
      return;
    }
    if (opts.replace) {
      global.history.replaceState({}, '', path);
    } else {
      global.history.pushState({}, '', path);
    }
    render();
  }

  function buildNav() {
    var nav = document.getElementById('nav');
    if (!nav) return;
    var html = ROUTES.map(function (r) {
      var iconPath = NAV_ICONS[r.page] || NAV_ICONS.default;
      return '<a class="nav__item" href="' + r.path + '" data-route="' + r.path + '">' +
               '<svg class="nav__icon" viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">' + iconPath + '</svg>' +
               '<span class="nav__label">' + r.title + '</span>' +
             '</a>';
    }).join('');
    nav.innerHTML = html;
  }

  /* Minimal inline icon paths (24x24 stroke icons) */
  const NAV_ICONS = {
    dashboard: '<path d="M3 12l2-2 7-7 7 7 2 2v8a2 2 0 01-2 2h-4v-7h-6v7H5a2 2 0 01-2-2v-8z"/>',
    settings:  '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 11-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 11-4 0v-.09A1.65 1.65 0 008 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 11-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 110-4h.09A1.65 1.65 0 004.6 8a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 112.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 114 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 112.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 110 4h-.09a1.65 1.65 0 00-1.51 1z"/>',
    test:      '<path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/>',
    history:   '<path d="M3 12a9 9 0 109-9 9.75 9.75 0 00-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/>',
    users:     '<path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75"/>',
    system:    '<circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/>',
    default:   '<circle cx="12" cy="12" r="10"/>'
  };

  function init() {
    buildNav();
    global.addEventListener('popstate', render);
    document.addEventListener('click', function (e) {
      var a = e.target.closest && e.target.closest('[data-route]');
      if (a) {
        e.preventDefault();
        navigate(a.getAttribute('data-route'));
      }
    });
  }

  global.WXRouter = {
    ROUTES: ROUTES,
    init: init,
    render: render,
    navigate: navigate,
    getRouteMeta: getRouteMeta
  };
})(window);
```

- [ ] **Step 2: Commit**

```bash
git add web/static/js/router.js
git commit -m "feat(ui): add router module (History API, route table, nav)"
```

---

## Task 13: Page — Settings (full implementation)

**Files:**
- Create: `web/static/js/pages/settings.js`

- [ ] **Step 1: Write the settings page**

Create `web/static/js/pages/settings.js`:

```js
/* Settings page — full implementation.
 * Loads /api/config, renders form, supports add/remove/copy tokens,
 * dirty tracking, save/cancel, beforeunload prompt.
 */

(function (global) {
  'use strict';

  var original = { apiBaseUrl: '', tokens: [] };
  var current  = { apiBaseUrl: '', tokens: [] };
  var tokenRevealed = {};

  function escapeHtml(s) {
    var div = document.createElement('div');
    div.textContent = s == null ? '' : String(s);
    return div.innerHTML;
  }

  function mask(token) {
    if (!token) return '';
    if (token.length <= 12) return token.slice(0, 4) + '••••••••';
    return token.slice(0, 8) + '••••••••';
  }

  function isDirty() {
    if (original.apiBaseUrl !== current.apiBaseUrl) return true;
    if (original.tokens.length !== current.tokens.length) return true;
    for (var i = 0; i < original.tokens.length; i++) {
      if (original.tokens[i] !== current.tokens[i]) return true;
    }
    return false;
  }

  function updateSaveButton() {
    var btn = document.getElementById('settingsSaveBtn');
    if (!btn) return;
    btn.disabled = !isDirty();
  }

  function renderTokens() {
    var slot = document.getElementById('settingsTokenList');
    var countEl = document.getElementById('settingsTokenCount');
    if (!slot) return;
    if (!current.tokens.length) {
      slot.innerHTML = '<div class="empty"><div class="empty__title">暂无 token</div>' +
                       '<div class="empty__desc">在下方输入框中添加第一个 token</div></div>';
      if (countEl) countEl.textContent = '共 0 个 token';
      return;
    }
    slot.innerHTML = current.tokens.map(function (t, i) {
      var revealed = !!tokenRevealed[i];
      var valueHtml = revealed
        ? '<span class="token-item__value">' + escapeHtml(t) + '</span>'
        : '<span class="token-item__value token-item__value--masked">' + escapeHtml(mask(t)) + '</span>';
      return '<div class="token-item" data-idx="' + i + '">' +
               valueHtml +
               '<button type="button" class="btn btn--ghost btn--sm" data-action="copy" data-idx="' + i + '">复制</button>' +
               '<button type="button" class="btn btn--ghost btn--sm" data-action="toggle" data-idx="' + i + '">' + (revealed ? '隐藏' : '显示') + '</button>' +
               '<button type="button" class="btn btn--danger btn--sm" data-action="remove" data-idx="' + i + '">删除</button>' +
             '</div>';
    }).join('');
    if (countEl) countEl.textContent = '共 ' + current.tokens.length + ' 个 token';
  }

  function addToken(raw) {
    var token = String(raw || '').trim();
    if (!token) {
      if (global.WXToast) global.WXToast('Token 不能为空', 'error');
      return false;
    }
    if (current.tokens.indexOf(token) !== -1) {
      if (global.WXToast) global.WXToast('Token 已存在', 'error');
      return false;
    }
    current.tokens.push(token);
    renderTokens();
    updateSaveButton();
    return true;
  }

  function removeToken(idx) {
    if (idx < 0 || idx >= current.tokens.length) return;
    current.tokens.splice(idx, 1);
    delete tokenRevealed[idx];
    /* reindex reveal flags */
    var next = {};
    Object.keys(tokenRevealed).forEach(function (k) {
      var n = Number(k);
      if (n < idx) next[n] = tokenRevealed[k];
      else if (n > idx) next[n - 1] = tokenRevealed[k];
    });
    tokenRevealed = next;
    renderTokens();
    updateSaveButton();
  }

  function copyToken(idx) {
    var t = current.tokens[idx];
    if (!t) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(t).then(function () {
        if (global.WXToast) global.WXToast('已复制', 'success');
      }, function () {
        fallbackCopy(t);
      });
    } else {
      fallbackCopy(t);
    }
  }
  function fallbackCopy(text) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); } catch (e) { /* ignore */ }
    document.body.removeChild(ta);
    if (global.WXToast) global.WXToast('已复制', 'success');
  }

  async function save() {
    var apiBaseUrl = (document.getElementById('settingsApiBaseUrl').value || '').trim() ||
                     'http://127.0.0.1:2022';
    current.apiBaseUrl = apiBaseUrl;
    current.tokens = current.tokens.slice();

    var btn = document.getElementById('settingsSaveBtn');
    if (btn) { btn.disabled = true; btn.classList.add('is-loading'); btn.textContent = '保存中…'; }

    try {
      var res = await global.WXApi.authJson('/api/config', {
        method: 'PUT',
        body: JSON.stringify({ api_base_url: apiBaseUrl, tokens: current.tokens })
      });
      if (res.data && res.data.code === 0) {
        original = { apiBaseUrl: apiBaseUrl, tokens: current.tokens.slice() };
        if (global.WXToast) global.WXToast('保存成功', 'success');
      } else {
        if (global.WXToast) global.WXToast((res.data && res.data.msg) || '保存失败', 'error');
      }
    } catch (e) {
      if (global.WXToast) global.WXToast(e.message || '网络错误', 'error');
    } finally {
      if (btn) { btn.disabled = false; btn.classList.remove('is-loading'); btn.textContent = '保存配置'; }
      updateSaveButton();
    }
  }

  function cancel() {
    current = { apiBaseUrl: original.apiBaseUrl, tokens: original.tokens.slice() };
    var input = document.getElementById('settingsApiBaseUrl');
    if (input) input.value = original.apiBaseUrl;
    tokenRevealed = {};
    renderTokens();
    updateSaveButton();
  }

  function bindBeforeUnload() {
    global.addEventListener('beforeunload', function (e) {
      if (isDirty()) {
        e.preventDefault();
        e.returnValue = '';
      }
    });
  }

  async function load() {
    var res = await global.WXApi.authJson('/api/config');
    if (res.data && res.data.code === 0 && res.data.data) {
      original = {
        apiBaseUrl: res.data.data.api_base_url || 'http://127.0.0.1:2022',
        tokens: Array.isArray(res.data.data.tokens) ? res.data.data.tokens.slice() : []
      };
      current = { apiBaseUrl: original.apiBaseUrl, tokens: original.tokens.slice() };
      var input = document.getElementById('settingsApiBaseUrl');
      if (input) input.value = current.apiBaseUrl;
      renderTokens();
      updateSaveButton();
    }
  }

  function render(slot) {
    slot.innerHTML =
      '<div class="card">' +
        '<div class="card__title">后端 API</div>' +
        '<div class="form-group">' +
          '<label class="form-label" for="settingsApiBaseUrl">后端 API 地址</label>' +
          '<input type="text" class="input" id="settingsApiBaseUrl" placeholder="http://127.0.0.1:2022">' +
          '<div class="form-helper">调用微信解析后端的地址（内部 127.0.0.1:2022 服务）</div>' +
        '</div>' +
      '</div>' +

      '<div class="card">' +
        '<div class="card__title">认证 Token</div>' +
        '<div class="form-group">' +
          '<label class="form-label" for="settingsNewToken">新增 Token</label>' +
          '<div class="input-row">' +
            '<input type="text" class="input" id="settingsNewToken" placeholder="输入新 token 后回车或点添加">' +
            '<button type="button" class="btn btn--primary" id="settingsAddTokenBtn">添加</button>' +
          '</div>' +
        '</div>' +
        '<div id="settingsTokenList" class="token-list"></div>' +
        '<div class="token-item__count" id="settingsTokenCount"></div>' +
      '</div>' +

      '<div class="settings-actions">' +
        '<button type="button" class="btn btn--secondary" id="settingsCancelBtn">取消</button>' +
        '<button type="button" class="btn btn--primary" id="settingsSaveBtn">保存配置</button>' +
      '</div>';

    /* event delegation for token list */
    var list = document.getElementById('settingsTokenList');
    list.addEventListener('click', function (e) {
      var btn = e.target.closest('button[data-action]');
      if (!btn) return;
      var idx = Number(btn.getAttribute('data-idx'));
      var action = btn.getAttribute('data-action');
      if (action === 'remove') removeToken(idx);
      else if (action === 'copy') copyToken(idx);
      else if (action === 'toggle') {
        tokenRevealed[idx] = !tokenRevealed[idx];
        renderTokens();
      }
    });

    document.getElementById('settingsAddTokenBtn').addEventListener('click', function () {
      var input = document.getElementById('settingsNewToken');
      if (addToken(input.value)) input.value = '';
    });
    document.getElementById('settingsNewToken').addEventListener('keyup', function (e) {
      if (e.key === 'Enter') {
        var input = e.currentTarget;
        if (addToken(input.value)) input.value = '';
      }
    });
    document.getElementById('settingsApiBaseUrl').addEventListener('input', function (e) {
      current.apiBaseUrl = e.currentTarget.value;
      updateSaveButton();
    });
    document.getElementById('settingsCancelBtn').addEventListener('click', cancel);
    document.getElementById('settingsSaveBtn').addEventListener('click', save);

    load();
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.settings = { render: render };
})(window);
```

- [ ] **Step 2: Commit**

```bash
git add web/static/js/pages/settings.js
git commit -m "feat(ui): implement settings page (full functionality)"
```

---

## Task 14: Page — Dashboard (mock + real token count)

**Files:**
- Create: `web/static/js/pages/dashboard.js`

- [ ] **Step 1: Write the dashboard page**

Create `web/static/js/pages/dashboard.js`:

```js
/* Dashboard page — overview with stats and recent requests.
 * Phase 1: token count from /api/config; other stats = 0 (real data Phase 5).
 */

(function (global) {
  'use strict';

  async function load() {
    var tokenCount = 0;
    try {
      var res = await global.WXApi.authJson('/api/config');
      if (res.data && res.data.code === 0 && res.data.data) {
        tokenCount = (res.data.data.tokens || []).length;
      }
    } catch (e) { /* ignore */ }

    var countEl = document.getElementById('statTokenCount');
    if (countEl) countEl.textContent = String(tokenCount);
  }

  function render(slot) {
    slot.innerHTML =
      '<div class="section-title">概览</div>' +

      '<div class="stat-grid">' +
        '<div class="stat">' +
          '<div class="stat__label">配置的 Token 数</div>' +
          '<div class="stat__value" id="statTokenCount">–</div>' +
          '<div class="stat__note">从 /api/config 实时读取</div>' +
        '</div>' +
        '<div class="stat">' +
          '<div class="stat__label">今日调用</div>' +
          '<div class="stat__value">0</div>' +
          '<div class="stat__note">下期接入真实数据</div>' +
        '</div>' +
        '<div class="stat">' +
          '<div class="stat__label">平均耗时</div>' +
          '<div class="stat__value">–</div>' +
          '<div class="stat__note">下期接入真实数据</div>' +
        '</div>' +
      '</div>' +

      '<div class="section-title">最近请求</div>' +
      '<div class="card">' +
        '<div class="empty">' +
          '<div class="empty__icon">' +
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="24" height="24"><path d="M12 8v4l3 3"/><circle cx="12" cy="12" r="9"/></svg>' +
          '</div>' +
          '<div class="empty__title">暂无请求记录</div>' +
          '<div class="empty__desc">下个版本将展示实时请求历史</div>' +
        '</div>' +
      '</div>';

    load();
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.dashboard = { render: render };
})(window);
```

- [ ] **Step 2: Commit**

```bash
git add web/static/js/pages/dashboard.js
git commit -m "feat(ui): add dashboard page with token count"
```

---

## Task 15: Pages — Placeholders (test, history, users, system)

**Files:**
- Create: `web/static/js/pages/test.js`
- Create: `web/static/js/pages/history.js`
- Create: `web/static/js/pages/users.js`
- Create: `web/static/js/pages/system.js`

- [ ] **Step 1: Write `test.js`**

Create `web/static/js/pages/test.js`:

```js
(function (global) {
  'use strict';
  function render(slot) {
    slot.innerHTML =
      '<div class="empty">' +
        '<div class="empty__icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg></div>' +
        '<div class="empty__title">解析测试</div>' +
        '<div class="empty__desc">该功能将在下个版本上线。届时您可以粘贴微信分享链接或 objectId，调试 /wx 与 /wx/finder 并查看返回结果。</div>' +
      '</div>';
  }
  global.WXPages = global.WXPages || {};
  global.WXPages.test = { render: render };
})(window);
```

- [ ] **Step 2: Write `history.js`**

Create `web/static/js/pages/history.js`:

```js
(function (global) {
  'use strict';
  function render(slot) {
    slot.innerHTML =
      '<div class="empty">' +
        '<div class="empty__icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 12a9 9 0 109-9 9.75 9.75 0 00-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/></svg></div>' +
        '<div class="empty__title">解析历史</div>' +
        '<div class="empty__desc">该功能将在下个版本上线。届时您可以查看所有 /wx 请求的完整记录（时间、用户、状态、耗时、URL）。</div>' +
      '</div>';
  }
  global.WXPages = global.WXPages || {};
  global.WXPages.history = { render: render };
})(window);
```

- [ ] **Step 3: Write `users.js`**

Create `web/static/js/pages/users.js`:

```js
(function (global) {
  'use strict';
  function render(slot) {
    slot.innerHTML =
      '<div class="empty">' +
        '<div class="empty__icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75"/></svg></div>' +
        '<div class="empty__title">用户 / 角色</div>' +
        '<div class="empty__desc">该功能将在下个版本上线。届时支持多用户登录、角色权限分配、用量配额管理。</div>' +
      '</div>';
  }
  global.WXPages = global.WXPages || {};
  global.WXPages.users = { render: render };
})(window);
```

- [ ] **Step 4: Write `system.js`**

Create `web/static/js/pages/system.js`:

```js
(function (global) {
  'use strict';
  function render(slot) {
    slot.innerHTML =
      '<div class="empty">' +
        '<div class="empty__icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/></svg></div>' +
        '<div class="empty__title">系统信息</div>' +
        '<div class="empty__desc">该功能将在下个版本上线。届时展示服务运行状态、版本号、运行时信息与后端日志。</div>' +
      '</div>';
  }
  global.WXPages = global.WXPages || {};
  global.WXPages.system = { render: render };
})(window);
```

- [ ] **Step 5: Commit**

```bash
git add web/static/js/pages/test.js web/static/js/pages/history.js web/static/js/pages/users.js web/static/js/pages/system.js
git commit -m "feat(ui): add placeholder pages (test, history, users, system)"
```

---

## Task 16: App bootstrap (wires auth, router, login, topbar)

**Files:**
- Create: `web/static/js/app.js`

- [ ] **Step 1: Write the bootstrap**

Create `web/static/js/app.js`:

```js
/* App bootstrap — runs after all modules load.
 * 1. Toast helper
 * 2. Login form wiring
 * 3. Topbar wiring (collapse, logout, mobile drawer)
 * 4. Decide initial state: login overlay vs app shell
 * 5. Initialize router and render the initial page
 */

(function (global) {
  'use strict';

  /* ---------- Toast ---------- */
  function toast(msg, type) {
    var stack = document.getElementById('toastStack');
    if (!stack) return;
    var el = document.createElement('div');
    el.className = 'toast' + (type ? ' toast--' + type : '');
    el.textContent = msg;
    stack.appendChild(el);
    setTimeout(function () {
      el.classList.add('is-leaving');
      setTimeout(function () { if (el.parentNode) el.parentNode.removeChild(el); }, 220);
    }, 3000);
  }
  global.WXToast = toast;

  /* ---------- Topbar: sidebar collapse ---------- */
  function applyCollapsed(v) {
    if (global.WXStore) global.WXStore.setCollapsed(v);
  }
  function bindCollapse() {
    var btn = document.getElementById('collapseBtn');
    if (!btn) return;
    btn.addEventListener('click', function () {
      /* On mobile, this button is the drawer toggle, not a collapse toggle. */
      if (global.innerWidth <= 768) return;
      var next = !(global.WXStore && global.WXStore.isCollapsed());
      applyCollapsed(next);
    });
  }

  /* ---------- Topbar: logout ---------- */
  function bindLogout() {
    var btn = document.getElementById('logoutBtn');
    if (!btn) return;
    btn.addEventListener('click', function () {
      if (global.WXAuth) global.WXAuth.logout();
      if (global.location && global.location.pathname !== '/') {
        global.history.replaceState({}, '', '/');
      }
    });
  }

  /* ---------- Mobile: drawer / backdrop ---------- */
  function bindMobileDrawer() {
    var btn = document.getElementById('collapseBtn');
    var sb  = document.getElementById('sidebar');
    var bd  = document.getElementById('backdrop');
    if (!btn || !sb || !bd) return;
    btn.addEventListener('click', function (e) {
      if (global.innerWidth > 768) return; /* desktop = collapse toggle */
      sb.classList.toggle('open');
      bd.classList.toggle('show');
    });
    bd.addEventListener('click', function () {
      sb.classList.remove('open');
      bd.classList.remove('show');
    });
  }

  /* ---------- Login form ---------- */
  function bindLogin() {
    var form = document.getElementById('loginForm');
    if (!form) return;
    form.addEventListener('submit', async function (e) {
      e.preventDefault();
      var pwd = document.getElementById('loginPwd').value;
      var errEl = document.getElementById('loginError');
      if (errEl) errEl.textContent = '';
      if (!pwd) return;
      try {
        await global.WXAuth.login(pwd);
        var target = (global.WXStore && global.WXStore.getIntendedRoute()) || '/dashboard';
        if (global.WXStore) global.WXStore.clearIntendedRoute();
        if (global.WXAuth) global.WXAuth.showApp();
        if (global.WXRouter) {
          global.WXRouter.navigate(target, { replace: true });
        } else {
          global.history.replaceState({}, '', target);
        }
      } catch (err) {
        if (errEl) errEl.textContent = err.message || '登录失败';
      }
    });
  }

  /* ---------- Boot ---------- */
  function boot() {
    bindCollapse();
    bindLogout();
    bindMobileDrawer();
    bindLogin();
    if (global.WXRouter) global.WXRouter.init();

    /* restore collapsed state */
    if (global.WXStore && global.WXStore.isCollapsed()) {
      applyCollapsed(true);
    }

    /* decide initial view */
    if (global.WXAuth && global.WXAuth.isLoggedIn()) {
      global.WXAuth.showApp();
      if (global.WXRouter) global.WXRouter.render();
    } else {
      global.WXAuth.showLogin();
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot);
  } else {
    boot();
  }
})(window);
```

- [ ] **Step 2: Commit**

```bash
git add web/static/js/app.js
git commit -m "feat(ui): add app bootstrap (toast, topbar, login wiring, boot)"
```

---

## Task 17: Backend NoRoute fallback

**Files:**
- Modify: `main.go` (add `NoRoute` handler at the end, before `r.Run`)

- [ ] **Step 1: Add the fallback handler**

Open `main.go` and locate the line:

```go
log.Printf("wx_web_api starting on :%d (build: %s)", effectivePort, buildTag)
```

Insert the `NoRoute` block **immediately above** that line:

```go
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

```

- [ ] **Step 2: Verify the build compiles**

```bash
go build -o /tmp/wx_web_api_check . && rm -f /tmp/wx_web_api_check
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat(server): add NoRoute SPA fallback for client-side routing"
```

---

## Task 18: Smoke test in DevTools

**Files:** none (verification only)

- [ ] **Step 1: Build the binary**

```bash
go build -ldflags "-s -w" -o dist/wx_web_api.exe .
```

Expected: build success, binary at `dist/wx_web_api.exe`.

- [ ] **Step 2: Start the server in the background**

Run on port 13335 with a known password (replace `YOUR_PWD`). Use the harness's `run_in_background: true` for the Bash tool — the server blocks.

```bash
./dist/wx_web_api.exe -port 13335 -pwd YOUR_PWD
```

If `YOUR_PWD` is unset, default is `1` (see main.go). Wait for the log line `wx_web_api starting on :13335` to appear before proceeding.

- [ ] **Step 3: Open the browser and verify the smoke checklist**

Use Chrome DevTools MCP tools to:

1. Navigate to `http://127.0.0.1:13335/` → **expected**: login overlay appears, app shell hidden.
2. Type the wrong password → submit → **expected**: error message "登录失败" appears under the input.
3. Type the correct password → submit → **expected**: login overlay hides, app shell shows `/dashboard` with stat cards.
4. Click each of the 6 sidebar items → **expected**: URL changes, no page reload, content fades in, topbar title updates, nav item highlights.
5. Click browser back / forward → **expected**: same as click; scroll position preserved.
6. Visit `/settings` directly (paste URL) → **expected**: settings page loads (works whether logged in or not — if not, login first then auto-redirect).
7. Add a token "test-token-123", delete it, change API URL, click save → **expected**: Toast "保存成功"; refresh page; changes persist.
8. Add duplicate token "test-token-123" twice → **expected**: second add shows Toast "Token 已存在".
9. In DevTools, run `localStorage.setItem('wx_token','badtoken')` then call any API via `WXApi.authFetch('/api/config')` → **expected**: 401 → auto redirect to login overlay with Toast "登录已过期".
10. Resize browser to < 768px → **expected**: sidebar hidden, topbar collapse becomes hamburger, clicking it opens drawer with backdrop.
11. Visit `http://127.0.0.1:13335/history` directly (when logged in) → **expected**: placeholder page renders (not 404).
12. `POST /wx` with a real token in the `Authorization` header (use the original `/wx` API directly, not the UI) → **expected**: API still responds normally (no regression).

- [ ] **Step 4: Stop the server**

```bash
# kill whatever is bound to 13335
# Windows:
netstat -ano | findstr :13335
# then: taskkill /PID <pid> /F
```

- [ ] **Step 5: Final commit (if any tweaks were made)**

```bash
git status
# if changes:
git add -A
git commit -m "fix(ui): smoke-test fixes"
```

---

## Summary

- 5 CSS files, 11 JS files, 1 HTML rewrite, 1 backend change = 18 tasks
- Each task produces 1 commit
- No external dependencies added
- No API contracts changed
- All placeholder pages ready for Phase 2–5 content
