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
