/* App bootstrap — runs after all modules load.
 * 1. Toast helper
 * 2. Login form wiring
 * 3. Topbar wiring (collapse, logout, mobile drawer)
 * 4. Decide initial state: login overlay vs app shell
 * 5. Initialize router and render the initial page
 */

(function (global) {
  'use strict';

  /* ---------- Toast ---------- */
  function toast(msg, type) {
    var stack = document.getElementById('toastStack');
    if (!stack) return;
    var el = document.createElement('div');
    el.className = 'toast' + (type ? ' toast--' + type : '');
    el.textContent = msg;
    stack.appendChild(el);
    setTimeout(function () {
      el.classList.add('is-leaving');
      setTimeout(function () { if (el.parentNode) el.parentNode.removeChild(el); }, 220);
    }, 3000);
  }
  global.WXToast = toast;

  /* ---------- Topbar: sidebar collapse ---------- */
  function applyCollapsed(v) {
    if (global.WXStore) global.WXStore.setCollapsed(v);
  }
  function bindCollapse() {
    var btn = document.getElementById('collapseBtn');
    if (!btn) return;
    btn.addEventListener('click', function () {
      /* On mobile, this button is the drawer toggle, not a collapse toggle. */
      if (global.innerWidth <= 768) return;
      var next = !(global.WXStore && global.WXStore.isCollapsed());
      applyCollapsed(next);
    });
  }

  /* ---------- Topbar: logout ---------- */
  function bindLogout() {
    var btn = document.getElementById('logoutBtn');
    if (!btn) return;
    btn.addEventListener('click', function () {
      if (global.WXAuth) global.WXAuth.logout();
      if (global.location && global.location.pathname !== '/') {
        global.history.replaceState({}, '', '/');
      }
    });
  }

  /* ---------- Mobile: drawer / backdrop ---------- */
  function bindMobileDrawer() {
    var btn = document.getElementById('collapseBtn');
    var sb  = document.getElementById('sidebar');
    var bd  = document.getElementById('backdrop');
    if (!btn || !sb || !bd) return;
    btn.addEventListener('click', function (e) {
      if (global.innerWidth > 768) return; /* desktop = collapse toggle */
      sb.classList.toggle('open');
      bd.classList.toggle('show');
    });
    bd.addEventListener('click', function () {
      sb.classList.remove('open');
      bd.classList.remove('show');
    });
  }

  /* ---------- Login form ---------- */
  function bindLogin() {
    var form = document.getElementById('loginForm');
    if (!form) return;
    form.addEventListener('submit', async function (e) {
      e.preventDefault();
      var pwd = document.getElementById('loginPwd').value;
      var errEl = document.getElementById('loginError');
      if (errEl) errEl.textContent = '';
      if (!pwd) return;
      try {
        await global.WXAuth.login(pwd);
        var target = (global.WXStore && global.WXStore.getIntendedRoute()) || '/dashboard';
        if (global.WXStore) global.WXStore.clearIntendedRoute();
        if (global.WXAuth) global.WXAuth.showApp();
        if (global.WXRouter) {
          global.WXRouter.navigate(target, { replace: true });
        } else {
          global.history.replaceState({}, '', target);
        }
      } catch (err) {
        if (errEl) errEl.textContent = err.message || '登录失败';
      }
    });
  }

  /* ---------- Boot ---------- */
  function boot() {
    bindCollapse();
    bindLogout();
    bindMobileDrawer();
    bindLogin();
    if (global.WXRouter) global.WXRouter.init();

    /* restore collapsed state */
    if (global.WXStore && global.WXStore.isCollapsed()) {
      applyCollapsed(true);
    }

    /* decide initial view */
    if (global.WXAuth && global.WXAuth.isLoggedIn()) {
      global.WXAuth.showApp();
      if (global.WXRouter) global.WXRouter.render();
    } else {
      global.WXAuth.showLogin();
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot);
  } else {
    boot();
  }
})(window);
