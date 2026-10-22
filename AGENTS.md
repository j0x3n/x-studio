# X Studio：给编码代理的说明

内部电商管理工具（Go 后端 + React 前端，SQLite）。接手先读 [交接说明.md](交接说明.md)（现状、下一步、怎么跑）。没有固定的接口约定文档，需求会变，以代码为准。

## 结构

| 路径 | 内容 |
|---|---|
| `cmd/server/main.go` | 入口，模块列表在这里注册 |
| `internal/core/` | 公共框架：配置、路由和权限（`perm.go`）、迁移、审计（`audit.go`）、后台任务、AI 调用、品类 |
| `internal/modules/<模块>/` | 每个模块自己的路由、表、迁移、测试：`accounts` 账号和角色、`products` 商品和上架版本、`assets` 图片库、处理台、模特库（可全局共用）和试穿试戴（`models.go/model_files.go/model_jobs.go/model_workbench.go`）、商品底图槽位和高清化替换（`views*.go`）、`copywriting` 文案、`studio` 旧 HTML 模板和出图审核、`pipeline` 一键出图、`assistant` AI 助手、`tasks` 任务和通知、`inventory` 库存（`stock.go` 是给采购和订单用的库存 Go 接口）、`kb` 知识库、`purchase` 采购（供应商、采购单、分批到货质检、不合格退货）、`orders` 手工销售订单（占用、发货、售后）、`creative` AI 出图和出图方案（`plans*.go`）、`scenes` 场景库（静物台面、穿搭、光线、参考图、机位、布局框、定场图，绑定到 listing / 店铺，`scenes.Resolve` 给 creative 用）、`props` 道具库（道具设定卡、锁定参考图）、`market` 数据与运营（诊断和建议待做）、`assembly` 组装加工、`finance` 财务和对账 |
| `frontend/src/pages/` | 页面；`components/` 通用组件（`Shell` 侧栏、`ui` 基础组件、`TaskViews` 任务抽屉、`NoticeBell` 铃铛）；`lib/` 接口、类型、各模块的中文和工具函数 |
| `frontend/src/styles/` | 样式，按 admin-design-system 规范；模块样式写在 `app.css` 末尾的分段里 |
| `prompts/`、`templates/` | 提示词、详情页模板（不是 Go 代码） |
| `deploy/` | Docker 和服务器部署：`Dockerfile`、本机更新脚本 `local-update.sh`、服务器用的 compose / Caddy / `remote-deploy.sh`；GitHub Actions 在 `.github/workflows/deploy.yml` |
| `web/dist/` | 前端编译结果，`go build` 时打进程序 |
| `data/` | 本机数据（数据库、图片），不进 git |
| `.cache/` | 本机缓存、日志、数据库备份、程序，不进 git |

## 约定

- 模块之间不直接改对方的表；需要别的模块的事件就扫 `audit_log` / `jobs`（参照 `tasks` 模块）。库存变更通过 `inventory.StockIn/StockOut`；财务不写采购和订单表；事务里鉴权用同一个事务，避免单 SQLite 连接死锁。
- 新权限点加在 `internal/core/perm.go`，给内置角色补权限用 `accounts` 模块的新迁移。
- 写操作都调 `core.Audit`；请求要带 `X-Requested-With: fetch`。
- **改完代码要更新本机 Docker**：每完成一块能看到效果的改动（前端、后端、模板），提交前跑 `bash deploy/local-update.sh`（只改后端加 `--skip-web`），确认最后一行是「完成」，用户直接在 http://127.0.0.1:8080 看。只改提示词不用跑（目录挂进了容器）。部署方式见 [deploy/README.md](deploy/README.md)。
- 后端测试离线跑，命令见交接说明第 4 节。用户偏好少跑测试、少耗 AI 额度：只做必要的编译和离线检查，真实 AI 和浏览器效果留给用户手动验收。
- AI 补图编号带 `_AI` 后缀，证据位和模特位不收 AI 生成或改过的图（`internal/media`）；生图只能用三种尺寸，生成后要核对返回尺寸。生图模板字段见 `templates/生图模板/README.md`，内置场景见 `templates/生图场景/README.md`。
- 前端：侧栏 `Shell.tsx`（「素材」「制作」两组按上新流程排，图片处理、试穿试戴从商品页和模特库进）；商品底图槽位 `components/ProductViews.tsx`，出图方案 `PlanEditor.tsx`（表单）/ `CreativePlans.tsx`（模板库分页）/ `ProductPlan.tsx`（商品页），场景布局框 `SceneLayout.tsx`，道具库 `pages/Props.tsx`，这几块样式在 `styles/materials.css`；照片编号在界面上一律用 `useCodeName()` 显示中文名；通用组件 `Lightbox.tsx`（站内看大图，不要用 `target=_blank` 打开图片）、`ContextMenu.tsx`（右键菜单）、`AssetFolders.tsx`、`ListingPreview.tsx`（商品页模拟预览）；各模块类型和接口在 `lib/<模块>.ts`（如 `creative.ts`、`market.ts`、`scenes.ts`、`props.ts`、`views.ts`（底图和出图方案）、`shops.ts`、`tryon.ts`）。
- `lib/market.ts`、`lib/creative.ts` 带假数据，`localStorage xs-mock=1` 打开。
