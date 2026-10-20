// ==UserScript==
// @name         X Studio 采集助手
// @namespace    https://x-studio.local/collector
// @version      1.0.0
// @description  把当前页的淘宝搜索、淘宝商品、万相台关键词或生意参谋商品数据发到 X Studio
// @match        *://*.taobao.com/*
// @match        *://*.tmall.com/*
// @match        *://*.alimama.com/*
// @run-at       document-idle
// @noframes
// @grant        GM_setValue
// @grant        GM_getValue
// @grant        GM_xmlhttpRequest
// @grant        GM_registerMenuCommand
// @grant        unsafeWindow
// @connect      *
// ==/UserScript==

(function () {
  'use strict';

  const KEYS = { base: 'xs_base', token: 'xs_token', shop: 'xs_shop', category: 'xs_category' };

  function sleep(ms) {
    return new Promise(function (resolve) { setTimeout(resolve, ms); });
  }

  function clip(value, max) {
    if (value == null) return null;
    const text = String(value).replace(/\s+/g, ' ').trim();
    if (!text) return null;
    return Array.from(text).slice(0, max).join('');
  }

  function httpsURL(raw) {
    if (!raw) return null;
    let url = String(raw).trim().replace(/^\/\//, 'https://');
    url = url.replace(/_\d+x\d+[^/]*$/i, '').replace(/_\.webp$/i, '').split('?')[0];
    if (!/^https?:\/\//i.test(url)) return null;
    return url;
  }

  function finiteNumber(value) {
    const n = Number(value);
    if (!Number.isFinite(n) || n < 0) return null;
    return n;
  }

  function parseCount(raw) {
    if (raw == null) return null;
    const text = String(raw).replace(/,/g, '').replace(/\s/g, '').trim();
    if (!text || text === '-' || text === '--' || text === '—') return null;
    const matched = text.match(/^(\d+(?:\.\d+)?)(万)?/);
    if (!matched) return null;
    let n = Number(matched[1]);
    if (!Number.isFinite(n) || n < 0) return null;
    if (matched[2]) n *= 10000;
    return Math.round(n);
  }

  function parseMoney(raw) {
    if (raw == null) return null;
    const text = String(raw).replace(/,/g, '').replace(/元/g, '').replace(/\s/g, '').trim();
    if (!text || text === '-' || text === '--' || text === '—') return null;
    const matched = text.match(/^(\d+(?:\.\d+)?)(万)?/);
    if (!matched) return null;
    let n = Number(matched[1]);
    if (!Number.isFinite(n) || n < 0) return null;
    if (matched[2]) n *= 10000;
    return n;
  }

  function parsePrice(raw) {
    if (raw == null) return null;
    const text = String(raw).replace(/,/g, '').trim();
    if (!text || /\?/.test(text)) return null;
    const matched = text.match(/(\d+(?:\.\d+)?)/);
    if (!matched) return null;
    return finiteNumber(matched[1]);
  }

  function parsePercent(raw) {
    if (raw == null) return null;
    const matched = String(raw).replace(/,/g, '').match(/(\d+(?:\.\d+)?)\s*%?/);
    if (!matched) return null;
    const n = Number(matched[1]);
    if (!Number.isFinite(n) || n < 0 || n > 100) return null;
    return n;
  }

  function parseDay(raw) {
    if (raw == null) return null;
    const text = String(raw).trim();
    if (!text) return null;
    const dated = text.matchAll(/(20\d{2})[-/.年](\d{1,2})[-/.月](\d{1,2})/g);
    const found = Array.from(dated);
    if (found.length > 1) return null;
    if (found.length === 1) {
      return found[0][1] + '-' + found[0][2].padStart(2, '0') + '-' + found[0][3].padStart(2, '0');
    }
    const compact = text.match(/^(20\d{2})(\d{2})(\d{2})$/);
    if (!compact) return null;
    return compact[1] + '-' + compact[2] + '-' + compact[3];
  }

  function cfg() {
    return {
      base: String(GM_getValue(KEYS.base, '') || '').trim().replace(/\/+$/, ''),
      token: String(GM_getValue(KEYS.token, '') || '').trim().replace(/^Bearer\s+/i, ''),
      shop: String(GM_getValue(KEYS.shop, '') || '').trim(),
      category: String(GM_getValue(KEYS.category, '') || '').trim()
    };
  }

  function shopID(value) {
    if (!/^[1-9]\d*$/.test(value)) return null;
    const n = Number(value);
    if (!Number.isSafeInteger(n)) return null;
    return n;
  }

  function settingsError(current) {
    if (!/^https?:\/\//i.test(current.base)) return '先在脚本菜单的「采集设置」里填写 X Studio 地址，以 http:// 或 https:// 开头。';
    if (!current.token) return '先在脚本菜单的「采集设置」里填写个人令牌。';
    if (shopID(current.shop) == null) return '店铺 id 要填一个正整数。';
    return '';
  }

  function pageKind() {
    const host = location.hostname;
    const path = location.pathname || '';
    if (host === 'sycm.taobao.com' || host === 'sycm.tmall.com') return 'my_listing';
    if (host === 'one.alimama.com' || host.endsWith('.alimama.com')) return 'keyword';
    if (host === 's.taobao.com' && /\/search/.test(path)) return 'search';
    if (host === 'item.taobao.com' || host === 'detail.tmall.com' || /\/item\.html?/.test(path)) return 'item';
    return '';
  }

  function blockedReason() {
    const href = location.href;
    if (/login\.taobao\.com|login\.tmall\.com|login\.alibaba\.com|passport\.|havanaone\/login|\/member\/login/i.test(href)) {
      return '这是登录页。请先自己登录，脚本不会帮你填账号。';
    }
    if (/punish|captcha|verify\.|_____tmd_____|x5secdata/i.test(href)) {
      return '这是验证页面。请你自己处理完再点，脚本不会去点验证码。';
    }
    if (document.querySelector('#nocaptcha, .nc-container, .baxia-dialog, #baxia-dialog, iframe[src*="punish"], iframe[src*="captcha"]')) {
      return '页面上有验证码。请你自己处理完再点，脚本不会去点验证码。';
    }
    if (/验证码|安全验证|滑动验证|被挤爆|哎哟喂/.test(document.title || '')) {
      return '页面标题像是验证。请你自己处理完再点，脚本不会去点验证码。';
    }
    return '';
  }

  function sourceURL() {
    const url = new URL(location.href);
    ['token', 'tb_token', '_tb_token_', 'password', 'cookie', 'access_token', 'sec', 'x5secdata'].forEach(function (key) {
      url.searchParams.delete(key);
    });
    const keepHash = /alimama\.com$/.test(url.hostname) || url.hostname.indexOf('sycm.') === 0;
    return url.origin + url.pathname + url.search + (keepHash ? url.hash : '');
  }

  function nowUTC() {
    return new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
  }

  function queryValue(names) {
    const url = new URL(location.href);
    for (let i = 0; i < names.length; i++) {
      const value = url.searchParams.get(names[i]);
      if (value && value.trim()) return value.trim();
    }
    const hash = url.hash.replace(/^#/, '');
    const query = hash.indexOf('?') >= 0 ? hash.slice(hash.indexOf('?') + 1) : hash;
    const params = new URLSearchParams(query);
    for (let i = 0; i < names.length; i++) {
      const value = params.get(names[i]);
      if (value && value.trim()) return value.trim();
    }
    return null;
  }

  function clickLeaf(text) {
    if (/验证|登录|滑块|拖动/.test(text)) return false;
    const nodes = document.body ? document.body.querySelectorAll('a,button,span,div,li') : [];
    for (let i = 0; i < nodes.length; i++) {
      const el = nodes[i];
      if (el.closest && el.closest('#xs-collector-panel, #xs-collector-btn')) continue;
      if (el.children.length === 0 && (el.textContent || '').trim() === text) {
        el.click();
        return true;
      }
    }
    return false;
  }

  function searchCards() {
    const primary = document.querySelectorAll('a[class*="doubleCardWrapper"]');
    if (primary.length) return Array.from(primary);
    const secondary = document.querySelectorAll('a[class*="doubleCard"]');
    return Array.from(secondary);
  }

  function parseSearchCard(card, keyword, rank) {
    const lines = card.innerText.split('\n').map(function (s) { return s.trim(); }).filter(Boolean);
    const priceAt = lines.indexOf('¥') >= 0 ? lines.indexOf('¥') : lines.indexOf('￥');
    let price = null;
    if (priceAt >= 0) {
      const raw = (lines[priceAt + 1] || '') + ((lines[priceAt + 2] || '').charAt(0) === '.' ? lines[priceAt + 2] : '');
      price = parsePrice(raw);
    }
    const html = card.outerHTML || '';
    const idMatch = (card.href || '').match(/[?&]id=(\d+)/) || html.match(/[?&;]id=(\d{9,})/) || html.match(/(?:itemId|item_id|nid)["'=:\s]+(\d{9,})/);
    const img = Array.from(card.querySelectorAll('img')).map(function (node) {
      return node.currentSrc || node.src || node.getAttribute('data-src') || '';
    }).find(function (src) {
      return /bao\/uploaded|imgextra|O1CN/.test(src) && !/tps-\d+-\d+/.test(src);
    }) || '';
    const sales = lines.find(function (line) { return /人(付款|收货)|月销|已售|笔成交/.test(line); }) || null;
    let shop = null;
    for (let i = lines.length - 1; i >= 1; i--) {
      const line = lines[i];
      if (/^[¥￥]/.test(line) || /人付款|人收货|月销|已售|包邮|券/.test(line)) continue;
      if (line.length > 40) continue;
      shop = line;
      break;
    }
    return {
      keyword: keyword,
      rank: rank,
      item_id: idMatch ? idMatch[1] : null,
      title: clip(lines[0], 200),
      price: price,
      sales_text: sales ? clip(sales, 200) : null,
      shop_name: clip(shop, 200),
      image_url: httpsURL(img)
    };
  }

  async function collectSearch() {
    const why = blockedReason();
    if (why) return { error: why };
    let cards = [];
    for (let i = 0; i < 12; i++) {
      const blocked = blockedReason();
      if (blocked) return { error: blocked };
      cards = searchCards();
      if (cards.length) break;
      await sleep(1000);
    }
    for (let y = 0; y < 12000; y += 1500) {
      const blocked = blockedReason();
      if (blocked) return { error: blocked };
      window.scrollTo(0, y);
      await sleep(400);
    }
    await sleep(1200);
    const blocked = blockedReason();
    if (blocked) return { error: blocked };
    window.scrollTo(0, 0);
    cards = searchCards();
    if (!cards.length) return { error: '没找到搜索结果。如果页面在要验证码，请你自己处理后再点。' };
    const keyword = clip(queryValue(['q', 'wq', 'keyword']), 200);
    const start = parseCount(new URL(location.href).searchParams.get('s')) || 0;
    const seen = new Set();
    const items = [];
    cards.forEach(function (card, index) {
      const item = parseSearchCard(card, keyword, start + index + 1);
      if (item.item_id && seen.has(item.item_id)) return;
      if (item.item_id) seen.add(item.item_id);
      items.push(item);
    });
    return { items: items };
  }

  function pageLib() {
    try {
      if (typeof unsafeWindow !== 'undefined' && unsafeWindow.lib && unsafeWindow.lib.mtop) return unsafeWindow.lib;
    } catch (err) {
      return null;
    }
    return null;
  }

  function mtopProblem(payload) {
    const text = JSON.stringify(payload || {});
    if (/RGV587|FAIL_SYS_USER_VALIDATE|FAIL_SYS_TRAFFIC|验证码|滑动验证|被挤爆|哎哟喂/.test(text)) {
      return '淘宝要求验证。请你在页面上自己处理完再点，脚本不会去点验证码。';
    }
    return '';
  }

  function mtopCall(lib, api, data) {
    return new Promise(function (resolve) {
      let settled = false;
      function finish(value) {
        if (settled) return;
        settled = true;
        resolve(value);
      }
      function ok(res) {
        const problem = mtopProblem(res);
        if (problem) {
          finish({ captcha: problem });
          return;
        }
        finish({ data: (res && res.data) || {} });
      }
      function fail(err) {
        const problem = mtopProblem(err);
        if (problem) {
          finish({ captcha: problem });
          return;
        }
        finish({ err: JSON.stringify(err).slice(0, 200) });
      }
      try {
        const ret = lib.mtop.request({ api: api, v: '6.0', type: 'GET', dataType: 'json', data: data });
        if (ret && typeof ret.then === 'function') ret.then(ok, fail);
        else fail({ err: 'mtop 没有返回结果' });
      } catch (err) {
        fail(err);
      }
    });
  }

  function flattenProps(groupProps) {
    const out = {};
    (groupProps || []).forEach(function (group) {
      if (!group || typeof group !== 'object') return;
      Object.keys(group).forEach(function (key) {
        const rows = group[key];
        if (!Array.isArray(rows)) return;
        rows.forEach(function (row) {
          if (!row || typeof row !== 'object') return;
          if (row.name && (row.value != null || row.valueText != null)) {
            const name = clip(row.name, 200);
            const value = clip(row.value != null ? row.value : row.valueText, 200);
            if (name && value) out[name] = value;
            return;
          }
          Object.keys(row).forEach(function (prop) {
            const value = row[prop];
            if (value == null || typeof value === 'object') return;
            const name = clip(prop, 200);
            const text = clip(value, 200);
            if (name && text) out[name] = text;
          });
        });
      });
    });
    return Object.keys(out).length ? out : null;
  }

  function cleanImages(list) {
    const seen = new Set();
    const out = [];
    (list || []).forEach(function (raw) {
      const url = httpsURL(raw);
      if (!url || seen.has(url)) return;
      seen.add(url);
      out.push(url);
    });
    return out.length ? out : null;
  }

  function blankItem(id) {
    return {
      item_id: id || null,
      title: null,
      price: null,
      sales_text: null,
      params: null,
      reviews: null,
      images: null
    };
  }

  function itemFromDOM(id) {
    const item = blankItem(id);
    const titleNode = document.querySelector('h1, .tb-main-title, [class*="mainTitle"], [class*="ItemTitle"]');
    item.title = clip(titleNode ? titleNode.innerText : '', 200);
    const priceNode = document.querySelector('[class*="priceText"], [class*="Price--"], .tb-rmb-num, #J_StrPrice .tb-rmb-num, #J_PromoPriceNum');
    item.price = parsePrice(priceNode ? priceNode.innerText : '');
    const lines = (document.body ? document.body.innerText : '').split('\n').map(function (s) { return s.trim(); }).filter(function (s) { return s && s.length <= 40; });
    const sales = lines.find(function (line) { return /人(付款|收货)|月销|已售/.test(line); }) || null;
    item.sales_text = sales ? clip(sales, 200) : null;
    const params = {};
    document.querySelectorAll('#J_AttrUL li, [class*="Attrs"] li, [class*="infoItem"]').forEach(function (node) {
      const text = (node.innerText || '').replace(/\s+/g, ' ').trim();
      const pair = text.split(/[:：]/);
      if (pair.length < 2) return;
      const name = clip(pair[0], 200);
      const value = clip(pair.slice(1).join(':'), 200);
      if (name && value) params[name] = value;
    });
    item.params = Object.keys(params).length ? params : null;
    item.images = cleanImages(Array.from(document.querySelectorAll('#J_UlThumb img, [class*="PicGallery"] img, [class*="thumbnail"] img')).map(function (node) {
      return node.currentSrc || node.src || node.getAttribute('data-src') || '';
    }));
    return item;
  }

  function mergeDetail(item, data) {
    const info = data.item || {};
    if (info.title) item.title = clip(info.title, 200);
    const mainImages = (info.images || []).map(function (src) { return src; });
    item.images = cleanImages([].concat(mainImages, item.images || []));
    const props = flattenProps((data.props || {}).groupProps);
    if (props) item.params = props;
    let stack = {};
    try {
      stack = JSON.parse((((data.apiStack || [])[0] || {}).value) || '{}');
    } catch (err) {
      stack = {};
    }
    const priceText = (((stack.price || {}).price || {}).priceText) || '';
    const parsed = parsePrice(priceText);
    if (parsed != null) item.price = parsed;
    const sales = (stack.item || {}).vagueSellCount || (stack.item || {}).sellCount || '';
    if (sales) item.sales_text = clip(sales, 200);
    return item;
  }

  function reviewItems(list, left) {
    const out = [];
    (list || []).forEach(function (row) {
      if (out.length >= left) return;
      const text = clip(row && row.feedback, 200);
      let date = row && row.feedbackDate ? String(row.feedbackDate).trim() : '';
      const day = parseDay(date);
      if (day) date = day;
      else date = clip(date, 32);
      if (!text && !date) return;
      out.push({ text: text, date: date });
    });
    return out;
  }

  async function collectItem() {
    const blocked = blockedReason();
    if (blocked) return { error: blocked };
    const id = (function () {
      const fromQuery = queryValue(['id', 'itemId', 'item_id']);
      if (fromQuery && /^\d{6,}$/.test(fromQuery)) return fromQuery;
      const matched = location.href.match(/[?&](?:id|itemId|item_id)=(\d{6,})/);
      return matched ? matched[1] : null;
    })();
    if (!id) return { error: '地址里没有商品 id。' };
    const lib = pageLib();
    if (!lib) return { items: [itemFromDOM(id)] };
    const detail = await mtopCall(lib, 'mtop.taobao.detail.getdetail', { itemNumId: id });
    if (detail.captcha) return { error: detail.captcha };
    let item = itemFromDOM(id);
    if (!detail.err && detail.data) item = mergeDetail(item, detail.data);
    await sleep(1500 + Math.random() * 1000);
    const again = blockedReason();
    if (again) return { error: again };
    const desc = await mtopCall(lib, 'mtop.taobao.detail.getdesc', { id: id, type: '1' });
    if (desc.captcha) return { error: desc.captcha };
    if (!desc.err) {
      const html = JSON.stringify(desc.data || {});
      const found = html.match(/(?:https?:)?\/\/img\.alicdn\.com\/[^"\\ ]+?\.(?:jpg|png|gif|webp)/g) || [];
      item.images = cleanImages([].concat(item.images || [], found));
    }
    const reviews = [];
    for (let page = 1; page <= 3; page++) {
      await sleep(1500 + Math.random() * 1000);
      const stop = blockedReason();
      if (stop) return { error: stop };
      const rates = await mtopCall(lib, 'mtop.taobao.rate.detaillist.get', {
        showTrueCount: false,
        auctionNumId: id,
        pageNo: page,
        pageSize: 20,
        rateType: '',
        searchImpr: '-8',
        orderType: '',
        expression: '',
        rateSrc: 'pc_rate_list'
      });
      if (rates.captcha) return { error: rates.captcha };
      if (rates.err) break;
      const batch = reviewItems((rates.data && rates.data.rateList) || [], 50 - reviews.length);
      batch.forEach(function (row) { reviews.push(row); });
      if (batch.length < 20 || reviews.length >= 50) break;
    }
    item.reviews = reviews.length ? reviews.slice(0, 50) : null;
    item.item_id = id;
    return { items: [item] };
  }

  function grab(re, text) {
    const matched = text.match(re);
    return matched ? matched[1] : null;
  }

  function keywordItem(word, volume, clicks, ctr, cvr, competition) {
    return {
      word: clip(word, 200),
      search_volume: volume == null ? null : parseCount(volume),
      clicks: clicks == null ? null : parseCount(clicks),
      ctr: ctr == null ? null : parsePercent(ctr),
      cvr: cvr == null ? null : parsePercent(cvr),
      competition: competition == null ? null : parseCount(competition)
    };
  }

  function parseRelated(segment) {
    const lines = segment.split('\n').map(function (s) { return s.trim(); }).filter(Boolean);
    const out = [];
    for (let i = 0; i < lines.length - 3; i++) {
      if (/^[\d,]+$/.test(lines[i + 1]) && /%$/.test(lines[i + 2]) && /^[¥￥]/.test(lines[i + 3]) && !/^\d+$/.test(lines[i])) {
        out.push(keywordItem(lines[i], null, null, lines[i + 2], null, null));
        i += 3;
      }
    }
    return out.filter(function (item) { return item.word; });
  }

  async function collectKeyword() {
    const blocked = blockedReason();
    if (blocked) return { error: blocked };
    clickLeaf('市场数据趋势');
    await sleep(500);
    let summary = null;
    let slice = '';
    for (let i = 0; i < 15; i++) {
      await sleep(1000);
      const stop = blockedReason();
      if (stop) return { error: stop };
      const text = document.body ? document.body.innerText : '';
      const at = text.indexOf('市场数据总结');
      if (at < 0) continue;
      slice = text.slice(at, at + 1200);
      const word = grab(/[“"]([^”"]+)[”"]的搜索指数/, slice);
      if (!word) continue;
      summary = keywordItem(
        word,
        null,
        null,
        null,
        grab(/平均点击转化率为([\d.]+)\s*%/, slice),
        grab(/竞争指数[^（(]{0,8}[（(]([\d,]+)/, slice)
      );
      break;
    }
    if (!summary || !summary.word) return { error: '这一页没看到「市场数据总结」。请先打开万相台流量解析，选好关键词后再点。' };
    const related = [];
    if (clickLeaf('相关词推荐')) {
      const tag = '搜索“' + summary.word + '”的消费者同时会组合搜的词';
      const tagAscii = '搜索"' + summary.word + '"的消费者同时会组合搜的词';
      for (let i = 0; i < 12; i++) {
        await sleep(1000);
        const stop = blockedReason();
        if (stop) return { error: stop };
        const text = document.body ? document.body.innerText : '';
        let at = text.indexOf(tag);
        if (at < 0) at = text.indexOf(tagAscii);
        const next = text.indexOf('还会搜的词', at + 1);
        if (at > 0 && next > at) {
          parseRelated(text.slice(at, next)).forEach(function (item) { related.push(item); });
          parseRelated(text.slice(next, next + 4000)).forEach(function (item) { related.push(item); });
          if (related.length) break;
        }
      }
    }
    const seen = new Set([summary.word]);
    const items = [summary];
    related.forEach(function (item) {
      if (!item.word || seen.has(item.word)) return;
      seen.add(item.word);
      items.push(item);
    });
    return { items: items };
  }

  function headerText(node) {
    return (node.innerText || '').replace(/\s+/g, '');
  }

  function columnIndex(headers, accept, reject) {
    for (let i = 0; i < headers.length; i++) {
      const name = headers[i];
      if (reject && reject.test(name)) continue;
      if (accept.test(name)) return i;
    }
    return -1;
  }

  function readTables() {
    const tables = [];
    const seen = new Set();
    function push(headers, rows) {
      const cleanHeaders = headers.map(function (header) { return String(header || '').replace(/\s+/g, ''); }).filter(function (header, index, all) {
        return header || all.length;
      });
      const cleanRows = rows.map(function (row) {
        return row.map(function (cell) { return String(cell || '').replace(/\s+/g, ' ').trim(); });
      }).filter(function (row) { return row.some(Boolean); });
      if (!cleanHeaders.some(Boolean) || !cleanRows.length) return;
      const key = cleanHeaders.join('|') + '#' + cleanRows.length + '#' + (cleanRows[0] || []).join('|');
      if (seen.has(key)) return;
      seen.add(key);
      tables.push({ headers: cleanHeaders, rows: cleanRows });
    }
    document.querySelectorAll('table').forEach(function (table) {
      let headers = Array.from(table.querySelectorAll('thead th, thead td')).map(headerText);
      const body = Array.from(table.querySelectorAll('tbody tr'));
      if (!headers.length && body.length) {
        headers = Array.from(body[0].cells || []).map(headerText);
        push(headers, body.slice(1).map(function (row) { return Array.from(row.cells || []).map(function (cell) { return cell.innerText; }); }));
        return;
      }
      push(headers, body.map(function (row) { return Array.from(row.cells || []).map(function (cell) { return cell.innerText; }); }));
    });
    document.querySelectorAll('.ant-table, [class*="oui-table"]').forEach(function (box) {
      const headers = Array.from(box.querySelectorAll('.ant-table-thead th, thead th')).map(headerText);
      const rows = Array.from(box.querySelectorAll('.ant-table-tbody tr, tbody tr')).map(function (row) {
        return Array.from(row.querySelectorAll('td')).map(function (cell) { return cell.innerText; });
      });
      push(headers, rows);
    });
    return tables;
  }

  function cell(row, index) {
    if (index < 0 || index >= row.length) return null;
    return row[index];
  }

  function blankListing(itemID, date) {
    return {
      date: date,
      item_id: itemID,
      listing_id: null,
      impressions: null,
      clicks: null,
      add_cart: null,
      orders: null,
      gmv: null,
      uv: null,
      search_terms: null
    };
  }

  function listingUseful(item) {
    return !!(item.date || item.item_id || item.impressions != null || item.clicks != null || item.add_cart != null || item.orders != null || item.gmv != null || item.uv != null || (item.search_terms && item.search_terms.length));
  }

  function parseTermTable(table) {
    const termAt = columnIndex(table.headers, /搜索词|关键词|搜索关键字/, /占比|趋势/);
    if (termAt < 0) return [];
    const uvAt = columnIndex(table.headers, /访客/, /占比/);
    const clickAt = columnIndex(table.headers, /点击人数|点击次数|点击量/, /率|转化/);
    const orderAt = columnIndex(table.headers, /支付件数|支付买家|成交件数|成交笔数|下单件数/, /金额|转化|率/);
    return table.rows.map(function (row) {
      return {
        term: clip(cell(row, termAt), 200),
        uv: parseCount(cell(row, uvAt)),
        clicks: parseCount(cell(row, clickAt)),
        orders: parseCount(cell(row, orderAt))
      };
    }).filter(function (row) { return row.term; });
  }

  function parseMetricTable(table, fallbackID) {
    const dateAt = columnIndex(table.headers, /统计日期|^日期$/, null);
    const itemAt = columnIndex(table.headers, /商品ID|宝贝ID|商品编号/, null);
    const impressionAt = columnIndex(table.headers, /曝光|展现/, /点击率|转化/);
    const clickAt = columnIndex(table.headers, /点击人数|点击次数|点击量/, /率|转化/);
    const cartAt = columnIndex(table.headers, /加购/, null);
    const orderAt = columnIndex(table.headers, /支付件数|支付买家数|支付订单|成交件数|成交笔数|下单件数/, /金额|转化|率/);
    const gmvAt = columnIndex(table.headers, /支付金额|成交金额|支付额/, null);
    const uvAt = columnIndex(table.headers, /访客数|^访客$/, /占比/);
    if (dateAt < 0 && itemAt < 0 && impressionAt < 0 && clickAt < 0 && cartAt < 0 && orderAt < 0 && gmvAt < 0 && uvAt < 0) return [];
    return table.rows.map(function (row) {
      const itemID = clip(cell(row, itemAt), 40) || fallbackID;
      return blankListing(itemID, parseDay(cell(row, dateAt)));
    }).map(function (item, index) {
      const row = table.rows[index];
      item.impressions = parseCount(cell(row, impressionAt));
      item.clicks = parseCount(cell(row, clickAt));
      item.add_cart = parseCount(cell(row, cartAt));
      item.orders = parseCount(cell(row, orderAt));
      item.gmv = parseMoney(cell(row, gmvAt));
      item.uv = parseCount(cell(row, uvAt));
      return item;
    }).filter(listingUseful);
  }

  function pageDate() {
    const found = [];
    document.querySelectorAll('input').forEach(function (input) {
      const day = parseDay(input.value || '');
      if (day && found.indexOf(day) < 0) found.push(day);
    });
    return found.length === 1 ? found[0] : null;
  }

  function collectMyListing() {
    const blocked = blockedReason();
    if (blocked) return { error: blocked };
    const fallbackID = (function () {
      const value = queryValue(['itemId', 'item_id', 'itemid']);
      if (value && /^\d{6,}$/.test(value)) return value;
      return null;
    })();
    const metrics = [];
    const terms = [];
    readTables().forEach(function (table) {
      if (columnIndex(table.headers, /搜索词|关键词|搜索关键字/, /占比|趋势/) >= 0) {
        parseTermTable(table).forEach(function (row) { terms.push(row); });
        return;
      }
      parseMetricTable(table, fallbackID).forEach(function (row) { metrics.push(row); });
    });
    let items = metrics;
    if (!items.length && (fallbackID || terms.length)) {
      items = [blankListing(fallbackID, pageDate())];
    }
    if (terms.length && items.length) items[0].search_terms = terms;
    items = items.filter(listingUseful);
    if (!items.length) return { error: '这一页没认出商品数据表。生意参谋建议优先导出 CSV，再在 X Studio 里导入。' };
    return { items: items };
  }

  async function collect(kind) {
    if (kind === 'search') return collectSearch();
    if (kind === 'item') return collectItem();
    if (kind === 'keyword') return collectKeyword();
    if (kind === 'my_listing') return collectMyListing();
    return { error: '这个页面不采集。' };
  }

  function postIngest(current, body) {
    return new Promise(function (resolve, reject) {
      GM_xmlhttpRequest({
        method: 'POST',
        url: current.base + '/api/market/ingest',
        anonymous: true,
        headers: {
          Authorization: 'Bearer ' + current.token,
          'X-Requested-With': 'fetch',
          'Content-Type': 'application/json'
        },
        data: JSON.stringify(body),
        timeout: 60000,
        onload: function (res) {
          let data = null;
          try { data = JSON.parse(res.responseText || ''); } catch (err) { data = null; }
          if (res.status < 200 || res.status >= 300) {
            const message = data && data.error ? data.error : String(res.responseText || '').slice(0, 300);
            reject(new Error('服务器返回 ' + res.status + (message ? '：' + message : '')));
            return;
          }
          resolve(data || {});
        },
        onerror: function () { reject(new Error('没连上 X Studio。请检查地址，以及脚本是否允许访问那个网站。')); },
        ontimeout: function () { reject(new Error('连接 X Studio 超时。')); }
      });
    });
  }

  function showResult(data, extra) {
    if (!data || typeof data !== 'object') {
      alert('服务器返回的内容认不出来。');
      return;
    }
    const lines = [];
    if (data.duplicate) lines.push('这批数据和上次一样，服务器当成重复，没有再写。');
    const skipped = Array.isArray(data.skipped) ? data.skipped : [];
    const inserted = Number(data.inserted || 0);
    const updated = Number(data.updated || 0);
    if (!data.duplicate) lines.push('成功 ' + (inserted + updated) + ' 条（新写入 ' + inserted + '，更新 ' + updated + '）。');
    lines.push('跳过 ' + skipped.length + ' 条。');
    skipped.slice(0, 20).forEach(function (row) {
      const index = Number(row && row.index);
      const where = Number.isFinite(index) ? '第 ' + (index + 1) + ' 条' : '一条';
      lines.push(where + '：' + ((row && row.reason) || '没写原因'));
    });
    if (skipped.length > 20) lines.push('还有 ' + (skipped.length - 20) + ' 条跳过原因没列出来。');
    if (extra) lines.push('页面上还有 ' + extra + ' 条没发，一次最多 2000 条。');
    alert(lines.join('\n'));
  }

  let busy = false;

  async function sendCurrent(button) {
    if (busy) return;
    const kind = pageKind();
    if (!kind) {
      alert('这个页面不采集。');
      return;
    }
    const current = cfg();
    const problem = settingsError(current);
    if (problem) {
      alert(problem);
      return;
    }
    busy = true;
    button.disabled = true;
    button.textContent = '正在采集…';
    try {
      const stopped = blockedReason();
      if (stopped) {
        alert(stopped);
        return;
      }
      const result = await collect(kind);
      if (result.error) {
        alert(result.error);
        return;
      }
      const all = result.items || [];
      if (!all.length) {
        alert('这一页没有采到可以发送的数据。');
        return;
      }
      const extra = all.length > 2000 ? all.length - 2000 : 0;
      const body = {
        shop_id: shopID(current.shop),
        kind: kind,
        source_url: sourceURL(),
        captured_at: nowUTC(),
        category: current.category,
        items: all.slice(0, 2000)
      };
      const response = await postIngest(current, body);
      showResult(response, extra);
    } catch (err) {
      alert(err && err.message ? err.message : String(err));
    } finally {
      busy = false;
      button.disabled = false;
      button.textContent = '发送到 X Studio';
    }
  }

  function field(label, input) {
    const wrap = document.createElement('label');
    wrap.style.cssText = 'display:block;margin:0 0 10px;color:#1c1c1c;font:13px/1.4 "Segoe UI",sans-serif;';
    const name = document.createElement('span');
    name.textContent = label;
    name.style.cssText = 'display:block;margin:0 0 4px;';
    input.style.cssText = 'width:100%;box-sizing:border-box;border:1px solid #c8c8c8;border-radius:4px;padding:6px 8px;font:inherit;background:#fff;color:#1c1c1c;';
    wrap.appendChild(name);
    wrap.appendChild(input);
    return wrap;
  }

  function openSettings() {
    const old = document.getElementById('xs-collector-panel');
    if (old) old.remove();
    const current = cfg();
    const panel = document.createElement('div');
    panel.id = 'xs-collector-panel';
    panel.style.cssText = 'position:fixed;right:24px;bottom:72px;z-index:2147483647;width:320px;background:#fff;border:1px solid #d5d5d5;border-radius:8px;padding:14px;box-sizing:border-box;';
    const title = document.createElement('div');
    title.textContent = '采集设置';
    title.style.cssText = 'margin:0 0 10px;font:600 14px/1.4 "Segoe UI",sans-serif;color:#1c1c1c;';
    const base = document.createElement('input');
    base.value = current.base;
    const token = document.createElement('input');
    token.type = 'password';
    token.value = current.token;
    const shop = document.createElement('input');
    shop.value = current.shop;
    shop.inputMode = 'numeric';
    const category = document.createElement('input');
    category.value = current.category;
    const status = document.createElement('div');
    status.style.cssText = 'min-height:18px;margin:0 0 8px;font:12px/1.4 "Segoe UI",sans-serif;color:#3d6b3d;';
    const row = document.createElement('div');
    row.style.cssText = 'display:flex;gap:8px;';
    const save = document.createElement('button');
    save.type = 'button';
    save.textContent = '保存';
    save.style.cssText = 'border:0;border-radius:4px;padding:6px 12px;background:#2457c5;color:#fff;font:13px/1 "Segoe UI",sans-serif;cursor:pointer;';
    const close = document.createElement('button');
    close.type = 'button';
    close.textContent = '关闭';
    close.style.cssText = 'border:1px solid #c8c8c8;border-radius:4px;padding:6px 12px;background:#fff;color:#1c1c1c;font:13px/1 "Segoe UI",sans-serif;cursor:pointer;';
    save.addEventListener('click', function () {
      GM_setValue(KEYS.base, base.value.trim());
      GM_setValue(KEYS.token, token.value.trim());
      GM_setValue(KEYS.shop, shop.value.trim());
      GM_setValue(KEYS.category, category.value.trim());
      status.textContent = '已保存';
    });
    close.addEventListener('click', function () { panel.remove(); });
    row.appendChild(save);
    row.appendChild(close);
    panel.appendChild(title);
    panel.appendChild(field('X Studio 地址', base));
    panel.appendChild(field('个人令牌', token));
    panel.appendChild(field('店铺 id', shop));
    panel.appendChild(field('品类', category));
    panel.appendChild(status);
    panel.appendChild(row);
    (document.body || document.documentElement).appendChild(panel);
  }

  function mountButton() {
    if (document.getElementById('xs-collector-btn')) return;
    const button = document.createElement('button');
    button.id = 'xs-collector-btn';
    button.type = 'button';
    button.textContent = '发送到 X Studio';
    button.style.cssText = 'position:fixed;right:24px;bottom:24px;z-index:2147483646;border:0;border-radius:6px;padding:10px 14px;background:#2457c5;color:#fff;font:14px/1 "Segoe UI",sans-serif;cursor:pointer;';
    button.addEventListener('click', function () { sendCurrent(button); });
    (document.body || document.documentElement).appendChild(button);
  }

  function syncButton() {
    const button = document.getElementById('xs-collector-btn');
    if (!pageKind()) {
      if (button) button.remove();
      return;
    }
    if (!button) mountButton();
  }

  GM_registerMenuCommand('采集设置', openSettings);
  syncButton();
  setInterval(syncButton, 1000);
})();
