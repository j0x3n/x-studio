// 款编辑页：加载数据、照片库、上传、生成任务、中间预览。右侧面板在 product-inspector.js。
(function () {
  'use strict';
  const { API, esc, STEPS, statusPill, icons, toast } = App;
  const id = new URLSearchParams(location.search).get('id');
  const S = window.S = { id, data: null, codes: [], sel: null, view: 'm34', tab: 'photo', job: null, dirty: false };
  const $ = s => document.querySelector(s);

  // ---- 加载 ----
  S.load = async function () {
    try { S.data = await API.get('/api/products/' + id); }
    catch (e) { toast(e.message, 'error'); return; }
    const p = S.data.product;
    document.title = p.name + ' · 珠光秘境出图';
    $('#title').textContent = p.name;
    $('#pid').textContent = '#' + p.id;
    $('#statusPill').innerHTML = statusPill(p.status);
    const zip = S.data.outputs && S.data.outputs.zip;
    $('#dlBtn').classList.toggle('hidden', !zip);
    if (zip) $('#dlBtn').href = '/api/products/' + id + '/download';
    const running = S.data.job && (S.data.job.status === 'running' || S.data.job.status === 'queued');
    $('#runBtn').disabled = running || !S.data.photos.length;
    $('#runBtn').innerHTML = running ? '<i data-lucide="loader" class="spin"></i>生成中…'
      : `<i data-lucide="sparkles"></i>${p.status === 'draft' ? '一键生成' : '重新生成'}`;
    if (S.sel && !S.data.photos.some(x => x.id === S.sel)) S.sel = null;
    renderPhotos(); renderSteps(); renderStage(); S.renderInspector();
    if (running) poll(S.data.job.id);
  };

  // ---- 照片库 ----
  const thumbUrl = ph => '/files/products/' + id + '/' + ph.file + '?w=480';
  function badFlag(ph) {
    const m = ph.metrics || {};
    const why = [];
    if (m.blur_level === '模糊') why.push('模糊');
    if (m.clip_level === '严重') why.push('高光死白');
    if (m.dup_of) why.push('和别的图重复');
    if (m.error) why.push('读不了');
    return why;
  }
  function thumb(ph) {
    const bad = badFlag(ph);
    return `<button class="thumb${S.sel === ph.id ? ' selected' : ''}${ph.code ? '' : ' unused'}" data-id="${ph.id}" title="${esc(ph.orig_name)}">
      <img src="${esc(thumbUrl(ph))}" alt="" loading="lazy">
      <div class="meta"><span class="code${ph.code ? '' : ' none'}">${esc(ph.code || '未用')}</span>
      ${bad.length ? `<span class="flag" title="${esc(bad.join('、'))}"><i data-lucide="alert-triangle"></i></span>` : ''}</div>
      ${ph.ai_generated ? '<span class="chip manual ai" title="AI 生成的补图，图上会标「AI 生成」">AI 生成</span>'
        : ph.ai_enhanced ? '<span class="chip manual ai" title="实拍图做了 AI 高清化，不打标识">AI 增强</span>'
        : ph.manual ? '<span class="chip neutral manual" title="人工改过编号，AI 不会再覆盖">手改</span>' : ''}
    </button>`;
  }
  function renderPhotos() {
    const ps = S.data.photos;
    const used = ps.filter(x => x.code).sort((a, b) => a.code.localeCompare(b.code));
    const unused = ps.filter(x => !x.code);
    $('#used').innerHTML = used.map(thumb).join('');
    $('#unused').innerHTML = unused.map(thumb).join('');
    $('#usedN').textContent = used.length; $('#unusedN').textContent = unused.length;
    document.querySelectorAll('.thumb').forEach(b => b.onclick = () => {
      S.sel = +b.dataset.id; S.tab = 'photo'; renderPhotos(); S.renderInspector();
    });
    icons();
  }

  // ---- 上传 ----
  async function upload(files) {
    files = [...files].filter(f => /^image\/(jpeg|png|webp)$/.test(f.type));
    if (!files.length) { toast('只认 jpg、png、webp 图片', 'error'); return; }
    const big = files.filter(f => f.size > 25 * 1024 * 1024);
    if (big.length) { toast(big.length + ' 张超过 25 MB，没传', 'error'); files = files.filter(f => f.size <= 25 * 1024 * 1024); }
    const fd = new FormData();
    files.forEach(f => fd.append('files', f));
    const drop = $('#drop'); drop.querySelector('b').textContent = `上传中（${files.length} 张）…`;
    try { await API.post('/api/products/' + id + '/photos', fd); toast(`已上传 ${files.length} 张`, 'ok'); S.markDirty(); await S.load(); }
    catch (e) { toast(e.message, 'error'); }
    drop.querySelector('b').textContent = '上传实拍图';
  }
  $('#file').onchange = e => { upload(e.target.files); e.target.value = ''; };
  const drop = $('#drop');
  ['dragenter', 'dragover'].forEach(t => drop.addEventListener(t, e => { e.preventDefault(); drop.classList.add('over'); }));
  ['dragleave', 'drop'].forEach(t => drop.addEventListener(t, e => { e.preventDefault(); drop.classList.remove('over'); }));
  drop.addEventListener('drop', e => upload(e.dataTransfer.files));

  // ---- 生成任务 ----
  S.markDirty = function () { S.dirty = true; $('#dirtyNote').classList.remove('hidden'); };
  S.run = async function (q) {
    try {
      const r = await API.post('/api/products/' + id + '/run' + (q ? '?' + q : ''));
      S.dirty = false; $('#dirtyNote').classList.add('hidden');
      toast('开始生成', 'ok');
      await S.load();
      if (r && r.job) poll(r.job.id);
    } catch (e) { toast(e.message, 'error'); }
  };
  $('#runBtn').onclick = () => S.run('');
  let pollTimer = null;
  function poll(jid) {
    clearTimeout(pollTimer);
    pollTimer = setTimeout(async () => {
      try {
        const { job } = await API.get('/api/jobs/' + jid);
        S.data.job = job; renderSteps();
        if (job.status === 'running' || job.status === 'queued') poll(jid);
        else { if (job.status === 'failed') toast('生成失败：' + (job.message || ''), 'error'); else toast('生成完成', 'ok'); S.load(); }
      } catch (e) { poll(jid); }
    }, 1500);
  }
  function renderSteps() {
    const p = S.data.product, job = S.data.job;
    const cur = STEPS.findIndex(s => s[0] === p.step);
    const running = job && (job.status === 'running' || job.status === 'queued');
    $('#steps').innerHTML = STEPS.map((s, i) => {
      let c = '', ic = 'circle';
      if (p.status === 'ready' || i < cur) { c = 'done'; ic = 'check-circle-2'; }
      else if (i === cur && running) { c = 'running'; ic = 'loader'; }
      else if (i === cur && p.status === 'failed') { c = 'failed'; ic = 'x-circle'; }
      return `<span class="step ${c}"><i data-lucide="${ic}"${c === 'running' ? ' class="spin"' : ''}></i>${esc(s[1])}</span>`;
    }).join('') + (running && job.progress != null ? `<span class="mono fs-12 t3">${job.progress}%</span>` : '');
    icons();
  }

  // ---- 中间预览 ----
  document.querySelectorAll('#view button').forEach(b => b.onclick = () => {
    document.querySelectorAll('#view button').forEach(x => x.classList.toggle('active', x === b));
    S.view = b.dataset.v; renderStage();
  });
  function notices() {
    const p = S.data.product, cov = S.data.coverage || {}, out = [];
    if (p.status === 'failed' && p.error) out.push(['err', 'x-circle', `<b>生成失败（${esc(App.stepName(p.step))}）</b><div>${esc(p.error)}</div>`]);
    // 主图和详情页可能缺同一类照片（如"尺寸"），按候选编号去重
    const seen = new Set();
    const need = (cov.need_reshoot || []).filter(n => { const k = (n.candidates || []).join(); if (seen.has(k)) return false; seen.add(k); return true; });
    if (need.length) out.push(['warn', 'camera', '<b>这几张必须是实拍，请上游或你补拍</b>' +
      need.map(n => `<div>${esc(n.name || n.slot)}：${esc((n.candidates || []).join(' / '))}</div>`).join('') +
      '<div class="t3">补拍后上传，再点"重新生成"。上耳图不能用 AI 生成。</div>']);
    const errs = ((p.listing && p.listing.compliance) || []).filter(c => c.level === 'error');
    if (errs.length) out.push(['err', 'shield-alert', `<b>文案有 ${errs.length} 处不合规，上架前要改</b>` + errs.map(c => `<div>${esc(c.text)}</div>`).join('') + '<div class="t3">在右侧"文案"或"标题规格"里改。</div>']);
    // 主图用到的照片：裁剪后短边不到 900 像素，放大成 1440×1920 会发糊
    const used = [];
    ((p.main && p.main.imgs) || []).forEach(m => { if (m.f) used.push(m.f); (m.items || []).forEach(it => it.f && used.push(it.f)); });
    const small = [];
    used.forEach(f => {
      const code = f.replace(/\.\w+$/, '').replace(/_白底$/, '');
      const ph = S.data.photos.find(x => x.code === code);
      if (!ph || !ph.width || ph.ai_enhanced || ph.ai_generated) return;
      const c = ph.crop || [0, 0, 1, 1];
      const w = ph.width * c[2], h = ph.height * c[3];
      if (Math.min(w, h) < 900) small.push(`${code}（裁剪后约 ${Math.round(w)}×${Math.round(h)}）`);
    });
    if (small.length) out.push(['warn', 'zoom-in', `<b>${small.length} 张主图用的照片太小，放大后会发糊</b><div>${esc(small.join('、'))}</div>
      <div class="t3">请上游发原图：微信发送时勾选"原图"，或者用文件方式发。</div>`]);
    const aiPh = S.data.photos.filter(x => x.ai_generated && x.code);
    if (aiPh.length) out.push(['', 'sparkles', `<b>${aiPh.length} 张是 AI 补的图：${esc(aiPh.map(x => x.code).join('、'))}</b>
      <div>图上已自动标「AI 生成」。上架前请对照实物看一眼款式、颜色、大小有没有走样；有实拍后上传同编号照片，会替换掉 AI 图。</div>`]);
    if (!S.data.photos.length) out.push(['', 'info', '<b>先在左边上传实拍图</b><div class="t3">4–8 张就够：上耳、托盘实拍、微距、尺子、白底各一张最理想。</div>']);
    return out.map(([k, ic, h]) => `<div class="notice ${k}"><i data-lucide="${ic}"></i><div class="grow">${h}</div></div>`).join('');
  }
  function fig(url, cap, dl) {
    const small = url.replace(/\?v=/, '?w=960&v=');
    return `<figure><img src="${esc(small)}" alt="" loading="lazy"><figcaption><span>${esc(cap)}</span>
      <a href="${esc(url)}" download="${esc(dl)}" title="下载这张"><i data-lucide="download"></i></a></figcaption></figure>`;
  }
  function renderStage() {
    const o = S.data.outputs || {}, v = S.view;
    let body = '';
    const empty = t => `<div class="notice"><i data-lucide="image-off"></i><div class="grow">${t}</div></div>`;
    const bust = '?v=' + encodeURIComponent(S.data.product.updated_at || '');
    if (v === 'm34' || v === 'm11') {
      const list = (v === 'm34' ? o.main34 : o.main11) || [];
      body = list.length ? `<div class="gallery g${v.slice(1)}">${list.map((u, i) => fig(u + bust, `第 ${i + 1} 张${i === 0 ? ' · 首图' : ''}`, `主图${v === 'm34' ? '3x4' : '1x1'}_${String(i + 1).padStart(2, '0')}.jpg`)).join('')}</div>`
        : empty('还没生成主图。');
    } else if (v === 'white') {
      body = o.white ? `<div class="gallery g11" style="max-width:420px">${fig(o.white + bust, '白底图 1200×1200', '白底.jpg')}</div>` : empty('还没有白底图。需要一张 A1（平放正面）照片。');
    } else if (v === 'detail') {
      const list = o.detail || [];
      body = list.length ? `<div class="fs-12 t3">共 ${list.length} 张切片，按顺序上传到详情页。手机上看就是下面这样连起来的。</div>
        <div class="longpage">${list.map(u => `<img src="${esc(u + bust)}" alt="" loading="lazy">`).join('')}</div>` : empty('还没生成详情页。');
    } else if (v === 'live') {
      body = `<div class="segmented" id="liveSeg"><button data-u="detail" class="active">详情页</button><button data-u="main">主图</button></div>
        <iframe class="frame-live" id="live" src="/render/详情页模板/模板.html?p=${id}"></iframe>
        <div class="fs-12 t3">实时预览直接读当前文案和照片，没照片的位置显示虚线框；下载用的是"生成"后的图。</div>`;
    }
    $('#stage').innerHTML = notices() + body;
    const seg = document.getElementById('liveSeg');
    if (seg) seg.querySelectorAll('button').forEach(b => b.onclick = () => {
      seg.querySelectorAll('button').forEach(x => x.classList.toggle('active', x === b));
      $('#live').src = b.dataset.u === 'main' ? `/render/主图模板/模板.html?p=${id}` : `/render/详情页模板/模板.html?p=${id}`;
    });
    icons();
  }
  S.renderStage = renderStage;

  (async () => {
    if (!id) { location.href = '/'; return; }
    if (!(await App.shell('products'))) return;
    try { S.codes = (await API.get('/api/codes')).codes || []; } catch (e) { S.codes = []; }
    S.load();
  })();
})();
