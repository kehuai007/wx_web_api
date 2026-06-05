/* Fetch wrapper that injects Authorization header and handles 401.
 * Paired with auth.js — 401 triggers login redirect. */

(function (global) {
  'use strict';

  function getToken() {
    try { return localStorage.getItem('wx_token') || ''; } catch (e) { return ''; }
  }

  async function authFetch(url, options) {
    options = options || {};
    const token = getToken();
    const headers = Object.assign(
      { 'Content-Type': 'application/json' },
      options.headers || {}
    );
    if (token) headers['Authorization'] = token;

    let res;
    try {
      res = await fetch(url, Object.assign({}, options, { headers: headers }));
    } catch (e) {
      // network error
      throw new Error('网络错误');
    }

    if (res.status === 401) {
      if (global.WXAuth && global.WXAuth.handle401) {
        global.WXAuth.handle401();
      }
      throw new Error('未授权');
    }
    return res;
  }

  async function authJson(url, options) {
    const res = await authFetch(url, options);
    let data = null;
    try { data = await res.json(); } catch (e) { /* leave null */ }
    return { status: res.status, data: data };
  }

  global.WXApi = { authFetch, authJson, getToken };
})(window);
