(function (global) {
  'use strict';
  function render(slot) {
    slot.innerHTML =
      '<div class="empty">' +
        '<div class="empty__icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg></div>' +
        '<div class="empty__title">解析测试</div>' +
        '<div class="empty__desc">该功能将在下个版本上线。届时您可以粘贴微信分享链接或 objectId，调试 /wx 与 /wx/finder 并查看返回结果。</div>' +
      '</div>';
  }
  global.WXPages = global.WXPages || {};
  global.WXPages.test = { render: render };
})(window);
