(function() {
    const AUTH_KEY = 'wx_token';
    const DEFAULT_API_BASE = 'http://127.0.0.1:2022';

    function getToken() { return localStorage.getItem(AUTH_KEY) || ''; }
    function setToken(token) { localStorage.setItem(AUTH_KEY, token); }
    function clearToken() { localStorage.removeItem(AUTH_KEY); }

    function authFetch(url, options) {
        options = options || {};
        var token = getToken();
        var headers = new Headers(options.headers || {});
        if (token) headers.set('Authorization', token);
        return fetch(url, Object.assign({}, options, { headers: headers }));
    }

    function showLoginScreen() {
        document.getElementById('loginScreen').style.display = 'flex';
        document.getElementById('mainContainer').style.display = 'none';
    }

    function showMainView() {
        document.getElementById('loginScreen').style.display = 'none';
        document.getElementById('mainContainer').style.display = 'block';
    }

    function showMsg(msg, type) {
        var errEl = document.getElementById('msgError');
        var succEl = document.getElementById('msgSuccess');
        errEl.classList.remove('show');
        succEl.classList.remove('show');
        if (type === 'error') {
            errEl.textContent = msg;
            errEl.classList.add('show');
        } else {
            succEl.textContent = msg;
            succEl.classList.add('show');
        }
        setTimeout(function() {
            errEl.classList.remove('show');
            succEl.classList.remove('show');
        }, 3000);
    }

    function simpleHash(data) {
        var h = 0;
        var primes = [31, 37, 41, 43, 47, 53, 59, 61, 67, 71, 73, 79];
        for (var i = 0; i < data.length; i++) {
            h += data.charCodeAt(i) * primes[(i + 1) % 12];
        }
        return ('0000000000000000' + h.toString(16)).slice(-16);
    }

    window.doLogin = function() {
        var pwd = document.getElementById('loginPwd').value;
        if (!pwd) return;
        document.getElementById('loginError').style.display = 'none';

        fetch('/api/login/challenge')
            .then(function(r) { return r.json(); })
            .then(function(challengeData) {
                if (challengeData.code !== 0) {
                    document.getElementById('loginError').textContent = challengeData.msg || '获取挑战失败';
                    document.getElementById('loginError').style.display = 'block';
                    return;
                }
                var challenge = challengeData.challenge;
                var response = simpleHash(pwd + challenge);

                return fetch('/api/login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ pwd: pwd, challenge: challenge, response: response })
                }).then(function(r) { return r.json(); });
            })
            .then(function(data) {
                if (data.code === 0) {
                    setToken(data.token);
                    document.getElementById('loginPwd').value = '';
                    showMainView();
                    loadConfig();
                } else {
                    document.getElementById('loginError').textContent = data.msg || '登录失败';
                    document.getElementById('loginError').style.display = 'block';
                }
            })
            .catch(function(e) {
                document.getElementById('loginError').textContent = '网络错误';
                document.getElementById('loginError').style.display = 'block';
            });
    };

    window.doLogout = function() {
        clearToken();
        showLoginScreen();
    };

    window.loadConfig = function() {
        console.log('loadConfig called');
        document.getElementById('apiBaseUrl').value = DEFAULT_API_BASE;
        window.configTokens = [];
        renderTokens([]);

        authFetch('/api/config')
            .then(function(r) {
                console.log('config response status:', r.status);
                if (r.status === 401) {
                    clearToken();
                    showLoginScreen();
                    return null;
                }
                if (!r.ok) return null;
                return r.json();
            })
            .then(function(data) {
                console.log('config data:', data);
                if (!data) return;
                if (data.code === 0) {
                    document.getElementById('apiBaseUrl').value = data.data.api_base_url || DEFAULT_API_BASE;
                    window.configTokens = data.data.tokens || [];
                    renderTokens(window.configTokens);
                }
            });
    };

    function renderTokens(tokens) {
        var container = document.getElementById('tokenList');
        if (!tokens || tokens.length === 0) {
            container.innerHTML = '<div class="token-empty">暂无 token，请添加</div>';
            return;
        }
        container.innerHTML = tokens.map(function(token, idx) {
            return '<div class="token-item">' +
                '<code>' + escapeHtml(token) + '</code>' +
                '<button class="btn btn-red btn-sm" onclick="removeToken(' + idx + ')">删除</button>' +
            '</div>';
        }).join('');
    }

    window.addToken = function() {
        var input = document.getElementById('newToken');
        var token = input.value.trim();
        if (!token) return;
        var tokens = window.configTokens || [];
        if (tokens.indexOf(token) !== -1) {
            showMsg('Token 已存在', 'error');
            return;
        }
        tokens.push(token);
        window.configTokens = tokens;
        renderTokens(tokens);
        input.value = '';
        showMsg('已添加，请保存配置', 'success');
    };

    window.removeToken = function(idx) {
        var tokens = window.configTokens || [];
        if (idx < 0 || idx >= tokens.length) return;
        tokens.splice(idx, 1);
        window.configTokens = tokens;
        renderTokens(tokens);
        showMsg('已删除，请保存配置', 'success');
    };

    window.saveConfig = function() {
        var apiBaseUrl = document.getElementById('apiBaseUrl').value.trim() || DEFAULT_API_BASE;
        var tokens = window.configTokens || [];

        authFetch('/api/config', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                api_base_url: apiBaseUrl,
                tokens: tokens
            })
        })
            .then(function(r) {
                if (r.status === 401) {
                    clearToken();
                    showLoginScreen();
                    showMsg('登录已过期，请重新登录', 'error');
                    return null;
                }
                return r.json();
            })
            .then(function(data) {
                if (!data) return;
                if (data.code === 0) {
                    showMsg('保存成功', 'success');
                } else {
                    showMsg(data.msg || '保存失败', 'error');
                }
            })
            .catch(function(e) {
                showMsg('网络错误', 'error');
            });
    };

    function escapeHtml(str) {
        if (!str) return '';
        var div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }

    // 初始化：检查登录状态
    var token = getToken();
    if (!token) {
        showLoginScreen();
        loadConfig();
    } else {
        showMainView();
        loadConfig();
    }
})();