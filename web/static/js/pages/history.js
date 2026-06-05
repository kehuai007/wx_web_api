(function (global) {
  'use strict';
  function render(slot) {
    slot.innerHTML =
      '<div class="empty">' +
        '<div class="empty__icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 12a9 9 0 109-9 9.75 9.75 0 00-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/></svg></div>' +
        '<div class="empty__title">解析历史</div>' +
        '<div class="empty__desc">该功能将在下个版本上线。届时您可以查看所有 /wx 请求的完整记录（时间、用户、状态、耗时、URL）。</div>' +
      '</div>';
  }
  global.WXPages = global.WXPages || {};
  global.WXPages.history = { render: render };
})(window);
