# X Studio

内部电商管理工具：商品库、模特库、场景库、模板库、文案、AI 出图、图片处理，以及库存、采购、订单、组装、财务、任务、知识库和数据与运营；多账户、多店铺权限。

Go 1.26 + SQLite + chromedp。React 前端编译结果（`web/dist/`）embed 进程序；模板、提示词运行时从磁盘读取。代码结构见 [AGENTS.md](AGENTS.md)，当前进度和本机环境见 [交接说明.md](交接说明.md)。

## Windows 本机运行

先安装 Go 1.26、Google Chrome 和 Git for Windows。在 PowerShell 中：

```powershell
cd C:\Users\xcc19\Documents\x-studio
go mod download
$env:ADMIN_USER = 'admin'
$env:ADMIN_PASSWORD = '请换成自己的初始密码123'
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
$secretBytes = New-Object byte[] 32; $rng.GetBytes($secretBytes)
$env:SESSION_SECRET = [Convert]::ToBase64String($secretBytes); $rng.Dispose()
$env:AI_MODEL_VISION = 'gpt-6-sol'
$env:AI_MODEL_TEXT = 'gpt-6-sol'
$env:AI_REASONING_EFFORT = 'medium'
$env:IMAGE_MODEL = 'gpt-image-2.5-sunburst'
$env:AI_API_KEY = ''  # 空值也能启动，AI 步骤会报中文错误
$env:REMBG_URL = 'http://127.0.0.1:7000'
go run ./cmd/server
```

打开 <http://localhost:8080>。Chrome 从常见安装目录或 PATH 自动寻找；也可设置 `$env:CHROME_PATH = 'C:\Program Files\Google\Chrome\Application\chrome.exe'`。前端尚未构建时显示“前端未构建”；在 `frontend/` 执行 `npm ci && npm run build` 后重新编译 Go。运行目录应为仓库根目录，这样默认相对路径能找到模板和提示词。

只有数据库里还没有任何用户时，才会使用 `ADMIN_USER` / `ADMIN_PASSWORD` 建管理员。密码要求 8–72 字节。首次成功后可以删除这两个环境变量，用管理员登录后在「设置 → 账号」建员工账号。`SESSION_SECRET` 必须至少 32 字符，应保存好并在重启时复用；改变它会使已有登录失效。修改密码、角色或禁用账号也会使该账号已有会话失效。最后一个启用的管理员不能被禁用或降级。

rembg 可以单独运行，也可以不运行；请求失败、超时、返回错误图片时自动采用 Go 的边缘连通抠图，并在 `retouch_log.cutout` 中写 `fallback`。复杂背景与珍珠颜色相近时应人工复核。

## Docker Compose 部署

在仓库根目录的 `.env` 中填真实配置（不要提交密钥）：

```dotenv
ADMIN_USER=admin
ADMIN_PASSWORD=换成自己的初始密码123
SESSION_SECRET=换成至少32字符的随机密钥并长期保存
AI_API_KEY=
AI_MODEL_VISION=gpt-6-sol
AI_MODEL_TEXT=gpt-6-sol
AI_REASONING_EFFORT=medium
IMAGE_MODEL=gpt-image-2.5-sunburst
IMAGE_TIMEOUT=240
AUTO_UPSCALE=1
AUTO_FILL=1
UPSCALE_MIN_SIDE=900
APP_PORT=8080
COOKIE_SECURE=0
WORKERS=2
```

```bash
docker compose up -d --build
docker compose logs -f app
```

app 镜像安装 Chromium、`fonts-noto-cjk` 和 CA 证书，以非 root 用户运行。Docker 构建上下文就是仓库根目录，主图和详情页模板、店标都在 `templates/`，直接在这里改。`prompts/` 通过 compose 只读挂载，修改提示词无须重编译。数据放在 `app-data` 卷；rembg 模型缓存放在 `rembg-models` 卷，首次使用模型可能需要联网下载。rembg 镜像已含 `rembg` 入口，因此 compose 的 command 从 `s` 开始。接口按 [rembg 官方文档](https://github.com/danielgatis/rembg#rembg-s) 的 multipart `POST /api/remove` 调用。

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
| `IMAGE_MODEL` | `gpt-image-2.5-sunburst`；高清化和补图 |
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

## 验证

```powershell
go build ./...
go vet ./...
go test ./...
# 仅跑实际 Chrome + 假 AI 的完整流程：
go test ./internal/modules/pipeline -run TestRenderFullPipelineDimensionsAndZip -count=1 -v
docker build -t xstudio .
```

没有 Chrome 时只跳过真实渲染测试；检测到 Chrome 却启动或渲染失败会让测试失败。渲染测试会自动采用 compose 相同的 `CHROME_NO_SANDBOX=1` 模式，以便在受限桌面和 CI 中运行，仍然真实启动 Chrome。正常本机服务默认保留 Chrome 沙箱；受限环境启动服务需要时也可设置该变量。

测试覆盖登录、退出、限流、权限、禁用会话、CSRF、上传格式/大小/路径、EXIF 8 种方向与手动旋转裁剪组合、GPS 去除、保守调色、粉珠色相、高光、白底像素、rembg 降级、AI 重试和分拣冲突、缺图清理、槽位前缀、队列并发与重启中断。实际 Chrome 测试由假 AI 驱动全流程，检查两种主图尺寸、白底尺寸、详情缺图跳屏/切片尺寸、ZIP 结构和下载。
