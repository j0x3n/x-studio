# 生图模板

creative 模块启动时从这里读模板种子（表里没有的 key 插入，已有的只补空字段）。字段说明见 [接口约定-AI出图.md](../../接口约定-AI出图.md) 第 3.1 节；提示词前缀在 `prompts/生图-主图.md`、`prompts/生图-详情.md`。

## 默认用哪套

| 位置 | 默认模板 | 备选 |
|---|---|---|
| main1 首图 | `main_hero_stone`（A/B 两版） | `main_hero_model` |
| main2 上身 | `main_wear` | — |
| main3 细节 | `main_detail_macro`（实拍） | — |
| main4 尺寸 | 2 个以上尺寸 `main_size_compare`，否则 `main_size_ruler` | — |
| main5 规格 | `main_spec` | — |
| sku | `main_sku` | 礼盒装用实拍包装照 |
| d1 首屏 | 按 `params.hero_style`：`detail_hero_vertical` / `detail_hero_album` / `detail_hero_window` | — |
| d2 | 珍珠 `detail_real_pearl`，水晶 `detail_real_crystal` | — |
| d3–d9 | `detail_macro`、`detail_wear`、`detail_size`、`detail_series`（未启用）、`detail_pack`、`detail_care`、`detail_about` | — |

**商品模式默认怎么选（2026-10-21 用户定）**：首图、规格、SKU、详情页首屏和同系列用 `redraw`（GPT 照实拍直接出）；细节、真假对比、尺寸、包装这些证据屏用 `pixel_lock` + `rect`（实拍整张贴回）；上身和模特首图用 `model`。`pixel_lock` 抠图模式保留，模板里可以改回去。

## 约定以外多出来的字段（阶段 2 由 Claude 和 Codex 接进后端）

| 字段 / 写法 | 含义 |
|---|---|
| `wear.body_part: "auto"` | 按商品 `wear_type` 定：necklace→neck，earring→ear，bracelet→wrist，ring→hand；判断不出就报错让人选 |
| `lock_shape: "rect"` | 像素锁定时**整张照片按矩形贴进 `product_box`**（cover 裁切），不抠图。详情页的托盘、微距、刻度尺、包装屏用。缺省 `cutout`（抠图） |
| `size_compare: true` | 这张模板出的是多尺寸对比图（每个 `params.sizes` 一张） |
| `hero_style` | d1 三选一的风格名，对应 `params.hero_style` |
| `texts[].from` 的 `{i}` | 本屏第几张（0 起），如 `content.details.{i}.title`；提示词里的 `{i}` 是 1 起 |
| `texts[].from: "size.label"` | 多尺寸对比图当前这张的尺寸标签 |
| `from` 指向数组 | 字符串数组按行拼接；`[名, 值]` 二维数组每行拼成「名 值」 |

## 参考图

`参考图/` 现在是空的，所有模板 `style_refs` 为空数组。真实跑第一款时挑出满意的成品放进来再引用（阶段 2）。

## 改模板

改这里的文件只影响**新装**或表里对应字段为空的情况；已经在模板中心改过的以数据库为准。
