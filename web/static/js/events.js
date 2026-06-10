/* WebSocket 客户端单例 — 整个 SPA 共享一条 /ws/events 连接。
 * - subscribe(type, handler) / unsubscribe(type, handler):按事件类型订阅
 * - onStatusChange(handler):订阅连接状态变化
 * - start() / stop():启动 / 停止(登出时停)
 * - connectionStatus: 'connecting' | 'ok' | 'err' | 'auth_err'
 *
 * 重连:指数退避 1s → 30s;watchdog 2s 检查,6s 未收帧视为 stale 主动 close。
 * 鉴权:断开时 probe /api/system,401 → auth_err + WXAuth.handle401。
 * 首帧:onopen 后发 {type:'client.hello'},服务端立即推 system.snapshot。
 *
 * 依赖: WXAuth(用于 401 跳登录与 token 读取),WXApi(用于 probe)。
 * 加载顺序:auth.js → events.js → api.js → app.js。
 */
(function (global) {
  'use strict';

  var RECONNECT_BASE_MS = 1000;
  var RECONNECT_MAX_MS = 30000;
  var STALE_THRESHOLD_MS = 6000;       // > 6s 未收帧视为 stale
  var WATCHDOG_INTERVAL_MS = 2000;
  var PING_FRAME = JSON.stringify({ type: 'client.hello' });

  var state = {
    ws: null,
    reconnectAttempt: 0,
    reconnectTimer: null,
    watchdogTimer: null,
    lastFrameAt: 0,
    connectionStatus: 'connecting',
    running: false,
    handlers: Object.create(null),      // { 'log.new': Set<fn>, ... }
    statusHandlers: new Set(),
  };

  function setStatus(next) {
    if (state.connectionStatus === next) return;
    state.connectionStatus = next;
    state.statusHandlers.forEach(function (fn) { try { fn(next); } catch (e) { /* ignore */ } });
  }

  function getToken() {
    if (global.WXAuth && global.WXAuth.getToken) return global.WXAuth.getToken();
    try { return localStorage.getItem('wx_token') || ''; } catch (e) { return ''; }
  }

  function buildWsUrl() {
    var proto = global.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return proto + '//' + global.location.host + '/ws/events?token=' + encodeURIComponent(getToken());
  }

  function connect() {
    if (!state.running) return;
    if (state.ws) { try { state.ws.close(); } catch (e) { /* ignore */ } }
    if (state.reconnectTimer) { clearTimeout(state.reconnectTimer); state.reconnectTimer = null; }

    setStatus('connecting');
    var url;
    try { url = buildWsUrl(); } catch (e) {
      scheduleReconnect();
      return;
    }

    var ws;
    try { ws = new global.WebSocket(url); }
    catch (e) { scheduleReconnect(); return; }
    state.ws = ws;

    ws.onopen = function () {
      state.reconnectAttempt = 0;
      // 请求服务端立即推一帧 system.snapshot 作为首帧
      try { ws.send(PING_FRAME); } catch (e) { /* ignore */ }
    };

    ws.onmessage = function (e) {
      state.lastFrameAt = Date.now();
      var frame;
      try { frame = JSON.parse(e.data); }
      catch (err) { if (global.console) console.error('events: bad frame', err); return; }
      if (frame && frame.type) {
        if (state.connectionStatus !== 'ok') setStatus('ok');
        var set = state.handlers[frame.type];
        if (set) {
          set.forEach(function (fn) { try { fn(frame); } catch (err) { if (global.console) console.error('events: handler error', err); } });
        }
      }
    };

    ws.onerror = function () { handleDisconnect(); };
    ws.onclose = function () { handleDisconnect(); };
  }

  function probeAuth() {
    if (!global.WXApi || !global.WXApi.authJson) return Promise.resolve(true);
    return global.WXApi.authJson('/api/system').then(function () { return true; }).catch(function (e) {
      if (e && e.isAuth) return false;
      return true; // 网络/500/parse 错误不视为鉴权失败
    });
  }

  function handleDisconnect() {
    if (!state.running) return;
    setStatus('connecting');
    probeAuth().then(function (ok) {
      if (!ok) {
        setStatus('auth_err');
        if (global.WXAuth && global.WXAuth.handle401) global.WXAuth.handle401();
        return;
      }
      setStatus('err');
      scheduleReconnect();
    });
  }

  function scheduleReconnect() {
    if (!state.running || state.reconnectTimer) return;
    var delay = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * Math.pow(2, state.reconnectAttempt));
    state.reconnectAttempt++;
    state.reconnectTimer = setTimeout(function () {
      state.reconnectTimer = null;
      connect();
    }, delay);
  }

  function startWatchdog() {
    if (state.watchdogTimer) return;
    state.watchdogTimer = setInterval(function () {
      if (state.connectionStatus === 'ok' && state.lastFrameAt > 0) {
        var staleFor = Date.now() - state.lastFrameAt;
        if (staleFor > STALE_THRESHOLD_MS && state.ws) {
          try { state.ws.close(); } catch (e) { /* ignore */ }
        }
      }
    }, WATCHDOG_INTERVAL_MS);
  }

  function stopWatchdog() {
    if (state.watchdogTimer) { clearInterval(state.watchdogTimer); state.watchdogTimer = null; }
  }

  function start() {
    if (state.running) return;
    state.running = true;
    state.reconnectAttempt = 0;
    connect();
    startWatchdog();
  }

  function stop() {
    state.running = false;
    if (state.reconnectTimer) { clearTimeout(state.reconnectTimer); state.reconnectTimer = null; }
    stopWatchdog();
    if (state.ws) { try { state.ws.close(); } catch (e) { /* ignore */ } state.ws = null; }
    setStatus('connecting');
  }

  function subscribe(type, handler) {
    if (!type || typeof handler !== 'function') return function () {};
    if (!state.handlers[type]) state.handlers[type] = new Set();
    state.handlers[type].add(handler);
    return function () { unsubscribe(type, handler); };
  }

  function unsubscribe(type, handler) {
    var set = state.handlers[type];
    if (set) set.delete(handler);
  }

  function onStatusChange(handler) {
    if (typeof handler !== 'function') return function () {};
    state.statusHandlers.add(handler);
    return function () { state.statusHandlers.delete(handler); };
  }

  global.WXEvents = {
    start: start,
    stop: stop,
    subscribe: subscribe,
    unsubscribe: unsubscribe,
    onStatusChange: onStatusChange,
    get connectionStatus() { return state.connectionStatus; },
  };
})(window);
