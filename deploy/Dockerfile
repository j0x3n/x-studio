# X Studio 镜像。构建上下文是仓库根目录：docker build -f deploy/Dockerfile .
#   GitHub Actions 用完整流程（前端 → Go → 运行环境）。
#   本机更新（deploy/local-update.sh）只用 base 这一段，程序在本机交叉编译好再放进去，几十秒就能换上。

FROM node:22-bookworm-slim AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npx tsc -b && npx vite build

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

# 运行环境：Chromium 出 HTML 详情图，ffmpeg 做视频封面，sqlite3 给部署脚本备份数据库，wget 做健康检查
FROM debian:bookworm-slim AS base
RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium fonts-noto-cjk ca-certificates ffmpeg sqlite3 wget tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home app \
    && mkdir -p /app/data /app/prompts /app/templates && chown -R app:app /app/data
WORKDIR /app
ENV LISTEN=:8080 DATA_DIR=/app/data TEMPLATE_DIR=/app/templates PROMPT_DIR=/app/prompts \
    CHROME_PATH=/usr/bin/chromium CHROME_NO_SANDBOX=1 TZ=Asia/Shanghai
USER app
EXPOSE 8080
ENTRYPOINT ["/app/server"]

FROM base
COPY --from=build /server /app/server
COPY templates/ /app/templates/
COPY prompts/ /app/prompts/
