# X Studio 采集助手

这个目录里是一个 Tampermonkey 脚本。你在淘宝搜索、淘宝商品页、万相台关键词或生意参谋商品页上点右下角的「发送到 X Studio」，它才把当前页的数据发给 X Studio。不点就不会采，也不会自己换词、翻页。

`原脚本/` 是改写前的三份旧脚本，留着对照，不要删。

## 怎么装

用 Chrome 或 Edge。到扩展商店安装 Tampermonkey。装好后，把本目录的 `x-studio-采集助手.user.js` 拖进浏览器窗口，Tampermonkey 会问要不要安装，确认安装。

再点工具栏里的 Tampermonkey 图标，看「X Studio 采集助手」是不是开着的。拖文件没反应的话，打开 Tampermonkey 的管理面板，用「实用工具」里的从文件导入，或者新建一个脚本，把文件内容全部贴进去再保存。

## 怎么填地址和令牌

点 Tampermonkey 图标，在这个脚本的菜单里选「采集设置」。四个空会记在浏览器里（`GM_setValue`），不写进脚本文件。

X Studio 地址填你打开后台用的那个地址，例如 `http://127.0.0.1:8080`。末尾的斜杠可以留着，脚本会去掉。个人令牌填下面拿到的那一串。店铺 id 填这家店在 X Studio 里的数字。品类填例如 `pearl`，可以空着，空着就发空字符串。

个人令牌的页面入口以后做在「数据」页。现在登录 X Studio 后，自己发这个请求就行。账号要有 `market.import`。

`POST /api/market/tokens`

请求体是 `{"name":"采集助手"}`，头里要带 `X-Requested-With: fetch`。返回的 `token` 只出现这一次，以 `xst_` 开头，抄进采集设置。库里只存哈希，不存明文。每人最多 10 个还没吊销的令牌。不要了就 `DELETE /api/market/tokens/{id}`，只能吊销自己的。

## 每种页面怎么用

先自己登录。右下角出现「发送到 X Studio」再点。发完会弹出：成功几条（新写入多少、更新多少）、跳过几条和原因。如果这批数据和上次完全一样，会告诉你是重复，服务器没有再写。

淘宝搜索结果，地址是 `s.taobao.com/search`。脚本按地址里的搜索词采集这一页，kind 是 `search`。点下去之后会先把页面向下滚，每 1500 像素停 0.4 秒，最多滚到 12000 像素，滚完再等 1.2 秒，让后面的卡片和图片加载出来。下一页要你自己点开，再点一次发送。每条有 `keyword`、`rank`、`item_id`、`title`、`price`、`sales_text`、`shop_name`、`image_url`。价格带问号时 `price` 是 null，不用区间估一个数。销量用页面上的原文，例如「月销 1000+」。

淘宝或天猫商品页，地址是 `item.taobao.com` 或 `detail.tmall.com` 的商品页。kind 是 `item`。页面里如果有淘宝自带的数据接口，就用它取标题、价格、参数、主图、详情图，以及全部评价里的前 50 条。评价只留文字和日期。每次请求隔 1.5 到 2.5 秒。没有这个接口时，只取页面上看得见的内容，评价一般是 null。字段是 `item_id`、`title`、`price`、`sales_text`、`params`、`reviews`、`images`。没取到的是 null。

万相台关键词，打开 `one.alimama.com` 的流量解析，先在页面上选好一个词，再点发送。kind 是 `keyword`。脚本会点「市场数据趋势」和「相关词推荐」这两个数据页签，然后每秒看一次页面上的字。主词最多等 15 秒，相关词最多等 12 秒。这一页没有搜索量和点击次数，所以主词的 `search_volume` 和 `clicks` 是 null。页面上的搜索指数、展现指数、出价都没有对应字段，不发。转化率用「平均点击转化率」，`competition` 用竞争指数。相关词只拿得到点击率，搜索量、点击次数、转化率和竞争都是 null。一次只采当前这个词，不会自动换成下一个。

生意参谋商品页，地址是 `sycm.taobao.com`。kind 是 `my_listing`。这一页是尽力采集，建议优先把生意参谋导出的 CSV 拿到 X Studio 里导入。脚本只读已经显示出来的表格，不翻页，也不去点验证码。

生意参谋上经常对不上、于是是 null 的：

- `listing_id`：页面上没有 X Studio 自己的上架版本号，一直是 null。能对上的是商品 ID，放在 `item_id`。
- `impressions`：商品表多数只有浏览量，没有曝光量。表头没写「曝光」或「展现」就是 null。浏览量不会拿来顶曝光。
- `clicks`：表头是点击率的时候不填，因为那不是次数。没有「点击人数 / 点击次数 / 点击量」就是 null。
- `search_terms`：页面上没有搜索词表就是 null。商品概览通常没有。有多行商品数据时，搜索词只挂在第一行，免得每一天重复记一遍。
- `date`：某一格里只有一个日期才填。看到的是一段日期范围就不猜，是 null。
- `add_cart`、`orders`、`gmv`、`uv`：表头要分别像加购、支付件数或支付买家数、支付金额、访客数。对不上就是 null。

## 验证码和登录

碰到登录页，或者页面上出现滑块、验证码，脚本会停下来告诉你，不会去点。你自己处理完，再点一次发送。

脚本不读 cookie，也不把 cookie、账号发给 X Studio。发往 X Studio 的请求是匿名的，只带你填的令牌。

## 为什么 @connect 写了 *

Tampermonkey 要求脚本事先声明允许连接哪些网站。X Studio 的地址是你自己填的，脚本里写不死具体域名，所以这里写成 `*`，你填什么地址就能连什么地址。这不表示脚本会把淘宝的 cookie 转发出去。

## 用本机假服务看请求长什么样

先在一个终端把下面存成 `fake_ingest.py` 再跑 `python fake_ingest.py`。它听 `127.0.0.1:8765`，把收到的路径、头和正文打印出来，并回一个采集接口那样的成功结果。

```python
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(n)
        print(self.path)
        print(self.headers)
        print(body.decode("utf-8", "replace"))
        out = b'{"snapshot_id":1,"duplicate":false,"inserted":1,"updated":0,"skipped":[]}'
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(out)))
        self.end_headers()
        self.wfile.write(out)

ThreadingHTTPServer(("127.0.0.1", 8765), Handler).serve_forever()
```

另开一个 PowerShell，用 curl 发一条和脚本相同格式的请求：

```powershell
curl.exe -s http://127.0.0.1:8765/api/market/ingest `
  -H "Authorization: Bearer xst_示例令牌" `
  -H "X-Requested-With: fetch" `
  -H "Content-Type: application/json" `
  --data-raw '{"shop_id":1,"kind":"search","source_url":"https://s.taobao.com/search?q=珍珠耳钉","captured_at":"2026-10-21T02:00:00Z","category":"pearl","items":[{"keyword":"珍珠耳钉","rank":1,"item_id":"123456789012","title":"淡水珍珠耳钉","price":39.9,"sales_text":"月销 1000+","shop_name":"示例店","image_url":"https://img.alicdn.com/example.jpg"}]}'
```

脚本真正发出去的就是这种 POST：地址是 `{你填的地址}/api/market/ingest`，头是 `Authorization: Bearer <令牌>`、`X-Requested-With: fetch`、`Content-Type: application/json`。正文固定有 `shop_id`、`kind`、`source_url`、`captured_at`、`category`、`items`。`kind` 分别是 `search`、`item`、`keyword`、`my_listing`。某项没采到时，那一项是 null，不会省掉这个字段。
