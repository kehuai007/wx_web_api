/* Test page (解析测试) — operator-facing debug tool for /wx and /wx/finder.
 * Loads /api/config for the token dropdown, fires a raw fetch with the
 * operator's chosen cfg.Tokens value, renders the parsed payload
 * (fields + cover thumbnail + latency), keeps an in-page history of
 * the last 20 calls with refill.
 *
 * Why raw fetch() and not WXApi.authFetch:
 *   - WXApi.authFetch auto-injects the admin session token from localStorage,
 *     which would clobber the operator's chosen token.
 *   - WXApi.authFetch's 401 handler triggers WXAuth.handle401() and logs the
 *     operator out — wrong for a debug page where 401 is an expected result.
 */

(function (global) {
  'use strict';

  var MAX_HISTORY = 20;
  var MEDIA_TYPE_LABELS = { 1: '图片', 2: '视频', 4: '视频' };

  var state = {
    tokens: [],
    currentToken: '',
    activeTab: 'url',        // 'url' | 'finder'
    urlInput: '',
    objectIdInput: '',
    objectNonceIdInput: '',
    lastResult: null,         // result object from callEndpoint() (or null)
    history: []               // array of {ts, endpoint, ok, request, response, elapsed, token, tab}
  };

  /* ---------- helpers ---------- */

  function escapeHtml(s) {
    var div = document.createElement('div');
    div.textContent = s == null ? '' : String(s);
    return div.innerHTML;
  }

  function pad2(n) { return n < 10 ? '0' + n : '' + n; }

  function formatTime(ts) {
    var d = new Date(ts);
    return pad2(d.getHours()) + ':' + pad2(d.getMinutes()) + ':' + pad2(d.getSeconds());
  }

  function truncate(s, n) {
    s = s == null ? '' : String(s);
    return s.length > n ? s.slice(0, n) + '…' : s;
  }

  // Per the implementation plan: 1/2/4 are mapped, all other values (including 0)
  // are rendered as "N (未知)" — the spec table's "0=未知" is not special-cased.
  function mediaTypeLabel(t) {
    if (MEDIA_TYPE_LABELS[t]) return t + ' (' + MEDIA_TYPE_LABELS[t] + ')';
    return t + ' (未知)';
  }

  function tokenDisplay(t) {
    var head = (t.value || '').slice(0, 8) + '…';
    if (!t.expires_at) return head + ' · 永久';
    return head + ' · ' + t.expires_at;
  }

  function summarizeRequest(entry) {
    if (entry.tab === 'url') {
      return truncate(entry.request.body.url || '', 60);
    }
    var oid = entry.request.body.objectId || '';
    return 'objectId:' + truncate(oid, 30);
  }

  function copyToClipboard(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () {
        if (global.WXToast) global.WXToast('已复制', 'success');
      }, function () { fallbackCopy(text); });
    } else {
      fallbackCopy(text);
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

  function redactedRequest(req) {
    // Strip token values from headers for display. Body redaction is intentionally
    // omitted: the test page only ever puts {url} or {objectId, objectNonceId} in
    // the body, so no auth-shaped field can leak. If a future endpoint takes a
    // token in the body, add a `token` key check here.
    var copy = {
      method: req.method,
      url: req.url,
      headers: {},
      body: req.body
    };
    for (var k in req.headers) {
      if (Object.prototype.hasOwnProperty.call(req.headers, k)) {
        var lower = k.toLowerCase();
        if (lower === 'authorization') {
          copy.headers[k] = '***';
        } else {
          copy.headers[k] = req.headers[k];
        }
      }
    }
    return copy;
  }

  /* ---------- fetch / call ---------- */

  async function callEndpoint(endpoint, body, token) {
    var t0 = (global.performance && performance.now) ? performance.now() : Date.now();
    var reqForLog = {
      method: 'POST',
      url: endpoint,
      headers: { 'Authorization': token, 'Content-Type': 'application/json', 'X-Wx-Source': 'admin_test' },
      body: body
    };
    var res, networkError = false, networkMsg = '';
    try {
      res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Authorization': token, 'Content-Type': 'application/json', 'X-Wx-Source': 'admin_test' },
        body: JSON.stringify(body)
      });
    } catch (e) {
      networkError = true;
      networkMsg = (e && e.message) || '网络错误';
    }
    var t1 = (global.performance && performance.now) ? performance.now() : Date.now();
    if (networkError) {
      return {
        ok: false,
        elapsed: t1 - t0,
        request: reqForLog,
        response: null,
        networkError: true,
        networkMsg: networkMsg,
        code: null, msg: '网络错误', data: null
      };
    }
    var parsed = null, parseError = false;
    try { parsed = await res.json(); } catch (e) { parseError = true; }
    return {
      ok: res.ok && !parseError && parsed && parsed.code === 0,
      elapsed: t1 - t0,
      request: reqForLog,
      response: { status: res.status, body: parsed },
      parseError: parseError,
      code: parsed ? parsed.code : null,
      msg: parsed ? parsed.msg : (parseError ? '响应不是合法 JSON' : ('HTTP ' + res.status)),
      data: parsed && parsed.data ? parsed.data : null
    };
  }

  /* ---------- render ---------- */

  function renderInputCard() {
    var tokens = state.tokens;
    var hasTokens = tokens.length > 0;
    var tokenOptions;
    if (!hasTokens) {
      tokenOptions = '<option value="">(无 token，请先在配置页添加)</option>';
    } else {
      tokenOptions = tokens.map(function (t) {
        var sel = t.value === state.currentToken ? ' selected' : '';
        return '<option value="' + escapeHtml(t.value) + '"' + sel + '>' +
               escapeHtml(tokenDisplay(t)) + '</option>';
      }).join('');
    }
    var submitDisabled = !hasTokens || !state.currentToken;

    return '<div class="card">' +
             '<div class="card__title">输入</div>' +
             '<div class="test-token-row">' +
               '<label class="test-token-row__label" for="testTokenSelect">鉴权 Token</label>' +
               '<select class="input" id="testTokenSelect"' + (hasTokens ? '' : ' disabled') + '>' +
                 tokenOptions +
               '</select>' +
             '</div>' +
             '<div class="tabs" role="tablist">' +
               '<button type="button" class="tab' + (state.activeTab === 'url' ? ' tab--active' : '') +
                 '" data-tab="url" role="tab">URL</button>' +
               '<button type="button" class="tab' + (state.activeTab === 'finder' ? ' tab--active' : '') +
                 '" data-tab="finder" role="tab">objectId</button>' +
             '</div>' +
             (state.activeTab === 'url'
               ? '<div class="form-group">' +
                   '<label class="form-label" for="testUrlInput">微信分享 URL</label>' +
                   '<input type="text" class="input" id="testUrlInput" placeholder="https://mp.weixin.qq.com/s/... 或 https://weixin.qq.com/sph/..." value="' + escapeHtml(state.urlInput) + '">' +
                   '<div class="form-helper">支持公众号文章链接和视频号分享链接</div>' +
                 '</div>'
               : '<div class="form-group">' +
                   '<label class="form-label" for="testObjectIdInput">objectId</label>' +
                   '<input type="text" class="input" id="testObjectIdInput" placeholder="objectId" value="' + escapeHtml(state.objectIdInput) + '">' +
                 '</div>' +
                 '<div class="form-group">' +
                   '<label class="form-label" for="testObjectNonceIdInput">objectNonceId</label>' +
                   '<input type="text" class="input" id="testObjectNonceIdInput" placeholder="objectNonceId" value="' + escapeHtml(state.objectNonceIdInput) + '">' +
                 '</div>') +
             '<div class="test-form-actions">' +
               '<button type="button" class="btn btn--primary" id="testSubmitBtn"' + (submitDisabled ? ' disabled' : '') + '>测试</button>' +
             '</div>' +
           '</div>';
  }

  function renderResultCard() {
    if (!state.lastResult) return '';
    var r = state.lastResult;
    var statusHtml = r.ok
      ? '<span class="result-status result-status--ok">✅ 成功</span>'
      : '<span class="result-status result-status--fail">❌ 失败</span>';
    var latencyText = r.elapsed < 1 ? '< 1ms' : Math.round(r.elapsed) + ' ms';
    var endpointHtml = '<span class="result-endpoint">' + escapeHtml(r.request.url) + '</span>';
    var latencyHtml = '<span class="result-latency">耗时 ' + escapeHtml(latencyText) + '</span>';

    var bodyHtml = '';
    if (r.ok && r.data) {
      bodyHtml = renderResultFields(r.data);
    } else {
      bodyHtml = '<div class="result-msg">' + escapeHtml(r.msg || '未知错误') + '</div>';
    }

    var detailsHtml = renderDetails(r);

    return '<div class="card">' +
             '<div class="result-header">' +
               statusHtml + latencyHtml + endpointHtml +
               '<span style="flex:1"></span>' +
               '<button type="button" class="btn btn--ghost btn--sm" id="testClearBtn">清空</button>' +
             '</div>' +
             bodyHtml +
             detailsHtml +
           '</div>';
  }

  function renderResultFields(d) {
    var rows = [];
    if (d.author) {
      rows.push(fieldRow('Author', escapeHtml(d.author), d.author));
    }
    if (d.title) {
      rows.push(fieldRow('Title', escapeHtml(d.title), d.title));
    }
    if (d.cover_url) {
      var coverImg = '<img class="result-cover" src="' + escapeHtml(d.cover_url) + '" alt="cover" referrerpolicy="no-referrer">';
      var coverLink = '<a href="' + escapeHtml(d.cover_url) + '" target="_blank" rel="noopener noreferrer">' + escapeHtml(d.cover_url) + '</a>';
      rows.push('<div class="field">' +
                  '<div class="field-label">Cover</div>' +
                  '<div class="field-value field-value--cover-url">' + coverImg + coverLink + '</div>' +
                  '<button type="button" class="copy-btn" data-copy="' + escapeHtml(d.cover_url) + '">复制</button>' +
                '</div>');
    }
    if (d.video_url) {
      var videoLink = '<a href="' + escapeHtml(d.video_url) + '" target="_blank" rel="noopener noreferrer">' + escapeHtml(d.video_url) + '</a>';
      rows.push('<div class="field">' +
                  '<div class="field-label">Video URL</div>' +
                  '<div class="field-value field-value--video-url">' + videoLink + '</div>' +
                  '<button type="button" class="copy-btn" data-copy="' + escapeHtml(d.video_url) + '">复制</button>' +
                '</div>');
    }
    if (d.decode_key) {
      rows.push(fieldRow('Decode Key', escapeHtml(d.decode_key), d.decode_key));
    }
    rows.push('<div class="field">' +
                '<div class="field-label">Media Type</div>' +
                '<div class="field-value">' + escapeHtml(mediaTypeLabel(d.media_type)) + '</div>' +
                '<span></span>' +
              '</div>');
    return '<div class="result-fields">' + rows.join('') + '</div>';
  }

  function fieldRow(label, valueHtml, raw) {
    return '<div class="field">' +
             '<div class="field-label">' + escapeHtml(label) + '</div>' +
             '<div class="field-value field-value--text">' + valueHtml + '</div>' +
             '<button type="button" class="copy-btn" data-copy="' + escapeHtml(raw) + '">复制</button>' +
           '</div>';
  }

  function renderDetails(r) {
    var safeReq = redactedRequest(r.request);
    var reqJson = JSON.stringify(safeReq, null, 2);
    var resJson = r.response ? JSON.stringify(r.response, null, 2) : '(no response — network error)';
    return '<div class="details" id="testDetails">' +
             '<button type="button" class="details__toggle" id="testDetailsToggle">' +
               '<span class="chev">▶</span> 请求/响应详情' +
             '</button>' +
             '<div class="details__body">' +
               '<div>' +
                 '<div class="details__section-title">Request</div>' +
                 '<pre>' + escapeHtml(reqJson) + '</pre>' +
               '</div>' +
               '<div>' +
                 '<div class="details__section-title">Response</div>' +
                 '<pre>' + escapeHtml(resJson) + '</pre>' +
               '</div>' +
             '</div>' +
           '</div>';
  }

  function renderHistoryCard() {
    if (state.history.length === 0) return '';
    var items = state.history.map(function (entry, idx) {
      var icon = entry.ok
        ? '<span class="history-item__icon history-item__icon--ok">✓</span>'
        : '<span class="history-item__icon history-item__icon--fail">✗</span>';
      return '<div class="history-item" data-idx="' + idx + '">' +
               '<span class="history-item__time">' + escapeHtml(formatTime(entry.ts)) + '</span>' +
               icon +
               '<span class="history-item__endpoint">' + escapeHtml(entry.endpoint) + '</span>' +
               '<span class="history-item__summary">' + escapeHtml(summarizeRequest(entry)) + '</span>' +
               '<button type="button" class="btn btn--ghost btn--sm" data-action="refill" data-idx="' + idx + '">回填</button>' +
             '</div>';
    }).join('');
    return '<div class="card">' +
             '<div class="history-header">' +
               '<div class="card__title" style="margin:0">本次会话历史</div>' +
               '<div>' +
                 '<span class="history-header__count">共 ' + state.history.length + ' 条</span>' +
                 '<button type="button" class="btn btn--ghost btn--sm" id="testHistoryClearBtn" style="margin-left:var(--s-3)">清空</button>' +
               '</div>' +
             '</div>' +
             '<div class="history-list">' + items + '</div>' +
           '</div>';
  }

  function renderAll(slot) {
    slot.innerHTML = renderInputCard() + renderResultCard() + renderHistoryCard();
    bindEvents(slot);
  }

  /* ---------- event binding ---------- */

  function bindEvents(slot) {
    var tokenSel = slot.querySelector('#testTokenSelect');
    if (tokenSel) {
      tokenSel.addEventListener('change', function () {
        state.currentToken = tokenSel.value;
        // re-render to update submit button disabled state
        renderAll(slot);
      });
    }
    var tabs = slot.querySelectorAll('.tab');
    for (var i = 0; i < tabs.length; i++) {
      tabs[i].addEventListener('click', function (e) {
        var tab = e.currentTarget.getAttribute('data-tab');
        if (!tab || tab === state.activeTab) return;
        // Clear the OTHER tab's inputs to avoid accidental cross-tab use
        if (tab === 'url') {
          state.objectIdInput = '';
          state.objectNonceIdInput = '';
        } else {
          state.urlInput = '';
        }
        state.activeTab = tab;
        renderAll(slot);
      });
    }
    var submitBtn = slot.querySelector('#testSubmitBtn');
    if (submitBtn) submitBtn.addEventListener('click', function () { submit(slot); });
    var clearBtn = slot.querySelector('#testClearBtn');
    if (clearBtn) clearBtn.addEventListener('click', function () {
      state.lastResult = null;
      renderAll(slot);
    });
    var detailsToggle = slot.querySelector('#testDetailsToggle');
    if (detailsToggle) detailsToggle.addEventListener('click', function () {
      var d = slot.querySelector('#testDetails');
      if (d) d.classList.toggle('details--open');
    });
    var copyBtns = slot.querySelectorAll('.copy-btn');
    for (var j = 0; j < copyBtns.length; j++) {
      copyBtns[j].addEventListener('click', function (e) {
        var val = e.currentTarget.getAttribute('data-copy');
        if (val) copyToClipboard(val);
      });
    }
    var histClear = slot.querySelector('#testHistoryClearBtn');
    if (histClear) histClear.addEventListener('click', function () {
      state.history = [];
      renderAll(slot);
    });
    var refillBtns = slot.querySelectorAll('button[data-action="refill"]');
    for (var k = 0; k < refillBtns.length; k++) {
      refillBtns[k].addEventListener('click', function (e) {
        var idx = Number(e.currentTarget.getAttribute('data-idx'));
        refillFromHistory(idx, slot);
      });
    }
  }

  /* ---------- submit + history ---------- */

  function readFormInputs(slot) {
    var urlEl = slot.querySelector('#testUrlInput');
    var oidEl = slot.querySelector('#testObjectIdInput');
    var onidEl = slot.querySelector('#testObjectNonceIdInput');
    if (urlEl) state.urlInput = urlEl.value;
    if (oidEl) state.objectIdInput = oidEl.value;
    if (onidEl) state.objectNonceIdInput = onidEl.value;
  }

  async function submit(slot) {
    if (!state.currentToken) {
      if (global.WXToast) global.WXToast('请先选择 token', 'error');
      return;
    }
    readFormInputs(slot);
    var endpoint, body;
    if (state.activeTab === 'url') {
      var url = (state.urlInput || '').trim();
      if (!url) {
        if (global.WXToast) global.WXToast('请输入 URL', 'error');
        return;
      }
      endpoint = '/wx';
      body = { url: url };
    } else {
      var oid = (state.objectIdInput || '').trim();
      var onid = (state.objectNonceIdInput || '').trim();
      if (!oid) { if (global.WXToast) global.WXToast('请输入 objectId', 'error'); return; }
      if (!onid) { if (global.WXToast) global.WXToast('请输入 objectNonceId', 'error'); return; }
      endpoint = '/wx/finder';
      body = { objectId: oid, objectNonceId: onid };
    }

    var submitBtn = slot.querySelector('#testSubmitBtn');
    if (submitBtn) { submitBtn.disabled = true; submitBtn.classList.add('is-loading'); submitBtn.textContent = '请求中…'; }
    try {
      var result = await callEndpoint(endpoint, body, state.currentToken);
      result.tab = state.activeTab;
      result.token = state.currentToken;
      result.ts = Date.now();
      state.lastResult = result;
      addToHistory({
        ts: result.ts,
        endpoint: endpoint,
        ok: result.ok,
        request: result.request,
        response: result.response,
        elapsed: result.elapsed,
        tab: result.tab,
        token: result.token
      });
    } finally {
      if (submitBtn) { submitBtn.disabled = false; submitBtn.classList.remove('is-loading'); submitBtn.textContent = '测试'; }
    }
    renderAll(slot);
  }

  function addToHistory(entry) {
    state.history.unshift(entry);
    if (state.history.length > MAX_HISTORY) state.history.length = MAX_HISTORY;
  }

  function refillFromHistory(idx, slot) {
    var entry = state.history[idx];
    if (!entry) return;
    state.activeTab = entry.tab || 'url';
    if (entry.tab === 'finder') {
      state.objectIdInput = (entry.request.body.objectId || '');
      state.objectNonceIdInput = (entry.request.body.objectNonceId || '');
      state.urlInput = '';
    } else {
      state.urlInput = (entry.request.body.url || '');
      state.objectIdInput = '';
      state.objectNonceIdInput = '';
    }
    // do NOT change currentToken — spec says the test page never persists or
    // changes the selected token; the operator stays in control
    renderAll(slot);
  }

  /* ---------- boot ---------- */

  async function loadTokens() {
    try {
      var res = await global.WXApi.authJson('/api/config');
      if (res.data && res.data.code === 0 && res.data.data) {
        var toks = Array.isArray(res.data.data.tokens) ? res.data.data.tokens : [];
        state.tokens = toks;
        if (!state.currentToken && toks.length > 0) {
          state.currentToken = toks[0].value;
        }
      } else {
        state.tokens = [];
      }
    } catch (e) {
      state.tokens = [];
    }
  }

  function render(slot) {
    renderAll(slot);
    loadTokens().then(function () {
      renderAll(slot);
    });
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.test = { render: render };
})(window);
