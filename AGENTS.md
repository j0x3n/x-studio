# X Studio：给编码代理的说明

内部电商管理工具（Go 后端 + React 前端，SQLite）。接手先读 [交接说明.md](交接说明.md)，再读要做的那块的「接口约定-*.md」。

## 结构

| 路径 | 内容 |
|---|---|
| `cmd/server/main.go` | 入口，模块列表在这里注册 |
| `internal/core/` | 公共框架：配置、路由和权限（`perm.go`）、迁移、审计（`audit.go`）、后台任务、AI 调用、品类 |
| `internal/modules/<模块>/` | 每个模块自己的路由、表、迁移、测试：`accounts` 账号和角色、`products` 商品和上架版本、`assets` 图片库和处理、`copywriting` 文案、`studio` 模板和出图审核、`pipeline` 一键出图、`assistant` AI 助手、`tasks` 任务和通知 |
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
- 分工和并行规则见 [并行开发规则.md](并行开发规则.md)。
