// 万相台关键词数据批量采集 v3（2026-10-17）。
// 在 Chrome（已登录千牛、已签万相台协议、广告拦截已放行 alimama）打开 one.alimama.com 的流量解析页，用 javascript_tool 执行。
// 原理：改页面 hash 切换关键词（不重载页面，只弹一次确认）→ 解析"市场数据总结" → 点「相关词推荐」解析两张相关词表 → 存 localStorage。
// 注意：万相台会把词改写（如"叠戴手链"→"手链叠戴"），所以按页面上出现的词解析，norm 字段记改写后的词。
// 输出：{kw, norm, cat, searchIdx, compIdx, bid, cvr, showIdx, wow, crowd, peak, combo:[[词,展现指数,点击率,出价]], also:[...]}
// 速度：每词约 12–20 秒。启动后用 Bash 后台 sleep 等，最后一次性导出（见 浏览器调研方法.md）。
const KW = ['珍珠耳环','珍珠耳钉'];
const sleep = ms => new Promise(r => setTimeout(r, ms));
const g = (re, t) => { const m = t.match(re); return m ? m[1] : null; };
const clickLeaf = txt => { const e = [...document.querySelectorAll('*')].find(e => e.children.length === 0 && e.textContent.trim() === txt); if (e) e.click(); return !!e; };
const parseRows = seg => { const L = seg.split('\n').map(s => s.trim()).filter(Boolean), out = [];
  for (let k = 0; k < L.length - 3; k++) if (/^[\d,]+$/.test(L[k+1]) && /%$/.test(L[k+2]) && /^¥/.test(L[k+3]) && !/^\d+$/.test(L[k])) { out.push([L[k], +L[k+1].replace(/,/g,''), L[k+2], L[k+3].slice(1)]); k += 3; }
  return out; };
localStorage.removeItem('wxt__done');
(async () => { let prev = '';
 for (const kw of KW) { if (localStorage.getItem('wxt_' + kw)) continue;
  clickLeaf('市场数据趋势'); await sleep(500);
  location.hash = '#!/insight/search/traffic-analysis/index?word=' + encodeURIComponent(kw);
  let rec = { kw, err: 'nodata' };
  for (let k = 0; k < 15; k++) { await sleep(1000); const t = document.body.innerText; const j = t.indexOf('市场数据总结');
   if (j < 0) continue; const s = t.slice(j, j + 1200); const norm = g(/“([^”]+)”的搜索指数/, s);
   if (!norm || s.slice(0, 200) === prev) continue; prev = s.slice(0, 200);
   const c = t.indexOf('选择行业类目');
   rec = { kw, norm, cat: t.slice(c, c + 200).split('\n').map(x => x.trim()).filter(Boolean)[2] || '',
     searchIdx: g(/搜索指数[^（(]{0,4}[（(](\d+)/, s), compIdx: g(/竞争指数[^（(]{0,4}[（(](\d+)/, s), bid: g(/平均出价为([\d.]+)/, s),
     cvr: g(/平均点击转化率为([\d.]+)%/, s), showIdx: g(/展现指数（[^）]*）为(\d+)/, s), wow: g(/周环比(-?[\d.]+)%/, s),
     crowd: g(/消费人群集中在([^。\n]+)/, s), peak: g(/流量高峰时段是([\d:\-]+)/, s) };
   break; }
  if (rec.norm && clickLeaf('相关词推荐')) {
   // 必须等表头出现"搜索“<当前词>”的消费者"，否则会读到上一个词的旧表（v2 踩过这个坑，48 个词里 12 个串页）
   const tag = '搜索“' + rec.norm + '”的消费者同时会组合搜的词';
   for (let k = 0; k < 12; k++) { await sleep(1000); const t = document.body.innerText; const a = t.indexOf(tag), b = t.indexOf('还会搜的词', a + tag.length);
    if (a > 0 && b > 0) { rec.combo = parseRows(t.slice(a, b)); rec.also = parseRows(t.slice(b, b + 4000)); if (rec.combo.length || rec.also.length) break; } } }
  localStorage.setItem('wxt_' + kw, JSON.stringify(rec)); await sleep(1000 + Math.random() * 1000); }
 localStorage.setItem('wxt__done', '1'); })();
'started ' + KW.length
