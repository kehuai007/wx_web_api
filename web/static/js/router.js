/* Client-side History API router.
 * - Single source of truth: ROUTES table
 * - Each route: { path, title, render(ctx) } where ctx = { params, query }
 * - Renders into #pageContent
 * - Updates <title> and #pageTitle
 * - Updates active state in #nav
 * - Intercepts clicks on [data-route] anchors
 * - popstate handler for back/forward
 */

(function (global) {
  'use strict';

  const ROUTES = [
    { path: '/dashboard', title: '概览',     page: 'dashboard' },
    { path: '/settings',  title: '配置',     page: 'settings'  },
    { path: '/test',      title: '解析测试', page: 'test'      },
    { path: '/history',   title: '解析历史', page: 'history'   },
    { path: '/users',     title: '用户/角色', page: 'users'    },
    { path: '/system',    title: '系统信息', page: 'system'    }
  ];

  const DEFAULT_ROUTE = '/dashboard';

  function getRouteMeta(path) {
    for (var i = 0; i < ROUTES.length; i++) {
      if (ROUTES[i].path === path) return ROUTES[i];
    }
    return null;
  }

  function parseQuery() {
    var q = {};
    var s = global.location.search.replace(/^\?/, '');
    if (!s) return q;
    s.split('&').forEach(function (pair) {
      if (!pair) return;
      var kv = pair.split('=');
      q[decodeURIComponent(kv[0])] = decodeURIComponent(kv[1] || '');
    });
    return q;
  }

  function renderActiveNav(path) {
    var links = document.querySelectorAll('#nav .nav__item');
    for (var i = 0; i < links.length; i++) {
      var a = links[i];
      if (a.getAttribute('data-route') === path) {
        a.classList.add('active');
      } else {
        a.classList.remove('active');
      }
    }
  }

  function render() {
    var path = global.location.pathname || DEFAULT_ROUTE;
    var meta = getRouteMeta(path);
    if (!meta) { path = DEFAULT_ROUTE; meta = getRouteMeta(path); }

    /* update topbar */
    var titleEl = document.getElementById('pageTitle');
    if (titleEl) titleEl.textContent = meta.title;
    document.title = meta.title + ' · wx_web_api';

    /* update nav highlight */
    renderActiveNav(path);

    /* render page */
    var slot = document.getElementById('pageContent');
    slot.innerHTML = '';
    var mod = global.WXPages && global.WXPages[meta.page];
    if (mod && typeof mod.render === 'function') {
      var ret = mod.render(slot, { path: path, query: parseQuery() });
      if (ret && typeof ret.then === 'function') {
        ret.catch(function (e) {
          if (global.WXToast) global.WXToast('页面加载失败', 'error');
          if (global.console) console.error(e);
        });
      }
    } else {
      slot.innerHTML = '<div class="empty"><div class="empty__title">页面模块缺失</div></div>';
    }

    /* close mobile drawer after navigation */
    var sb = document.getElementById('sidebar');
    if (sb) sb.classList.remove('open');
    var bd = document.getElementById('backdrop');
    if (bd) bd.classList.remove('show');

    /* scroll to top of content */
    var main = document.querySelector('.main');
    if (main) main.scrollTop = 0;
  }

  function navigate(path, opts) {
    opts = opts || {};
    if (!global.WXAuth || !global.WXAuth.isLoggedIn()) {
      if (global.WXStore) global.WXStore.setIntendedRoute(path);
      if (global.WXAuth) global.WXAuth.showLogin();
      return;
    }
    if (path === global.location.pathname && !opts.force) {
      render();
      return;
    }
    if (opts.replace) {
      global.history.replaceState({}, '', path);
    } else {
      global.history.pushState({}, '', path);
    }
    render();
  }

  function buildNav() {
    var nav = document.getElementById('nav');
    if (!nav) return;
    var html = ROUTES.map(function (r) {
      var iconPath = NAV_ICONS[r.page] || NAV_ICONS.default;
      return '<a class="nav__item" href="' + r.path + '" data-route="' + r.path + '">' +
               '<svg class="nav__icon" viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">' + iconPath + '</svg>' +
               '<span class="nav__label">' + r.title + '</span>' +
             '</a>';
    }).join('');
    nav.innerHTML = html;
  }

  /* Minimal inline icon paths (24x24 stroke icons) */
  const NAV_ICONS = {
    dashboard: '<path d="M3 12l2-2 7-7 7 7 2 2v8a2 2 0 01-2 2h-4v-7h-6v7H5a2 2 0 01-2-2v-8z"/>',
    settings:  '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 11-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 11-4 0v-.09A1.65 1.65 0 008 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 11-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 110-4h.09A1.65 1.65 0 004.6 8a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 112.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 114 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 112.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 110 4h-.09a1.65 1.65 0 00-1.51 1z"/>',
    test:      '<path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/>',
    history:   '<path d="M3 12a9 9 0 109-9 9.75 9.75 0 00-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/>',
    users:     '<path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75"/>',
    system:    '<circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/>',
    default:   '<circle cx="12" cy="12" r="10"/>'
  };

  function init() {
    buildNav();
    global.addEventListener('popstate', render);
    document.addEventListener('click', function (e) {
      var a = e.target.closest && e.target.closest('[data-route]');
      if (a) {
        e.preventDefault();
        navigate(a.getAttribute('data-route'));
      }
    });
  }

  global.WXRouter = {
    ROUTES: ROUTES,
    init: init,
    render: render,
    navigate: navigate,
    getRouteMeta: getRouteMeta
  };
})(window);
