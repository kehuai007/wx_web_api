/* System page (系统信息) — admin-only runtime / version / config / health view.
 * Loads /api/system for static values, opens a WebSocket to /ws/system for
 * live updates every 2s, auto-reconnects with exponential backoff.
 *
 * WebSocket auth: the browser WS API cannot send custom headers, so the
 * session token is passed via the URL query string. The server's SessionAuth
 * middleware already accepts both Authorization header and ?token= query.
 */

(function (global) {
  'use strict';

  var RECONNECT_BASE_MS = 1000;
  var RECONNECT_MAX_MS = 30000;
  var STALE_THRESHOLD_MS = 6000; // > 3 missed ticks => consider connection stale

  var state = {
    staticLoaded: false,
    ws: null,
    reconnectAttempt: 0,
    reconnectTimer: null,
    watchdogTimer: null,
    lastSnapshotTs: 0,
    lastFrameAt: 0,
    connectionStatus: 'connecting' // 'ok' | 'connecting' | 'err' | 'auth_err'
  };

  function escapeHtml(s) {
    var div = document.createElement('div');
    div.textContent = s == null ? '' : String(s);
    return div.innerHTML;
  }

  function pad2(n) { return n < 10 ? '0' + n : '' + n; }

  function formatBytes(n) {
    if (n == null || n === 0) return '—';
    if (n < 1024) return n + ' B';
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
    if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB';
    return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB';
  }

  function formatDuration(seconds) {
    if (seconds == null) return '—';
    var d = Math.floor(seconds / 86400);
    var h = Math.floor((seconds % 86400) / 3600);
    var m = Math.floor((seconds % 3600) / 60);
    var s = Math.floor(seconds % 60);
    var parts = [];
    if (d > 0) parts.push(d + 'd');
    parts.push(pad2(h) + ':' + pad2(m) + ':' + pad2(s));
    return parts.join(' ');
  }

  function ago(ms) {
    if (!ms) return '—';
    var diff = Math.max(0, Date.now() - ms);
    if (diff < 1000) return '刚刚';
    if (diff < 60000) return Math.floor(diff / 1000) + 's 前';
    if (diff < 3600000) return Math.floor(diff / 60000) + 'm 前';
    return Math.floor(diff / 3600000) + 'h 前';
  }

  /* ---------- skeleton ---------- */

  function renderSkeleton() {
    return '<div class="card">' +
             '<div class="card__title">运行时</div>' +
             '<dl class="kv" id="sysRuntimeKv"></dl>' +
           '</div>' +
           '<div class="card">' +
             '<div class="card__title">版本</div>' +
             '<dl class="kv" id="sysVersionKv"></dl>' +
           '</div>' +
           '<div class="card">' +
             '<div class="card__title">配置</div>' +
             '<dl class="kv" id="sysConfigKv"></dl>' +
           '</div>' +
           '<div class="card">' +
             '<div class="card__title">健康度</div>' +
             '<dl class="kv" id="sysHealthKv"></dl>' +
             '<div class="health-note">' +
               '性能分析（pprof）: ' +
               '<a href="/debug/pprof/" target="_blank" rel="noopener noreferrer" class="link-btn">/debug/pprof/ ↗</a>' +
               ' <span class="kv__sub">（需要服务端启用 pprof 路由）</span>' +
             '</div>' +
           '</div>';
  }

  function placeholderRow(label, value) {
    return '<dt>' + escapeHtml(label) + '</dt><dd class="kv__value--muted">' + escapeHtml(value) + '</dd>';
  }

  function realRow(label, value, sub) {
    var html = escapeHtml(value);
    if (sub) html += ' <span class="kv__sub">' + escapeHtml(sub) + '</span>';
    return '<dt>' + escapeHtml(label) + '</dt><dd>' + html + '</dd>';
  }

  function connectionBadge() {
    var label;
    if (state.connectionStatus === 'ok') {
      label = '🟢 已连接 (' + ago(state.lastFrameAt) + ')';
    } else if (state.connectionStatus === 'connecting') {
      label = '🟡 连接中...';
    } else if (state.connectionStatus === 'auth_err') {
      label = '🔴 鉴权失败,请重新登录';
    } else {
      label = '🔴 已断开 (重连中...)';
    }
    return '<dt>连接状态</dt><dd>' +
             '<span class="conn-badge conn-badge--' + state.connectionStatus + '">' +
               '<span class="conn-badge__dot"></span>' + label +
             '</span>' +
           '</dd>';
  }

  /* ---------- static fetch ---------- */

  function applyStatic(slot, d) {
    var rt = slot.querySelector('#sysRuntimeKv');
    var ver = slot.querySelector('#sysVersionKv');
    var cfg = slot.querySelector('#sysConfigKv');

    rt.innerHTML =
      connectionBadge() +
      placeholderRow('Go version', d.go_version || '—') +
      placeholderRow('GOOS / GOARCH', (d.goos || '—') + ' / ' + (d.goarch || '—')) +
      placeholderRow('Goroutine 数', '—') +
      placeholderRow('内存 Alloc', '—') +
      placeholderRow('内存 HeapSys', '—') +
      placeholderRow('内存 Sys', '—') +
      placeholderRow('启动时长', '—');

    ver.innerHTML =
      realRow('Build tag', d.build_tag || '—') +
      realRow('编译时间', d.build_time || '—') +
      realRow('Git SHA', d.git_sha || '—');

    cfg.innerHTML =
      realRow('监听端口', String(d.port)) +
      realRow('API base URL', d.api_base_url || '—') +
      realRow('Token 数', String(d.token_count)) +
      realRow('配置文件', d.config_path || '—') +
      realRow('DB 路径', d.db_path || '—') +
      realRow('DB 大小', formatBytes(d.db_size));

    slot.dataset.goVersion = d.go_version || '';
    slot.dataset.goOsArch = (d.goos || '') + ' / ' + (d.goarch || '');
  }

  async function loadStatic(slot) {
    try {
      var res = await global.WXApi.authJson('/api/system');
      if (res.data && res.data.code === 0 && res.data.data) {
        applyStatic(slot, res.data.data);
        state.staticLoaded = true;
      } else {
        renderStaticError(slot, (res.data && res.data.msg) || '未知错误');
      }
    } catch (e) {
      renderStaticError(slot, e.message || '网络错误');
    }
  }

  function renderStaticError(slot, msg) {
    var cards = slot.querySelectorAll('.card');
    if (cards[0]) {
      cards[0].insertAdjacentHTML('beforeend',
        '<div class="result-msg">' + escapeHtml('静态字段加载失败: ' + msg) + '</div>');
    }
  }

  /* ---------- snapshot apply ---------- */

  function applySnapshot(slot, snap) {
    state.lastFrameAt = Date.now();
    state.lastSnapshotTs = snap.ts || 0;
    if (state.connectionStatus !== 'ok') {
      state.connectionStatus = 'ok';
      updateConnectionBadge(slot);
    }
    var rt = slot.querySelector('#sysRuntimeKv');
    if (!rt) return;
    // Replace the placeholder rows (everything after the connection badge)
    var rows = [];
    rows.push(connectionBadge());
    rows.push(realRow('Go version', slot.dataset.goVersion || '—'));
    rows.push(realRow('GOOS / GOARCH', slot.dataset.goOsArch || '—'));
    rows.push(realRow('Goroutine 数', String(snap.goroutines != null ? snap.goroutines : '—')));
    rows.push(realRow('内存 Alloc', formatBytes(snap.mem && snap.mem.alloc)));
    rows.push(realRow('内存 HeapSys', formatBytes(snap.mem && snap.mem.heap_sys)));
    rows.push(realRow('内存 Sys', formatBytes(snap.mem && snap.mem.sys)));
    rows.push(realRow('启动时长', formatDuration(snap.uptime_seconds)));
    rt.innerHTML = rows.join('');

    var hk = slot.querySelector('#sysHealthKv');
    if (hk) {
      var healthRows = [];
      if (snap.stats) {
        healthRows.push(realRow('请求总数', String(snap.stats.total)));
        healthRows.push(realRow('今日调用', String(snap.stats.today)));
        var errPct = snap.stats.total > 0
          ? ((snap.stats.errors / snap.stats.total) * 100).toFixed(1) + '% (' + snap.stats.errors + ' / ' + snap.stats.total + ')'
          : '—';
        healthRows.push(realRow('错误率', errPct));
      } else {
        healthRows.push(placeholderRow('请求总数', '—'));
        healthRows.push(placeholderRow('今日调用', '—'));
        healthRows.push(placeholderRow('错误率', '—'));
      }
      healthRows.push('<dt>—</dt><dd><span class="kv__sub">数据来源: /api/history 后台聚合 (request_log 表)</span></dd>');
      hk.innerHTML = healthRows.join('');
    }
  }

  function updateConnectionBadge(slot) {
    var rt = slot.querySelector('#sysRuntimeKv');
    if (!rt) return;
    // Replace only the first row (connection badge)
    var firstDt = rt.querySelector('dt');
    if (!firstDt) return;
    var firstRow = connectionBadge();
    var tmp = document.createElement('div');
    tmp.innerHTML = firstRow;
    var firstDd = rt.querySelector('dd');
    if (firstDd) {
      firstDd.outerHTML = tmp.querySelector('dd').outerHTML;
    }
  }

  /* ---------- websocket ---------- */

  function buildWsUrl() {
    var proto = global.location.protocol === 'https:' ? 'wss:' : 'ws:';
    var token = '';
    try { token = localStorage.getItem('wx_token') || ''; } catch (e) { token = ''; }
    return proto + '//' + global.location.host + '/ws/system?token=' + encodeURIComponent(token);
  }

  function connectWS(slot) {
    if (state.ws) {
      try { state.ws.close(); } catch (e) { /* ignore */ }
    }
    if (state.reconnectTimer) {
      clearTimeout(state.reconnectTimer);
      state.reconnectTimer = null;
    }
    state.connectionStatus = 'connecting';
    updateConnectionBadge(slot);

    var url = buildWsUrl();
    var ws;
    try {
      ws = new WebSocket(url);
    } catch (e) {
      scheduleReconnect(slot);
      return;
    }
    state.ws = ws;

    ws.onopen = function () {
      state.reconnectAttempt = 0;
      // first snapshot will set state.connectionStatus = 'ok' via applySnapshot
    };
    ws.onmessage = function (e) {
      try {
        var snap = JSON.parse(e.data);
        if (snap && snap.type === 'snapshot') applySnapshot(slot, snap);
      } catch (err) {
        if (global.console) console.error('system: bad frame', err);
      }
    };
    ws.onerror = function () {
      handleDisconnect(slot);
    };
    ws.onclose = function () {
      handleDisconnect(slot);
    };
  }

  /* Probe /api/system once per disconnect cycle to distinguish auth failure
   * (401 after server restart) from transient network issues. Returns true if
   * auth still looks valid (or probe itself failed for non-auth reasons) — in
   * that case we proceed with the existing reconnect-with-backoff path.
   * Returns false only when the probe definitively says 401. */
  async function probeAuth() {
    try {
      var res = await global.WXApi.authJson('/api/system');
      return true;
    } catch (e) {
      if (e && e.isAuth) return false;
      // network / 500 / parse error — don't claim auth failure
      return true;
    }
  }

  async function handleDisconnect(slot) {
    state.connectionStatus = 'connecting';
    updateConnectionBadge(slot);

    var authOk = await probeAuth();
    if (!authOk) {
      state.connectionStatus = 'auth_err';
      updateConnectionBadge(slot);
      return;
    }

    state.connectionStatus = 'err';
    updateConnectionBadge(slot);
    scheduleReconnect(slot);
  }

  function scheduleReconnect(slot) {
    if (state.reconnectTimer) return;
    var delay = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * Math.pow(2, state.reconnectAttempt));
    state.reconnectAttempt++;
    state.reconnectTimer = setTimeout(function () {
      state.reconnectTimer = null;
      connectWS(slot);
    }, delay);
  }

  /* ---------- stale-frame watchdog ---------- */

  function startWatchdog(slot) {
    if (state.watchdogTimer) {
      clearInterval(state.watchdogTimer);
      state.watchdogTimer = null;
    }
    state.watchdogTimer = setInterval(function () {
      if (state.connectionStatus === 'ok' && state.lastFrameAt > 0) {
        var staleFor = Date.now() - state.lastFrameAt;
        if (staleFor > STALE_THRESHOLD_MS) {
          // Server missed > 3 ticks; treat as disconnect and force reconnect
          if (state.ws) {
            try { state.ws.close(); } catch (e) { /* ignore */ }
          }
        }
      }
    }, 2000);
  }

  /* ---------- boot ---------- */

  function render(slot) {
    // Cleanup any prior render's state (router re-renders on every visit).
    if (state.watchdogTimer) {
      clearInterval(state.watchdogTimer);
      state.watchdogTimer = null;
    }
    if (state.reconnectTimer) {
      clearTimeout(state.reconnectTimer);
      state.reconnectTimer = null;
    }
    if (state.ws) {
      try { state.ws.close(); } catch (e) { /* ignore */ }
      state.ws = null;
    }
    slot.innerHTML = renderSkeleton();
    loadStatic(slot);
    connectWS(slot);
    startWatchdog(slot);
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.system = { render: render };
})(window);
