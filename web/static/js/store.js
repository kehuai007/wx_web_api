/* Minimal state store. Persists only UI prefs (sidebar, intendedRoute).
 * Sensitive data (token) lives in localStorage via auth.js. */

(function (global) {
  'use strict';

  const KEY_COLLAPSED = 'wx_ui_collapsed';
  const KEY_INTENT    = 'wx_ui_intended_route';

  function safeGet(key) {
    try { return localStorage.getItem(key); } catch (e) { return null; }
  }
  function safeSet(key, value) {
    try { localStorage.setItem(key, value); } catch (e) { /* ignore */ }
  }
  function safeDel(key) {
    try { localStorage.removeItem(key); } catch (e) { /* ignore */ }
  }

  const Store = {
    isCollapsed() { return safeGet(KEY_COLLAPSED) === '1'; },
    setCollapsed(v) {
      if (v) safeSet(KEY_COLLAPSED, '1'); else safeDel(KEY_COLLAPSED);
      document.getElementById('appRoot')?.classList.toggle('collapsed', v);
    },
    toggleCollapsed() { Store.setCollapsed(!Store.isCollapsed()); },

    getIntendedRoute() { return safeGet(KEY_INTENT) || '/dashboard'; },
    setIntendedRoute(p) { safeSet(KEY_INTENT, p); },
    clearIntendedRoute() { safeDel(KEY_INTENT); }
  };

  global.WXStore = Store;
})(window);
