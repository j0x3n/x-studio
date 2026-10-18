// 详情页模板公共引擎：所有风格共用。负责读商品数据、照片位、导出模式、9 屏的顺序和默认排版。
// 每套风格的 html 只写自己的 CSS，需要不同排版的屏再用 XS.run({ screens: { hero: fn, … } }) 覆盖。
//
// 用法：<风格>.html?p=<款>          预览整页（缺照片显示虚线框，提示要拍什么）
//       <风格>.html?p=<款>&s=<屏号> 只出一屏（导出用：底色纯绿 #00FF00，裁掉；缺照片的位自动收起，照片全缺的屏不出）
// 数据：商品/<款>/内容.js 里的 window.PRODUCT，格式见 商品/示例款/内容.js。所有风格吃同一份数据。
var XS = (function () {
  var q = new URLSearchParams(location.search);
  var slug = q.get('p') || '示例款';
  var only = q.get('s');
  var EXPORT = !!only;
  var base = '商品/' + slug + '/';
  var opts = null, data = null;
  if (EXPORT) document.documentElement.classList.add('export');

  function esc(t) { return String(t == null ? '' : t).replace(/[&<>"]/g, function (c) { return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]; }); }
  function br(t) { return esc(t).replace(/\n/g, '<br>'); }

  // 照片位。p: {f:'B1.jpg', need:'要拍什么', stamp:'角落小字'}；overlay 叠在图上（如 mm 标签）
  function ph(p, cls, overlay) {
    p = p || {};
    if (EXPORT && !p.f) return '';
    var need = (p.f ? p.f.replace(/\.\w+$/, '') + '  ' : '') + (p.need || '');
    var img = p.f ? '<img src="' + esc(base + '照片/' + p.f) + '" onerror="XS.miss(this)">' : '';
    var stamp = p.stamp ? '<span class="stamp">' + esc(p.stamp) + '</span>' : '';
    return '<div class="ph ' + (cls || '') + (p.f ? '' : ' missing') + '" data-need="放照片\n' + esc(need) + '">' + img + stamp + (overlay || '') + '</div>';
  }
  // "照片 + 说明"单元；导出时照片缺了整个单元一起收起
  function unit(phHtml, rest, cls) {
    if (!phHtml) return '';
    return '<div class="unit ' + (cls || '') + '">' + phHtml + (rest || '') + '</div>';
  }
  function miss(img) {
    var box = img.parentNode;
    img.remove();
    if (!EXPORT) { box.classList.add('missing'); return; }
    var u = box.closest('.unit');
    (u || box).remove();
    prune();
  }
  function prune() {
    if (!EXPORT) return;
    document.querySelectorAll('.grid2,.prune-empty').forEach(function (g) { if (!g.querySelector('.ph')) g.remove(); });
    document.querySelectorAll('section[data-need]').forEach(function (sec) { if (!sec.querySelector('.ph')) sec.remove(); });
  }
  // 屏标题：英文小字 + 中文标题 + 引导语
  function head(eb, title, lead, center) {
    return '<div class="hd' + (center ? ' center' : '') + '"><div class="eyebrow">' + esc(eb) + '</div><div class="h2">' + esc(title) + '</div>' +
      (lead ? '<div class="lead">' + br(lead) + '</div>' : '') + '<div class="rule"></div></div>';
  }
  function list(arr, fn) { return (arr || []).map(fn).join(''); }
  function params(P) {
    return '<div class="params">' + list(P.params, function (r) { return '<div class="row"><div class="k">' + esc(r[0]) + '</div><div class="v">' + esc(r[1]) + '</div></div>'; }) + '</div>';
  }
  // 尺寸图：多个尺寸 → 同一只耳朵戴各尺寸的小图（标 mm）；只有一个尺寸 → 标尺图。导出时两种都放，CSS 藏标尺图，小图全缺时标尺图自动露出
  function sizePics(P) {
    var sizes = (P.sizeWear || []).filter(function (x) { return x && (x.f || !EXPORT); });
    var grid = sizes.length > 1 ? '<div class="grid2 sizes">' + list(sizes, function (x) { return unit(ph(x, '', '<span class="mm">' + esc(x.mm) + '</span>')); }) + '</div>' : '';
    var ruler = unit(ph(P.photos.size), '', 'ruler');
    return grid && !EXPORT ? grid : grid + ruler;
  }
  function cmpTable(R) {
    return '<div class="cmp"><div class="row head"><div></div>' +
      list(R.cols, function (c, i) { return '<div class="' + (i ? '' : 'ours') + '">' + br(c) + '</div>'; }) + '</div>' +
      list(R.rows, function (r) {
        return '<div class="row"><div class="k">' + esc(r[0]) + '</div>' +
          r.slice(1).map(function (v, i) { return '<div class="' + (i ? 'fake' : 'ours') + '">' + esc(v) + '</div>'; }).join('') + '</div>';
      }) + '</div>';
  }

  // 9 屏的默认排版。tone：dark 深色屏 / alt 浅色屏 / base 底色屏，颜色由各风格的 CSS 决定
  var screens = {
    hero: function (P) {
      return '<section class="screen dark s-hero">' +
        '<div class="top"><div class="eyebrow">Mystic Lustre</div><div class="series">' + esc(P.series) + '</div></div>' +
        unit(ph(P.photos.hero), '', 'frame') +
        '<div class="center"><div class="name">' + esc(P.name) + '</div><div class="en">' + esc(P.nameEn) + '</div></div>' +
        '<div class="one center">' + br(P.oneLiner) + '</div>' +
        '<div class="tags">' + list(P.tags, function (t) { return '<span>' + esc(t) + '</span>'; }) + '</div></section>';
    },
    real: function (P) {
      var R = P.real, tray = P.photos.tray || {};
      return '<section class="screen alt pad s-real">' + head(R.kicker || 'Real Freshwater Pearl', R.title || '怎么分清真珍珠', R.lead, true) +
        unit(ph(tray), tray.cap ? '<div class="cap">' + esc(tray.cap) + '</div>' : '', 'tray') +
        cmpTable(R) + (R.note ? '<div class="note">' + esc(R.note) + '</div>' : '') + '</section>';
    },
    details: function (P) {
      return '<section class="screen base pad s-details" data-need>' + head('Details', '细节', P.detailsLead) +
        list(P.details, function (d, i) {
          return unit(ph(d.photo), '<div class="txt"><div class="no">0' + (i + 1) + '</div><div><div class="t">' + esc(d.title) + '</div><div class="d">' + esc(d.text) + '</div></div></div>', 'detail');
        }) + '</section>';
    },
    wear: function (P) {
      var W = P.wear;
      var small = list([W.a, W.b].filter(Boolean), function (p) { return unit(ph(p), p.cap ? '<div class="fig-cap">' + esc(p.cap) + '</div>' : ''); });
      return '<section class="screen base2 pad s-wear" data-need>' + head('On You', '上耳效果', W.lead) +
        unit(ph(W.big), '', 'big') + (small ? '<div class="grid2">' + small + '</div>' : '') + '</section>';
    },
    size: function (P) {
      return '<section class="screen base pad s-size">' + head('Size & Spec', '尺寸参数', P.sizeLead) +
        sizePics(P) + params(P) + '<div class="tip">' + esc(P.sizeTip) + '</div></section>';
    },
    related: function (P) {
      if (!P.related || !P.related.length) return '';
      return '<section class="screen alt pad s-related" data-need>' + head('The Collection', P.series + ' · 同系列', P.relatedLead) +
        '<div class="series-grid">' + list(P.related, function (r) {
          return unit(ph(r.photo), '<div class="n">' + esc(r.name) + '</div><div class="x">' + esc(r.text) + '</div>', 'card');
        }) + '</div></section>';
    },
    pack: function (P) {
      return '<section class="screen dark pad s-pack" data-need>' + head('Gift Ready', '包装', P.pack.lead, true) +
        unit(ph(P.photos.pack)) + '<div class="items">' + list(P.pack.items, function (t) { return '<div>' + esc(t) + '</div>'; }) + '</div></section>';
    },
    care: function (P) {
      var ol = function (a) { return '<ol>' + list(a, function (t) { return '<li><span>' + esc(t) + '</span></li>'; }) + '</ol>'; };
      return '<section class="screen base2 pad s-care">' + head('Care & Service', '保养与售后') +
        '<div class="block"><div class="bt">' + (P.careTitle || '珍珠怎么保养') + '</div>' + ol(P.care) + '</div>' +
        '<div class="block"><div class="bt">发货与售后</div>' + ol(P.service) + '</div></section>';
    },
    about: function (P) {
      return '<section class="screen dark pad s-about">' +
        '<img class="logo" src="../店铺/店标/矢量标志/' + esc(opts.logo || '主标志_透明底金字_2400.png') + '" alt="">' +
        '<div class="txt">' + (P.about || []).map(esc).join('<br>') + '</div><div class="sign">MYSTIC LUSTRE</div></section>';
    }
  };
  var ORDER = ['hero', 'real', 'details', 'wear', 'size', 'related', 'pack', 'care', 'about'];

  function render() {
    if (!opts || !data) return;
    var fns = Object.assign({}, screens, opts.screens || {});
    var S = ORDER.map(function (k) { return fns[k](data, H); });
    document.getElementById('page').innerHTML = only ? (S[+only - 1] || '') : S.join('');
    prune();
    document.title = data.name + ' · 详情页';
    if (opts.after) opts.after(data, H);
  }
  var H = { esc: esc, br: br, ph: ph, unit: unit, head: head, list: list, params: params, sizePics: sizePics, cmpTable: cmpTable, EXPORT: EXPORT, screens: screens };

  var s = document.createElement('script');
  s.src = base + '内容.js';
  s.onload = function () { data = window.PRODUCT; render(); };
  s.onerror = function () { document.getElementById('page').textContent = '找不到 ' + base + '内容.js'; };
  document.head.appendChild(s);

  return {
    miss: miss,
    run: function (o) { opts = o || {}; render(); }
  };
})();
