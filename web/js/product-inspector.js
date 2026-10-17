// 款编辑页右侧面板：照片（编号、旋转、白底）、文案（基础信息和详情页文字）、标题规格。
(function () {
  'use strict';
  const { API, esc, icons, toast, drawer } = App;
  const S = window.S;
  const $ = s => document.querySelector(s);

  document.querySelectorAll('#itab button').forEach(b => b.onclick = () => { S.tab = b.dataset.t; S.renderInspector(); });

  S.renderInspector = function () {
    if (!S.data) return;
    document.querySelectorAll('#itab button').forEach(b => b.classList.toggle('active', b.dataset.t === S.tab));
    ({ photo: photoTab, copy: copyTab, listing: listingTab })[S.tab]();
    icons();
  };

  // 淘宝标题按字符算：汉字 2，英文数字 1，最多 60
  const tbLen = s => [...(s || '')].reduce((n, c) => n + (/[\x00-\xff]/.test(c) ? 1 : 2), 0);

  // ================= 照片 =================
  function photoTab() {
    const ph = S.data.photos.find(x => x.id === S.sel);
    const foot = $('#ifoot');
    if (!ph) {
      $('#ibody').innerHTML = `<div class="inspector-section"><div class="eyebrow">照片</div>
        <div class="fs-12 t3">点左边的照片，可以改它的编号、方向和要不要做白底图。</div>
        <div class="fs-12 t3">编号决定照片放在主图和详情页的哪个位置；AI 分拣完会自动填，填错了在这里改。</div></div>
        ${coverageHtml()}`;
      foot.innerHTML = '';
      return;
    }
    const m = ph.metrics || {};
    const base = (ph.code || '').split('_')[0];
    const suffix = (ph.code || '').includes('_') ? ph.code.slice(base.length + 1) : '';
    const crop = ph.crop;
    const groups = {};
    S.codes.forEach(c => (groups[c.group] = groups[c.group] || []).push(c));
    const opts = '<option value="">不用这张</option>' + Object.entries(groups).map(([g, cs]) =>
      `<optgroup label="${esc(g)}">${cs.map(c => `<option value="${esc(c.code)}"${c.code === base ? ' selected' : ''}>${esc(c.code)} · ${esc(c.name)}</option>`).join('')}</optgroup>`).join('');
    const needSuffix = c => c === 'A3' || c === 'C4';
    $('#ibody').innerHTML = `
      <div class="inspector-section">
        <div class="photo-preview"><img src="/files/products/${S.id}/${esc(ph.file)}?w=960" alt="" style="transform:rotate(${ph.rotate || 0}deg)">
          ${crop ? `<span class="crop-box" style="left:${crop[0] * 100}%;top:${crop[1] * 100}%;width:${crop[2] * 100}%;height:${crop[3] * 100}%" title="AI 建议的裁剪范围"></span>` : ''}</div>
        <div class="fs-12 t3 ellipsis" title="${esc(ph.orig_name)}">${esc(ph.orig_name)} · <span class="mono">${ph.width}×${ph.height}</span></div>
        ${ph.ai_generated ? '<div class="fs-12" style="color:var(--purple-text)"><b>AI 生成的补图</b>，不是实拍。上架前请对照实物核对外观。</div>' : ''}
        ${ph.ai_enhanced ? '<div class="fs-12 t2">这张实拍图太小，已做 AI 高清化。改裁剪或方向后会重新高清化。</div>' : ''}
        ${ph.ai_note ? `<div class="fs-12 t2"><i data-lucide="sparkles" style="width:12px;height:12px;vertical-align:-1px;color:var(--primary)"></i> ${esc(ph.ai_note)}</div>` : ''}
      </div>
      <div class="inspector-section">
        <div class="eyebrow">编号</div>
        <select class="input" id="codeSel">${opts}</select>
        <div id="sufWrap" class="${needSuffix(base) ? '' : 'hidden'}"><label class="label" id="sufLabel">${base === 'C4' ? '尺寸（如 8mm）' : '规格（如 白）'}</label><input class="input" id="suf" value="${esc(suffix)}"></div>
        <div class="fs-12 t3">同一个编号只能给一张图。改过的编号 AI 不会再覆盖。</div>
      </div>
      <div class="inspector-section">
        <div class="eyebrow">修图</div>
        <div><label class="label">方向</label><div class="segmented" id="rot">${[0, 90, 180, 270].map(r => `<button data-r="${r}" class="${(ph.rotate || 0) === r ? 'active' : ''}">${r}°</button>`).join('')}</div></div>
        <div class="setting"><div class="grow"><div class="title">做白底图</div><div class="desc">抠成纯白背景，用作白底图和规格图</div></div><input type="checkbox" class="switch" id="wb"${ph.white_bg ? ' checked' : ''}></div>
      </div>
      <div class="inspector-section">
        <div class="eyebrow">照片质量</div>
        <div class="kv">
          <span class="k">清晰度</span><span class="v">${esc(m.blur_level || '—')}</span>
          <span class="k">高光死白</span><span class="v">${esc(m.clip_level || '—')}</span>
          <span class="k">偏色</span><span class="v">${esc(m.cast_note || '—')}</span>
          <span class="k">重复</span><span class="v">${m.dup_of ? '和 #' + esc(m.dup_of) + ' 很像' : '无'}</span>
          <span class="k">定位信息</span><span class="v">${m.has_gps ? '有（导出时会去掉）' : '无'}</span>
        </div>
      </div>
      <div class="inspector-section"><button class="btn btn-ghost btn-sm" id="delPh" style="align-self:flex-start;color:var(--danger-text)"><i data-lucide="trash-2"></i>删除这张</button></div>`;
    foot.innerHTML = `<button class="btn btn-secondary btn-block" id="savePh"><i data-lucide="check"></i>保存，从修图开始重新生成</button>`;

    let rot = ph.rotate || 0;
    $('#codeSel').onchange = e => {
      const c = e.target.value;
      $('#sufWrap').classList.toggle('hidden', !needSuffix(c));
      $('#sufLabel').textContent = c === 'C4' ? '尺寸（如 8mm）' : '规格（如 白）';
      if (/^A[123]$/.test(c)) $('#wb').checked = true;
    };
    document.querySelectorAll('#rot button').forEach(b => b.onclick = () => {
      rot = +b.dataset.r;
      document.querySelectorAll('#rot button').forEach(x => x.classList.toggle('active', x === b));
      document.querySelector('.photo-preview img').style.transform = `rotate(${rot}deg)`;
    });
    $('#delPh').onclick = async () => {
      if (!confirm('删除这张照片？')) return;
      try { await API.del('/api/photos/' + ph.id); S.sel = null; S.markDirty(); S.load(); } catch (e) { toast(e.message, 'error'); }
    };
    $('#savePh').onclick = async () => {
      let code = $('#codeSel').value;
      if (needSuffix(code)) {
        const s = $('#suf').value.trim().replace(/[\s_/\\]+/g, '');
        if (!s) { toast(code === 'C4' ? '填一下尺寸' : '填一下规格名', 'error'); return; }
        code += '_' + s;
      }
      try {
        await API.patch('/api/photos/' + ph.id, { code: code || null, rotate: rot, white_bg: $('#wb').checked });
        await S.run('from=retouch');
      } catch (e) { toast(e.message, 'error'); }
    };
  }

  function coverageHtml() {
    const cov = S.data.coverage || {};
    const row = x => `<div class="list-item" style="display:flex;gap:8px;align-items:center;font-size:12px;padding:4px 0">
      <span class="grow">${esc(x.name)}</span>${x.hit ? `${x.ai ? '<span class="pill pill-purple">AI 补</span>' : ''}<span class="mono t2">${esc(x.hit)}</span>` : `<span class="pill ${x.required ? 'pill-danger' : 'pill-neutral'}">${x.required ? '缺，要补拍' : '没有'}</span>`}</div>`;
    if (!cov.main) return '';
    return `<div class="inspector-section"><div class="eyebrow">主图位置</div>${(cov.main || []).map(row).join('')}</div>
      <div class="inspector-section"><div class="eyebrow">详情页各屏</div>${(cov.detail || []).map(row).join('')}</div>`;
  }

  // ================= 文案 =================
  function copyTab() {
    const p = S.data.product, info = p.info || {}, c = p.content || {};
    const field = (k, label, v, ph) => `<div><label class="label">${label}</label><input class="input" data-i="${k}" value="${esc(v == null ? '' : v)}" placeholder="${esc(ph || '')}"></div>`;
    $('#ibody').innerHTML = `
      <div class="inspector-section">
        <div class="eyebrow">基础信息</div>
        ${field('name', '款名', p.name)}
        <div class="form-row">${field('category', '品类', info.category)}${field('series', '系列', info.series)}</div>
        <div class="form-row">${field('size', '珍珠尺寸', info.size, '7–8 mm')}${field('price', '售价', info.price)}</div>
        ${field('colors', '颜色 / 规格', (info.colors || []).join('、'))}
        ${field('metal', '配件', info.metal)}
        <div><label class="label">亮点和备注</label><textarea class="textarea" data-i="highlights" rows="2">${esc(info.highlights || '')}</textarea></div>
        <div class="setting"><div class="grow"><div class="title">缺图用 AI 补</div><div class="desc">白底、场景、上耳效果图缺了就生成，图上标「AI 生成」。尺寸、托盘、微距、包装不补</div></div><input type="checkbox" class="switch" data-sw="auto_fill"${info.auto_fill === false ? '' : ' checked'}></div>
        <div class="setting"><div class="grow"><div class="title">小图 AI 高清化</div><div class="desc">照片太小会发糊时自动高清化</div></div><input type="checkbox" class="switch" data-sw="auto_upscale"${info.auto_upscale === false ? '' : ' checked'}></div>
        <div class="fs-12 t3">改基础信息后，要"重新写文案"才会用上；两个开关在下次"重新生成"时生效。</div>
      </div>
      ${c.name ? `<div class="inspector-section">
        <div class="head"><span class="eyebrow">详情页文字</span><button class="btn btn-ghost btn-sm" id="jsonBtn"><i data-lucide="braces"></i>全部字段</button></div>
        <div class="form-row"><div><label class="label">款名</label><input class="input" data-c="name" value="${esc(c.name)}"></div><div><label class="label">英文名</label><input class="input" data-c="nameEn" value="${esc(c.nameEn || '')}"></div></div>
        <div><label class="label">一句话</label><textarea class="textarea" data-c="oneLiner" rows="3">${esc(c.oneLiner || '')}</textarea></div>
        <div><label class="label">首屏标签（顿号分开）</label><input class="input" data-c="tags" value="${esc((c.tags || []).join('、'))}"></div>
        ${(c.details || []).map((d, i) => `<div><label class="label">细节 ${i + 1} · ${esc(d.title)}</label><textarea class="textarea" data-d="${i}" rows="2">${esc(d.text)}</textarea></div>`).join('')}
        <div><label class="label">尺寸参数</label><div class="list-edit">${(c.params || []).map((r, i) => `<div class="li"><input class="input" style="flex:0 0 84px" data-pk="${i}" value="${esc(r[0])}"><input class="input" data-pv="${i}" value="${esc(r[1])}"></div>`).join('')}</div></div>
      </div>` : `<div class="inspector-section"><div class="fs-12 t3">还没生成文案。点上方"一键生成"后，这里可以逐条改。</div></div>`}`;
    $('#ifoot').innerHTML = `<div class="row" style="gap:8px"><button class="btn btn-secondary grow" id="reCopy"><i data-lucide="sparkles"></i>重新写文案</button>${c.name ? '<button class="btn btn-secondary grow" id="saveCopy"><i data-lucide="check"></i>保存并出图</button>' : ''}</div>`;

    async function saveInfo() {
      const v = k => (document.querySelector(`[data-i="${k}"]`) || {}).value;
      const ni = Object.assign({}, info, {
        category: v('category'), series: v('series'), size: v('size'), metal: v('metal'), highlights: v('highlights'),
        price: v('price') ? Number(v('price')) : null, colors: (v('colors') || '').split(/[、,，\s]+/).filter(Boolean),
      });
      document.querySelectorAll('[data-sw]').forEach(el => { ni[el.dataset.sw] = el.checked; });
      await API.patch('/api/products/' + S.id, { name: v('name').trim() || p.name, info: ni });
    }
    $('#reCopy').onclick = async () => {
      if (c.name && !confirm('重新写文案会覆盖右边改过的文字，继续？')) return;
      try { await saveInfo(); await S.run('from=copy'); } catch (e) { toast(e.message, 'error'); }
    };
    const sc = $('#saveCopy');
    if (sc) sc.onclick = async () => {
      const nc = JSON.parse(JSON.stringify(c));
      document.querySelectorAll('[data-c]').forEach(el => {
        nc[el.dataset.c] = el.dataset.c === 'tags' ? el.value.split(/[、,，]+/).map(s => s.trim()).filter(Boolean) : el.value;
      });
      document.querySelectorAll('[data-d]').forEach(el => { nc.details[+el.dataset.d].text = el.value; });
      document.querySelectorAll('[data-pk]').forEach(el => { nc.params[+el.dataset.pk][0] = el.value; });
      document.querySelectorAll('[data-pv]').forEach(el => { nc.params[+el.dataset.pv][1] = el.value; });
      try { await saveInfo(); await API.patch('/api/products/' + S.id, { content: nc }); await S.run('from=render'); }
      catch (e) { toast(e.message, 'error'); }
    };
    const jb = $('#jsonBtn');
    if (jb) jb.onclick = () => jsonDrawer();
  }

  // 全部字段：直接改 content / main 的 JSON（给熟手用）
  function jsonDrawer() {
    const p = S.data.product;
    drawer(`<div class="drawer-head">全部字段<span class="fs-12 t3" style="font-weight:400">详情页和主图的原始数据</span><button class="close-btn" data-close><i data-lucide="x"></i></button></div>
      <div class="drawer-body">
        <div class="segmented" id="jseg"><button data-k="content" class="active">详情页</button><button data-k="main">主图</button></div>
        <textarea class="textarea json-edit" id="jtxt" spellcheck="false"></textarea>
        <div class="fs-12 t3">照片字段 f 只能写左边已有的编号文件名，如 B1.jpg。没有照片的位置写 null。</div>
      </div>
      <div class="drawer-foot"><button class="btn btn-secondary" data-close>取消</button><button class="btn btn-primary" id="jsave"><i data-lucide="check"></i>保存并出图</button></div>`,
      (el, close) => {
        el.style.width = '560px';
        const buf = { content: JSON.stringify(p.content || {}, null, 2), main: JSON.stringify(p.main || {}, null, 2) };
        let k = 'content';
        const t = el.querySelector('#jtxt'); t.value = buf[k];
        el.querySelectorAll('#jseg button').forEach(b => b.onclick = () => {
          buf[k] = t.value; k = b.dataset.k; t.value = buf[k];
          el.querySelectorAll('#jseg button').forEach(x => x.classList.toggle('active', x === b));
        });
        el.querySelector('#jsave').onclick = async () => {
          buf[k] = t.value;
          let body;
          try { body = { content: JSON.parse(buf.content), main: JSON.parse(buf.main) }; }
          catch (e) { toast('JSON 格式不对：' + e.message, 'error'); return; }
          try { await API.patch('/api/products/' + S.id, body); close(); await S.run('from=render'); }
          catch (e) { toast(e.message, 'error'); }
        };
      });
  }

  // ================= 标题规格 =================
  function listingTab() {
    const L = S.data.product.listing || {};
    const copyBtn = t => `<button class="btn btn-ghost btn-icon btn-sm" data-copy="${esc(t)}" title="复制"><i data-lucide="copy"></i></button>`;
    const titleRow = (t, i) => { const n = tbLen(t); return `<div class="li" style="align-items:center"><input class="input" data-t="${i}" value="${esc(t)}"><span class="charcount${n > 60 ? ' over' : ''}" data-n="${i}">${n}/60</span>${copyBtn(t)}</div>`; };
    if (!L.title) {
      $('#ibody').innerHTML = `<div class="inspector-section"><div class="fs-12 t3">还没生成标题和规格。点上方"一键生成"。</div></div>`;
      $('#ifoot').innerHTML = ''; return;
    }
    const comp = L.compliance || [];
    $('#ibody').innerHTML = `
      <div class="inspector-section"><div class="eyebrow">标题</div>
        <div class="list-edit">${[L.title].concat(L.title_alts || []).map(titleRow).join('')}</div>
        <div class="fs-12 t3">第一条是推荐的。淘宝按字符算：汉字算 2 个，最多 60。</div></div>
      <div class="inspector-section"><div class="eyebrow">规格（SKU）</div>
        <table class="table" style="font-size:12px"><thead><tr><th>规格名</th><th class="num">价格</th></tr></thead>
        <tbody>${(L.sku || []).map(s => `<tr><td>${esc(s.name)}</td><td class="num mono">${s.price == null ? '—' : '¥' + esc(s.price)}</td></tr>`).join('')}</tbody></table></div>
      <div class="inspector-section"><div class="head"><span class="eyebrow">属性</span>${copyBtn((L.attrs || []).map(a => a.join('：')).join('\n'))}</div>
        <div class="kv">${(L.attrs || []).map(a => `<span class="k">${esc(a[0])}</span><span class="v">${esc(a[1])}</span>`).join('')}</div></div>
      <div class="inspector-section"><div class="eyebrow">合规检查</div>
        <div class="compliance">${comp.length ? comp.map(c => `<div class="c ${esc(c.level)}"><i data-lucide="${c.level === 'error' ? 'x-circle' : 'alert-triangle'}"></i><span>${esc(c.text)}</span></div>`).join('')
          : '<div class="c"><i data-lucide="check-circle-2" style="color:var(--success)"></i><span>没发现问题。上架前你再过一眼。</span></div>'}</div></div>`;
    $('#ifoot').innerHTML = `<button class="btn btn-secondary btn-block" id="saveTitle"><i data-lucide="check"></i>保存标题</button>`;
    document.querySelectorAll('[data-t]').forEach(el => el.oninput = () => {
      const n = tbLen(el.value), c = document.querySelector(`[data-n="${el.dataset.t}"]`);
      c.textContent = n + '/60'; c.classList.toggle('over', n > 60);
    });
    document.querySelectorAll('[data-copy]').forEach(b => b.onclick = async () => {
      const row = b.closest('.li'); const txt = row ? row.querySelector('input').value : b.dataset.copy;
      try { await navigator.clipboard.writeText(txt); toast('已复制', 'ok'); } catch (e) { toast('复制失败，手动选中复制', 'error'); }
    });
    $('#saveTitle').onclick = async () => {
      const ts = [...document.querySelectorAll('[data-t]')].map(el => el.value.trim());
      try { await API.patch('/api/products/' + S.id, { listing: Object.assign({}, L, { title: ts[0], title_alts: ts.slice(1) }) }); toast('已保存', 'ok'); S.load(); }
      catch (e) { toast(e.message, 'error'); }
    };
  }
})();
