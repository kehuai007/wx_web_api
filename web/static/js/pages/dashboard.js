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
