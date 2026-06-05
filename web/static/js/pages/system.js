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
