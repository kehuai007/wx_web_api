(function() {
    let sessionToken = localStorage.getItem('wx_token') || '';
    const pwdInput = document.getElementById('pwd');
    const challengeInput = document.getElementById('challenge');
    const responseInput = document.getElementById('response');
    const loginBtn = document.getElementById('loginBtn');
    const loginError = document.getElementById('loginError');
    const loginView = document.getElementById('loginView');
    const mainView = document.getElementById('mainView');
    const parseBtn = document.getElementById('parseBtn');
    const urlInput = document.getElementById('urlInput');
    const parseError = document.getElementById('parseError');
    const loading = document.getElementById('loading');
    const resultDiv = document.getElementById('resultDiv');

    function getChallenge() {
        fetch('/api/login/challenge')
            .then(r => r.json())
            .then(data => {
                if (data.code === 0) challengeInput.value = data.challenge;
            });
    }

    function sha256(data) {
        return crypto.subtle.digest('SHA-256', new TextEncoder().encode(data))
            .then(buf => Array.from(new Uint8Array(buf)).map(b => b.toString(16).padStart(2, '0')).join(''));
    }

    loginBtn.addEventListener('click', async () => {
        loginError.style.display = 'none';
        const pwd = pwdInput.value;
        const challenge = challengeInput.value;
        if (!pwd || !challenge) { loginError.textContent = '请输入密码'; loginError.style.display = 'block'; return; }
        loginBtn.disabled = true;
        const response = await sha256(pwd + challenge);
        fetch('/api/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ pwd, challenge, response })
        })
            .then(r => r.json())
            .then(data => {
                if (data.code === 0) {
                    sessionToken = data.token;
                    localStorage.setItem('wx_token', sessionToken);
                    showMainView();
                } else {
                    loginError.textContent = data.msg || '登录失败';
                    loginError.style.display = 'block';
                    loginBtn.disabled = false;
                    getChallenge();
                }
            })
            .catch(() => { loginError.textContent = '网络错误'; loginError.style.display = 'block'; loginBtn.disabled = false; });
    });

    parseBtn.addEventListener('click', () => {
        const url = urlInput.value.trim();
        parseError.style.display = 'none';
        resultDiv.classList.remove('show');
        if (!url) { parseError.textContent = '请输入URL'; parseError.style.display = 'block'; return; }
        loading.classList.add('show');
        fetch('/wx', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + sessionToken },
            body: JSON.stringify({ url })
        })
            .then(r => r.json())
            .then(data => {
                loading.classList.remove('show');
                if (data.code === 0) {
                    document.getElementById('resAuthor').textContent = data.data.author || '-';
                    document.getElementById('resTitle').textContent = data.data.title || '-';
                    document.getElementById('resCover').innerHTML = data.data.cover_url ? '<a href="'+data.data.cover_url+'" target="_blank">查看封面</a>' : '-';
                    document.getElementById('resVideo').innerHTML = data.data.video_url ? '<a href="'+data.data.video_url+'" target="_blank">查看视频</a>' : '-';
                    document.getElementById('resDecodeKey').textContent = data.data.decode_key || '-';
                    document.getElementById('resMediaType').textContent = data.data.media_type || '-';
                    resultDiv.classList.add('show');
                } else {
                    parseError.textContent = data.msg || '解析失败';
                    parseError.style.display = 'block';
                }
            })
            .catch(e => { loading.classList.remove('show'); parseError.textContent = '网络错误'; parseError.style.display = 'block'; });
    });

    function showMainView() {
        loginView.style.display = 'none';
        mainView.style.display = 'block';
    }

    if (sessionToken) {
        showMainView();
    } else {
        loginView.style.display = 'block';
        mainView.style.display = 'none';
        getChallenge();
    }
})();