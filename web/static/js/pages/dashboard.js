/* Dashboard page — overview with stats and recent requests.
 * - Token count: /api/config (live).
 * - Recent requests: last 10 rows from /api/history?range=all (one-shot
 *   fetch on render; click "刷新" or revisit the page to refresh).
 */

(function (global) {
  'use strict';

  var RECENT_SIZE = 10;
  var recent = [];
  var unsubscribers = [];

  function escapeHtml(s) {
    var div = document.createElement('div');
    div.textContent = s == null ? '' : String(s);
    return div.innerHTML;
  }

  function pad2(n) { return n < 10 ? '0' + n : '' + n; }

  function fmtTs(ms) {
    if (!ms) return '—';
    var d = new Date(ms);
    return pad2(d.getHours()) + ':' + pad2(d.getMinutes()) + ':' + pad2(d.getSeconds());
  }

  function summarizeRequest(req, kind) {
    if (!req) return '—';
    if (kind === 'url') return req.url || '—';
    if (kind === 'finder') return (req.objectId || '—') + (req.objectNonceId ? ' / ' + req.objectNonceId : '');
    if (kind === 'auth') return req.path || '—';
    try { return JSON.stringify(req); } catch (e) { return '—'; }
  }

  function statusBadge(status) {
    if (status === 0) return '<span class="badge badge--ok">成功</span>';
    if (status === 1) return '<span class="badge badge--err">业务错</span>';
    if (status === 401) return '<span class="badge badge--auth_err">鉴权失败</span>';
    return '<span class="badge">' + escapeHtml(String(status)) + '</span>';
  }

  function kindBadge(kind) {
    var label = kind === 'url' ? 'URL' : kind === 'finder' ? 'finder' : kind === 'auth' ? 'auth' : (kind || '?');
    return '<span class="badge badge--kind-' + escapeHtml(kind) + '">' + escapeHtml(label) + '</span>';
  }

  /* ---------- recent requests ---------- */

  function renderRecentRows(items) {
    if (!items || items.length === 0) {
      return '<div class="empty">' +
        '<div class="empty__icon">' +
          '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="24" height="24"><path d="M12 8v4l3 3"/><circle cx="12" cy="12" r="9"/></svg>' +
        '</div>' +
        '<div class="empty__title">暂无请求记录</div>' +
        '<div class="empty__desc">试试在解析测试页发一次请求</div>' +
      '</div>';
    }
    var rows = items.map(function (r) {
      var summary = summarizeRequest(r.request, r.kind);
      return '<div class="recent-row">' +
        '<div class="recent-row__ts">' + escapeHtml(fmtTs(r.ts)) + '</div>' +
        '<div class="recent-row__kind">' + kindBadge(r.kind) + '</div>' +
        '<div class="recent-row__status">' + statusBadge(r.status) + '</div>' +
        '<div class="recent-row__token" title="' + escapeHtml(r.token_label || '') + '">' + escapeHtml(r.token_label || '(无)') + '</div>' +
        '<div class="recent-row__ip" title="' + escapeHtml(r.client_ip || '') + '">' + escapeHtml(r.client_ip || '—') + '</div>' +
        '<div class="recent-row__summary" title="' + escapeHtml(summary) + '">' + escapeHtml(summary) + '</div>' +
        '<div class="recent-row__latency">' + escapeHtml(String(r.latency_ms)) + 'ms</div>' +
      '</div>';
    }).join('');
    return '<div class="recent-list">' + rows + '</div>';
  }

  async function loadRecent(slot) {
    var card = slot.querySelector('[data-role="recent-card"]');
    if (!card) return;
    var body = card.querySelector('[data-role="recent-body"]');
    if (!body) return;
    try {
      var res = await global.WXApi.authJson('/api/history?range=all&page=1&size=' + RECENT_SIZE);
      if (res.data && res.data.code === 0 && res.data.data) {
        recent = (res.data.data.items || []).slice();
        renderRecent(slot);
      } else {
        body.innerHTML = '<div class="result-msg">加载失败: ' + escapeHtml((res.data && res.data.msg) || '未知错误') + '</div>';
      }
    } catch (e) {
      if (e && e.isAuth) return;
      body.innerHTML = '<div class="result-msg">加载失败: ' + escapeHtml(e.message || '网络错误') + '</div>';
    }
  }

  function renderRecent(slot) {
    var card = slot.querySelector('[data-role="recent-card"]');
    if (!card) return;
    var body = card.querySelector('[data-role="recent-body"]');
    if (!body) return;
    body.innerHTML = renderRecentRows(recent);
  }

  async function loadTokenCount(slot) {
    var countEl = slot.querySelector('#statTokenCount');
    if (!countEl) return;
    try {
      var res = await global.WXApi.authJson('/api/config');
      if (res.data && res.data.code === 0 && res.data.data) {
        countEl.textContent = String((res.data.data.tokens || []).length);
      }
    } catch (e) { /* leave the dash placeholder */ }
  }

  function bindEvents(slot) {
    if (!global.WXEvents) return;
    unsubscribers.push(global.WXEvents.subscribe('log.new', function (frame) {
      if (!frame || !frame.log) return;
      recent.unshift(frame.log);
      if (recent.length > RECENT_SIZE) recent.length = RECENT_SIZE;
      renderRecent(slot);
    }));
    unsubscribers.push(global.WXEvents.subscribe('config.changed', function () {
      loadTokenCount(slot);
    }));
  }

  function cleanup() {
    unsubscribers.forEach(function (u) { try { u(); } catch (e) { /* ignore */ } });
    unsubscribers = [];
  }

  function render(slot) {
    cleanup();
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
          '<div class="stat__note">见 /system 页面实时数据</div>' +
        '</div>' +
        '<div class="stat">' +
          '<div class="stat__label">平均耗时</div>' +
          '<div class="stat__value">–</div>' +
          '<div class="stat__note">见 /history 页面明细</div>' +
        '</div>' +
      '</div>' +

      '<div class="section-title">' +
        '<span>最近请求</span>' +
        '<span class="section-title__actions">' +
          '<button class="btn btn--secondary" data-role="recent-refresh">刷新</button> ' +
          '<a class="link-btn" data-route="/history">查看全部 →</a>' +
        '</span>' +
      '</div>' +
      '<div class="card" data-role="recent-card">' +
        '<div data-role="recent-body">' +
          '<div class="result-msg">加载中…</div>' +
        '</div>' +
      '</div>';

    var refresh = slot.querySelector('[data-role="recent-refresh"]');
    if (refresh) refresh.addEventListener('click', function () { loadRecent(slot); });

    var jump = slot.querySelector('a[data-route="/history"]');
    if (jump) jump.addEventListener('click', function (e) {
      e.preventDefault();
      if (global.WXRouter && global.WXRouter.navigate) global.WXRouter.navigate('/history');
    });

    loadTokenCount(slot);
    loadRecent(slot);
    bindEvents(slot);
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.dashboard = { render: render };
})(window);
