# X Studio：给编码代理的说明

内部电商管理工具（Go 后端 + React 前端，SQLite）。接手先读 [交接说明.md](交接说明.md)，再读要做的那块的「接口约定-*.md」。

## 结构

| 路径 | 内容 |
|---|---|
| `cmd/server/main.go` | 入口，模块列表在这里注册 |
| `internal/core/` | 公共框架：配置、路由和权限（`perm.go`）、迁移、审计（`audit.go`）、后台任务、AI 调用、品类 |
| `internal/modules/<模块>/` | 每个模块自己的路由、表、迁移、测试：`accounts` 账号和角色、`products` 商品和上架版本、`assets` 图片库、处理台、店铺模特库和试穿试戴（`models.go/model_files.go/model_jobs.go/model_workbench.go`）、`copywriting` 文案、`studio` 模板和出图审核、`pipeline` 一键出图、`assistant` AI 助手、`tasks` 任务和通知、`inventory` 库存（`stock.go` 是给采购和订单用的库存 Go 接口）、`kb` 知识库、`purchase` 采购（供应商、采购单、分批到货质检、不合格退货）、`orders` 手工销售订单（占用、发货、售后）、`creative` AI 出图、`scenes` 场景库（静物台面、穿搭、光线、参考图，绑定到 listing / 店铺，`scenes.Resolve` 给 creative 用）、`market` 数据与运营（阶段 1 已实现，market 诊断建议待做） |
| `frontend/src/pages/` | 页面；`components/` 通用组件（`Shell` 侧栏、`ui` 基础组件、`TaskViews` 任务抽屉、`NoticeBell` 铃铛）；`lib/` 接口、类型、各模块的中文和工具函数 |
| `frontend/src/styles/` | 样式，按 admin-design-system 规范；模块样式写在 `app.css` 末尾的分段里 |
| `prompts/`、`templates/` | 提示词、详情页模板（不是 Go 代码） |
| `web/dist/` | 前端编译结果，`go build` 时打进程序 |
| `data/` | 本机数据（数据库、图片），不进 git |
| `.cache/` | 本机缓存、日志、数据库备份、程序，不进 git |

## 约定

- 模块之间不直接改对方的表；需要别的模块的事件就扫 `audit_log` / `jobs`（参照 `tasks` 模块）。
- 新权限点加在 `internal/core/perm.go`，给内置角色补权限用 `accounts` 模块的新迁移。
- 写操作都调 `core.Audit`；请求要带 `X-Requested-With: fetch`。
- 后端测试离线跑，命令见交接说明第 4 节。
- 素材文件夹见 `接口约定-图片库与处理.md` 第 8 节（后端 `assets/folders.go`）；前端通用组件：`Lightbox.tsx`（站内看大图，不要再用 `target=_blank` 打开图片）、`ContextMenu.tsx`（右键菜单）、`AssetFolders.tsx`。
- AI 模特入口 `/models` 和 `/tryon`，契约见 `接口约定-AI模特.md`；前端类型在 `lib/tryon.ts`，模特参考永久保存，生成候选和试穿复用本人 workbench。
- 三期经营已开发并升级本机：组装 `internal/modules/assembly`、财务 `finance`；新入口 `/assembly`、`/finance`、`/replenishment`、`/purchase-recognize`，契约见 `接口约定-三期经营.md`。库存变更仍通过 `inventory.StockIn/StockOut`，财务不写采购/订单表，事务内鉴权用同一事务避免单 SQLite 连接死锁。
- 本轮用户要求减少测试与额度占用，全部开发完成后统一手动验证；三期已于 2026-10-21 按 `手动验收-三期经营.md` 验收通过。知识库助手/经营派活已恢复整合，不要重复合并历史暂停分支。
- AI 工作流改造（AI 主图、模特、生图屏模板、三层复核、数据与运营）按 [AI工作流改造方案.md](AI工作流改造方案.md) 来做；这一块的分工以方案第 7 节为准，契约见 [接口约定-AI出图.md](接口约定-AI出图.md)、[接口约定-数据与运营.md](接口约定-数据与运营.md)。AI 补图编号带 `_AI` 后缀，实拍证据位不收 AI 生成或改过的图（`internal/media`）；生图只能用三种尺寸，生成后要核对返回尺寸。
- 分工和并行规则见 [并行开发规则.md](并行开发规则.md)：当前模式是 Codex / Grok Build / agy 并行实施，Claude 选模块分派并审核整合（第 0 节）。
- AI 出图 / 数据与运营（阶段 1 后端已合并，见交接说明 2.8）：契约 `接口约定-AI出图.md`、`接口约定-数据与运营.md`；前端 `lib/creative.ts`、`lib/market.ts`（含假数据，`localStorage xs-mock=1`）、`components/Creative*.tsx`、`pages/{CreativeReview,Market,Advice}.tsx`；生图模板 `templates/生图模板/`（字段扩展见其 README）；提示词 `prompts/生图-*.md`。
- 界面重整（2026-10-22，见交接说明 2.10）：契约 [接口约定-素材场景店铺.md](接口约定-素材场景店铺.md)。侧栏「上新」组按流程排（`Shell.tsx`）；素材库是商品库的「全部图片和视频」视图，默认隐藏 `stale`；照片编号在界面上一律用 `useCodeName()` 显示中文名；场景 `pages/Scenes.tsx` + `lib/scenes.ts`，内置场景 `templates/生图场景/`；店铺平台和站点 `lib/shops.ts`；模拟预览 `components/ListingPreview.tsx`。
