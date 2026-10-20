// 淘宝搜索结果采集（带主图和商品 ID），给"参考图 + 参考定价"用（2026-10-18）。
// 和 taobao_search_scraper.js 同一套做法：在已登录 Chrome 的任意 s.taobao.com 搜索页用 javascript_tool 执行，
//   同源隐藏 iframe 依次加载每个关键词，每个词抓完存 localStorage('tbi_<词>')，全部完成后自动 POST 到 local_receiver.py。
// 先在 Bash 后台跑：timeout 900 python -I local_receiver.py <输出文件>
// 字段：[商品ID, 标题, 价格, 价格是否打码(1=用价格区间标签估算), 付款人数下限, 店名, 主图地址]
const SORT = 'sale-desc';
const KW = ['淡水珍珠耳钉 925银针', '粉色珍珠耳钉 淡水', '紫色珍珠耳钉 淡水', '珍珠碎银耳钉', '珍珠蝴蝶结耳钉', '珍珠耳夹 无耳洞', '珍珠月光石耳环', '珍珠海蓝宝耳环', '珍珠粉晶耳环', '巴洛克珍珠耳环', 'keshi珍珠耳环', '小米珠项链 淡水珍珠', '珍珠耳环项链套装', '石榴石手串'];
const sleep = ms => new Promise(r => setTimeout(r, ms));
const num = s => { const m = s.match(/([\d.]+)(万)?\+?人(付款|收货)/); if (!m) return 0; return parseFloat(m[1]) * (m[2] ? 10000 : 1); };
const full = s => (s || '').replace(/^\/\//, 'https://').replace(/_\d+x\d+[^/]*$/i, '').replace(/_\.webp$/, '').split('?')[0];
const parse = doc => [...doc.querySelectorAll('a[class*="doubleCardWrapper"]')].map(a => {
  const L = a.innerText.split('\n').map(s => s.trim()).filter(Boolean);
  const i = L.indexOf('¥'); const raw = i >= 0 ? L[i + 1] + ((L[i + 2] || '').startsWith('.') ? L[i + 2] : '') : ''; let p = NaN, m = 0;
  if (/\?/.test(raw)) { m = 1; const r = L.find(s => /^\d+-\d+元$/.test(s)); if (r) { const [x, y] = r.match(/\d+/g).map(Number); p = (x + y) / 2; } } else p = parseFloat(raw);
  const html = a.outerHTML; const idm = (a.href || '').match(/[?&]id=(\d+)/) || html.match(/[?&;]id=(\d{9,})/) || html.match(/(?:itemId|item_id|nid)["'=:\s]+(\d{9,})/);
  const img = [...a.querySelectorAll('img')].map(x => x.currentSrc || x.src || x.dataset.src || '').find(s => /bao\/uploaded|imgextra|O1CN/.test(s) && !/tps-\d+-\d+/.test(s)) || '';
  return [idm ? idm[1] : '', L[0].slice(0, 60), p, m, num(L.find(s => /人(付款|收货)/.test(s)) || ''), L[L.length - 1].slice(0, 20), full(img)];
});
localStorage.removeItem('tbi__done');
(async () => {
  for (const kw of KW) {
    if (localStorage.getItem('tbi_' + kw)) continue;
    const f = document.createElement('iframe'); f.style.cssText = 'width:1400px;height:3000px;position:absolute;left:-9999px';
    f.src = '/search?sort=' + SORT + '&q=' + encodeURIComponent(kw); document.body.appendChild(f);
    let items = []; for (let k = 0; k < 12; k++) { await sleep(1000); try { items = parse(f.contentDocument); } catch (e) { } if (items.length >= 40) break; }
    for (let y = 0; y < 12000; y += 1500) { try { f.contentWindow.scrollTo(0, y); } catch (e) { } await sleep(400); }
    await sleep(1200); try { items = parse(f.contentDocument); } catch (e) { }
    if (items.length) localStorage.setItem('tbi_' + kw, JSON.stringify(items)); f.remove(); await sleep(1500 + Math.random() * 1500);
  }
  localStorage.setItem('tbi__done', '1');
  const d = {}; Object.keys(localStorage).filter(k => k.startsWith('tbi_') && k !== 'tbi__done').forEach(k => d[k.slice(4)] = JSON.parse(localStorage.getItem(k)));
  try { await fetch('http://127.0.0.1:8765/', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ cols: ['id', 'title', 'price', 'priceMasked', 'pay', 'shop', 'img'], sort: SORT, date: new Date().toISOString().slice(0, 10), data: d }) }); localStorage.setItem('tbi__sent', '1'); } catch (e) { localStorage.setItem('tbi__sent', 'fail ' + e); }
})();
'started'
// 导出后清理：Object.keys(localStorage).filter(k=>k.startsWith('tbi_')).forEach(k=>localStorage.removeItem(k))
