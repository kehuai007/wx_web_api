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
    const callerHeaders = options.headers || {};
    const headers = Object.assign({}, callerHeaders);
    if (token) headers['Authorization'] = token;
    // Default Content-Type for JSON bodies only; let the browser set multipart
    // boundary for FormData/Blob, and respect caller-supplied Content-Type.
    if (!headers['Content-Type'] && !(options.body instanceof FormData)
        && !(options.body instanceof Blob)
        && typeof options.body !== 'undefined') {
      headers['Content-Type'] = 'application/json';
    }

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
      const err = new Error('未授权');
      err.isAuth = true;
      throw err;
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
