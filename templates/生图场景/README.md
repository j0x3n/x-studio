# 内置场景

scenes 模块启动时读这里的 `*.json`，`builtin_key`（文件里的 `key`）在库里不存在才插入，已有的不覆盖（用户在场景库里改过的以库为准）。每个文件一个场景：`{"key","name","categories","still","wear","light","props","note","refs"}`，参考图启动时复制进 `DATA_DIR/scenes/`。

- `default: true` 的是兜底场景：款和店铺都没指定场景时用它。只能有一个。
- `still` / `wear` / `light` / `props` 会原样拼进生图提示词（模板里的 `{场景}`、`{穿搭}`）。用中文写，方便在场景库里看懂和修改（生图模型能读中文）。
- `refs` 是参考图，`{"use":"still|wear","file":"参考图/xxx.jpg"}`，路径相对本目录。

| key | 名字 | 说明 |
|---|---|---|
| stone_grey | 冷灰石材 | 默认（兜底），原来模板里写死的风格 |
| paper_ivory | 米白画册 | 亮、干净 |
| window_knit | 窗边针织 | 秋冬、偏暗 |
| morning_linen | 清透晨光 | 明亮清爽，浅色水晶 |
