/* History page (解析历史) — admin-only log viewer.
 * Lists request_log rows with filter bar, expandable rows, pagination,
 * and single/batch/full delete. State cleanup on every render() because
 * the router re-renders on every visit.
 */

(function (global) {
  'use strict';

  var DEFAULT_SIZE = 50;

  var state = {
    filter: { range: 'today', kind: 'all', status: 'all', token: 'all', q: '' },
    page: 1,
    size: DEFAULT_SIZE,
    data: null,            // {total, page, size, items}
    expanded: new Set(),
    selected: new Set(),
    abortCtrl: null,
    loadId: 0,
    tokenLabels: [],       // populated from /api/config
  };

  function escapeHtml(s) {
    var div = document.createElement('div');
    div.textContent = s == null ? '' : String(s);
    return div.innerHTML;
  }

  function copyToClipboard(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text).catch(function () { return fallbackCopy(text); });
    }
    return Promise.resolve(fallbackCopy(text));
  }

  function fallbackCopy(text) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'absolute';
    ta.style.left = '-9999px';
    document.body.appendChild(ta);
    ta.select();
    var ok = false;
    try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
    document.body.removeChild(ta);
    return ok;
  }

  function pad2(n) { return n < 10 ? '0' + n : '' + n; }

  function fmtTs(ms) {
    if (!ms) return '—';
    var d = new Date(ms);
    return pad2(d.getHours()) + ':' + pad2(d.getMinutes()) + ':' + pad2(d.getSeconds());
  }

  function fmtDate(ms) {
    var d = new Date(ms);
    return d.getFullYear() + '-' + pad2(d.getMonth() + 1) + '-' + pad2(d.getDate());
  }

  function summarizeRequest(req, kind) {
    if (!req) return '—';
    if (kind === 'url') return req.url || '—';
    if (kind === 'finder') return (req.objectId || '—') + (req.objectNonceId ? ' / ' + req.objectNonceId : '');
    if (kind === 'auth') return req.path || '—';
    return JSON.stringify(req);
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

  function sourceBadge(src) {
    var label = src === 'admin_test' ? 'admin_test' : 'external';
    return '<span class="badge badge--source-' + escapeHtml(src) + '">' + escapeHtml(label) + '</span>';
  }

  /* ---------- skeleton ---------- */

  function renderSkeleton() {
    return '' +
      '<div class="card history-card">' +
        '<div class="card__title">解析历史</div>' +
        '<div class="history-filter">' +
          filterField('range', '时间',
            '<option value="today">今天</option>' +
            '<option value="7d">近 7 天</option>' +
            '<option value="30d">近 30 天</option>' +
            '<option value="all">全部</option>') +
          filterField('kind', '类型',
            '<option value="all">全部</option>' +
            '<option value="url">URL</option>' +
            '<option value="finder">finder</option>' +
            '<option value="auth">鉴权失败</option>') +
          filterField('status', '状态',
            '<option value="all">全部</option>' +
            '<option value="ok">成功</option>' +
            '<option value="err">业务错</option>' +
            '<option value="auth_err">鉴权失败</option>') +
          '<div class="filter-field" data-role="token-field">' +
            '<label class="filter-field__label">Token</label>' +
            '<select class="input" data-role="filter-token"><option value="all">全部</option></select>' +
          '</div>' +
          '<div class="filter-field">' +
            '<label class="filter-field__label">搜索 (URL/objectId)</label>' +
            '<input class="input" data-role="filter-q" placeholder="LIKE 匹配 request 列">' +
          '</div>' +
        '</div>' +
        '<div class="history-filter__actions">' +
          '<button class="btn btn--secondary" data-role="filter-clear">清空过滤</button>' +
        '</div>' +
        '<div class="history-summary">' +
          '<span data-role="summary-text">加载中…</span>' +
          '<span>' +
            '<button class="btn btn--secondary" data-role="batch-delete" disabled>批量删除</button> ' +
          '</span>' +
        '</div>' +
      '</div>' +
      '<div class="card history-card">' +
        '<div data-role="list" class="history-table"></div>' +
        '<div data-role="pagination"></div>' +
      '</div>';
  }

  function filterField(role, label, options) {
    return '<div class="filter-field">' +
             '<label class="filter-field__label">' + escapeHtml(label) + '</label>' +
             '<select class="input" data-role="filter-' + role + '">' + options + '</select>' +
           '</div>';
  }

  /* ---------- event wiring ---------- */

  function wireFilter(slot) {
    var selects = slot.querySelectorAll('[data-role^="filter-"]');
    selects.forEach(function (el) {
      if (el.tagName === 'INPUT') {
        var debounceT = null;
        el.addEventListener('input', function () {
          clearTimeout(debounceT);
          debounceT = setTimeout(function () {
            state.filter.q = el.value;
            state.page = 1;
            load(slot);
          }, 300);
        });
      } else {
        el.addEventListener('change', function () {
          var role = el.getAttribute('data-role').replace('filter-', '');
          state.filter[role] = el.value;
          state.page = 1;
          load(slot);
        });
      }
    });

    var clearBtn = slot.querySelector('[data-role="filter-clear"]');
    clearBtn.addEventListener('click', function () {
      state.filter = { range: 'today', kind: 'all', status: 'all', token: 'all', q: '' };
      state.page = 1;
      syncFilterUI(slot);
      load(slot);
    });

    var batchBtn = slot.querySelector('[data-role="batch-delete"]');
    batchBtn.addEventListener('click', function () { batchDelete(slot); });
  }

  function syncFilterUI(slot) {
    var role;
    for (role in state.filter) {
      if (!Object.prototype.hasOwnProperty.call(state.filter, role)) continue;
      var el = slot.querySelector('[data-role="filter-' + role + '"]');
      if (el) el.value = state.filter[role];
    }
  }

  function populateTokenDropdown(slot) {
    var sel = slot.querySelector('[data-role="filter-token"]');
    if (!sel) return;
    // keep the "全部" option
    while (sel.options.length > 1) sel.remove(1);
    state.tokenLabels.forEach(function (lbl) {
      var opt = document.createElement('option');
      opt.value = lbl;
      opt.textContent = lbl;
      sel.appendChild(opt);
    });
  }

  /* ---------- list rendering ---------- */

  function renderList(slot) {
    var list = slot.querySelector('[data-role="list"]');
    var pag = slot.querySelector('[data-role="pagination"]');
    var sum = slot.querySelector('[data-role="summary-text"]');
    var data = state.data;
    if (!data) { list.innerHTML = ''; pag.innerHTML = ''; sum.textContent = '加载中…'; return; }

    sum.textContent = '共 ' + data.total + ' 条 · 第 ' + data.page + '/' + Math.max(1, Math.ceil(data.total / data.size)) + ' 页';

    if (data.total === 0) {
      list.innerHTML = '<div class="empty">' +
        '<div class="empty__icon">…</div>' +
        '<div class="empty__title">' + (hasActiveFilter() ? '无匹配记录' : '暂无请求记录') + '</div>' +
        '<div class="empty__desc">' + (hasActiveFilter() ? '试试调整过滤条件' : '试试在解析测试页发一次请求') + '</div>' +
      '</div>';
      pag.innerHTML = '';
      return;
    }

    var rows = data.items.map(function (r) { return renderRow(r); }).join('');
    list.innerHTML = rows;

    list.querySelectorAll('.history-row__check').forEach(function (cb) {
      cb.addEventListener('click', function (e) { e.stopPropagation(); toggleSelect(parseInt(cb.getAttribute('data-id'), 10), cb.checked); refreshBatchBtn(slot); });
    });
    list.querySelectorAll('.history-row__menu').forEach(function (btn) {
      btn.addEventListener('click', function (e) { e.stopPropagation(); oneDelete(slot, parseInt(btn.getAttribute('data-id'), 10)); });
    });
    list.querySelectorAll('.history-row').forEach(function (el) {
      el.addEventListener('click', function () { toggleExpand(parseInt(el.getAttribute('data-id'), 10), slot); });
    });
    list.querySelectorAll('button[data-copy]').forEach(function (btn) {
      btn.addEventListener('click', function (e) {
        e.stopPropagation();
        var text = btn.getAttribute('data-copy') || '';
        copyToClipboard(text).then(function (ok) {
          if (global.WXToast) global.WXToast(ok !== false ? '已复制' : '复制失败', ok !== false ? 'success' : 'error');
        });
      });
    });

    pag.innerHTML = renderPagination(data.total, data.page, data.size);
    pag.querySelectorAll('[data-page]').forEach(function (b) {
      b.addEventListener('click', function () {
        var p = parseInt(b.getAttribute('data-page'), 10);
        if (!isNaN(p) && p !== state.page) {
          state.page = p;
          load(slot);
        }
      });
    });
  }

  function hasActiveFilter() {
    var f = state.filter;
    return f.range !== 'today' || f.kind !== 'all' || f.status !== 'all' || f.token !== 'all' || !!f.q;
  }

  function renderRow(r) {
    var expanded = state.expanded.has(r.id);
    var checked = state.selected.has(r.id);
    var summary = summarizeRequest(r.request, r.kind);
    // Status cell: badge always, plus a tiny inline msg snippet (truncated)
    // for non-success rows. Hover the cell to see the full message via title.
    var statusInner = statusBadge(r.status);
    if (r.status !== 0 && r.msg) {
      var short = r.msg.length > 40 ? r.msg.slice(0, 40) + '…' : r.msg;
      statusInner += ' <span class="kv__sub" title="' + escapeHtml(r.msg) + '">' + escapeHtml(short) + '</span>';
    }
    var html = '<div class="history-row" data-id="' + r.id + '">' +
      '<div class="history-row__check">' +
        '<input type="checkbox" data-id="' + r.id + '" ' + (checked ? 'checked' : '') + '>' +
      '</div>' +
      '<div class="history-row__cell history-row__cell--ts">' + escapeHtml(fmtTs(r.ts)) + ' <span style="color:var(--text-faint)">' + escapeHtml(fmtDate(r.ts)) + '</span></div>' +
      '<div class="history-row__cell history-row__cell--kind">' + kindBadge(r.kind) + '</div>' +
      '<div class="history-row__cell history-row__cell--token">' + escapeHtml(r.token_label || '(无)') + '</div>' +
      '<div class="history-row__cell history-row__cell--status" title="' + escapeHtml(r.msg || '') + '">' + statusInner + '</div>' +
      '<div class="history-row__cell history-row__cell--latency">' + escapeHtml(String(r.latency_ms)) + 'ms</div>' +
      '<div class="history-row__cell history-row__cell--source">' + sourceBadge(r.source) + '</div>' +
      '<div class="history-row__cell history-row__cell--summary" title="' + escapeHtml(JSON.stringify(r.request)) + '">' + escapeHtml(summary) + '</div>' +
      '<div class="history-row__cell history-row__cell--menu">' +
        '<button class="copy-btn history-row__menu" data-id="' + r.id + '" title="删除">⋯</button>' +
      '</div>' +
    '</div>';
    if (expanded) html += renderDetail(r);
    return html;
  }

  function renderDetail(r) {
    var sections = [];
    sections.push('<div class="history-detail__section-title">入参</div>');
    sections.push('<pre>' + escapeHtml(JSON.stringify(r.request, null, 2)) + '</pre>');
    if (r.msg) {
      sections.push('<div class="history-detail__section-title">业务消息</div>');
      sections.push('<div class="result-msg">' + escapeHtml(r.msg) + '</div>');
    }
    if (r.result) {
      sections.push('<div class="history-detail__section-title">解析结果</div>');
      var res = r.result;
      var rows = [];
      ['author', 'title', 'cover_url', 'video_url', 'decode_key', 'media_type'].forEach(function (k) {
        if (res[k] != null && res[k] !== '') {
          var display = (k === 'media_type') ? res[k] + ' (' + mediaTypeName(res[k]) + ')' : res[k];
          rows.push('<div class="field"><div class="field-label">' + escapeHtml(k) + '</div>' +
                    '<div class="field-value field-value--text">' + escapeHtml(String(display)) + '</div>' +
                    '<button class="copy-btn" data-copy="' + escapeHtml(String(display)) + '">复制</button></div>');
        }
      });
      if (res.cover_url) {
        // Promote cover to an <img> above its text row for thumbnail preview.
        rows.unshift('<div class="field"><div class="field-label">cover</div>' +
                     '<div class="field-value"><img class="result-cover" src="' + escapeHtml(res.cover_url) + '" alt="cover" onerror="this.style.display=\'none\'"></div></div>');
      }
      sections.push(rows.join(''));
      sections.push('<div class="history-detail__section-title">原始 JSON</div>');
      sections.push('<pre>' + escapeHtml(JSON.stringify(r.result, null, 2)) + '</pre>');
    }
    return '<div class="history-detail">' + sections.join('') +
      '<div style="text-align:right"><button class="btn btn--secondary" data-role="close-detail" data-id="' + r.id + '">关闭</button></div>' +
      '</div>';
  }

  function mediaTypeName(n) {
    if (n === 1) return '图片';
    if (n === 2) return '视频';
    if (n === 4) return '文章';
    return '未知';
  }

  function renderPagination(total, page, size) {
    var totalPages = Math.max(1, Math.ceil(total / size));
    var btns = [];
    var prevDisabled = page <= 1 ? 'disabled' : '';
    var nextDisabled = page >= totalPages ? 'disabled' : '';
    btns.push('<button class="history-pagination__btn" data-page="' + (page - 1) + '" ' + prevDisabled + '>‹</button>');
    var startP = Math.max(1, page - 2);
    var endP = Math.min(totalPages, page + 2);
    if (startP > 1) {
      btns.push('<button class="history-pagination__btn" data-page="1">1</button>');
      if (startP > 2) btns.push('<span class="history-pagination__btn" style="cursor:default">…</span>');
    }
    for (var p = startP; p <= endP; p++) {
      btns.push('<button class="history-pagination__btn ' + (p === page ? 'history-pagination__btn--active' : '') + '" data-page="' + p + '">' + p + '</button>');
    }
    if (endP < totalPages) {
      if (endP < totalPages - 1) btns.push('<span class="history-pagination__btn" style="cursor:default">…</span>');
      btns.push('<button class="history-pagination__btn" data-page="' + totalPages + '">' + totalPages + '</button>');
    }
    btns.push('<button class="history-pagination__btn" data-page="' + (page + 1) + '" ' + nextDisabled + '>›</button>');
    btns.push('<button class="btn btn--secondary" data-role="clear-all" style="margin-left:var(--s-3)">清空</button>');
    return '<div class="history-pagination">' +
             '<div class="history-pagination__pages">' + btns.join('') + '</div>' +
           '</div>';
  }

  /* ---------- toggle helpers ---------- */

  function toggleExpand(id, slot) {
    if (state.expanded.has(id)) state.expanded.delete(id); else state.expanded.add(id);
    renderList(slot);
  }

  function toggleSelect(id, on) {
    if (on) state.selected.add(id); else state.selected.delete(id);
  }

  function refreshBatchBtn(slot) {
    var btn = slot.querySelector('[data-role="batch-delete"]');
    if (!btn) return;
    var n = state.selected.size;
    btn.disabled = n === 0;
    btn.textContent = n > 0 ? '批量删除 (' + n + ')' : '批量删除';
  }

  /* ---------- delete ops ---------- */

  function oneDelete(slot, id) {
    if (!global.confirm('确定删除此条记录?')) return;
    callDelete(slot, '?id=' + id).then(function () {
      state.expanded.delete(id);
      state.selected.delete(id);
      load(slot);
    });
  }

  function batchDelete(slot) {
    var ids = Array.from(state.selected);
    if (ids.length === 0) return;
    if (!global.confirm('确定删除 ' + ids.length + ' 条记录?')) return;
    callDelete(slot, '?id=' + ids.join(',')).then(function () {
      state.selected.clear();
      load(slot);
    });
  }

  function clearAll(slot) {
    if (!state.data || state.data.total === 0) return;
    if (!global.confirm('确定删除全部 ' + state.data.total + ' 条历史?此操作不可撤销')) return;
    callDelete(slot, '?all=1').then(function () {
      state.selected.clear();
      state.expanded.clear();
      load(slot);
    });
  }

  function callDelete(slot, query) {
    return global.WXApi.authFetch('/api/history' + query, { method: 'DELETE' })
      .then(function (res) { return res.json(); })
      .then(function (body) {
        if (body && body.code === 0) {
          if (global.WXToast) global.WXToast('已删除 ' + (body.data ? body.data.deleted : 0) + ' 条', 'success');
        } else {
          if (global.WXToast) global.WXToast('删除失败: ' + (body && body.msg || '未知错误'), 'error');
          throw new Error(body && body.msg || 'delete failed');
        }
      })
      .catch(function (e) {
        if (e && e.isAuth) return; // already handled by api.js
        if (global.WXToast) global.WXToast('删除失败: ' + e.message, 'error');
      });
  }

  /* ---------- load ---------- */

  function buildQuery() {
    var qs = [];
    qs.push('range=' + encodeURIComponent(state.filter.range));
    qs.push('kind=' + encodeURIComponent(state.filter.kind));
    qs.push('status=' + encodeURIComponent(state.filter.status));
    qs.push('token=' + encodeURIComponent(state.filter.token));
    if (state.filter.q) qs.push('q=' + encodeURIComponent(state.filter.q));
    qs.push('page=' + state.page);
    qs.push('size=' + state.size);
    return '/api/history?' + qs.join('&');
  }

  function load(slot) {
    if (state.abortCtrl) state.abortCtrl.abort();
    state.abortCtrl = new AbortController();
    var url = buildQuery();
    var prevData = state.data;
    state.data = null;
    state.loadId++;
    var myLoadId = state.loadId;
    renderList(slot);

    global.WXApi.authJson(url)
      .then(function (res) {
        if (myLoadId !== state.loadId) return;
        state.abortCtrl = null;
        if (res.data && res.data.code === 0 && res.data.data) {
          state.data = res.data.data;
          renderList(slot);
        } else {
          state.data = prevData;
          renderList(slot);
          renderError(slot, (res.data && res.data.msg) || '未知错误');
        }
      })
      .catch(function (e) {
        if (myLoadId !== state.loadId) return;
        if (e && e.isAuth) return;
        state.data = prevData;
        renderList(slot);
        renderError(slot, e.message || '网络错误');
      });
  }

  function renderError(slot, msg) {
    var list = slot.querySelector('[data-role="list"]');
    if (list) {
      list.innerHTML = '<div class="result-msg">' + escapeHtml('加载失败: ' + msg) + ' <button class="btn btn--secondary" data-role="retry">重试</button></div>';
      var btn = list.querySelector('[data-role="retry"]');
      if (btn) btn.addEventListener('click', function () { load(slot); });
    }
  }

  /* ---------- config + bootstrap ---------- */

  function loadTokenLabels() {
    return global.WXApi.authJson('/api/config')
      .then(function (res) {
        if (res.data && res.data.code === 0 && res.data.data) {
          state.tokenLabels = (res.data.data.tokens || []).map(function (t) { return t.label; }).filter(Boolean);
        }
      })
      .catch(function () { /* non-fatal */ });
  }

  /* ---------- render ---------- */

  function render(slot) {
    if (state.abortCtrl) { state.abortCtrl.abort(); state.abortCtrl = null; }
    state.data = null;
    state.expanded.clear();
    state.selected.clear();
    slot.innerHTML = renderSkeleton();
    syncFilterUI(slot);
    populateTokenDropdown(slot);
    wireFilter(slot);
    var pag = slot.querySelector('[data-role="pagination"]');
    if (pag) pag.addEventListener('click', function (e) {
      if (e.target.matches('[data-role="clear-all"]')) clearAll(slot);
    });
    loadTokenLabels().then(function () { populateTokenDropdown(slot); });
    load(slot);
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.history = { render: render };
})(window);
