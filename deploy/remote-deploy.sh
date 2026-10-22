#!/bin/sh
# 服务器上的部署脚本（照 x-console 的做法）：
#   sh remote-deploy.sh <镜像>          部署：先备份数据库，换镜像，90 秒内没就绪就自动回退到旧镜像和备份
#   sh remote-deploy.sh rollback        回退到上一次成功部署前的镜像和数据库
#   sh remote-deploy.sh restore <备份>  只恢复数据库（文件名在卷里 backups/ 下）并重启
# 第一次部署时没有 .env 会自动生成（需要环境变量 XS_DOMAIN，可选 AI_API_KEY）。
set -eu
cd "$(dirname "$0")"

set_image() {
	if grep -q '^XS_IMAGE=' .env; then
		sed -i "s|^XS_IMAGE=.*|XS_IMAGE=$1|" .env
	else
		printf 'XS_IMAGE=%s\n' "$1" >>.env
	fi
}

fetch_image() {
	docker image inspect "$1" >/dev/null 2>&1 || docker pull -q "$1" >/dev/null
}

health() {
	i=0
	until docker compose exec -T app wget -qO /dev/null http://127.0.0.1:8080/login >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -ge 45 ] && return 1
		sleep 2
	done
}

check_name() {
	case "$1" in
		*/* | *..*) echo "无效的备份文件名：$1" >&2; return 1 ;;
		app-*.db) ;;
		*) echo "无效的备份文件名：$1" >&2; return 1 ;;
	esac
}

restore_database() {
	check_name "$1"
	docker compose run --rm --no-deps -T --entrypoint sh app -c 'test -f "$1"' sh "/app/data/backups/$1"
	docker compose stop app
	docker compose run --rm --no-deps -T --user 0 --entrypoint sh app -c '
		set -e
		cp "$1" /app/data/app.db
		chown 10001:10001 /app/data/app.db
		rm -f /app/data/app.db-wal /app/data/app.db-shm
	' sh "/app/data/backups/$1"
}

rollback_to() {
	set_image "$1"
	if restore_database "$2" && docker compose up -d --remove-orphans && health; then
		echo "已回退到 $1，数据库恢复自 $2"
		return 0
	fi
	echo "回退失败，需要人工检查容器和备份：$2" >&2
	return 1
}

case "${1:-}" in
	rollback)
		[ -f .previous-deploy ] || { echo "没有可回退的上一版本" >&2; exit 1; }
		rollback_to "$(sed -n 1p .previous-deploy)" "$(sed -n 2p .previous-deploy)"
		exit
		;;
	restore)
		[ "$#" -eq 2 ] || { echo "用法: remote-deploy.sh restore <备份文件>" >&2; exit 2; }
		restore_database "$2"
		docker compose up -d --remove-orphans
		health
		echo "已恢复数据库：$2"
		exit
		;;
	'') echo "用法: remote-deploy.sh <镜像>|rollback|restore <备份文件>" >&2; exit 2 ;;
esac
IMAGE=$1

if [ ! -f .env ]; then
	[ -n "${XS_DOMAIN:-}" ] || { echo "没有 .env，也没有提供 XS_DOMAIN。" >&2; exit 1; }
	PASS=$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')
	umask 077
	{
		echo "XS_DOMAIN=$XS_DOMAIN"
		echo "XS_PORT=${XS_PORT:-17390}"
		echo "SESSION_SECRET=$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
		echo "ADMIN_USER=admin"
		echo "ADMIN_PASSWORD=$PASS"
		echo "COOKIE_SECURE=1"
		echo "TZ=Asia/Shanghai"
		echo "AI_BASE_URL=${AI_BASE_URL:-https://subapi.xcc.im/v1}"
		echo "AI_API_KEY=${AI_API_KEY:-}"
		echo "WORKERS=2"
	} >.env
	echo "已创建 .env：$(pwd)/.env。管理员 admin 的初始密码在里面（ADMIN_PASSWORD）。"
	[ -n "${AI_API_KEY:-}" ] || echo "注意：AI_API_KEY 是空的，AI 功能用不了，在 .env 里补上后再部署一次。" >&2
fi

# GitHub Secret 里的 AI_API_KEY 有值时，每次部署都同步进 .env（换 key 只要改 Secret 再部署）
if [ -n "${AI_API_KEY:-}" ] && [ "$(sed -n 's/^AI_API_KEY=//p' .env)" != "$AI_API_KEY" ]; then
	umask 077
	grep -v '^AI_API_KEY=' .env >.env.tmp || true
	printf 'AI_API_KEY=%s\n' "$AI_API_KEY" >>.env.tmp
	mv .env.tmp .env
	echo "已更新 .env 里的 AI_API_KEY"
fi

OLD_IMAGE=$(sed -n 's/^XS_IMAGE=//p' .env)
BACKUP=
if [ -n "$(docker compose ps --status running -q app)" ]; then
	TAG=$(printf '%s' "${OLD_IMAGE##*:}" | tr -c '[:alnum:]._-' '_')
	BACKUP="app-$(date -u +%Y%m%d-%H%M%S)-$TAG.db"
	docker compose exec -T app sh -c "mkdir -p /app/data/backups && chmod 700 /app/data/backups && sqlite3 /app/data/app.db \"VACUUM INTO '/app/data/backups/$BACKUP'\" && chmod 600 /app/data/backups/$BACKUP"
	echo "部署前数据库备份：$BACKUP"
fi

set_image "$IMAGE"
if ! fetch_image "$IMAGE" || ! docker compose up -d --remove-orphans || ! health; then
	echo "新版本没有就绪，最近日志：" >&2
	docker compose logs --tail 50 app >&2 || true
	[ -n "$BACKUP" ] && { rollback_to "$OLD_IMAGE" "$BACKUP" || true; }
	exit 1
fi

if [ -n "$BACKUP" ]; then
	printf '%s\n%s\n' "$OLD_IMAGE" "$BACKUP" >.previous-deploy
	docker compose exec -T app sh -c 'ls -1t /app/data/backups/app-*.db | tail -n +11 | xargs -r rm --'
fi
# 只留当前和上一个版本的镜像，方便回退
docker images --format '{{.Repository}}:{{.Tag}}' "${IMAGE%%:*}" | grep -v -e "^$IMAGE\$" -e "^$OLD_IMAGE\$" | xargs -r docker rmi >/dev/null 2>&1 || true
echo "部署完成，程序在 127.0.0.1:$(sed -n 's/^XS_PORT=//p' .env)"
