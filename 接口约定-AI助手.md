# 第二期·首页和 AI 助手：接口约定

- **日期**：2026-10-18
- **背景**：界面改成「清爽后台」（侧边栏按模块分组）+ 首页「今天」+ 右侧常驻 AI 助手。对应 [功能模块设计.md](功能模块设计.md) 第 1 号「工作台」和第 8 号「AI 助手」。
- **共同依据**：[接口约定.md](接口约定.md)、[并行开发规则.md](并行开发规则.md)（第 3 节共同约束照样适用）、各模块约定。
- **分工**：后端新建 `internal/modules/assistant/`（编码 AI）；前端、提示词 `prompts/助手.md`（Claude）。

## 1. 结论

1. **助手只读、只提议，不直接改数据**。后端让 AI 调用只读工具查数据，最后返回一段回复和若干「动作卡」。用户在前端点确认后，**前端用用户自己的登录态调用现有接口**执行。这样权限、额度、日志全部沿用现有接口，助手本身不会越权。
2. **首页和助手共用一套「今天的待办」统计**，后端一个函数，两个出口：`GET /api/home` 和工具 `today`。
3. 所有数据都按当前用户的可见范围过滤（`CanProduct` / `CanListing` / `CanShop`），工具和动作卡都不能带出用户看不到的商品、上架版本、素材。

## 2. 文件归属

| 路径 | 归谁 |
|---|---|
| `internal/modules/assistant/**`（新建） | 编码 AI |
| `cmd/server/main.go` | 编码 AI，只在模块列表末尾加 `assistant.New(a)` |
| `README.md` | 编码 AI，只在末尾加「AI 助手」小节 |
| `internal/core/settings.go` 的 `aiFeatures` | Claude 已加 `assistant`，编码 AI 不改 |
| `prompts/助手.md`、`frontend/**`、所有约定文档 | Claude |
| 其他所有文件 | 冻结；确实需要改的写进汇报 |

不建新表。只读其他模块的表（products、listings、assets、jobs、copy_versions、listing_render、shops、ai_usage、quotas、presets 等），不写。

## 3. 首页：`GET /api/home`

权限 `LoggedIn`。返回：

```json
{
  "counts": {
    "in_progress": 64,        // 状态 sampling 或 pending 的商品数
    "review_pending": 4,      // 待审核的上架版本数；没有 render.review 权限时为 null
    "failed": 2,              // pipeline_status=failed 的上架版本数
    "no_photo": 5,            // 未归档、一张素材都没有的商品数
    "copy_unapplied": 3       // 最新文案版本是 AI 生成/改写、还没应用的上架版本数；没有 copy.view 时为 null
  },
  "todos": [Todo],            // 按 level（danger → warning → info）再按时间排，最多 10 条
  "recent_assets": [          // 最近产出：看得到的素材，created_at 倒序，最多 8 张
    {"id": 31, "product_id": 3, "product_name": "珠蝶", "title": "…", "source": "ai_enhanced",
     "thumb_url": "/api/assets/31/file?w=240", "created_at": "…"}
  ]
}
```

`thumb_url` 用图片库现有的缩略图地址格式（先看 assets 模块的 `thumb_url` 怎么拼，保持一致）。

### Todo

```json
{
  "kind": "render_failed | review_pending | copy_unapplied | no_photo",
  "level": "danger | warning | info",
  "title": "南洋金珠吊坠 11mm · 出图失败",
  "detail": "虾皮台湾 · AI 分拣超时",
  "count": 1,
  "product_id": 7, "listing_id": 12,   // 有就给，没有省略
  "action": Action | null               // 可以一键处理的给一个动作卡，没有就是 null
}
```

| kind | 怎么算 | 一条还是汇总 | title / detail | action |
|---|---|---|---|---|
| `render_failed` | `pipeline_status='failed'` 的上架版本 | 每个版本一条，level `danger` | 「商品名 · 出图失败」/「店名 · pipeline_error 前 40 字」 | 有 render.run 时 `run_listing` |
| `review_pending` | 审核队列（和 `GET /api/review/queue` 同口径） | 汇总一条，level `warning`；没有 render.review 不出 | 「审核 N 款成品图」/「前两个商品名 等」 | `open` `/review` |
| `copy_unapplied` | 上架版本的最新一个 copy_versions 是 `ai_generate`/`ai_rewrite` 且 `applied_at` 为空 | 汇总一条，level `info`；没有 copy.view 不出 | 「确认 AI 写好的 N 份文案」/「前两个商品名 等」 | `open` `/copy?listing=<第一个的 listing_id>` |
| `no_photo` | 未归档、素材数为 0 的商品 | 汇总一条，level `info` | 「N 款还没传照片」/「前两个商品名 等」 | `open` `/products?tab=sampling` |

计数为 0 的不出现在 todos 里。

## 4. 助手：`POST /api/assistant/chat`

权限 `LoggedIn`（AI 额度由 `core.AIJSON` 计量，功能名 `assistant`，计入文本额度）。同步返回，不建后台任务。

### 4.1 请求

```json
{
  "messages": [
    {"role": "user", "content": "珠蝶的虾皮版本为什么出图失败？"},
    {"role": "assistant", "content": "……"},
    {"role": "user", "content": "那帮我重跑"}
  ],
  "context": {
    "path": "/products/3",
    "title": "商品 · 珠蝶",
    "selection": {"type": "product | listing | asset", "ids": [3]}
  }
}
```

- `messages`：1–20 条，最后一条必须是 `user`；每条 `content` 1–2000 字（按 rune 算）。超出返回 400 中文错误。
- `context` 可选；`selection.ids` 最多 50 个，**只把用户看得到的 id 交给 AI**，看不到的静默丢掉。
- 历史对话由前端保存，后端不存。

### 4.2 后端流程

1. 读 `prompts/助手.md`（每次读，缺失时返回 500「缺少提示词 助手.md」），替换占位符：
   - `{{today}}`：上海时区日期，如「2026-10-18 周日」
   - `{{user}}`：显示名 + 角色名 + 分到的店铺名
   - `{{tools}}`：按用户权限生成的工具清单（见 4.3，只列有权限的）
   - `{{actions}}`：按用户权限生成的动作清单（见 4.4，只列有权限的）
   - `{{context}}`：上面 context 的中文描述（页面路径、标题、选中了哪些对象，带名称）
2. 循环最多 **5 轮**，每轮一次 `core.AIJSON`（`WithFeature(ctx, "assistant")`，模型用文本模型）。`content` 传对话记录：用户/助手消息 + 之前各轮的工具调用和工具结果。
3. AI 每轮返回二选一：
   - 调工具：`{"tool_calls": [{"name": "get_product", "args": {"id": 3}}]}`，一轮最多 4 个调用；
   - 给答复：`{"reply": "…", "actions": [Action], "refs": [Ref]}`。
4. 收到 `tool_calls` 就执行，结果（每个工具结果 JSON 截到 6000 字）追加到对话记录，进下一轮。未知工具、参数不对、没权限、看不到的对象，都作为工具结果返回中文错误给 AI，不中断。
5. 第 5 轮还在调工具：最后再调一次，在对话末尾加「工具调用次数已用完，必须现在用已有信息给出 reply」，这次只接受答复格式。
6. 校验答复（见 4.4），返回。
7. 记 `core.Audit`：action `assistant.chat`，target `user`/本人 id，detail `{rounds, tools:[工具名], actions:[动作类型], chars}`，**不记对话内容**。

AI 调用失败（没配置 key、额度不够、超时）时返回 502，`error` 用 `core.AIJSON` 给的中文错误。

### 4.3 工具（全部只读）

| 名称 | 需要权限 | 参数 | 返回 |
|---|---|---|---|
| `today` | 登录 | — | 和 `/api/home` 的 `counts` + `todos` 相同 |
| `search_products` | product.view | `q?` 款名/编号模糊，`status?`，`shop_id?`，`limit?` ≤20（默认 10） | `[{id, name, status, asset_count, listings:[{id, shop_id, shop_name, pipeline_status, pipeline_step, pipeline_error, review_status}]}]` |
| `get_product` | product.view | `id` | 基本信息（`info`；有 product.cost 才带 `cost`、`supplier`）、`listings`（同上 + 当前出图模板、最近一个任务的 status/message/finished_at）、素材按来源计数 `{original, ai_enhanced, ai_edited, ai_generated}` |
| `list_assets` | product.view | `product_id`，`source?`，`tag?`，`limit?` ≤50 | `[{id, code, title, source, tags, width, height, parent_id, created_at}]` |
| `recent_jobs` | 登录 | `status?`（`failed`/`running`/`done`），`limit?` ≤20 | 本人发起、或目标对象本人看得到的任务：`[{id, kind, target_type, target_id, target_name, status, message, created_at, finished_at}]` |
| `review_queue` | render.review | `shop_id?` | 和 `GET /api/review/queue` 同口径，最多 20 条 |
| `list_presets` | asset.upload | — | `[{id, name, shared, ops}]` |
| `list_templates` | product.view | `shop_id?` | `{detail:[{key,name,is_default,disabled}], main:[…]}` |
| `my_usage` | 登录 | — | 本月 `{text:{used, quota}, image:{used, quota}}`，quota 为 null 表示不限 |

工具清单写进 `{{tools}}` 时，每个工具一行：名称、用途、参数。

### 4.4 动作卡

后端**不执行**，只校验后返回给前端。前端把 `type` 映射到现有接口，用户确认后调用。

```json
{
  "type": "run_listing",
  "label": "重跑出图",                       // 按钮文字，≤12 字
  "summary": "南洋金珠吊坠 · 虾皮台湾 · 从头跑一键出图",  // 卡片说明，≤60 字
  "params": {"listing_id": 12},
  "cost_hint": "约用 6 张图额度"              // 可选，后端估算，见下
}
```

| type | 需要权限 | params | 前端调用 |
|---|---|---|---|
| `open` | 登录 | `{path}`，必须以 `/` 开头、只能是前端路由（`/`、`/products…`、`/assets…`、`/process…`、`/copy…`、`/templates…`、`/review…`、`/admin…`、`/settings…`），不能带 `//` 或协议 | 跳转，不用确认 |
| `run_listing` | render.run + 看得到该版本 | `{listing_id}` | `POST /api/listings/{id}/run` |
| `render_listing` | render.run | `{listing_id}` | `POST /api/listings/{id}/render/run` |
| `render_batch` | render.run | `{listing_ids}` ≤50 | `POST /api/render/batch` |
| `process_assets` | 按 ops 判断，规则同 `POST /api/assets/process` | `{asset_ids, ops, preset_id?}`，asset_ids 必须同一商品、≤20 | `POST /api/assets/process` |
| `generate_copy` | copy.edit | `{listing_id, instructions?}` | `POST /api/listings/{id}/copies/generate` |
| `review` | render.review | `{listing_id, status: approved\|rejected, note}`；rejected 时 note 必填 | `POST /api/listings/{id}/review` |

校验规则：
- 未知 type、缺参数、没权限、对象看不到、数量超限的动作**丢掉**，并在响应里 `dropped` 加 1；全部丢掉也照常返回 reply。
- 一次答复最多 4 个动作。
- `cost_hint`：`process_assets` 含 ai 或 upscale 时为「约用 N 张图额度」（N = 张数）；`run_listing` 为「约用 N 张图额度」（N 按 pipeline 现有的预估方法，拿不到就不给）；其他不给。
- `label`、`summary` 超长就截断。

### Ref

```json
{"type": "product | listing | asset | job", "id": 3, "label": "珠蝶", "path": "/products/3"}
```

答复里提到的对象，前端渲染成可点的小链接。看不到的对象丢掉。

### 4.5 响应

```json
{"reply": "…", "actions": [Action], "refs": [Ref], "dropped": 0, "rounds": 2}
```

`reply` 为空字符串时后端替换成「我没能给出答复，换个说法试试。」。

## 5. 验收

- `go test ./...` 通过；测试只用 httptest 假 AI 和临时目录。
- 必须有的测试：
  - `/api/home`：四类待办的计数和权限（没有 render.review 不出审核待办、`review_pending` 为 null；外部协作者只看到分享给他的款）。
  - 助手多轮：假 AI 第一轮要 `get_product`、第二轮给答复，结果里能看到工具结果被传进了第二轮的请求。
  - 越权：AI 请求一个看不到的商品 → 工具返回错误；AI 给出看不到的 listing 的 `run_listing` → 被丢掉、`dropped=1`。
  - 没有 render.run 的用户：`{{actions}}` 里没有 run_listing，AI 硬给也被丢掉。
  - 5 轮上限：假 AI 一直调工具，第 6 次调用后返回答复。
  - `open` 的 path 校验：`//evil.com`、`https://…`、`/api/…` 都被丢掉。
  - 请求校验：空 messages、最后一条不是 user、超长内容 → 400。
  - 提示词缺失 → 500 中文错误。
- 汇报：改了哪些文件、测试命令和结果、和本约定不一致的地方、风险。

## 6. 补充：一句话派活（2026-10-19）

用户对助手说「让小林明天下午把珠蝶修一下」，助手查到人和商品后给一张 `create_task` 动作卡，用户确认后前端调 `POST /api/tasks`。规则照第 1 节：助手只提议，真正建任务走任务模块的接口（权限、日志都在那边）。

### 6.1 文件归属

| 路径 | 归谁 |
|---|---|
| `internal/modules/assistant/**` | 编码 AI |
| `prompts/助手.md`、`frontend/**`、本约定 | Claude |
| 其他所有文件（含 `internal/modules/tasks/**`） | 冻结，只读 |

assistant 模块**不 import tasks 包**，需要的检查用 `core` 已有的 `a.User(id)`、`u.Has`、`CanProduct` / `CanListing` / `assetProduct` 自己做，口径和 tasks 模块的 `checkMember`、`requireTarget` 一致。

### 6.2 `{{today}}` 带上时间

改成「2026-10-19 周一 14:30」（上海时区，到分钟），AI 才能把「明天下午」「两小时后」换算成截止时间。

### 6.3 新工具 `list_assignees`

| 名称 | 需要权限 | 参数 | 返回 |
|---|---|---|---|
| `list_assignees` | task.view | `target_type?`（`product`/`listing`/`asset`）+ `target_id?`，两个要么都给要么都不给 | `[{id, name, username, role_name, open_count}]` |

- 口径同 `GET /api/tasks/assignees`：未停用、有 `task.view` 的账号；给了对象时只返回看得到它的人。对象当前用户看不到 → 工具返回错误。
- `name` 是显示名，`open_count` 是手上 todo/doing/waiting 的任务数。
- 当前用户**没有 `task.manage`** 时只返回他自己（反正派不了别人）。
- 最多 50 人，按 id 排。

### 6.4 新动作 `create_task`

需要 `task.view`。

```json
{
  "type": "create_task",
  "label": "建任务",
  "summary": "（后端重写，见下）",
  "params": {
    "title": "修图：珠蝶",
    "body": "主图去灰、调白平衡",
    "assignee_id": 3,
    "priority": "high",
    "due_at": "2026-10-20T18:00:00+08:00",
    "target_type": "product", "target_id": 7,
    "checklist": [{"text": "去灰", "done": false}],
    "watcher_ids": [5]
  },
  "preview": {"assignee": "小林", "due": "10-20 周二 18:00", "target": "珠蝶"}
}
```

参数和校验（不合格的整张卡丢掉，`dropped` 加 1）：

| 参数 | 规则 |
|---|---|
| `title` | 必填，去掉首尾空白后 1–100 字 |
| `body` | 可选，≤1000 字 |
| `assignee_id` | 可选。不给 = 派给自己；`0` = 暂不派人；其他 = 别人。派给别人或 `0` 需要当前用户有 `task.manage`。负责人必须存在、未停用、有 `task.view`、看得到挂的对象 |
| `priority` | 可选，`low`/`normal`/`high`/`urgent` |
| `due_at` | 可选，RFC3339 带时区（`time.RFC3339` 能解析）；早于现在 1 小时以上的丢掉 |
| `target_type` + `target_id` | 可选，两个要么都给要么都不给；当前用户必须看得到 |
| `checklist` | 可选，≤20 项，每项 `text` 1–100 字，`done` 只能是 false |
| `watcher_ids` | 可选，≤10 个，不重复，规则同负责人；不能包含负责人 |
| 其他键 | 有就丢掉整张卡 |

后端校验通过后**规范化**卡片，前端照着发、照着显示，不信 AI 写的文字：

- `params.assignee_id` 一定写出来（没给时填当前用户 id），其他参数原样保留（`title` 用去掉空白后的）。
- `preview`（新字段，`omitempty`，只有 `create_task` 有）：
  - `assignee`：负责人显示名；是自己时为「自己」；`0` 时为「暂不派人」。
  - `due`：上海时区「01-02 周一 15:04」；没有截止时间为空串。
  - `target`：对象名称（同 Ref 的 label 规则）；没挂对象为空串。
- `summary` 由后端重写：`标题 · 负责人 · 截止 · 对象`，空的部分省略，截到 60 字。
- `label` 照常截 12 字，为空时填「建任务」。
- 不给 `cost_hint`。

### 6.5 `open` 支持任务页

`safePath` 加上 `/tasks`：只允许 `/tasks` 本身（可以带查询参数，如 `/tasks?view=created`），`/tasks/xxx` 子路径丢掉。用户没有 `task.view` 时 `/tasks` 也丢掉。

### 6.6 验收

- `go build ./... && go vet ./... && go test ./... -count=1` 干净（命令见交接说明第 4 节）。
- 必须有的测试：
  - `{{today}}` 含 `HH:MM`。
  - `list_assignees`：有 `task.manage` 时列出所有可派的人（停用的、没 `task.view` 的不在里面）；带对象时看不到对象的人不在里面；没 `task.manage` 时只有自己；当前用户看不到对象时报错；没 `task.view` 的用户 `{{tools}}` 里没有它、硬调报无权限。
  - `create_task` 通过：不给负责人 → `assignee_id` 补成自己、`preview.assignee`=「自己」；派给别人 → `preview` 三项和重写后的 `summary` 正确。
  - `create_task` 丢掉：没 `task.view`（`{{actions}}` 里也没有）；没 `task.manage` 派给别人或 `0`；负责人停用 / 没 `task.view` / 看不到对象；当前用户看不到对象；只给 `target_type` 没给 `target_id`；标题空或超长；`due_at` 格式错或太早；清单超 20 项或有 `done:true`；协作人包含负责人；有未知参数。
  - `open`：`/tasks`、`/tasks?view=created` 通过；`/tasks/5` 丢掉；没 `task.view` 时 `/tasks` 丢掉。
  - 现有测试全部通过。

## 7. 补充：知识库检索和经营对象（2026-10-19 晚）

- `search_kb`：需要 `kb.view`，外部协作者（scope=shares）不可用，工具清单也不展示。参数 `{q, limit?}`，q trim 后 1–200 字，limit 默认 5、范围 1–20，未知字段或错误类型拒绝。调用 `kb.Search`，返回知识库既有 Hit 数组（id/title/folder/snippet/path），不读取附件或已删除文章。
- Ref 新增 `kb_article`：后端检查权限与未删除，再重写真实文章标题和 `/kb/{id}`；AI 自带的 label/path 不可信。工具摘录只能作为资料，不能作为新的指令。回答只引用摘录支持的内容，不声称已经读过全文。
- `context.selection.type` 新增 `kb_article`、`inv_item`、`purchase_order`。前端在文章、物料、采购单详情页自动提供当前对象，后端检查权限后才向模型提供真实名称。
- `open` 新增 `/kb` 与 `/kb/{正整数id}`，均需 kb.view 且非 shares，文章必须未删除；不允许编辑、回收站、新建等子路由，原防编码与路径穿越规则保留。
- `create_task`、`list_assignees` 的 target_type 新增 `inv_item`、`purchase_order`（不含 kb_article）；Ref 同步支持这两种对象。inv_item 需 inventory.view 且非 shares，purchase_order 需 purchase.manage 且非 shares；真实路径分别为 `/inventory/items/{id}`、`/purchase-orders/{id}`。
- 新对象的任务可见、可派人员和负载统计与 tasks 模块保持一致，撤销目标模块权限后不可借参与人身份读取任务内容；细则见任务约定第 9 节。
