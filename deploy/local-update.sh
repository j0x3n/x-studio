#!/usr/bin/env bash
# 本机 Docker 一键更新：构建前端 → 交叉编译 Linux 程序 → 打成 x-studio:local → 备份数据库 → 重启 → 健康检查。
# 用法（仓库根目录）：bash deploy/local-update.sh [--skip-web]
#   --skip-web  只改了后端时跳过前端构建
# 日志在 .cache/docker-local/update.log。第一次运行会把 ./data 复制进 Docker 卷 x-studio_app-data。
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$(pwd)
ROOT_HOST=$(pwd -W 2>/dev/null || pwd)  # Windows 上给 docker -v 用 C:/... 形式
export MSYS_NO_PATHCONV=1
OUT=.cache/docker-local
mkdir -p "$OUT"
LOG="$ROOT/$OUT/update.log"
: >"$LOG"
step() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$LOG"; }
run() { "$@" >>"$LOG" 2>&1 || { echo "失败：$*，日志末尾：" >&2; tail -30 "$LOG" >&2; exit 1; }; }

SKIP_WEB=0
[ "${1:-}" = "--skip-web" ] && SKIP_WEB=1

NODE_DIR="${APPDATA:-}/fnm/node-versions/v22.23.3/installation"
[ -d "$NODE_DIR" ] && export PATH="$NODE_DIR:$PATH"
export GOMODCACHE="$ROOT_HOST/.cache/go-mod" GOCACHE="$ROOT_HOST/.cache/go-build" GOFLAGS=-mod=mod GOPROXY=off

if [ "$SKIP_WEB" = 0 ]; then
  step "构建前端"
  (cd frontend && run timeout 600 npx tsc -b && run timeout 600 npx vite build)
fi

step "编译 Linux 程序"
run env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 timeout 900 go build -trimpath -ldflags="-s -w" -o "$OUT/server" ./cmd/server

step "运行环境镜像（没变时直接用缓存）"
run timeout 1800 docker build --target base -t x-studio-base:local -f deploy/Dockerfile deploy

step "打包 x-studio:local"
rm -rf "$OUT/ctx" && mkdir -p "$OUT/ctx"
cp "$OUT/server" "$OUT/ctx/server"
cp -r templates "$OUT/ctx/templates"
cp -r prompts "$OUT/ctx/prompts"
cat >"$OUT/ctx/Dockerfile" <<'EOF'
FROM x-studio-base:local
COPY server /app/server
COPY templates/ /app/templates/
COPY prompts/ /app/prompts/
EOF
run timeout 600 docker build -t x-studio:local "$OUT/ctx"

VOLUME=x-studio_app-data
if ! docker volume inspect "$VOLUME" >/dev/null 2>&1; then
  step "第一次运行：把 ./data 复制进卷 $VOLUME"
  run docker volume create "$VOLUME"
  if [ -f data/app.db ]; then
    run timeout 900 docker run --rm -v "$VOLUME:/dst" -v "$ROOT_HOST/data:/src:ro" --entrypoint sh --user 0 x-studio:local \
      -c 'cp -a /src/. /dst/ && chown -R 10001:10001 /dst'
  fi
fi

if [ -n "$(docker compose ps --status running -q app 2>/dev/null)" ]; then
  NAME="app-$(date +%Y%m%d-%H%M%S).db"
  step "备份数据库：卷里 backups/$NAME（保留最近 10 份）"
  run docker compose exec -T app sh -c "mkdir -p /app/data/backups && sqlite3 /app/data/app.db \"VACUUM INTO '/app/data/backups/$NAME'\" && ls -1t /app/data/backups/app-*.db | tail -n +11 | xargs -r rm --"
fi

step "启动"
run timeout 300 docker compose up -d --remove-orphans

step "等待就绪"
for _ in $(seq 1 45); do
  if docker compose exec -T app wget -qO /dev/null http://127.0.0.1:8080/login 2>/dev/null; then
    step "完成：http://127.0.0.1:${APP_PORT:-8080}"
    exit 0
  fi
  sleep 2
done
echo "90 秒内没就绪，最近日志：" >&2
docker compose logs --tail 50 app >&2
exit 1
