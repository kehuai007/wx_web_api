/* Settings page — full implementation with token expiration.
 * Loads /api/config, renders form, supports add/remove/copy/reveal tokens
 * and per-row expiration date (manual + 5 quick presets + status badge),
 * dirty tracking, save/cancel, beforeunload prompt.
 *
 * 实时事件(Task 15):
 * - 订阅 WXEvents 'config.changed',在表单 dirty 时顶部出现"配置已被其他会话更新"
 *   提示条;clean 时静默 reload。
 * - 保存成功后设置 2s 的 ignoreConfigChangedUntil 窗口,抑制自身广播。
 * - render 入口先 cleanup 旧订阅,避免 router 切换时回调打到 detach 的 slot。
 */

(function (global) {
  'use strict';

  var original = { apiBaseUrl: '', tokens: [], historyRetentionDays: 0 };
  var current  = { apiBaseUrl: '', tokens: [], historyRetentionDays: 0 };
  var tokenRevealed = {};
  var beforeUnloadBound = false;

  var state = {
    config: null,                  // 最近一次拉取的 config(用于 reload + 自我忽略后回填)
    dirty: false,                  // 表单是否被改(sticky,任何 input 都置 true)
    ignoreConfigChangedUntil: 0,   // 自我保存后抑制 config.changed 的截止时间戳
    unsubscribers: [],
  };

  var PRESETS = [
    { label: '7天',  days: 7 },
    { label: '30天', days: 30 },
    { label: '90天', days: 90 },
    { label: '1年',  days: 365 }
  ];

  function escapeHtml(s) {
    var div = document.createElement('div');
    div.textContent = s == null ? '' : String(s);
    return div.innerHTML;
  }

  function todayStr() {
    var d = new Date();
    var pad = function (n) { return n < 10 ? '0' + n : '' + n; };
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  }

  function presetDate(days) {
    var d = new Date();
    d.setDate(d.getDate() + days);
    var pad = function (n) { return n < 10 ? '0' + n : '' + n; };
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  }

  function parseDate(s) {
    if (!s) return null;
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s);
    if (!m) return null;
    var d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
    if (isNaN(d.getTime())) return null;
    if (d.getFullYear() !== Number(m[1]) || d.getMonth() !== Number(m[2]) - 1 || d.getDate() !== Number(m[3])) return null;
    return d;
  }

  function daysUntil(dateStr) {
    var d = parseDate(dateStr);
    if (!d) return null;
    var now = new Date();
    var todayMid = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    var diffMs = d.getTime() - todayMid.getTime();
    return Math.round(diffMs / 86400000);
  }

  function mask(token) {
    if (!token) return '';
    if (token.length <= 12) return token.slice(0, 4) + '••••••••';
    return token.slice(0, 8) + '••••••••';
  }

  function isDirty() {
    if (original.apiBaseUrl !== current.apiBaseUrl) return true;
    if ((original.historyRetentionDays || 0) !== (current.historyRetentionDays || 0)) return true;
    if (original.tokens.length !== current.tokens.length) return true;
    var sig = function (list) {
      return list.map(function (t) {
        return [t.value || '', t.label || '', t.expires_at || ''];
      }).sort(function (a, b) {
        return a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : (a[1] < b[1] ? -1 : a[1] > b[1] ? 1 : (a[2] < b[2] ? -1 : a[2] > b[2] ? 1 : 0));
      });
    };
    var a = sig(original.tokens);
    var b = sig(current.tokens);
    for (var i = 0; i < a.length; i++) {
      if (a[i][0] !== b[i][0] || a[i][1] !== b[i][1] || a[i][2] !== b[i][2]) return true;
    }
    return false;
  }

  function updateSaveButton() {
    var btn = document.getElementById('settingsSaveBtn');
    if (!btn) return;
    btn.disabled = !isDirty();
  }

  function badgeHtmlFor(dateStr) {
    var d = daysUntil(dateStr);
    if (d == null) return '';
    if (d < 0)  return '<span class="badge badge--danger">已过期</span>';
    if (d === 0) return '<span class="badge badge--danger">今天过期</span>';
    if (d <= 7) return '<span class="badge badge--warning">' + d + ' 天后过期</span>';
    return '';
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
        ? '<span class="token-item__value">' + escapeHtml(t.value) + '</span>'
        : '<span class="token-item__value token-item__value--masked">' + escapeHtml(mask(t.value)) + '</span>';
      var expires = t.expires_at || '';
      var presetsHtml = PRESETS.map(function (p) {
        var active = expires && expires === presetDate(p.days);
        return '<button type="button" class="btn btn--sm btn--preset' + (active ? ' btn--active' : '') +
               '" data-action="preset" data-idx="' + i + '" data-days="' + p.days + '">' + p.label + '</button>';
      }).join('');
      var permanentActive = !expires ? ' btn--active' : '';
      var badge = badgeHtmlFor(expires);
      return '<div class="token-item token-item--with-expiry" data-idx="' + i + '">' +
               '<div class="token-item__row">' +
                 valueHtml +
                 '<button type="button" class="btn btn--ghost btn--sm" data-action="copy" data-idx="' + i + '">复制</button>' +
                 '<button type="button" class="btn btn--ghost btn--sm" data-action="toggle" data-idx="' + i + '">' + (revealed ? '隐藏' : '显示') + '</button>' +
                 '<button type="button" class="btn btn--danger btn--sm" data-action="remove" data-idx="' + i + '">删除</button>' +
               '</div>' +
               '<div class="token-item__row token-item__row--expiry">' +
                 '<label class="token-item__expiry-label">过期日期</label>' +
                 '<input type="date" class="input input--date" data-action="expiry" data-idx="' + i + '" value="' + escapeHtml(expires) + '" placeholder="yyyy-MM-dd">' +
                 '<div class="token-item__presets">' + presetsHtml +
                   '<button type="button" class="btn btn--sm btn--preset' + permanentActive + '" data-action="preset-permanent" data-idx="' + i + '">永久</button>' +
                 '</div>' +
               '</div>' +
               '<div class="token-item__row">' +
                 '<label class="token-item__expiry-label">显示名称</label>' +
                 '<input type="text" class="input" data-action="label" data-idx="' + i + '" value="' + escapeHtml(t.label || '') + '" placeholder="可选,默认取前 8 字符" maxlength="64">' +
               '</div>' +
               (badge ? '<div class="token-item__badge">' + badge + '</div>' : '') +
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
    for (var i = 0; i < current.tokens.length; i++) {
      if (current.tokens[i].value === token) {
        if (global.WXToast) global.WXToast('Token 已存在', 'error');
        return false;
      }
    }
    current.tokens.push({ value: token, label: '', expires_at: '' });
    state.dirty = true;
    renderTokens();
    updateSaveButton();
    return true;
  }

  function removeToken(idx) {
    if (idx < 0 || idx >= current.tokens.length) return;
    current.tokens.splice(idx, 1);
    delete tokenRevealed[idx];
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
      navigator.clipboard.writeText(t.value).then(function () {
        if (global.WXToast) global.WXToast('已复制', 'success');
      }, function () {
        fallbackCopy(t.value);
      });
    } else {
      fallbackCopy(t.value);
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

  function setExpiry(idx, value) {
    if (idx < 0 || idx >= current.tokens.length) return;
    current.tokens[idx].expires_at = value || '';
    renderTokens();
    updateSaveButton();
  }

  async function save() {
    var apiBaseUrl = (document.getElementById('settingsApiBaseUrl').value || '').trim() ||
                     'http://127.0.0.1:2022';
    current.apiBaseUrl = apiBaseUrl;
    var retentionInput = document.getElementById('settingsRetentionDays');
    current.historyRetentionDays = retentionInput ? (Number(retentionInput.value) || 0) : 0;
    var tokensToSend = current.tokens.map(function (t) {
      return { value: t.value, label: t.label || '', expires_at: t.expires_at || '' };
    });

    var btn = document.getElementById('settingsSaveBtn');
    if (btn) { btn.disabled = true; btn.classList.add('is-loading'); btn.textContent = '保存中…'; }

    try {
      var res = await global.WXApi.authJson('/api/config', {
        method: 'PUT',
        body: JSON.stringify({
          api_base_url: apiBaseUrl,
          tokens: tokensToSend,
          history_retention_days: current.historyRetentionDays
        })
      });
      if (res.data && res.data.code === 0) {
        original = {
          apiBaseUrl: apiBaseUrl,
          historyRetentionDays: current.historyRetentionDays,
          tokens: tokensToSend.map(function (t) { return { value: t.value, label: t.label, expires_at: t.expires_at }; })
        };
        // 同步本地 config 缓存,后续 reloadFromServer 时有 baseline;
        // 设 2s 忽略窗口,避免自身保存触发的 config.changed 再次弹出"已被其他会话更新"。
        state.config = {
          api_base_url: apiBaseUrl,
          history_retention_days: current.historyRetentionDays,
          tokens: tokensToSend.map(function (t) { return { value: t.value, label: t.label, expires_at: t.expires_at }; })
        };
        state.ignoreConfigChangedUntil = Date.now() + 2000;
        state.dirty = false;
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
    current = {
      apiBaseUrl: original.apiBaseUrl,
      historyRetentionDays: original.historyRetentionDays,
      tokens: original.tokens.map(function (t) { return { value: t.value, label: t.label, expires_at: t.expires_at }; })
    };
    var input = document.getElementById('settingsApiBaseUrl');
    if (input) input.value = original.apiBaseUrl;
    var retentionInput = document.getElementById('settingsRetentionDays');
    if (retentionInput) retentionInput.value = String(original.historyRetentionDays);
    tokenRevealed = {};
    state.dirty = false;
    renderTokens();
    updateSaveButton();
  }

  function bindBeforeUnload() {
    if (beforeUnloadBound) return;
    beforeUnloadBound = true;
    global.addEventListener('beforeunload', function (e) {
      if (isDirty()) {
        e.preventDefault();
        e.returnValue = '';
      }
    });
  }

  async function loadConfig() {
    var res = await global.WXApi.authJson('/api/config');
    if (res.data && res.data.code === 0 && res.data.data) {
      var toks = Array.isArray(res.data.data.tokens) ? res.data.data.tokens : [];
      original = {
        apiBaseUrl: res.data.data.api_base_url || 'http://127.0.0.1:2022',
        historyRetentionDays: Number(res.data.data.history_retention_days) || 0,
        tokens: toks.map(function (t) { return { value: t.value, label: t.label || '', expires_at: t.expires_at || '' }; })
      };
      current = {
        apiBaseUrl: original.apiBaseUrl,
        historyRetentionDays: original.historyRetentionDays,
        tokens: original.tokens.map(function (t) { return { value: t.value, label: t.label, expires_at: t.expires_at }; })
      };
      state.config = {
        api_base_url: original.apiBaseUrl,
        history_retention_days: original.historyRetentionDays,
        tokens: original.tokens.map(function (t) { return { value: t.value, label: t.label, expires_at: t.expires_at }; })
      };
      var input = document.getElementById('settingsApiBaseUrl');
      if (input) input.value = current.apiBaseUrl;
      var retentionInput = document.getElementById('settingsRetentionDays');
      if (retentionInput) retentionInput.value = String(current.historyRetentionDays);
      renderTokens();
      updateSaveButton();
      // 静默 reload 也走这里(原表单 dirty 状态保留给 hideStaleBar)
      fetchRecordCount();
    }
  }

  function fetchRecordCount() {
    var span = document.getElementById('settingsRecordCount');
    if (!span || !global.WXApi || !global.WXApi.authJson) return;
    global.WXApi.authJson('/api/history?range=all&size=1').then(function (res) {
      if (span && res && res.data && res.data.code === 0 && res.data.data && typeof res.data.data.total === 'number') {
        span.textContent = res.data.data.total.toLocaleString();
      }
    }, function () {
      /* leave as '—' on failure */
    });
  }

  /* ---------- config-stale bar + WXEvents 订阅 ---------- */

  function hideStaleBar(slot) {
    var el = slot.querySelector('[data-role="config-stale"]');
    if (el) el.hidden = true;
  }

  function reloadFromServer(slot) {
    loadConfig().then(function () { hideStaleBar(slot); });
  }

  function onConfigChanged(slot) {
    // 自我推送免提示窗口(自身保存后 2s 内的广播)
    if (Date.now() < state.ignoreConfigChangedUntil) return;
    if (state.dirty) {
      var el = slot.querySelector('[data-role="config-stale"]');
      if (el) el.hidden = false;
    } else {
      reloadFromServer(slot);
    }
  }

  function cleanup() {
    state.unsubscribers.forEach(function (u) { try { u(); } catch (e) { /* ignore */ } });
    state.unsubscribers = [];
    state.config = null;
    state.dirty = false;
    state.ignoreConfigChangedUntil = 0;
  }

  function bindEvents(slot) {
    if (!global.WXEvents) return;
    state.unsubscribers.push(global.WXEvents.subscribe('config.changed', function () { onConfigChanged(slot); }));
  }

  function render(slot) {
    cleanup(); // router 重新进入时先清掉旧订阅 / 状态
    tokenRevealed = {};
    slot.innerHTML =
      '<div class="card" data-role="config-stale" hidden>' +
        '<div class="card__title">配置已被其他会话更新</div>' +
        '<div class="form-helper">当前表单有未保存的修改,刷新将丢失这些修改。</div>' +
        '<div style="margin-top:var(--s-3)">' +
          '<button type="button" class="btn btn--primary" data-role="config-stale-reload">重新加载</button> ' +
          '<button type="button" class="btn btn--secondary" data-role="config-stale-ignore">忽略</button>' +
        '</div>' +
      '</div>' +

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

      '<div class="card">' +
        '<div class="card__title">数据保留</div>' +
        '<dl class="kv">' +
          '<dt>历史保留天数</dt><dd>' +
            '<input type="number" class="input" id="settingsRetentionDays" min="0" max="365" step="1" value="0">' +
            ' <span class="kv__sub">0 = 永久</span>' +
          '</dd>' +
          '<dt>当前已记录</dt><dd>' +
            '<span id="settingsRecordCount">—</span> <span class="kv__sub">条</span>' +
          '</dd>' +
        '</dl>' +
      '</div>' +

      '<div class="settings-actions">' +
        '<button type="button" class="btn btn--secondary" id="settingsCancelBtn">取消</button>' +
        '<button type="button" class="btn btn--primary" id="settingsSaveBtn">保存配置</button>' +
      '</div>';

    var list = document.getElementById('settingsTokenList');

    list.addEventListener('click', function (e) {
      var btn = e.target.closest('button[data-action]');
      if (!btn) return;
      var idx = Number(btn.getAttribute('data-idx'));
      var action = btn.getAttribute('data-action');
      if (action === 'remove') { removeToken(idx); state.dirty = true; }
      else if (action === 'copy') copyToken(idx);
      else if (action === 'toggle') {
        tokenRevealed[idx] = !tokenRevealed[idx];
        renderTokens();
      } else if (action === 'preset') {
        var days = Number(btn.getAttribute('data-days'));
        setExpiry(idx, presetDate(days));
        state.dirty = true;
      } else if (action === 'preset-permanent') {
        setExpiry(idx, '');
        state.dirty = true;
      }
    });

    list.addEventListener('change', function (e) {
      var input = e.target.closest('input[data-action="expiry"]');
      if (!input) return;
      var idx = Number(input.getAttribute('data-idx'));
      setExpiry(idx, input.value);
      state.dirty = true;
    });

    list.addEventListener('input', function (e) {
      var input = e.target.closest('input[data-action="label"]');
      if (!input) return;
      var idx = Number(input.getAttribute('data-idx'));
      if (idx < 0 || idx >= current.tokens.length) return;
      current.tokens[idx].label = input.value;
      state.dirty = true;
      updateSaveButton();
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
      current.apiBaseUrl = e.currentTarget.value.trim();
      state.dirty = true;
      updateSaveButton();
    });
    var retentionInput = document.getElementById('settingsRetentionDays');
    if (retentionInput) {
      retentionInput.addEventListener('input', function (e) {
        current.historyRetentionDays = Number(e.currentTarget.value) || 0;
        state.dirty = true;
        updateSaveButton();
      });
    }
    document.getElementById('settingsCancelBtn').addEventListener('click', cancel);
    document.getElementById('settingsSaveBtn').addEventListener('click', save);

    var reloadBtn = slot.querySelector('[data-role="config-stale-reload"]');
    if (reloadBtn) reloadBtn.addEventListener('click', function () { reloadFromServer(slot); });
    var ignoreBtn = slot.querySelector('[data-role="config-stale-ignore"]');
    if (ignoreBtn) ignoreBtn.addEventListener('click', function () { hideStaleBar(slot); });

    bindBeforeUnload();
    loadConfig();
    bindEvents(slot);
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.settings = { render: render };
})(window);
