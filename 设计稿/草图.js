// 详情页风格草图：同一份商品数据，5 套风格各自排版。照片用 测试照片/ 里的实拍（不提交 git）。
const DIR = '../测试照片/10-11mm强光爱迪生蝴蝶结耳钉/';
const IMG = {
  box: DIR + '微信图片_2026-10-17_235619_579.jpg',
  box2: DIR + '微信图片_2026-10-17_235629_229.jpg',
  ear: DIR + '微信图片_2026-10-17_235633_899.jpg'
};
const P = {
  name: '珠蝶', en: 'PEARL BUTTERFLY', no: 'No.03',
  one: '一对白色爱迪生淡水珍珠耳钉，配锆石蝴蝶结。日常出门或送礼都合适。',
  tags: ['淡水养殖珍珠', '锆石蝴蝶结', '实拍'],
  size: '10–11', note: '每天出门前，我会先挑一对戴上。这对珠蝶是最常被同事问链接的——珠子够大，光泽是从里面透出来的，蝴蝶结又把它压得刚刚好，不会显得太隆重。',
  cmp: [['里面', '几乎整颗珍珠层', '玻璃芯外涂一层', '贝壳粉芯外涂一层'],
        ['表面', '细小生长纹，每颗略不同', '很圆很匀很光滑', '很圆很匀很光滑'],
        ['光泽', '光从珠层里透出来', '整颗一样亮', '整颗一样亮']],
  params: [['珍珠', '淡水养殖珍珠（爱迪生）'], ['珠径', '约 10–11 mm'], ['颜色', '白色'], ['配件', '锆石蝴蝶结'], ['重量', '一对约 4 g']],
  pack: ['品牌礼盒', '保养卡', '擦拭布']
};
const img = (src) => `<div class="ph"><img src="${src}" alt=""></div>`;
const dl = rows => rows.map(([k, v]) => `<div><dt>${k}</dt><dd>${v}</dd></div>`).join('');

const STYLES = [
  {
    cls: 's1', name: '深蓝烫金', fit: '现有品牌模板升级版。适合全店默认、高客单（颈链、正圆高亮）',
    chips: ['店标配色', '宋体', '稳重高级'],
    html: () => `
      <div class="scr hero"><div class="brand">MYSTIC LUSTRE</div>${img(IMG.box2)}
        <h3>${P.name}</h3><div class="en">${P.en}</div><p class="one">${P.one}</p>
        <div class="tags">${P.tags.map(t => `<span>${t}</span>`).join('')}</div></div>
      <div class="sec cream"><div class="eyebrow">REAL FRESHWATER PEARL</div><h4>怎么分清真珍珠</h4>
        <table><tr><th></th><th class="me">淡水珍珠·本款</th><th>施家珠</th><th>贝珠</th></tr>
        ${P.cmp.map(r => `<tr><th>${r[0]}</th><td class="me">${r[1]}</td><td>${r[2]}</td><td>${r[3]}</td></tr>`).join('')}</table></div>
      <div class="sec wear"><div class="eyebrow">ON YOU</div><h4>上耳效果</h4>${img(IMG.ear)}<p class="cap">实拍上耳，未用 AI 模特</p></div>
      <div class="sec spec"><div class="eyebrow">SIZE &amp; SPEC</div><h4>尺寸参数</h4><dl>${P.params.map(([k, v]) => `<dt>${k}</dt><dd>${v}</dd>`).join('')}</dl></div>
      <div class="foot">MYSTIC LUSTRE</div>`
  },
  {
    cls: 's2', name: '奶油留白', fit: '主理人手记感，学一茉的路子。适合 69–79 元设计款耳饰、日常通勤款',
    chips: ['大留白', '细字重', '手记口吻'],
    html: () => `
      <div class="scr hero">${img(IMG.box)}
        <div class="no"><span>${P.no}</span><span>${P.size} MM</span></div>
        <h3>${P.name}</h3><p class="one">淡水养殖珍珠 · 锆石蝴蝶结</p>
        <div class="note">${P.note}<div class="sign">—— 主理人手记（示例文案，按真实情况写）</div></div></div>
      <div class="sec"><h4><small>HOW TO TELL</small>真珍珠和仿珍珠</h4>
        <div class="cmp">${P.cmp.map(r => `<div><span><b>本款 · ${r[0]}</b>${r[1]}</span><span><b>仿珍珠</b>${r[2]}</span></div>`).join('')}</div></div>
      <div class="sec"><h4><small>DETAILS</small>戴上和细节</h4>
        <div class="duo"><div>${img(IMG.ear)}<p>侧面上耳</p></div><div>${img(IMG.box2)}<p>蝴蝶结镶锆石</p></div></div></div>
      <div class="sec"><h4><small>SPEC</small>参数</h4><dl>${dl(P.params)}</dl></div>
      <div class="foot">MYSTIC LUSTRE</div>`
  },
  {
    cls: 's3', name: '杂志编辑', fit: '大字号、强对比，像时尚杂志内页。适合主推爆款、大珠、上新首发',
    chips: ['超大标题', '黑白 + 一个强调色', '数字做视觉'],
    html: () => `
      <div class="scr hero">${img(IMG.ear)}<span class="issue">ISSUE ${P.no.slice(3)} · PEARL</span>
        <h3>${P.name}</h3><div class="kicker">EDISON FRESHWATER PEARL</div><p class="one">${P.one}</p>
        <div class="big"><b>${P.size}</b><span>MM · 爱迪生淡水珍珠<br>比常见耳钉大一圈</span></div></div>
      <div class="sec dark"><h4>三点看出真珍珠</h4>
        ${P.cmp.map((r, i) => `<div class="pt"><i>0${i + 1}</i><p><b>看${r[0]}</b>真珍珠：${r[1]}；仿珍珠：${r[2]}。</p></div>`).join('')}</div>
      <div class="wear">${img(IMG.box2)}<em>"光是从里面透出来的。"</em></div>
      <div class="sec"><h4>参数</h4><table>${P.params.map(([k, v]) => `<tr><td>${k}</td><td>${v}</td></tr>`).join('')}</table></div>
      <div class="foot"><span>MYSTIC LUSTRE</span><span>${P.no}</span></div>`
  },
  {
    cls: 's4', name: '柔粉礼物', fit: '圆角卡片、贴纸、礼物感。适合七夕/情人节/生日送礼、耳夹、年轻客群',
    chips: ['圆角卡片', '贴纸标签', '突出礼盒'],
    html: () => `
      <div class="scr hero"><div class="ph"><img src="${IMG.box}" alt=""><span class="sticker">送礼不踩雷</span></div>
        <h3>${P.name} · 珍珠蝴蝶结</h3><p class="one">${P.one}</p>
        <div class="pills">${P.tags.map(t => `<span>${t}</span>`).join('')}</div></div>
      <div class="sec"><div class="card"><h4>是真珍珠哦</h4>
        <div class="vs"><div class="me"><b>本款</b>淡水珍珠<br>有生长纹<br>光从里面透</div><div><b>施家珠</b>玻璃芯<br>太圆太匀<br>镜面反光</div><div><b>贝珠</b>贝壳粉芯<br>太圆太匀<br>整颗一样亮</div></div></div></div>
      <div class="sec"><div class="gift"><img src="${IMG.box2}" alt=""><p>拆开就是这样，不用另外买礼盒</p></div>
        <div class="box">${P.pack.map(t => `<span>${t}</span>`).join('')}</div></div>
      <div class="sec"><div class="card"><h4>尺寸参数</h4><dl>${dl(P.params)}</dl></div></div>
      <div class="foot">♡ 珠光秘境 · 每一对都自己挑 ♡</div>`
  },
  {
    cls: 's5', name: '新中式红', fit: '宣纸底、竖排标题、朱红印章。适合 2027 春节和本命年红色系（石榴石、红玛瑙配珍珠）',
    chips: ['竖排标题', '朱红印章', '节日限定'],
    html: () => `
      <div class="scr hero">${img(IMG.box)}
        <div class="vt"><h3>${P.name}</h3><small>淡水珍珠 · 蝴蝶结</small><span class="seal">珠光<br>秘境</span></div>
        <p class="one">新年礼物 · 送给自己，也送给重要的人</p></div>
      <div class="sec"><h4>辨珍珠</h4>
        <table><tr><th></th><th>本款</th><th>施家珠</th><th>贝珠</th></tr>
        ${P.cmp.map(r => `<tr><th>${r[0]}</th><td class="me">${r[1]}</td><td>${r[2]}</td><td>${r[3]}</td></tr>`).join('')}</table></div>
      <div class="sec"><h4>上耳</h4></div>
      <div class="wear">${img(IMG.ear)}<p class="cap">实拍上耳</p></div>
      <div class="sec red"><h4>规格</h4><dl>${dl(P.params)}</dl></div>
      <div class="foot">珠 光 秘 境</div>`
  }
];

document.getElementById('cols').innerHTML = STYLES.map((s, i) => `
  <section class="col">
    <div class="col-head"><div class="no">风格 ${i + 1}</div><h2>${s.name}</h2><div class="fit">${s.fit}</div>
      <div class="chips">${s.chips.map(c => `<span>${c}</span>`).join('')}</div></div>
    <div class="phone ${s.cls}">${s.html()}</div>
  </section>`).join('');
