/* Settings page — full implementation.
 * Loads /api/config, renders form, supports add/remove/copy tokens,
 * dirty tracking, save/cancel, beforeunload prompt.
 */

(function (global) {
  'use strict';

  var original = { apiBaseUrl: '', tokens: [] };
  var current  = { apiBaseUrl: '', tokens: [] };
  var tokenRevealed = {};
  var beforeUnloadBound = false;

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
    if (beforeUnloadBound) return;
    beforeUnloadBound = true;
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
    /* Reset per-mount state so reveal/dirty don't leak across visits. */
    tokenRevealed = {};
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
      current.apiBaseUrl = e.currentTarget.value.trim();
      updateSaveButton();
    });
    document.getElementById('settingsCancelBtn').addEventListener('click', cancel);
    document.getElementById('settingsSaveBtn').addEventListener('click', save);

    bindBeforeUnload();
    load();
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.settings = { render: render };
})(window);
