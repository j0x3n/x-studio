# 生图模板

creative 模块启动时从这里读模板种子（表里没有的 key 插入，已有的只补空字段）。字段见下面「文件格式」；提示词前缀在 `prompts/生图-主图.md`、`prompts/生图-详情.md`。

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

## 文件格式

一个文件一个模板，风格参考图放 `参考图/`：

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
| `prompt` | 占位符：`{款名}` `{英文名}` `{一句话}` `{商品}`（info 拼成一行）`{尺寸}` `{颜色}` `{品类}` `{文字}`（本模板要写在图上的字，逐条带引号）`{模特}`（模特描述）`{佩戴位置}`。没值的占位符换成空串 |
| `texts` | 图上的字：`from` 是取值路径（`content.*`、`listing.*`、`info.*`，或 `literal:原文`）；`max` 字数上限；`critical`=true 表示参数、mm、925、价格这类，比对不过就不能导出。**内容含数字、mm、cm、925、元、¥ 的一律按 critical 处理**，不管模板怎么写 |
| `photo_codes` | 选商品参考图的编号优先级（只取实拍和只做过高清化的，`media.AIMade` 为真的不要） |
| `product_mode` | `pixel_lock` 像素锁定 / `redraw` 参考图重画 / `model` 模特 / `none` 不放商品 / `photo` 实拍原图直接用（只高清化或缩放） |
| `product_box` | 像素锁定时商品放在哪：生成画布上的比例 `[x,y,w,h]`，商品按长边 contain 进去、居中 |
| `gen_size` | `1024x1024` / `1024x1536` / `1536x1024` 三种之一，生成后会核对返回尺寸 |
| `export` | 导出尺寸列表；主图 3:4 是 1440×1920，1:1 是 1440×1440，详情页是宽 750（`h` 写 0 = 按比例） |
| `count` | 这一屏出几张（1–3） |
| `wear` | 模特模式才有：`{"slot": "female_main", "body_part": "ear", "framing": "closeup"}`，`slot` 是店铺模特位 `female_main` / `male` / `hand` |

## 补充字段

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
