// 前端公共代码：接口请求、图标栏、提示条、小工具。接口约定见 ../接口约定.md 第 6 节。
(function () {
  'use strict';

  // ---- 接口 ----
  async function api(method, url, body) {
    const opt = { method, headers: { 'X-Requested-With': 'fetch' }, credentials: 'same-origin' };
    if (body instanceof FormData) opt.body = body;
    else if (body !== undefined) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
    const res = await fetch(url, opt);
    if (res.status === 401 && !url.endsWith('/api/login')) {
      location.href = '/login.html?next=' + encodeURIComponent(location.pathname + location.search);
      throw new Error('请先登录');
    }
    const ct = res.headers.get('Content-Type') || '';
    const data = ct.includes('application/json') ? await res.json() : null;
    if (!res.ok) throw new Error((data && data.error) || ('请求失败（' + res.status + '）'));
    return data;
  }
  const API = {
    get: u => api('GET', u),
    post: (u, b) => api('POST', u, b === undefined ? {} : b),
    patch: (u, b) => api('PATCH', u, b),
    del: u => api('DELETE', u),
  };

  // ---- 文本和格式 ----
  const esc = t => String(t == null ? '' : t).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  function ago(ts) {
    if (!ts) return '';
    const d = new Date(ts), s = (Date.now() - d.getTime()) / 1000;
    if (s < 60) return '刚刚';
    if (s < 3600) return Math.floor(s / 60) + ' 分钟前';
    if (s < 86400) return Math.floor(s / 3600) + ' 小时前';
    if (s < 86400 * 7) return Math.floor(s / 86400) + ' 天前';
    return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
  }

  // 款的状态和步骤
  const STATUS = {
    draft: { t: '草稿', cls: 'pill-neutral' },
    processing: { t: '生成中', cls: 'pill-info' },
    ready: { t: '已完成', cls: 'pill-success' },
    failed: { t: '失败', cls: 'pill-danger' },
  };
  const STEPS = [
    ['check', '检查照片'], ['classify', 'AI 分拣'], ['enhance', 'AI 补图'], ['retouch', '修图'], ['copy', 'AI 写文案'], ['render', '出图打包'],
  ];
  const stepName = k => (STEPS.find(s => s[0] === k) || [k, k])[1];
  const statusPill = s => { const x = STATUS[s] || { t: s, cls: 'pill-neutral' }; return `<span class="pill dot ${x.cls}">${esc(x.t)}</span>`; };

  // ---- 图标栏 ----
  async function shell(active) {
    let me = null;
    try { me = (await API.get('/api/me')).user; } catch (e) { return null; }
    const rail = document.querySelector('nav.rail');
    if (rail) {
      const item = (href, icon, text, key) => `<a class="rail-item${active === key ? ' active' : ''}" href="${href}"><i data-lucide="${icon}"></i>${text}</a>`;
      rail.innerHTML =
        `<a class="rail-logo" href="/" title="X Studio"><i data-lucide="gem"></i></a>` +
        item('/', 'layout-grid', '款式', 'products') +
        (me.role === 'admin' ? item('/users.html', 'users', '账号', 'users') : '') +
        item('/help.html', 'book-open', '拍摄要求', 'help') +
        `<span class="rail-spacer"></span>` +
        `<button class="rail-item" id="logoutBtn" title="退出登录"><i data-lucide="log-out"></i>退出</button>` +
        `<span class="avatar lg" title="${esc(me.username)}">${esc(me.username.slice(0, 2).toUpperCase())}</span>`;
      rail.querySelector('#logoutBtn').onclick = async () => { await API.post('/api/logout'); location.href = '/login.html'; };
    }
    icons();
    return me;
  }
  const icons = () => window.lucide && window.lucide.createIcons({ attrs: { 'stroke-width': 1.75 } });

  // ---- 提示条（右下角，3 秒消失）----
  function toast(msg, kind) {
    let box = document.getElementById('toasts');
    if (!box) { box = document.createElement('div'); box.id = 'toasts'; document.body.appendChild(box); }
    const el = document.createElement('div');
    el.className = 'toast ' + (kind || '');
    el.textContent = msg;
    box.appendChild(el);
    setTimeout(() => el.remove(), kind === 'error' ? 6000 : 3000);
  }

  // ---- 抽屉 ----
  function drawer(html, onMount) {
    const mask = document.createElement('div');
    mask.className = 'mask';
    mask.innerHTML = `<aside class="drawer" role="dialog">${html}</aside>`;
    document.body.appendChild(mask);
    const close = () => mask.remove();
    mask.addEventListener('mousedown', e => { if (e.target === mask) close(); });
    mask.querySelectorAll('[data-close]').forEach(b => b.onclick = close);
    document.addEventListener('keydown', function k(e) { if (e.key === 'Escape') { close(); document.removeEventListener('keydown', k); } });
    icons();
    onMount && onMount(mask.querySelector('.drawer'), close);
    return close;
  }

  window.App = { API, esc, ago, STATUS, STEPS, stepName, statusPill, shell, icons, toast, drawer };
})();
