/* Dashboard page — overview with stats, per-token breakdown, custom range,
 * and trend/heatmap charts.
 *
 *  - Top 6 stat cards: Token 数 / 今日成功 / 本周成功 / 本月成功 / 总计 / 平均耗时.
 *    When a custom range is applied, a 7th "自定义" card is appended.
 *  - Per-token breakdown table: rows = current cfg.Tokens, columns = 4 fixed
 *    intervals. When custom range is active, a "自定义" column is appended.
 *  - Charts card: SVG trend line + CSS Grid heatmap. Top-row token selector
 *    filters both charts; manual refresh button re-pulls /api/stats/daily.
 *  - "最近请求" card is preserved from the previous version.
 *
 * Data flow:
 *  - system.snapshot (WS, 2s) drives stat cards + breakdown table.
 *  - config.changed (WS) reloads the token list and re-renders the table
 *    and the chart's token <select>.
 *  - log.new (WS) is debounced 3s and triggers a chart refresh.
 *  - /api/stats       (REST) — custom range popover, on Apply.
 *  - /api/stats/daily (REST) — chart data, on load / token change / manual refresh.
 */

(function (global) {
  'use strict';

  var RECENT_SIZE = 10;
  var CHART_DEBOUNCE_MS = 3000;

  var state = {
    stats: null,                  // latest ReqStats from system.snapshot
    tokens: [],                   // latest cfg.Tokens
    customRange: null,            // { start: 'yyyy-MM-dd', end: 'yyyy-MM-dd' } or null
    customStats: null,            // { success_total, by_token: [{label,count}] } from /api/stats
    chartToken: '',               // '' = all; else token label
    daily: null,                  // { days, token, series: [{date,count}] }
    chartDebounceTimer: null,
    unsubscribers: [],
  };
  var recent = [];                // last 10 rows, used by loadRecent + log.new unshift

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

  function todayDateStr() {
    var d = new Date();
    return d.getFullYear() + '-' + pad2(d.getMonth() + 1) + '-' + pad2(d.getDate());
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

  function renderRecent(slot) {
    var body = slot.querySelector('[data-role="recent-body"]');
    if (!body) return;
    body.innerHTML = renderRecentRows(recent);
  }

  async function loadRecent(slot) {
    var body = slot.querySelector('[data-role="recent-body"]');
    if (!body) return;
    try {
      var res = await global.WXApi.authJson('/api/history?range=all&page=1&size=' + RECENT_SIZE);
      if (res.data && res.data.code === 0 && res.data.data) {
        var fetched = res.data.data.items || [];
        var fetchedIds = new Set(fetched.map(function (it) { return it.id; }));
        var fresher = recent.filter(function (it) { return !fetchedIds.has(it.id); });
        recent = fresher.concat(fetched).slice(0, RECENT_SIZE);
        renderRecent(slot);
      } else {
        body.innerHTML = '<div class="result-msg">加载失败: ' + escapeHtml((res.data && res.data.msg) || '未知错误') + '</div>';
      }
    } catch (e) {
      if (e && e.isAuth) return;
      body.innerHTML = '<div class="result-msg">加载失败: ' + escapeHtml(e.message || '网络错误') + '</div>';
    }
  }

  /* ---------- stat grid + custom range card ---------- */

  function statNumber(s, key) {
    if (!s || s[key] == null) return '—';
    return Number(s[key]).toLocaleString();
  }

  function renderStatGrid() {
    var grid = document.querySelector('[data-role="stat-grid"]');
    if (!grid) return;
    var s = state.stats;
    var retention = (s && s.retention_days) ? s.retention_days : 0;
    var customNum = state.customStats ? Number(state.customStats.success_total).toLocaleString() : null;

    var cards = [
      { label: '配置的 Token 数', value: state.tokens.length, note: '从 /api/config 实时读取' },
      { label: '今日成功', value: statNumber(s, 'success_today'), note: 'status=0 调用数' },
      { label: '本周成功', value: statNumber(s, 'success_week'),  note: '本周一 00:00 起' },
      { label: '本月成功', value: statNumber(s, 'success_month'), note: '本月 1 日 00:00 起' },
      { label: '总计成功', value: statNumber(s, 'success_total'), note: '近 ' + retention + ' 天' },
      { label: '平均耗时', value: (s && s.avg_latency_today_ms) ? (s.avg_latency_today_ms + 'ms') : '—', note: '今日成功调用' }
    ];
    if (state.customStats) {
      cards.push({
        label: '自定义成功',
        value: customNum,
        note: state.customRange.start + ' ~ ' + state.customRange.end,
        custom: true
      });
    }
    grid.innerHTML = cards.map(function (c) {
      return '<div class="stat' + (c.custom ? ' stat--custom' : '') + '">' +
        '<div class="stat__label">' + escapeHtml(c.label) + '</div>' +
        '<div class="stat__value">' + c.value + '</div>' +
        '<div class="stat__note">' + escapeHtml(c.note) + '</div>' +
      '</div>';
    }).join('');
  }

  function renderCustomRangeButton() {
    var wrap = document.querySelector('[data-role="custom-range-wrap"]');
    if (!wrap) return;
    var btn = wrap.querySelector('.custom-range-btn');
    if (!btn) return;
    if (state.customRange) {
      btn.classList.add('is-active');
      btn.textContent = '自定义: ' + state.customRange.start + ' ~ ' + state.customRange.end + ' ✕';
    } else {
      btn.classList.remove('is-active');
      btn.textContent = '自定义区间';
    }
  }

  function openCustomRangePopover() {
    var pop = document.querySelector('[data-role="custom-range-popover"]');
    if (!pop) return;
    var sIn = pop.querySelector('[data-role="custom-start"]');
    var eIn = pop.querySelector('[data-role="custom-end"]');
    var retention = (state.stats && state.stats.retention_days) ? state.stats.retention_days : 60;
    var minDay = new Date();
    minDay.setDate(minDay.getDate() - (retention - 1));
    var minStr = minDay.getFullYear() + '-' + pad2(minDay.getMonth() + 1) + '-' + pad2(minDay.getDate());
    if (sIn) { sIn.min = minStr; sIn.max = todayDateStr(); sIn.value = state.customRange ? state.customRange.start : minStr; }
    if (eIn) { eIn.min = minStr; eIn.max = todayDateStr(); eIn.value = state.customRange ? state.customRange.end : todayDateStr(); }
    pop.hidden = false;
  }

  function closeCustomRangePopover() {
    var pop = document.querySelector('[data-role="custom-range-popover"]');
    if (pop) pop.hidden = true;
  }

  async function applyCustomRange() {
    var pop = document.querySelector('[data-role="custom-range-popover"]');
    if (!pop) return;
    var sIn = pop.querySelector('[data-role="custom-start"]');
    var eIn = pop.querySelector('[data-role="custom-end"]');
    var start = sIn && sIn.value;
    var end = eIn && eIn.value;
    if (!start || !end) {
      if (global.WXToast) global.WXToast('请选择起止日期', 'error');
      return;
    }
    if (end < start) {
      if (global.WXToast) global.WXToast('结束日期不能早于开始日期', 'error');
      return;
    }
    try {
      var res = await global.WXApi.authJson('/api/stats?start=' + encodeURIComponent(start) + '&end=' + encodeURIComponent(end));
      if (res.data && res.data.code === 0 && res.data.data) {
        state.customRange = { start: start, end: end };
        state.customStats = res.data.data;
        renderStatGrid();
        renderCustomRangeButton();
        renderTokenBreakdownCard();
        closeCustomRangePopover();
      } else {
        if (global.WXToast) global.WXToast((res.data && res.data.msg) || '查询失败', 'error');
      }
    } catch (e) {
      if (e && e.isAuth) return;
      if (global.WXToast) global.WXToast(e.message || '网络错误', 'error');
    }
  }

  function clearCustomRange() {
    state.customRange = null;
    state.customStats = null;
    renderStatGrid();
    renderCustomRangeButton();
    renderTokenBreakdownCard();
    closeCustomRangePopover();
  }

  /* ---------- per-token breakdown table ---------- */

  function renderTokenBreakdownCard() {
    var body = document.querySelector('[data-role="breakdown-body"]');
    if (!body) return;
    var s = state.stats;
    if (!s || !Array.isArray(s.by_token) || s.by_token.length === 0) {
      body.innerHTML = '<div class="empty"><div class="empty__title">尚未配置 token</div><div class="empty__desc">在配置页添加 token 后此处会显示每个 token 的调用明细</div></div>';
      return;
    }
    var byLabel = {};
    (s.by_token || []).forEach(function (t) { byLabel[t.label] = t; });
    var customMap = {};
    if (state.customStats && Array.isArray(state.customStats.by_token)) {
      state.customStats.by_token.forEach(function (t) { customMap[t.label] = t.count; });
    }
    var hasCustom = !!state.customStats;

    var head = '<tr>' +
      '<th>Token</th>' +
      '<th class="num">今日</th>' +
      '<th class="num">本周</th>' +
      '<th class="num">本月</th>' +
      '<th class="num">总计</th>' +
      (hasCustom ? '<th class="num col-custom">自定义</th>' : '') +
      '</tr>';

    var bodyRows = (s.by_token || []).map(function (t) {
      return '<tr>' +
        '<td title="' + escapeHtml(t.label) + '">' + escapeHtml(t.label) + '</td>' +
        '<td class="num">' + Number(t.today || 0).toLocaleString() + '</td>' +
        '<td class="num">' + Number(t.week || 0).toLocaleString() + '</td>' +
        '<td class="num">' + Number(t.month || 0).toLocaleString() + '</td>' +
        '<td class="num">' + Number(t.total || 0).toLocaleString() + '</td>' +
        (hasCustom ? '<td class="num col-custom">' + Number(customMap[t.label] || 0).toLocaleString() + '</td>' : '') +
        '</tr>';
    }).join('');

    body.innerHTML = '<table class="token-breakdown-table">' + head + bodyRows + '</table>';
  }

  /* ---------- charts: trend + heatmap ---------- */

  function renderChartControls() {
    var sel = document.querySelector('[data-role="chart-token"]');
    if (!sel) return;
    var opts = ['<option value="">全部</option>'];
    state.tokens.forEach(function (t) {
      var sel2 = (state.chartToken === t.label) ? ' selected' : '';
      opts.push('<option value="' + escapeHtml(t.label) + '"' + sel2 + '>' + escapeHtml(t.label) + '</option>');
    });
    sel.innerHTML = opts.join('');
  }

  function renderChartsCard() {
    var trendHost = document.querySelector('[data-role="trend-chart"]');
    var heatHost = document.querySelector('[data-role="heatmap"]');
    if (!trendHost || !heatHost) return;

    var series = (state.daily && Array.isArray(state.daily.series)) ? state.daily.series : [];
    var max = 0;
    series.forEach(function (d) { if (d.count > max) max = d.count; });
    if (max === 0) max = 1;

    trendHost.innerHTML = renderTrendSvg(series, max);
    heatHost.innerHTML = renderHeatmapGrid(series, max);

    // bind hover for tooltip
    bindChartTooltips();
  }

  function renderTrendSvg(series, maxValue) {
    if (!series || series.length === 0) {
      return '<svg class="trend-chart" viewBox="0 0 600 200" preserveAspectRatio="none">' +
        '<text x="300" y="100" text-anchor="middle" class="trend-chart__label">暂无数据</text>' +
      '</svg>';
    }
    var w = 600, h = 200, padL = 30, padR = 10, padT = 10, padB = 20;
    var innerW = w - padL - padR;
    var innerH = h - padT - padB;
    var n = series.length;
    var xStep = n > 1 ? innerW / (n - 1) : 0;

    var pts = series.map(function (d, i) {
      var x = padL + (n > 1 ? i * xStep : innerW / 2);
      var y = padT + innerH - (d.count / maxValue) * innerH;
      return { x: x, y: y, d: d };
    });

    var linePts = pts.map(function (p) { return p.x.toFixed(1) + ',' + p.y.toFixed(1); }).join(' ');
    // Area path: line then down to baseline
    var areaD = 'M ' + pts[0].x.toFixed(1) + ',' + (padT + innerH).toFixed(1) +
      ' L ' + linePts.replace(/ /g, ' L ') +
      ' L ' + pts[pts.length - 1].x.toFixed(1) + ',' + (padT + innerH).toFixed(1) + ' Z';

    // X ticks: 5 evenly spaced indices (first, last, 3 in between)
    var tickIdx = [];
    if (n <= 5) {
      for (var i = 0; i < n; i++) tickIdx.push(i);
    } else {
      for (var k = 0; k < 5; k++) tickIdx.push(Math.round((k * (n - 1)) / 4));
    }
    var xLabels = tickIdx.map(function (i) {
      var p = pts[i];
      var d = series[i].date.slice(5); // MM-DD
      return '<text x="' + p.x.toFixed(1) + '" y="' + (h - 4) + '" text-anchor="middle" class="trend-chart__label">' + escapeHtml(d) + '</text>';
    }).join('');

    // Y labels: 0, max/2, max
    var yLabels = [
      { v: 0, y: padT + innerH },
      { v: Math.round(maxValue / 2), y: padT + innerH / 2 },
      { v: maxValue, y: padT }
    ].map(function (yl) {
      return '<text x="' + (padL - 4) + '" y="' + (yl.y + 3) + '" text-anchor="end" class="trend-chart__label">' + yl.v + '</text>';
    }).join('');

    var dots = pts.map(function (p, i) {
      return '<circle class="trend-chart__dot" data-idx="' + i + '" cx="' + p.x.toFixed(1) + '" cy="' + p.y.toFixed(1) + '" r="3"></circle>';
    }).join('');

    return '<svg class="trend-chart" viewBox="0 0 ' + w + ' ' + h + '" preserveAspectRatio="none">' +
      '<line class="trend-chart__axis" x1="' + padL + '" y1="' + (padT + innerH) + '" x2="' + (w - padR) + '" y2="' + (padT + innerH) + '"></line>' +
      '<path class="trend-chart__area" d="' + areaD + '"></path>' +
      '<polyline class="trend-chart__line" points="' + linePts + '"></polyline>' +
      yLabels + xLabels + dots +
    '</svg>';
  }

  function renderHeatmapGrid(series, maxValue) {
    if (!series || series.length === 0) {
      return '<div class="empty" style="padding:var(--s-3)"><div class="empty__title" style="font-size:var(--t-sm)">暂无数据</div></div>';
    }
    return series.map(function (d) {
      var level = 0;
      if (d.count > 0 && maxValue > 0) {
        var pct = d.count / maxValue;
        if (pct > 0.75) level = 4;
        else if (pct > 0.50) level = 3;
        else if (pct > 0.25) level = 2;
        else level = 1;
      }
      return '<div class="heatmap__cell" data-level="' + level + '" data-date="' + escapeHtml(d.date) + '" data-count="' + d.count + '"></div>';
    }).join('');
  }

  function ensureTooltip() {
    var t = document.getElementById('chartTooltip');
    if (!t) {
      t = document.createElement('div');
      t.id = 'chartTooltip';
      t.className = 'chart-tooltip';
      t.hidden = true;
      document.body.appendChild(t);
    }
    return t;
  }

  function bindChartTooltips() {
    var tooltip = ensureTooltip();
    function show(html, x, y) {
      tooltip.innerHTML = html;
      tooltip.hidden = false;
      // position so the tooltip is above-right of the cursor, clamped to viewport
      var tw = tooltip.offsetWidth;
      var th = tooltip.offsetHeight;
      var nx = x + 12;
      var ny = y - th - 12;
      if (nx + tw > global.innerWidth - 8) nx = global.innerWidth - 8 - tw;
      if (ny < 8) ny = y + 12;
      tooltip.style.left = nx + 'px';
      tooltip.style.top = ny + 'px';
    }
    function hide() { tooltip.hidden = true; }
    document.querySelectorAll('.trend-chart__dot').forEach(function (dot) {
      dot.addEventListener('mouseenter', function (e) {
        var idx = Number(dot.getAttribute('data-idx'));
        if (!state.daily || !state.daily.series[idx]) return;
        var d = state.daily.series[idx];
        show(escapeHtml(d.date) + ' · ' + d.count + ' 次', e.clientX, e.clientY);
        dot.classList.add('is-active');
      });
      dot.addEventListener('mousemove', function (e) {
        var idx = Number(dot.getAttribute('data-idx'));
        if (!state.daily || !state.daily.series[idx]) return;
        var d = state.daily.series[idx];
        show(escapeHtml(d.date) + ' · ' + d.count + ' 次', e.clientX, e.clientY);
      });
      dot.addEventListener('mouseleave', function () {
        hide();
        dot.classList.remove('is-active');
      });
    });
    document.querySelectorAll('.heatmap__cell').forEach(function (cell) {
      cell.addEventListener('mouseenter', function (e) {
        var date = cell.getAttribute('data-date') || '';
        var count = cell.getAttribute('data-count') || '0';
        show(escapeHtml(date) + ' · ' + count + ' 次', e.clientX, e.clientY);
      });
      cell.addEventListener('mousemove', function (e) {
        var date = cell.getAttribute('data-date') || '';
        var count = cell.getAttribute('data-count') || '0';
        show(escapeHtml(date) + ' · ' + count + ' 次', e.clientX, e.clientY);
      });
      cell.addEventListener('mouseleave', hide);
    });
  }

  async function loadChartData() {
    try {
      var q = '/api/stats/daily?token=' + encodeURIComponent(state.chartToken);
      var res = await global.WXApi.authJson(q);
      if (res.data && res.data.code === 0 && res.data.data) {
        state.daily = res.data.data;
        renderChartsCard();
      }
    } catch (e) {
      if (e && e.isAuth) return;
      // non-fatal; leave the previous chart in place
    }
  }

  function scheduleChartRefresh() {
    if (state.chartDebounceTimer) clearTimeout(state.chartDebounceTimer);
    state.chartDebounceTimer = setTimeout(function () {
      state.chartDebounceTimer = null;
      loadChartData();
    }, CHART_DEBOUNCE_MS);
  }

  /* ---------- token list + config reload ---------- */

  async function loadTokensAndCount() {
    try {
      var res = await global.WXApi.authJson('/api/config');
      if (res.data && res.data.code === 0 && res.data.data) {
        state.tokens = res.data.data.tokens || [];
        renderStatGrid();
        renderChartControls();
        renderTokenBreakdownCard();
      }
    } catch (e) { /* leave as-is */ }
  }

  /* ---------- subscriptions ---------- */

  function bindEvents() {
    if (!global.WXEvents) return;
    state.unsubscribers.push(global.WXEvents.subscribe('system.snapshot', function (frame) {
      if (frame && frame.stats) {
        state.stats = frame.stats;
        renderStatGrid();
        renderTokenBreakdownCard();
      }
    }));
    state.unsubscribers.push(global.WXEvents.subscribe('config.changed', function () {
      loadTokensAndCount();
    }));
    state.unsubscribers.push(global.WXEvents.subscribe('log.new', function (frame) {
      if (!frame || !frame.log) return;
      recent.unshift(frame.log);
      if (recent.length > RECENT_SIZE) recent.length = RECENT_SIZE;
      renderRecent(document.getElementById('pageContent'));
      scheduleChartRefresh();
    }));
  }

  var dismissPopover = null;

  function cleanup() {
    state.unsubscribers.forEach(function (u) { try { u(); } catch (e) { /* ignore */ } });
    state.unsubscribers = [];
    if (state.chartDebounceTimer) { clearTimeout(state.chartDebounceTimer); state.chartDebounceTimer = null; }
    if (dismissPopover) {
      document.removeEventListener('click', dismissPopover);
      dismissPopover = null;
    }
  }

  /* ---------- boot ---------- */

  function render(slot) {
    cleanup();
    slot.innerHTML =
      '<div class="section-title">概览</div>' +

      '<div class="stat-grid" data-role="stat-grid">' +
        '<div class="stat"><div class="stat__label">加载中…</div></div>' +
      '</div>' +

      '<div class="section-title">' +
        '<span>Token 调用明细</span>' +
        '<span class="section-title__actions">' +
          '<span class="custom-range-wrap" data-role="custom-range-wrap">' +
            '<button type="button" class="custom-range-btn" data-role="custom-range-btn">自定义区间</button>' +
            '<div class="custom-range-popover" data-role="custom-range-popover" hidden>' +
              '<div class="custom-range-popover__row"><label>起</label>' +
                '<input type="date" class="input" data-role="custom-start"></div>' +
              '<div class="custom-range-popover__row"><label>止</label>' +
                '<input type="date" class="input" data-role="custom-end"></div>' +
              '<div class="custom-range-popover__actions">' +
                '<button type="button" class="btn btn--secondary btn--sm" data-role="custom-clear">清空</button>' +
                '<button type="button" class="btn btn--primary btn--sm" data-role="custom-apply">应用</button>' +
              '</div>' +
            '</div>' +
          '</span>' +
        '</span>' +
      '</div>' +
      '<div class="card" data-role="breakdown-card">' +
        '<div data-role="breakdown-body">' +
          '<div class="result-msg">加载中…</div>' +
        '</div>' +
      '</div>' +

      '<div class="section-title">调用趋势与热力</div>' +
      '<div class="card charts-card" data-role="charts-card">' +
        '<div class="charts-card__head">' +
          '<div class="charts-card__title">近 ' + ((state.stats && state.stats.retention_days) || 60) + ' 天</div>' +
          '<div class="charts-card__controls">' +
            '<select data-role="chart-token" aria-label="选择 token"></select>' +
            '<button type="button" class="btn btn--secondary btn--sm" data-role="chart-refresh">刷新</button>' +
          '</div>' +
        '</div>' +
        '<div data-role="trend-chart"></div>' +
        '<div class="heatmap" data-role="heatmap"></div>' +
        '<div class="heatmap__legend">' +
          '<span>少</span>' +
          '<div class="heatmap__legend-cells">' +
            '<div class="heatmap__legend-cell" style="background:var(--surface-2)"></div>' +
            '<div class="heatmap__legend-cell" style="background:color-mix(in srgb, var(--primary) 25%, var(--surface-2))"></div>' +
            '<div class="heatmap__legend-cell" style="background:color-mix(in srgb, var(--primary) 50%, var(--surface-2))"></div>' +
            '<div class="heatmap__legend-cell" style="background:color-mix(in srgb, var(--primary) 75%, var(--surface-2))"></div>' +
            '<div class="heatmap__legend-cell" style="background:var(--primary)"></div>' +
          '</div>' +
          '<span>多</span>' +
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

    // custom range popover handlers
    var rangeBtn = slot.querySelector('[data-role="custom-range-btn"]');
    if (rangeBtn) rangeBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      if (state.customRange) {
        clearCustomRange();
        return;
      }
      var pop = slot.querySelector('[data-role="custom-range-popover"]');
      if (!pop) return;
      if (pop.hidden) openCustomRangePopover(); else closeCustomRangePopover();
    });
    var applyBtn = slot.querySelector('[data-role="custom-apply"]');
    if (applyBtn) applyBtn.addEventListener('click', applyCustomRange);
    var clearBtn = slot.querySelector('[data-role="custom-clear"]');
    if (clearBtn) clearBtn.addEventListener('click', clearCustomRange);
    dismissPopover = function (e) {
      var pop = slot.querySelector('[data-role="custom-range-popover"]');
      var wrap = slot.querySelector('[data-role="custom-range-wrap"]');
      if (!pop || pop.hidden) return;
      if (wrap && wrap.contains(e.target)) return;
      pop.hidden = true;
    };
    document.addEventListener('click', dismissPopover);

    // chart controls
    var chartSel = slot.querySelector('[data-role="chart-token"]');
    if (chartSel) chartSel.addEventListener('change', function (e) {
      state.chartToken = e.currentTarget.value || '';
      loadChartData();
    });
    var chartRefresh = slot.querySelector('[data-role="chart-refresh"]');
    if (chartRefresh) chartRefresh.addEventListener('click', loadChartData);

    // recent handlers
    var refresh = slot.querySelector('[data-role="recent-refresh"]');
    if (refresh) refresh.addEventListener('click', function () { loadRecent(slot); });
    var jump = slot.querySelector('a[data-route="/history"]');
    if (jump) jump.addEventListener('click', function (e) {
      e.preventDefault();
      if (global.WXRouter && global.WXRouter.navigate) global.WXRouter.navigate('/history');
    });

    // boot
    loadTokensAndCount();
    loadRecent(slot);
    loadChartData();
    bindEvents();
  }

  global.WXPages = global.WXPages || {};
  global.WXPages.dashboard = { render: render };
})(window);
