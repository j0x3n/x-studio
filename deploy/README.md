# 部署

## 本机 Docker

```bash
bash deploy/local-update.sh            # 前端 + 后端都重新构建，备份数据库后重启
bash deploy/local-update.sh --skip-web # 只改了后端
```

- 打开 http://127.0.0.1:8080 （端口可在根目录 `.env` 用 `APP_PORT` 改）。变量读根目录 `.env`。
- 前端在本机构建，Go 在本机交叉编译成 Linux 程序，放进 `x-studio-base:local`（Chromium、ffmpeg、中文字体、sqlite3），一次更新几十秒。运行环境只在 `deploy/Dockerfile` 的 base 段改了才重建。
- 数据在卷 `x-studio_app-data`。第一次运行时从 `./data` 复制进去，之后 `./data` 不再使用（还留着当旧备份）。每次更新前把数据库备份到卷里的 `backups/`，保留 10 份。
- 提示词目录 `./prompts` 直接挂进容器，改了不用重建；模板 `templates/` 打在镜像里，改了要跑一次更新。
- 日志：`docker compose logs -f app`；更新过程日志 `.cache/docker-local/update.log`。
- 回到某份备份：`docker compose stop app`，用 `docker run --rm -v x-studio_app-data:/d --entrypoint sh --user 0 x-studio:local -c 'cp /d/backups/<文件> /d/app.db && rm -f /d/app.db-wal /d/app.db-shm && chown 10001:10001 /d/app.db'`，再 `docker compose up -d`。

## 服务器（照 x-console 的方式）

推送到 `main`（或在 GitHub Actions 页面手动运行 Deploy）时：`go vet` + 编译检查和构建镜像同时跑，镜像推到 `ghcr.io/j0x3n/x-studio:sha-<提交号>`；都成功后 SSH 到服务器，上传 `docker-compose.yml`、`Caddyfile`、`remote-deploy.sh`，跑 `remote-deploy.sh <镜像>`：部署前把数据库 `VACUUM INTO` 备份到卷里的 `backups/`，换镜像，90 秒内 `/login` 不通就自动回退到旧镜像和备份。

现在的服务器：154.17.19.138（和 x-console 同一台），部署用户 `deploy`，目录 `/home/deploy/x-studio`，程序监听 `127.0.0.1:17390`，域名 `studio.xcc.im` 由服务器上的 naive Caddy（`/root/naive/Caddyfile`）反代。这台只有 1 核 2 GB、硬盘很紧，所以：

- 不启动 rembg（镜像 4 GB）；像素锁定抠图自动退回普通抠图。空间够了在服务器 `.env` 里加 `COMPOSE_PROFILES=rembg`。
- 程序容器内存上限 900 MB（`.env` 里 `XS_MEM_LIMIT` 可改）。
- 只保留当前和上一个版本的镜像。

### GitHub Secrets（仓库 Settings → Secrets and variables → Actions）

| 名称 | 内容 |
| --- | --- |
| `DEPLOY_HOST` | `154.17.19.138` |
| `DEPLOY_USER` | `deploy` |
| `DEPLOY_SSH_KEY` | 和 x-console 共用的部署私钥（服务器上 `/root/xconsole_deploy`） |
| `DEPLOY_DOMAIN` | `studio.xcc.im`（只在第一次生成 `.env` 时用） |
| `AI_API_KEY` | 可选，只在第一次生成 `.env` 时写进去；之后改服务器上的 `.env` |
| `DEPLOY_KNOWN_HOSTS` | 可选，`ssh-keyscan 154.17.19.138` 的输出 |

没配 `DEPLOY_HOST` 时只构建镜像不部署。

### 服务器上的操作

```bash
cd ~/x-studio
docker compose logs -f app          # 看日志
sh remote-deploy.sh rollback        # 回退到上一次部署前
docker compose exec app ls /app/data/backups
sh remote-deploy.sh restore app-20261022-090000-sha-xxxxxxx.db
```

第一次部署时自动生成 `.env`（随机 `SESSION_SECRET` 和管理员初始密码，在 `.env` 的 `ADMIN_PASSWORD`）。请备份这个文件。
