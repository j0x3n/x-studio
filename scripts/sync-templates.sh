#!/usr/bin/env bash
# 用法：在任意位置执行 bash 出图网站/scripts/sync-templates.sh。
# 只读取仓库中的模板和 PNG，输出到 出图网站/templates/；可重复执行。
set -euo pipefail
site="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
repo="$(cd -- "$site/.." && pwd)"
mkdir -p "$site/templates/主图模板" \
  "$site/templates/详情页模板/商品/示例款" \
  "$site/templates/店铺/店标/矢量标志"
cp -- "$repo/运营/主图模板/模板.html" "$site/templates/主图模板/模板.html"
cp -- "$repo/运营/详情页模板/模板.html" "$site/templates/详情页模板/模板.html"
# Older source templates treat ai as a boolean and hardcode the background
# label. Normalize only our copies; already updated templates remain unchanged.
for html in "$site/templates/主图模板/模板.html" "$site/templates/详情页模板/模板.html"; do
  sed -i "s@var tag = p\\.ai ? '<span class=\"ai-tag\">AI 生成背景</span>' : '';@var tag = p.ai ? '<span class=\"ai-tag\">' + esc(p.ai === true ? 'AI 生成背景' : p.ai) + '</span>' : '';@" "$html"
done
for name in 内容.js 主图.js; do
  cp -- "$repo/运营/详情页模板/商品/示例款/$name" "$site/templates/详情页模板/商品/示例款/$name"
done
shopt -s nullglob
logos=("$repo/店铺/店标/矢量标志/"*.png)
if ((${#logos[@]} == 0)); then
  echo '错误：没有找到店标 PNG' >&2
  exit 1
fi
cp -- "${logos[@]}" "$site/templates/店铺/店标/矢量标志/"
echo "模板已同步：$site/templates"
