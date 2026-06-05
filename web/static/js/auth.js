/* Authentication module.
 * - Token storage in localStorage
 * - Challenge-response login (matches server simpleHash)
 * - 401 handler: clear token, show login overlay, preserve intended route
 * - Login overlay show/hide
 */

(function (global) {
  'use strict';

  const TOKEN_KEY = 'wx_token';

  function getToken() {
    try { return localStorage.getItem(TOKEN_KEY) || ''; } catch (e) { return ''; }
  }
  function setToken(t) {
    try { localStorage.setItem(TOKEN_KEY, t); } catch (e) { /* ignore */ }
  }
  function clearToken() {
    try { localStorage.removeItem(TOKEN_KEY); } catch (e) { /* ignore */ }
  }

  function isLoggedIn() { return !!getToken(); }

  /* simpleHash must match the algorithm in internal/handler/handler.go */
  function simpleHash(data) {
    var h = 0;
    var primes = [31, 37, 41, 43, 47, 53, 59, 61, 67, 71, 73, 79];
    for (var i = 0; i < data.length; i++) {
      h += data.charCodeAt(i) * primes[(i + 1) % 12];
    }
    return ('0000000000000000' + (h >>> 0).toString(16)).slice(-16);
  }

  function showLogin() {
    document.getElementById('loginRoot').hidden = false;
    document.getElementById('appRoot').hidden = true;
    setTimeout(function () {
      var pwd = document.getElementById('loginPwd');
      if (pwd) { pwd.value = ''; pwd.focus(); }
    }, 50);
  }

  function showApp() {
    document.getElementById('loginRoot').hidden = true;
    document.getElementById('appRoot').hidden = false;
  }

  function handle401() {
    var current = (global.location && global.location.pathname) || '/dashboard';
    if (global.WXStore) {
      global.WXStore.setIntendedRoute(current);
    }
    clearToken();
    showLogin();
    if (global.WXToast) {
      global.WXToast('登录已过期，请重新登录', 'warning');
    }
  }

  async function login(pwd) {
    var challengeRes = await fetch('/api/login/challenge');
    var challengeData = await challengeRes.json();
    if (challengeData.code !== 0) {
      throw new Error(challengeData.msg || '获取挑战失败');
    }
    var challenge = challengeData.challenge;
    var response  = simpleHash(pwd + challenge);

    var loginRes = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pwd: pwd, challenge: challenge, response: response })
    });
    var loginData = await loginRes.json();
    if (loginData.code !== 0) {
      throw new Error(loginData.msg || '登录失败');
    }
    setToken(loginData.token);
    return loginData.token;
  }

  function logout() {
    clearToken();
    if (global.WXStore) global.WXStore.clearIntendedRoute();
    showLogin();
  }

  global.WXAuth = {
    isLoggedIn: isLoggedIn,
    getToken: getToken,
    setToken: setToken,
    clearToken: clearToken,
    login: login,
    logout: logout,
    showLogin: showLogin,
    showApp: showApp,
    handle401: handle401,
    simpleHash: simpleHash
  };
})(window);
