// 淘宝 / 天猫单品拆解（2026-10-21）：主图、SKU 和价格、详情图、评论（全部 / 中评 / 差评）、评价标签。
// 用法：在已登录 Chrome 的 s.taobao.com 搜索页用 javascript_tool 执行（先改 IDS 和 OUT_DATE）。
//   先在 Bash 后台跑：python -I local_receiver.py <输出文件>。抓完自动 POST 过去；搜索页允许往 127.0.0.1 发数据。
// 原理：直接调淘宝页面自带的 lib.mtop 接口，不打开商品页，所以天猫商品也能抓、只弹一次确认：
//   mtop.taobao.detail.getdetail v6.0（主图、标题、apiStack 里的 SKU 和价格）
//   mtop.taobao.detail.getdesc v6.0（详情页 HTML，从里面取图片地址）
//   mtop.taobao.rate.detaillist.get v6.0（评论，rateType '' 全部、'0' 中评、'-1' 差评；每页 20 条）
//   mtop.taobao.pcdetail.data.get 在搜索页调会 ABORT，不用。
// 每个接口之间隔 1.5–2.5 秒；遇到接口报 RGV587 / 验证码时停下等用户（见 浏览器调研方法.md）。
const IDS = [];
const OUT_DATE = '2026-10-21';
const sleep = ms => new Promise(z => setTimeout(z, ms + Math.random() * 1000));
const call = async (api, data) => { for (let t = 0; t < 60; t++) {
  try { const r = await lib.mtop.request({ api, v: '6.0', type: 'GET', dataType: 'json', data }); return r.data || {}; }
  catch (e) { const s = JSON.stringify(e); if (/RGV587|FAIL_SYS_USER_VALIDATE|验证/.test(s)) { window.__captcha = api; await sleep(10000); continue; } return { err: s.slice(0, 200) }; } }
  return { err: 'captcha timeout' }; };
const slim = l => (l || []).map(x => [x.rateType, (x.feedback || '').replace(/\s+/g, ' ').slice(0, 200), (x.skuValueStr || '').slice(0, 60), (x.feedPicPathList || []).length, (x.feedbackDate || '').slice(0, 10)]);
window.__out = {};
(async () => {
  for (const id of IDS) {
    const o = {};
    const d = await call('mtop.taobao.detail.getdetail', { itemNumId: id });
    if (d.err) o.detailErr = d.err; else {
      const it = d.item || {}; o.title = it.title; o.images = (it.images || []).map(s => s.replace(/^\/\//, 'https://'));
      o.skuProps = ((d.skuBase || {}).props || []).map(p => [p.name, (p.values || []).map(v => v.name)]);
      try { const v = JSON.parse(((d.apiStack || [])[0] || {}).value || '{}');
        o.price = ((v.price || {}).price || {}).priceText; o.sellCount = (v.item || {}).sellCount || (v.item || {}).vagueSellCount;
        const sk = (v.skuCore || {}).sku2info || {}; o.skuPrices = Object.fromEntries(Object.entries(sk).slice(0, 60).map(([k, x]) => [k, ((x.price || {}).priceText) || '']));
        if (!o.skuProps.length && v.skuBase) o.skuProps = (v.skuBase.props || []).map(p => [p.name, (p.values || []).map(x => x.name)]);
        o.skuMap = ((d.skuBase || v.skuBase || {}).skus || []).slice(0, 60).map(s => [s.skuId, s.propPath]);
        o.skuValueIds = ((d.skuBase || v.skuBase || {}).props || []).map(p => [p.pid, (p.values || []).map(x => [x.vid, x.name])]);
      } catch (e) { o.apiStackErr = String(e).slice(0, 100); }
      o.props = ((d.props || {}).groupProps || []).slice(0, 3);
    }
    await sleep(1500);
    const g = await call('mtop.taobao.detail.getdesc', { id, type: '1' });
    const html = JSON.stringify(g); o.descImgs = [...new Set((html.match(/(?:https?:)?\/\/img\.alicdn\.com\/[^"\\ ]+?\.(?:jpg|png|gif|webp)/g) || []).map(s => s.replace(/^\/\//, 'https://')))];
    await sleep(1500);
    const a = await call('mtop.taobao.rate.detaillist.get', { showTrueCount: false, auctionNumId: id, pageNo: 1, pageSize: 20, rateType: '', searchImpr: '-8', orderType: '', expression: '', rateSrc: 'pc_rate_list' });
    o.counts = { all: a.feedAllCountFuzzy || a.feedAllCount, good: a.feedGoodCountFuzzy || a.feedGoodCount, normal: a.feedNormalCountFuzzy || a.feedNormalCount, bad: a.feedBadCountFuzzy || a.feedBadCount, pic: a.feedPicCountFuzzy || a.feedPicCount, append: a.feedAppendCount };
    o.impr = a.imprItemVOS || a.imprNewItemVOS || null;
    o.rates = slim(a.rateList); await sleep(1500);
    const a2 = await call('mtop.taobao.rate.detaillist.get', { showTrueCount: false, auctionNumId: id, pageNo: 2, pageSize: 20, rateType: '', searchImpr: '-8', orderType: '', expression: '', rateSrc: 'pc_rate_list' });
    o.rates = o.rates.concat(slim(a2.rateList)); await sleep(1500);
    const m = await call('mtop.taobao.rate.detaillist.get', { showTrueCount: false, auctionNumId: id, pageNo: 1, pageSize: 20, rateType: '0', searchImpr: '-8', orderType: '', expression: '', rateSrc: 'pc_rate_list' });
    o.mid = slim(m.rateList); await sleep(1500);
    const b = await call('mtop.taobao.rate.detaillist.get', { showTrueCount: false, auctionNumId: id, pageNo: 1, pageSize: 20, rateType: '-1', searchImpr: '-8', orderType: '', expression: '', rateSrc: 'pc_rate_list' });
    o.bad = slim(b.rateList); await sleep(2000);
    window.__out[id] = o;
  }
  window.__done = 1;
  try { await fetch('http://127.0.0.1:8765/', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ date: OUT_DATE, data: window.__out }) }); window.__sent = 1; } catch (e) { window.__sent = 'fail ' + e; }
})();
'started ' + IDS.length
