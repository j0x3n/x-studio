# 珠光秘境出图服务

Go 1.26 + SQLite + chromedp。前端整目录 embed；模板、AI 提示词从磁盘读取。接口以 [接口约定.md](接口约定.md) 为准。

## Windows 本机运行

先安装 Go 1.26、Google Chrome 和 Git for Windows（同步脚本使用 Git Bash）。在 PowerShell 中：

```powershell
cd C:\Users\xcc19\Documents\跨境电商\出图网站
bash scripts/sync-templates.sh
go mod download
$env:ADMIN_USER = 'admin'
$env:ADMIN_PASSWORD = '请换成自己的初始密码123'
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
$secretBytes = New-Object byte[] 32; $rng.GetBytes($secretBytes)
$env:SESSION_SECRET = [Convert]::ToBase64String($secretBytes); $rng.Dispose()
$env:AI_MODEL_VISION = 'gpt-6-sol'
$env:AI_MODEL_TEXT = 'gpt-6-sol'
$env:AI_REASONING_EFFORT = 'medium'
$env:IMAGE_MODEL = 'gpt-image-2.5-burst'
$env:AI_API_KEY = ''  # 空值也能启动，AI 步骤会报中文错误
$env:REMBG_URL = 'http://127.0.0.1:7000'
go run ./cmd/server
```

打开 <http://localhost:8080>。Chrome 从常见安装目录或 PATH 自动寻找；也可设置 `$env:CHROME_PATH = 'C:\Program Files\Google\Chrome\Application\chrome.exe'`。前端页面缺失时服务仍能启动，页面会提示待同步；补齐 `web/` 后重新编译。运行目录应为 `出图网站/`，这样默认相对路径能找到模板和提示词。

只有数据库里还没有任何用户时，才会使用 `ADMIN_USER` / `ADMIN_PASSWORD` 建管理员。密码要求 8–72 字节。首次成功后可以删除这两个环境变量，用管理员登录 `/users.html` 建员工账号。`SESSION_SECRET` 必须至少 32 字符，应保存好并在重启时复用；改变它会使已有登录失效。修改密码、角色或禁用账号也会使该账号已有会话失效。最后一个启用的管理员不能被禁用或降级。

rembg 可以单独运行，也可以不运行；请求失败、超时、返回错误图片时自动采用 Go 的边缘连通抠图，并在 `retouch_log.cutout` 中写 `fallback`。复杂背景与珍珠颜色相近时应人工复核。

## Docker Compose 部署

先同步模板，在 `出图网站/.env` 中填真实配置（不要提交密钥）：

```dotenv
ADMIN_USER=admin
ADMIN_PASSWORD=换成自己的初始密码123
SESSION_SECRET=换成至少32字符的随机密钥并长期保存
AI_API_KEY=
AI_MODEL_VISION=gpt-6-sol
AI_MODEL_TEXT=gpt-6-sol
AI_REASONING_EFFORT=medium
IMAGE_MODEL=gpt-image-2.5-burst
IMAGE_TIMEOUT=240
AUTO_UPSCALE=1
AUTO_FILL=1
UPSCALE_MIN_SIDE=900
APP_PORT=8080
COOKIE_SECURE=0
WORKERS=2
```

```bash
bash scripts/sync-templates.sh
docker compose up -d --build
docker compose logs -f app
```

app 镜像安装 Chromium、`fonts-noto-cjk` 和 CA 证书，以非 root 用户运行。Docker 构建上下文就是 `出图网站/`，只使用 `templates/` 副本，不依赖上层仓库路径。`prompts/` 通过 compose 只读挂载，修改提示词无须重编译。数据放在 `app-data` 卷；rembg 模型缓存放在 `rembg-models` 卷，首次使用模型可能需要联网下载。rembg 镜像已含 `rembg` 入口，因此 compose 的 command 从 `s` 开始。接口按 [rembg 官方文档](https://github.com/danielgatis/rembg#rembg-s) 的 multipart `POST /api/remove` 调用。

compose 为受限的容器内核设置 `CHROME_NO_SANDBOX=1`，Chrome 因而关闭自身沙箱；app 仍以非 root 用户运行，模板响应限制外部资源加载。正常本机运行默认保留 Chrome 沙箱。对公网部署应由 HTTPS 反向代理接入，将 `COOKIE_SECURE=1`，并保留同源访问，不开放 rembg 端口。反向代理的上传限制应不小于 256 MB，渲染路由不能被代理缓存。

备份时停止 app，再备份整个数据卷（SQLite 和商品目录一起），不要仅在运行中复制 `app.db` 而遗漏 WAL。一个数据目录只供一个 app 进程使用。

## 环境变量

| 变量 | 默认值 / 说明 |
|---|---|
| `LISTEN` | `:8080` |
| `DATA_DIR` | `./data`；SQLite、原图、照片、成品 |
| `TEMPLATE_DIR` | `./templates` |
| `PROMPT_DIR` | `./prompts`；每次 AI 调用时重新读取 |
| `ADMIN_USER`、`ADMIN_PASSWORD` | 仅空数据库首次启动必填，之后忽略 |
| `SESSION_SECRET` | 必填，至少 32 字符 |
| `AI_BASE_URL` | `https://subapi.xcc.im/v1`；文本/看图追加 `/chat/completions`，图像编辑追加 `/images/edits` |
| `AI_API_KEY` | 可空；只在服务端使用 |
| `AI_MODEL_VISION` | `gpt-6-sol`；看图分拣 |
| `AI_MODEL_TEXT` | `gpt-6-sol`；文案 |
| `AI_REASONING_EFFORT` | `medium`；文本和看图请求携带 `reasoning_effort`，显式配置为空时不带 |
| `IMAGE_MODEL` | `gpt-image-2.5-burst`；高清化和补图 |
| `IMAGE_TIMEOUT` | `240` 秒；每次图像请求超时，可设 1–3600 |
| `AUTO_UPSCALE` | `1`；默认高清化，可设 `0` 关闭 |
| `AUTO_FILL` | `1`；默认补图，可设 `0` 关闭 |
| `UPSCALE_MIN_SIDE` | `900`；裁剪后短边小于该值才高清化，可设 1–10000 |
| `REMBG_URL` | `http://rembg:7000`；本机可设 `http://127.0.0.1:7000` |
| `CHROME_PATH` | 自动寻找 Chrome / Chromium，最后尝试 Edge |
| `WORKERS` | `2`，可设 1–32；同一款只允许一个排队或运行中的任务 |
| `COOKIE_SECURE` | `0`；HTTPS 反向代理部署设 `1` |
| `CHROME_NO_SANDBOX` | 默认未设置；仅受限运行环境确有需要时设 `1` |

文本/看图每次请求限时 120 秒，图像请求使用 `IMAGE_TIMEOUT`；失败重试一次。所有测试只用 `httptest` 假 AI，不访问真实模型。缺少 `分拣.md`、`文案.md` 或 `违禁词.txt` 时，对应 AI 步骤返回中文错误；缺少 `补图.md` 时 enhance 记录提示并继续处理原图。违禁词表兼容“一行一个词”和 `级别<TAB>词<TAB>说明`，也兼容字面 `\t` 分隔。

`.env` 由你自行维护，后端不会读取或修改该文件；compose 会将其中的变量传入容器。本机 `go run` 使用 PowerShell 的 `$env:变量`。显式关闭推理强度时，在 compose 的 `.env` 中写 `AI_REASONING_EFFORT=`；本机运行时须保证进程环境中该变量存在且为空，删除变量仍会使用 medium。已有环境变量或 `.env` 中的旧模型名会覆盖新默认值，请自行同步。

## enhance：高清化和补图

完整顺序：`check → classify → enhance → retouch → copy → render`。可用 `run?step=enhance` 只运行此步，或 `run?from=enhance` 从此步继续。每款 `info.auto_upscale`、`info.auto_fill` 可以填布尔值，覆盖环境默认值；省略时使用默认值。例如：

```json
{"info":{"auto_upscale":true,"auto_fill":false,"size":"7–8 mm","category":"耳钉"}}
```

高清化仅处理编号命中槽位候选的实拍图；先按 EXIF、手动旋转、裁剪得到要用的区域，短边小于阈值才调用图像编辑。已生成的 AI 图不再次高清化，避免反复生成。结果保存到 `原图/ai_up_<photo_id>.png`，原图不改，照片库缩略图仍引用原图。已有高清化结果重跑时复用；改 crop 或 rotate 会清空增强记录，需从 enhance 重跑。retouch 读取增强文件时不再旋转和裁剪。

补图只用本款实拍或其高清化结果作参考，最多 3 张，优先 A1、A2、F2、E4、B2、B3；D4 可增加已有 D1–D3。生成图不会作为新的参考图。每次判断：缺白底位生成 A1；必需首图/首屏候选均缺时生成 E1；没有 D 组照片时生成 D4。允许的完整编号白名单为 A1、E1、E2、E3、D4，目前自动决策只请求 A1、E1、D4。E5、B、C、F 组一律不生成。`补图.md` 的“规则”段不会发给模型。

每款数据库中最多保留 4 条 AI 生成照片记录（包括让位后编号为空的记录）；手动删除释放数量，重新生成同一 `ai_<code>.png` 时复用原记录。新实拍通过 classify 或手动编号占用同编号时，AI 图的 code 自动置空；AI 生成图不参加 classify。补图失败和数量限制会写入 job.message，并保留到任务最终完成/失败；高清化失败另写 `retouch_log.upscale_error`，用原图继续。

图像接口使用 multipart `POST /images/edits`，所有参考图字段均叫 `image`，附 `model`、`prompt`、最接近输入比例的三种 size 之一、`quality=high`，读取 `data[0].b64_json`。按实际返回尺寸保存 PNG，不强制拉伸到请求尺寸；后续仍按原有规则导出标准尺寸。

后端在 copy、PATCH、render 和渲染预览读取时按照片记录纠正 `ai`：生成图为 `AI 生成`，高清图为 `AI 增强`，实拍删除该字段。coverage 每项提供 `ai` 布尔值；need_reshoot 只提示不能用 AI 补的必需位，关掉自动补图后可补的位置仍在 coverage 显示缺失。同步脚本会让旧模板副本兼容字符串 ai 原样显示，不改上层源模板；修改上层模板后再次同步即可。

旧数据库启动时自动通过 `ALTER TABLE photos ADD COLUMN` 补齐以下字段，不清空数据，也无需手动重建数据库：

| 字段 | 类型与默认值 |
|---|---|
| `enhanced_file` | TEXT，`''` |
| `ai_enhanced` | INTEGER 布尔，`0` |
| `ai_generated` | INTEGER 布尔，`0` |
| `source` | TEXT，`original`；AI 补图为 `ai` |

迁移支持重复启动和部分字段已存在的数据库。上线前仍应备份原数据库和照片目录。

## 可复制的冒烟测试

在第一个终端按上面启动服务，先保持 `AI_API_KEY` 为空。下面在第二个 PowerShell 终端执行：登录、新建、上传 3 张自生成图片、跑全流程并确认 AI 失败、手动指定编号、修图、保存最小文案、真正用 Chrome 出图并下载 ZIP。脚本只在 `.smoke/` 写测试素材和下载包；服务内创建的“冒烟测试”款可在验收后删除。

```powershell
cd C:\Users\xcc19\Documents\跨境电商\出图网站
New-Item -ItemType Directory -Force .smoke | Out-Null
$base = 'http://127.0.0.1:8080'
$cookie = '.smoke/cookies.txt'
$utf8 = New-Object System.Text.UTF8Encoding($false)
function Send-JSON($method, $url, $body) {
  $jsonFile = Join-Path $PWD '.smoke/request.json'
  [IO.File]::WriteAllText($jsonFile, ($body | ConvertTo-Json -Depth 30 -Compress), $utf8)
  $reply = & curl.exe -f -sS -b $cookie -c $cookie -X $method `
    -H 'X-Requested-With: fetch' -H 'Content-Type: application/json' `
    --data-binary "@$jsonFile" "$base$url"
  if ($LASTEXITCODE -ne 0) { throw 'HTTP 请求失败' }
  return ($reply | ConvertFrom-Json)
}
function Wait-Job($jid) {
  $deadline = (Get-Date).AddMinutes(3)
  do {
    Start-Sleep -Milliseconds 500
    $reply = & curl.exe -f -sS -b $cookie "$base/api/jobs/$jid"
    if ($LASTEXITCODE -ne 0) { throw '查询任务失败' }
    $j = ($reply | ConvertFrom-Json).job
    if ($j.status -in @('done','failed')) { return $j }
  } while ((Get-Date) -lt $deadline)
  throw '任务等待超时'
}
# 这里的密码要与第一个终端 ADMIN_PASSWORD 相同。
Send-JSON 'POST' '/api/login' @{username='admin';password='请换成自己的初始密码123'} | Out-Null
$productID = (Send-JSON 'POST' '/api/products' @{name='冒烟测试';info=@{}}).product.id
Add-Type -AssemblyName System.Drawing
for ($i=1; $i -le 3; $i++) {
  $bmp = New-Object Drawing.Bitmap(240,320)
  $g = [Drawing.Graphics]::FromImage($bmp)
  $g.Clear([Drawing.Color]::WhiteSmoke)
  $g.FillEllipse([Drawing.Brushes]::Pink, 70, 90, 100, 130)
  $bmp.Save((Join-Path $PWD ".smoke/photo$i.png"), [Drawing.Imaging.ImageFormat]::Png)
  $g.Dispose(); $bmp.Dispose()
}
$uploaded = & curl.exe -f -sS -b $cookie -H 'X-Requested-With: fetch' `
  -F 'files=@.smoke/photo1.png' -F 'files=@.smoke/photo2.png' -F 'files=@.smoke/photo3.png' `
  "$base/api/products/$productID/photos"
if ($LASTEXITCODE -ne 0) { throw '上传失败' }
$photos = ($uploaded | ConvertFrom-Json).photos
$j = Send-JSON 'POST' "/api/products/$productID/run" @{}
$result = Wait-Job $j.job.id
$result | Format-List
if ($result.status -ne 'failed') { throw '此段预期 AI_API_KEY 为空而失败' }
$codes = @('A1','B1','C1')
for ($i=0; $i -lt 3; $i++) {
  Send-JSON 'PATCH' "/api/photos/$($photos[$i].id)" `
    @{code=$codes[$i];white_bg=($i -eq 0);rotate=0} | Out-Null
}
$j = Send-JSON 'POST' "/api/products/$productID/run?step=retouch" @{}
$result = Wait-Job $j.job.id
if ($result.status -ne 'done') { throw $result.message }
Send-JSON 'PATCH' "/api/products/$productID" @{
  content=@{name='冒烟测试';photos=@{hero=@{f='B1.jpg'}}}
  main=@{name='冒烟测试';imgs=@(@{n=1;layout='photo';f='B1.jpg'})}
  listing=@{title='测试珍珠耳钉';sku=@(@{name='测试规格';price=69})}
} | Out-Null
$j = Send-JSON 'POST' "/api/products/$productID/run?from=render" @{}
$result = Wait-Job $j.job.id
if ($result.status -ne 'done') { throw $result.message }
& curl.exe -f -sS -b $cookie "$base/api/products/$productID/download" -o .smoke/export.zip
if ($LASTEXITCODE -ne 0) { throw '下载失败' }
Get-Item .smoke/export.zip
```

## 验证

```powershell
go build ./...
go vet ./...
go test ./...
# 仅跑实际 Chrome + 假 AI 的完整流程：
go test ./internal/app -run TestRenderFullPipelineDimensionsAndZip -count=1 -v
docker build -t chutu .
```

没有 Chrome 时只跳过真实渲染测试；检测到 Chrome 却启动或渲染失败会让测试失败。渲染测试会自动采用 compose 相同的 `CHROME_NO_SANDBOX=1` 模式，以便在受限桌面和 CI 中运行，仍然真实启动 Chrome。正常本机服务默认保留 Chrome 沙箱；受限环境启动服务需要时也可设置该变量。

测试覆盖登录、退出、限流、权限、禁用会话、CSRF、上传格式/大小/路径、EXIF 8 种方向与手动旋转裁剪组合、GPS 去除、保守调色、粉珠色相、高光、白底像素、rembg 降级、AI 重试和分拣冲突、缺图清理、槽位前缀、队列并发与重启中断。实际 Chrome 测试由假 AI 驱动全流程，检查两种主图尺寸、白底尺寸、详情缺图跳屏/切片尺寸、ZIP 结构和下载。

## 对约定的具体解释与局限

- `photos.manual` 按处理流程补为持久字段；另有会话表、账号版本号，以及 jobs 的内部 `steps` 字段，支持退出/禁用后的会话撤销和重启继续 queued 任务。running 任务在重启后标记“服务重启中断”。
- `step` 只执行一步；`from` 执行该步及以后步骤；二者不能同时传。不带参数跑六步。单步或部分处理完成后保持 draft，render 成功后为 ready。保留已有的中间结果和成品，编辑后的预览需重新 render 才会更新下载包。
- AI 使用 JSON 模式，分拣同时接受数组和 `{photos:[...]}` 包装，以适配只支持 JSON 对象的兼容服务。crop 坐标以 **EXIF 转正 → 手动顺时针旋转** 后的图片为基准，再按比例裁剪。
- 没有提供之前 Python 版源码；清晰度、偏色、dHash 阈值采用本项目内明确实现的启发式规则。白平衡仅从中性色样本估计，通道增益 0.94–1.06，并增加色相保护；亮度增益 1–1.25，死白按 RGB 三通道均 ≥250 计算，增量最多 0.005。E5 不校色。
- 白底商品的长边占画布 65%。用最近邻缩放保留所取源像素；蒙版完全不透明处的 RGB 与修图后 JPG 解码得到的源像素逐像素相等。该保证在合成内存和透明 PNG 上成立，白底 JPG 按约定有损编码，不能保证保存后仍字节相等。
- 上传额外限制：每次最多 32 张、总请求 256 MB、单图最多 4000 万像素。扩展名按真实内容规范化；文本伪装 JPG 会被拒绝。
- 内部 `_t` 只对 `/render/` 有效，寿命 5 分钟。模板的相对脚本和图片请求通过限制在 `/render/` 的临时 HttpOnly cookie 继承登录态。不会放行 API 和原图路由。
- 详情裁切遵循现有脚本的绿底容差和等分规则。模板中首屏、真假对比、尺寸参数、保养与品牌文字屏会保留文字，只有标记为“缺图整屏不出”的屏才整屏跳过，这与原模板一致。
- 纯 Go 降级抠图不具备语义识别能力。AI 文案与材质、尺寸、服务承诺仍需人工复核；违禁词扫描只是提示。Docker 镜像依赖外部仓库，rembg 使用上游 latest，正式上线可固定经过验证的镜像摘要。

## 本次验收记录（2026-10-17）

- `go build ./...`、`go vet ./...`、`go test ./...` 通过，14 项测试全部执行，真实 Chrome 测试未跳过。
- Linux / amd64、`CGO_ENABLED=0` 交叉编译通过；`docker compose config --no-interpolate --quiet` 通过。
- 实际 `go run ./cmd/server` 启动，并复制执行上面的 curl 冒烟命令：上传 3 张、AI Key 为空报错、手动编号修图、Chrome 渲染、下载 ZIP 全部跑通。
- 当前受限 Windows 环境的 Chrome 自身沙箱会导致 GPU 子进程退出，验收使用 `CHROME_NO_SANDBOX=1`；没有修改正常服务的默认值。
- 已尝试 `docker build -t chutu .`，本机 Docker daemon 未运行（`//./pipe/docker_engine` 不存在），所以尚未实际完成镜像构建，也未启动真实 rembg 容器。rembg HTTP 集成与失败降级已用本地假服务测试。

## 高清化与补图验收（2026-10-18）

- `go build ./...`、`go vet ./...`、`go test ./... -count=1` 通过，共 33 项测试；Compose 配置检查通过。
- 新增测试涵盖默认配置与空推理强度、多 image 字段的 multipart、请求重试与超时、PNG/base64、阈值和裁剪、旧数据库及部分迁移、每款开关、禁止补图的编号、4 张限制、失败继续、AI 标识纠正、实拍优先和增强输入不重复裁剪。
- 假 AI 服务器与真实 Chrome 跑通完整六步：3 张实拍高清化、补 3 张图、保守修图、生成文案、导出 ZIP，并检查主图与详情页 DOM 实际显示“AI 生成”和“AI 增强”。测试未访问真实 SubAPI。
- `web/`、`prompts/`、实际 `.env` 未修改；旧版模板的字符串标识兼容只处理本站副本。
