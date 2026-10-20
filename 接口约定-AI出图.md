# 接口约定：AI 出图（creative）

- **日期**：2026-10-21，Claude 写，阶段 0。方案见 [AI工作流改造方案.md](AI工作流改造方案.md) 第 3–5、7 节；和 [接口约定-文案与出图.md](接口约定-文案与出图.md) 冲突时以本文件为准。
- **实施**：Codex 一个会话做到底（第 1–9 节的后端）；Claude 写提示词、生图模板、全部前端（第 10 节），阶段 2 整合。
- 金额、计数都是整数；时间 UTC RFC3339；所有写操作调 `core.Audit` / `core.AuditTx`；外部协作者（scope=shares）一律 403。

## 0. 阶段 0 已经做好的（Codex 不要再改）

| 位置 | 内容 |
|---|---|
| `internal/core/perm.go` | 权限点 `creative.run`（生成、重画、改字、导出）、`creative.review`（复核、人工判断、整套过目）、`creative.template`（模板、店铺出图设置） |
| `accounts` v6 | 运营：run + review；美工：run + review + template；摄影不给 |
| `cmd/server/main.go` | 已注册空模块 `internal/modules/creative`（`module.go`），Codex 在里面填 |
| `core/settings.go` | AI 功能名 `creative`（生图、改图，没配时跟 `process`）、`creative_verify`（看图复核，跟 `classify`）、`creative_text`（读图上文字，跟 `classify`）；设置 `creative`（下表），启动时自动补缺省值；`a.SettingWithDefaults("creative")` 读取 |
| 默认生图模型 | `gpt-image-2.5-sunburst`（`.env`、`config.go`） |
| pipeline | AI 补图编号改成 `A1_AI/E1_AI/E2_AI/E3_AI`；不再补 D4；证据位只认实拍（`media.AIMade`） |

设置 `creative`（`PATCH /api/settings`，`admin.settings`）：

| 字段 | 缺省 | 含义 |
|---|---|---|
| `final_glance` | true | 导出前整套必须有人点过「整套过目」 |
| `max_images_per_listing` | 40 | 每套累计生成次数（含重画、改字）上限，到了暂停 |
| `auto_pass_score` | 90 | 自动通过要求两次复核总分都 ≥ 它 |
| `manual_score_min` | 60 | 低于它且还能重画就自动重画 |
| `max_auto_redraw` | 2 | 每个位置最多自动重画次数 |
| `calibration_count` | 20 | 每类模特图校准期张数 |
| `calibration_agreement` | 0.95 | 一致率达到它才开放这一类自动通过 |
| `spot_check_rate` | 0.1 | 本该自动通过的图随机转人工的比例 |
| `size_tolerance` | 0.2 | 尺寸估算偏差上限 |
| `size_compare_tolerance` | 0.1 | 多尺寸对比图各张之间比例偏差上限 |
| `return_reason_threshold` | 2 | 同一款同一退货原因累计几次触发改图建议并收紧 |

## 1. 实测结论（2026-10-21，真实调用 SubAPI）

- `/v1/models` 的图像模型：gpt-image-1、1.5、2、2.5-flare、2.5-sunburst。
- **尺寸只能用 1024×1024、1024×1536、1536×1024**。请求 1440×1920、2048×2048 都返回 HTTP 200，但实际给的是 **1536×1024 横图**（悄悄换了尺寸和方向）。所以：`validImageSize` 保持三种；**每次生成后必须检查返回 PNG 的宽高等于请求的尺寸**，不等就当失败（算一次失败调用，不扣额度以外的处理照旧）。
- **`/images/edits` 支持 `mask`**：mask 是和参考图同尺寸的 PNG，**透明的地方可以改，不透明的地方保留**。保留区看起来没变，但像素不是原样（实测保留区内没有一个像素完全相同，边缘最大差 204），所以**像素锁定必须在生成后把原图像素贴回去**，不能只靠 mask。
- quality=low 一张 1024×1536 约 20–50 秒；正式出图用 `high`。

## 2. core 和 assets 的改造（Codex）

### 2.1 `core/image_ai.go`

```go
// EditImageMask 和 EditImageSize 一样，多传一张 mask（与 refs[0] 同尺寸，透明=可改）。
// mask 为 nil 时等同 EditImageSize。
func (a *Core) EditImageMask(ctx context.Context, prompt string, refs []image.Image, mask image.Image, size string) ([]byte, error)
```

- multipart 字段名 `mask`，`Content-Type: image/png`，放在所有 `image` 之后。
- `imageRequest` 增加返回尺寸检查（宽高必须等于 `size`），对 `GenerateImage`、`EditImageSize`、`EditImageMask` 都生效；不等报「图像服务返回的尺寸不对」。现有试穿、处理台不受影响（它们本来就只用三种尺寸）。

### 2.2 `core/cutout.go`

把 `pipeline/cutout.go` 的抠图移到 core，pipeline 改成调用 core，行为不变（现有 pipeline 测试必须照样过）：

```go
func (a *Core) ProductMask(ctx context.Context, src *image.NRGBA) (*image.Alpha, string) // 原 pipeline (a *Module).mask
func CompositeWhite(src *image.NRGBA, mask *image.Alpha) (*image.NRGBA, *image.NRGBA, error)
```

### 2.3 assets 给 creative 的 Go 接口（`internal/modules/assets/worn.go`）

生图要几十秒，**SQLite 只有一个连接，不能拿着事务等 AI**。所以和方案里写的 `GenerateWorn(ctx, tx, req)` 不同，拆成两步：

```go
type WornRequest struct {
    UserID       int64    // 发起人：重验 model.view、asset.process、店铺和商品权限，额度记在他名下
    ModelID      int64    // 店铺模特
    ProductID    int64
    SourceAssets []int64  // 商品参考图 1–2 张（这款的实拍或抠图）
    BodyPart     string   // 沿用 validBodyPart：ear / neck / wrist / hand …
    Framing      string   // closeup / half_body
    Size         string   // 三种之一
    Prompt       string   // creative 拼好的完整提示词，直接用，不再套 试穿-生成.md
}
// GenerateWorn 重验权限、取模特固定参考 + 商品参考，调 EditImageSize（feature 用 ctx 里的，creative 传 "creative"），返回 PNG。
func GenerateWorn(ctx context.Context, c *core.Core, req WornRequest) ([]byte, core.Object, error) // Object 是 op：用到的参考 id、模型、尺寸

type OutputRequest struct {
    ProductID int64
    ParentID  int64        // 主要参考图（实拍）的素材 id，可为 0
    PNG       []byte
    Source    string       // "ai_generated"（整张生成、模特图）或 "ai_edited"（像素锁定、改字）
    Title     string       // 如「AI 主图1 首图A」
    Tags      []string     // 至少含 "AI出图"
    Op        core.Object  // creative 的 item id、模板 key、版本、提示词摘要
    UserID    int64
}
// SaveOutput 在调用方事务里把成品写进 assets（kind='output'，code 为空），文件放在商品目录，返回素材 id。
func SaveOutput(ctx context.Context, tx *sql.Tx, c *core.Core, req OutputRequest) (int64, error)
```

- `kind='output'` 不会进一键出图的照片列表（`core.Photos` 只取 `photo`），素材库按 `kind=output` 能看到。
- 权限重验失败返回带 HTTP 状态的错误（403/404），creative 原样转给前端。

## 3. 生图模板

### 3.1 文件（Claude 写，Codex 读）

`templates/生图模板/main/*.json`、`templates/生图模板/detail/*.json`，风格参考图在 `templates/生图模板/参考图/`。一个文件一个模板：

```json
{
  "key": "main_hero_stone",
  "name": "首图·中灰石材",
  "kind": "main",
  "slot": "main1",
  "categories": ["pearl", "crystal"],
  "purpose": "首图，商品居中，画面不加字",
  "prompt": "……{款名}……{商品}……",
  "style_refs": ["参考图/hero_stone.jpg"],
  "texts": [
    {"key": "title", "from": "content.name", "max": 6, "critical": false}
  ],
  "photo_codes": ["A1", "A2", "E4", "B2"],
  "product_mode": "pixel_lock",
  "product_box": [0.2, 0.3, 0.6, 0.45],
  "gen_size": "1024x1536",
  "export": [{"name": "3x4", "w": 1440, "h": 1920}],
  "count": 1,
  "wear": null,
  "version": 1,
  "enabled": true
}
```

| 字段 | 说明 |
|---|---|
| `kind` | `main` / `detail` |
| `slot` | 位置：主图 `main1`（首图）`main2`（上身）`main3`（细节）`main4`（尺寸）`main5`（规格包装）`white`（白底）`sku`；详情 `d1`…`d9` |
| `categories` | 适用品类 key；空数组 = 全部 |
| `prompt` | 占位符：`{款名}` `{英文名}` `{一句话}` `{商品}`（info 拼成一行）`{尺寸}` `{颜色}` `{品类}` `{文字}`（本模板要写在图上的字，逐条带引号）`{模特}`（模特描述）`{佩戴位置}`（按方案 3.3）。没值的占位符换成空串 |
| `texts` | 图上的字：`from` 是取值路径（`content.*`、`listing.*`、`info.*`，或 `literal:原文`）；`max` 字数上限；`critical`=true 表示参数、mm、925、价格这类，比对不过就不能导出。**内容含数字、mm、cm、925、元、¥ 的一律按 critical 处理**，不管模板怎么写 |
| `photo_codes` | 选商品参考图的编号优先级（只取实拍和只做过高清化的，`media.AIMade` 为真的不要） |
| `product_mode` | `pixel_lock` 像素锁定 / `redraw` 参考图重画 / `model` 模特 / `none` 不放商品 / `photo` 实拍原图直接用（只高清化或缩放） |
| `product_box` | 像素锁定时商品放在哪：生成画布上的比例 `[x,y,w,h]`，商品按长边 contain 进去、居中 |
| `gen_size` | 三种之一 |
| `export` | 导出尺寸列表；主图 3:4 是 1440×1920，1:1 是 1440×1440，详情页是宽 750（`h` 写 0 = 按比例） |
| `count` | 这一屏出几张（1–3） |
| `wear` | 模特模式才有：`{"slot": "female_main", "body_part": "ear", "framing": "closeup"}`，`slot` 是店铺模特位 `female_main` / `male` / `hand` |

### 3.2 表 `creative_templates`

`id, key UNIQUE, name, kind, slot, categories(JSON), purpose, prompt, style_refs(JSON), texts(JSON), photo_codes(JSON), product_mode, product_box(JSON), gen_size, export(JSON), count, wear(JSON), version, enabled, source('file'|'custom'), updated_by, updated_at`。

- 启动时（`AfterMigrate`）从文件读：**表里没有的 key 插入；已有的只补空字段**（和品类种子一样），管理员改过的不覆盖。文件格式错的跳过并记日志，不让服务起不来。
- 模板改了 `prompt/texts/product_mode/product_box/gen_size` 任何一项，`version+1`。每张图记下生成时用的模板 key 和 version。
- 风格参考图：`style_refs` 里的图作为额外参考图传给生图接口（排在商品参考图后面，总数不超过 3 张）。

## 4. 表

```sql
creative_sets(
  id, listing_id REFERENCES listings ON DELETE CASCADE, version INTEGER,   -- 同一上架版本可以有多套，version 递增
  status TEXT,          -- draft / generating / reviewing / paused_limit / ready / exported
  template_keys JSON,   -- 这套用了哪些模板，按位置
  params JSON,          -- 生成时的要求：首屏风格、首图要不要 B 版、sizes[{label,mm}]、用户补充的一句话
  hero_b_item_id,       -- 首图 B 版是哪张
  images_used INTEGER, images_allowance INTEGER,  -- 已用生成次数 / 当前允许上限（初值=max_images_per_listing，确认继续时再加一个上限）
  glanced_by, glanced_at,  -- 整套过目
  exported_at, export_file,
  created_by, created_at, updated_at)
creative_items(
  id, set_id, slot, seq,                 -- seq：同一屏第几张
  template_key, template_version,
  attempt INTEGER,                       -- 第几次生成，1 起；重画产生新行，旧行 superseded=1
  superseded INTEGER DEFAULT 0,
  variant TEXT DEFAULT '',               -- 首图 'A' / 'B'
  product_mode, source_asset_ids JSON,   -- 用到的实拍
  model_id, model_ref_ids JSON, wear_type TEXT,  -- wear_type：necklace / earring / bracelet / ring / ''
  size_compare INTEGER DEFAULT 0,        -- 多尺寸对比图
  prompt TEXT, texts JSON,               -- 最终提示词；应写文字 [{key,text,critical}]
  output_asset_id, canvas_file,          -- 成品素材；像素锁定的底图（贴回和比对用，放 listing 目录）
  hard JSON,                             -- 第一层结果 {pixel:{pass,max_diff}, text:[{key,expect,found,pass}], banned:[], format:{pass}}
  review_a JSON, review_b JSON,          -- 两次 AI 复核原样
  route TEXT,                            -- auto_pass / auto_redraw / manual / pending
  route_reasons JSON,                    -- ["首图强制人工","校准期","抽检",…]
  risk INTEGER,                          -- 复核队列排序用，越大越靠前
  human TEXT DEFAULT '', human_note, human_by, human_at,  -- pass / redraw / discard
  error TEXT DEFAULT '',
  created_by, created_at)
creative_shop_settings(shop_id PRIMARY KEY, models JSON, template_set JSON, updated_by, updated_at)
  -- models: {"female_main": 3, "male": 5, "hand": 3}；template_set：每个位置默认用哪个模板 key
creative_calibration(category TEXT PRIMARY KEY, total, agree, open INTEGER, tightened INTEGER DEFAULT 0, tightened_reason, updated_at)
  -- category = wear_type（模特图），以及 'tight:<slot>' 形式的售后收紧记录
creative_suggestions(id, listing_id, set_id, item_id, slot, reason_code, count, status('open'|'done'|'ignored'), created_at, updated_at)
```

## 5. 生成流程

### 5.1 任务类型（`Jobs()`）

| kind | target_type / id | 做什么 |
|---|---|---|
| `creative.main` | `creative_set` / set id | 按模板出主图 5 张 + 首图 B 版 + SKU 图（白底直接复用 pipeline 的 `A1/A2_白底.jpg`，不调 AI） |
| `creative.detail` | `creative_set` / set id | 出详情页 9 屏 |
| `creative.item` | `creative_item` / item id | 单张重画（附一句要求）或改字 |
| `creative.verify` | `creative_item` / item id | 只重跑复核和分流（模板或阈值改了以后） |

- 每生成一张：先查 `images_used < images_allowance`，到了就把 set 置 `paused_limit`、任务正常结束并写明「已达上限，等人确认」；`POST /api/creative/sets/{id}/continue` 把 `images_allowance += max_images_per_listing` 再继续。`images_used` 在调用 AI 前 +1（失败也算一次）。
- 每张图生成完立刻跑复核和分流（第 6 节），需要自动重画的在同一个任务里接着重画。
- 任务执行前和写结果前都重验发起人权限（`creative.run` + 店铺）和上架版本还在。
- AI 调用用 `core.WithFeature(ctx, "creative")` / `"creative_verify"` / `"creative_text"`，额度走现有 `beginUsage`。

### 5.2 各商品模式

- **pixel_lock 像素锁定**
  1. 从 `photo_codes` 选第一张实拍，读高清化后的版本（有就用 `enhanced_file`），用 `core.ProductMask` 抠图。
  2. 按 `product_box` 把商品贴到 `gen_size` 的中性灰画布上，得到底图 canvas（存 `canvas_file`）和商品 alpha。
  3. mask：商品 alpha 区域不透明，其余透明；调 `EditImageMask(prompt, [canvas, 风格参考…], mask, gen_size)`。
  4. 生成后**把 canvas 里的商品像素按 alpha 贴回**，alpha 边缘羽化 2px。
  5. 导出尺寸比生成尺寸大时：背景按比例放大（CatmullRom 插值，不调 AI），**商品在导出尺寸上重新从原图贴一次**（原图分辨率更高）。3:4 从 1024×1536 居中裁成 1024×1365 再放大到 1440×1920；`product_box` 写的是裁剪前的比例，模板作者负责留出安全区。
  6. 硬检查：商品 alpha ≥ 0.99 且离边缘 3px 以上的像素，成品和 canvas（导出尺寸上和重贴源）逐像素比，**最大通道差 ≤ 2** 才算过（PNG 保存，比对在 JPEG 压缩前做）。
- **model 模特**：店铺设置里 `wear.slot` 对应的模特 id；没配就报错「先在 AI 出图设置里给店铺选模特」。调 `assets.GenerateWorn`。`wear_type` 由商品 `info.style` 判断：含 项链/颈链/锁骨链 → necklace；含 耳 → earring；含 手链/手串/手镯 → bracelet；含 戒 → ring；都不是就 ''（不进校准统计，但照样走复核）。
- **redraw 参考图重画**：`EditImageSize(prompt, [实拍…, 风格参考…], gen_size)`，不贴回。只给没法像素锁定的位置用（例如同系列搭配），默认模板不用。
- **photo 实拍原图**：直接用实拍（有高清化版本用高清化版本），只做裁切和缩放，不调生图；仍跑文字以外的硬检查，复核只做一次规范检查（不需要还原度对比）。
- **none 不放商品**：纯生成（`GenerateImage`），只查文字和规范。

### 5.3 首图 A/B、多尺寸对比

- 首图默认出 A、B 两版（`variant`），各自独立复核；人工可以在复核页指定哪版是 A。
- `main4` 尺寸位：`params.sizes` 有 2 个及以上时，用同一位模特、同一侧，每个尺寸出一张（`seq` 依次，`size_compare=1`），每张图上写 mm（critical 文字）；只有 1 个尺寸时用刻度尺实拍（`photo` 模式，编号 C1–C3）。

## 6. 三层复核和分流

### 6.1 第一层：程序硬检查

| 项 | 规则 |
|---|---|
| 像素 | 像素锁定图按 5.2 第 6 步；不过 = fail |
| 文字 | 调 `creative_text`（提示词 `prompts/生图-文字核对.md`）把图上的字全读出来，返回 `{"texts":[{"text":"…","box":[x,y,w,h]}]}`（box 是 0–1 比例）。每条应写文字：两边都去掉空白和标点（Unicode P、Z 类）后，读出的字拼起来要**包含**应写文字。critical 不过 = fail；非 critical 不过 = 标黄（`pass:false, warn:true`，不算 fail） |
| 违禁词 | 读出来的全部文字过 `core.CategoryBannedLines(违禁词.txt, 品类)`，命中 = fail |
| 格式 | 返回尺寸等于请求尺寸、PNG 可解码；导出尺寸正确 |

### 6.2 第二层：AI 复核

- 提示词 `prompts/生图-核对.md`（Claude 写），模型 `creative_verify`，用 `core.AIJSON`。**同一张图复核两次**：A 次图片顺序「原图…, 生成图」，B 次「生成图, 原图…」，提示词里用占位符 `{{order}}` 说明哪张是哪张。
- 输入 JSON：`{slot, product_mode, wear_type, size_compare, expected:{sizes, texts, placement}, product:{name, info}, category}`。
- 返回（两次都要校验格式，不合格当这次复核失败，重试一次，还不行算「不确定」）：

```json
{
  "checks": {
    "product": "pass|fail|unsure",
    "size": "pass|fail|unsure|na",
    "body": "pass|fail|unsure|na",
    "placement": "pass|fail|unsure|na",
    "style": "pass|fail|unsure"
  },
  "size_mm": [8.2],
  "score": 0,
  "issues": [{"check": "product", "box": [0.1, 0.2, 0.3, 0.3], "reason": "多了一颗珠子"}]
}
```

  - `product` 商品还原（颗数和段数、颜色、珠形、金属色、扣子、零件多少、链长落点）；`size` 尺寸比例；`body` 人体自然（手指数、耳饰和耳垂连接）；`placement` 佩戴位置按方案 3.3；`style` 视觉规范。
  - `size_mm`：模型按锚点估算的主尺寸（珠径或坠长，多尺寸对比图每张一个）。**程序**和 `params.sizes` 比：偏差 > `size_tolerance` → 把 `size` 改判 fail；多尺寸对比图各张估算值之比和 mm 之比偏差 > `size_compare_tolerance` → fail；没有资料尺寸就判 unsure。
  - 不是模特图时 `size/body/placement` 返回 `na`，按 pass 算。

### 6.3 分流（按顺序判断）

1. `forced` = 强制人工原因列表：首图（`main1`）；多尺寸对比图；模特图且 `creative_calibration[wear_type].open=0`；这个位置被售后收紧（`tight:<slot>` 的 tightened=1）；整套过目不算单张原因。
2. 硬检查有 fail，或两次复核**都**判 fail 的项有交集，或两次分数都 < `manual_score_min`：还能重画（同位置自动重画次数 < `max_auto_redraw` 且没到生成上限）→ `auto_redraw`，原因写进新一次的提示词（「上一次的问题：…」）；否则 → `manual`。
3. 否则，如果硬检查全过、两次复核每项都是 pass（或 na）、两次结论完全一致、两次分数都 ≥ `auto_pass_score`：
   - `forced` 非空 → `manual`（原因写进 `route_reasons`）；
   - 否则按 `spot_check_rate` 随机抽检，抽中 → `manual`（原因「抽检」）；
   - 否则 → `auto_pass`。
4. 其余（有 unsure、两次不一致、分数在中间）→ `manual`。

- 强制人工的图**照样会自动重画**明显不合格的版本，只是永远不会自动通过。
- `risk` 排序：硬检查 fail 100、首图 / 多尺寸 80、有 unsure 或不一致 60、抽检 20，再加 `100 - min(score)`。

### 6.4 人工判断和校准

- `POST /api/creative/items/{id}/judge {verdict: pass|redraw|discard, note}`（`creative.review`）。`redraw` 必须带 note，建一个 `creative.item` 重画任务（不占自动重画次数，但占生成上限）。
- **校准**：模特图（wear_type 非空）每次人工判断都记一次：AI 的判断 = 这张图在第 6.3 第 3 步是否满足自动通过条件（不看 forced 和抽检）；人的判断 = `pass` 或不是。一致则 `agree+1`，`total+1`。`total ≥ calibration_count` 且 `agree/total ≥ calibration_agreement` → `open=1`；之后一致率掉到线下 → `open=0`。
- **售后收紧**：见第 8 节。

### 6.5 整套过目和导出条件

- `final_glance` 开着时，导出前要有人 `POST /api/creative/sets/{id}/glance`（`creative.review`）。
- 能导出的条件：每个位置都有一张当前（未 superseded）的图，且它是 `auto_pass` 或人工 `pass`；**critical 文字比对没过的图不能导出**（即使人工点了通过也不行，必须先改字）；过目条件满足。不满足时 `POST …/export` 返回 409 和缺什么的列表。

## 7. 接口

全部 JSON，`creative.*` 权限 + 店铺权限（`RequireListing`）。

| 方法 路径 | 权限 | 说明 |
|---|---|---|
| `GET /api/creative/templates?kind=&category=` | creative.run | 模板列表 |
| `GET/PATCH /api/creative/templates/{key}` | 读 run / 改 template | 改字段（会让 version+1 的那几项照第 3.2 节） |
| `POST /api/creative/templates/{key}/trial` | creative.template | 用指定上架版本试跑一张：`{listing_id}`，建 `creative.item` 任务，结果不进任何 set（`set_id` 为空的 item） |
| `GET/PUT /api/creative/shops/{id}/settings` | 读 run / 改 template | `{models:{female_main,male,hand}, template_set:{slot: key}}`；模特 id 必须属于这个店 |
| `GET /api/listings/{id}/creative` | creative.run | 这个上架版本的所有 set（新的在前），每个 set 带当前 items 和进度 |
| `POST /api/listings/{id}/creative` | creative.run | 新建一套并开始生成：`{parts:["main","detail"], params:{hero_style, hero_b, sizes, note}}` → `{set, jobs}` |
| `GET /api/creative/sets/{id}` | creative.run | set + 全部 items（含 superseded，前端折叠） |
| `POST /api/creative/sets/{id}/continue` | creative.run | 到上限后人工确认继续 |
| `POST /api/creative/sets/{id}/glance` | creative.review | 整套过目 |
| `POST /api/creative/sets/{id}/export` | creative.run + export.run | 打包，返回 `{file}`；`GET /api/creative/sets/{id}/export` 下载 zip |
| `POST /api/creative/items/{id}/redraw` | creative.run | `{note}` 单张重画 |
| `POST /api/creative/items/{id}/retext` | creative.run | `{key, text}` 改字：用 OCR 的 box 外扩 10% 做 mask，`EditImageMask` 只改这块，改完重跑文字核对；不在 texts 里的 key 400 |
| `POST /api/creative/items/{id}/choose` | creative.review | 首图指定 A 版：`{variant:"A"}` |
| `GET /api/creative/review?shop_id=&slot=&page=` | creative.review | 待审队列：`route=manual` 且 `human=''` 且未 superseded，按 `risk` 降序；每项带原图素材 id、成品素材 id、两次复核 issues、硬检查结果 |
| `POST /api/creative/items/{id}/judge` | creative.review | 第 6.4 节 |
| `GET /api/creative/stats` | creative.review | 每类：校准 total/agree/open、自动通过率、人工改判率、被退货原因关联次数；售后建议列表 |
| `POST /api/creative/suggestions/{id}` | creative.review | `{status: done|ignored}` |
| `PATCH /api/creative/calibration/{category}` | creative.template | `{tightened:false}` 解除售后收紧；`{open:false}` 手动关闭某类自动通过 |

- 返回格式沿用现有习惯：列表 `{items,total}`，详情直接对象；错误 `{error}`。
- 设置页的阈值走 `PATCH /api/settings {"creative":{…}}`，不另开接口。

## 8. 售后回流（读 orders，不改 orders）

- agy 给 `order_return_lines` 加了 `reason_code`（`not_as_pictured` 与图片不符 / `smaller` 比想象小 / `color` 颜色不符 / `flaw` 瑕疵 / `other`）和 `creative_set_id`（见 [接口约定-数据与运营.md](接口约定-数据与运营.md) 第 6 节）。
- creative 每次启动和每小时扫一次新增的售后行（记水位，参照 `tasks` 扫 `audit_log`）：同一 listing、同一 `reason_code`（other 除外）累计 ≥ `return_reason_threshold`：
  - 建一条 `creative_suggestions`：`not_as_pictured`、`color` → 指向首图和上身；`smaller` → 指向尺寸位；`flaw` → 指向细节位；
  - 对应位置收紧：`creative_calibration('tight:<slot>').tightened=1`，之后这个位置的图全部强制人工，直到管理员在设置页解除（第 7 节的 calibration 接口）。

## 9. 导出

`listings/<id>/AI出图/set-<version>/`，zip 为 `AI出图-<款名>-v<version>.zip`：

```
主图/3x4_1440x1920/01.jpg … 05.jpg      # 01 是首图 A
主图/3x4_1440x1920/01B.jpg              # 首图 B 版
主图/1x1_1440x1440/01.jpg
白底/800x800.jpg                         # pipeline 的 A1/A2(_AI)_白底
SKU/<规格名>.jpg
详情页/750/01.jpg、01-2.jpg …            # 每屏多张时 -2、-3
复核记录.csv                              # 每张：位置、模板 key/version、route、人工判断、分数
```

- JPEG 质量从 92 往下降到 ≤ 900KB（同 pipeline `saveMain`）。
- 导出写 `exported_at`，set 置 `exported`；再次导出覆盖同一个 version 的目录。

## 10. Claude 负责的部分（供 Codex 对齐）

- 提示词：`prompts/生图-主图.md`、`生图-详情.md`、`生图-核对.md`、`生图-文字核对.md`、`生图-文字.md`（改字）。creative 用 `a.Prompt`/`core.FillPrompt` 读，占位符按第 3.1 节和第 6.2 节。阶段 1 期间 Codex 先用测试里的假提示词，不要等真文件。
- 模板和参考图：`templates/生图模板/`。
- 前端：商品详情「AI 出图」标签页、复核队列 `/creative/review`、模板中心的生图模板编辑和试跑、设置 → 店铺 AI 和复核阈值、侧栏入口。

## 11. 离线测试（Codex 必须写，用假 AI 服务）

1. 像素锁定：贴回后商品区域最大通道差 ≤ 2；假服务故意改商品像素也能被贴回修正；导出尺寸上重贴后仍满足。
2. 返回尺寸不等于请求尺寸 → 当失败。
3. critical 文字比对不过 → 不能导出（人工 pass 也不行）；改字后比对通过 → 能导出；非 critical 不过只标黄。
4. 分流：首图、多尺寸对比、校准期、收紧位置都不会 auto_pass；两次复核不一致 → manual；共同 fail → auto_redraw，最多 2 次后 manual；抽检比例 1.0 时全部 manual，0 时全部 auto_pass。
5. 校准：一致率达到后 open=1，掉下去 open=0。
6. 40 张上限：到了暂停、不再调 AI；continue 后能继续。
7. 权限：没 creative.run 不能生成；跨店铺 404；外部协作者 403；任务执行时撤权则失败不写结果。
8. 模板种子：已有记录只补空字段；改关键字段 version+1。
9. 售后回流：同款同原因第 2 次触发建议和收紧；other 不触发。
10. 现有 pipeline、assets 测试全部照过。

**Codex 只改**：`internal/core/image_ai.go`、新建 `internal/core/cutout.go`、`internal/modules/pipeline/cutout.go`（及调用处）、新建 `internal/modules/assets/worn.go`（及测试）、`internal/modules/creative/` 全部。其他共用文件需要改的，写进最后的汇报，由 Claude 改。
