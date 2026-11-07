# 在 Ubuntu 上部署 Ask4Me

适用于 Ubuntu 22.04 / 24.04，任何 VPS、云主机或自建虚拟机。下面用 **Docker Compose + Caddy 自动 HTTPS**，并给出标准镜像和 `scratch` 极简镜像两种跑法。

> Ask4Me 是长轮询 / SSE + SQLite 的有状态服务，**一台机器只跑一个实例**。不要用多副本、Serverless 或函数计算来跑这套代码。

## 0. 准备

| 项 | 建议 |
|---|---|
| 系统 | Ubuntu 22.04 / 24.04 LTS |
| 配置 | 单用户 1C1G；多用户试用 2C2G；系统盘 20GB+ |
| 域名 | 一个 A 记录指向公网 IP，例如 `ask.example.com` |
| 端口 | 只开放 22、80、443；**不要**把 8080 暴露到公网 |
| Go | Docker 构建用 1.27.3；宿主机直接编译需要 Go 1.27.3+ |

E2EE 依赖浏览器 WebCrypto，**必须 HTTPS**（`localhost` 除外）。纯 IP + HTTP 不要开 E2EE。

## 1. 域名、防火墙、用户

### 域名

在 DNS 服务商添加 A 记录：

```
ask.example.com  ->  你的公网 IP
```

等 DNS 生效后再启动 Caddy，否则签证书会失败。

### 防火墙

云厂商的安全组和系统 UFW 两边都要放行。以 UFW 为例：

```bash
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
sudo ufw status
```

不要放行 8080。

### 非 root 用户

```bash
sudo adduser deploy
sudo usermod -aG sudo deploy
su - deploy
```

后面的命令都用 `deploy` 用户执行。

## 2. 安装 Docker

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl git
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER"
newgrp docker
docker compose version
```

如果拉取镜像慢，配置 Docker 镜像加速，或把镜像推到自己的容器镜像服务。

## 3. 获取代码

```bash
sudo mkdir -p /opt/ask4me
sudo chown "$USER":"$USER" /opt/ask4me
cd /opt/ask4me
git clone https://github.com/easychen/ask4me.git .
```

默认是 `main`。部署自己的分支或 tag：

```bash
git checkout <branch-or-tag>
```

如果分支只在本地，先 `git push`；或从本地同步（**不要**带 `.env`、`data/`）：

```bash
rsync -av --exclude '.env' --exclude 'data' --exclude '.git' \
  ./ask4me/ deploy@your-server:/opt/ask4me/
```

> 本文按当前仓库版本（Go 1.27.3）编写；旧 tag 以那个 tag 的 Dockerfile / go.mod 为准。

## 4. 写 `.env`

```bash
cp .env.example .env
chmod 600 .env
```

### 4.1 单用户（自己用）

```dotenv
ASK4ME_BASE_URL=https://ask.example.com
ASK4ME_API_KEY=请换成一个长随机串
ASK4ME_SQLITE_PATH=/data/ask4me.db
ASK4ME_LISTEN_ADDR=:8080
ASK4ME_SERVERCHAN_SENDKEY=SCT...
TZ=Asia/Shanghai
```

### 4.2 多用户试用（无账号，key = 租户）

```dotenv
ASK4ME_BASE_URL=https://ask.example.com
ASK4ME_API_KEY=
ASK4ME_SQLITE_PATH=/data/ask4me.db
ASK4ME_LISTEN_ADDR=:8080

ASK4ME_MULTI_USER_ENABLED=true
ASK4ME_MULTI_USER_MODE=open
ASK4ME_MULTI_USER_INVITE_CODES=trial-2026
ASK4ME_MULTI_USER_REQUESTS_PER_DAY=200
ASK4ME_MULTI_USER_MAX_PENDING=5
ASK4ME_MULTI_USER_PER_IP_REQUESTS_PER_HOUR=60
ASK4ME_MULTI_USER_PER_IP_NEW_KEYS_PER_DAY=10
ASK4ME_MULTI_USER_GLOBAL_REQUESTS_PER_DAY=5000
ASK4ME_MULTI_USER_TRUST_PROXY=true

TZ=Asia/Shanghai
```

说明：

- 多用户下 `ASK4ME_API_KEY` 会被忽略，留空即可。
- 请求必须带 `notify`（ServerChan sendkey 或 Apprise URL）；全局渠道只在单用户模式使用。
- 想改成白名单：`ASK4ME_MULTI_USER_MODE=allowlist` + `ASK4ME_MULTI_USER_KEYS=key1,key2`；每 key 的配额可以用 YAML 配置。
- `open` 模式的 per-key 配额可以被换新 key 绕过，真正兜底是每 IP / 全局限额；公网试用建议再加一层 WAF 或网关限流。

生成试用 key：

```bash
invite='trial-2026'
printf 'a4m_%s_%s\n' "$invite" "$(openssl rand -hex 16)"
```

### 4.3 可选 E2EE

```dotenv
ASK4ME_E2EE_ENABLED=true
```

- `enabled=true` 只是允许使用 E2EE，**不强制**任何请求；用户自己决定是否加密，明文请求照常工作。
- 必须 `ASK4ME_BASE_URL=https://...`。
- 加密由 SDK/CLI 发起：`e2ee: { password }`；口令要另行告知回答者。
- 服务端只存密文和盐，不接触密码。

### 4.4 关于 Apprise

Docker 运行时镜像只带 `ca-certificates` / `tzdata`，**不带 apprise**：

- 用 ServerChan：零依赖，客户端在 `notify.serverchan_sendkey` 传 SendKey。
- 要用 Apprise：自定义镜像，在运行时层加：

```dockerfile
FROM ask4me:local
USER root
RUN apk add --no-cache python3 py3-pip \
 && pip install --break-system-packages apprise
```

或者宿主机直接跑二进制（宿主机装 apprise），不走 Docker。

## 5. 构建镜像（标准 / 极轻量）

### 直接用 GHCR 镜像

如果不想在服务器上构建，可以直接拉多架构镜像（`linux/amd64` + `linux/arm64`，x86 服务器会自动选 amd64）：

```bash
docker pull ghcr.io/easychen/ask4me:feat-e2ee
docker pull ghcr.io/easychen/ask4me:feat-e2ee-slim
```

固定 digest 的示例见 [docker-compose.ghcr.yml](docker-compose.ghcr.yml)。`feat-e2ee` 是开发分支 tag；正式发布请用 release tag。

### 标准镜像（推荐先跑通）

```bash
cd /opt/ask4me
docker build -t ask4me:local .
```

- 基于 `alpine:3.21`，带 `docker-entrypoint.sh`，会把环境变量写成 `/data/.env`。
- 方便 `docker exec` 进去看文件，排障简单。
- 体积比 slim 大几 MB。

### 极轻量镜像 `ask4me:slim`

```bash
docker build --target slim -t ask4me:slim .
```

- 基于 `scratch`：只留静态 Go 二进制、CA 根证书、zoneinfo。
- **没有 shell、没有包管理器**，不能 `docker exec sh`；排障靠 `docker logs`。
- 没有 `docker-entrypoint.sh`，容器直接从**环境变量**读配置（Go 端已支持），也可以用 `-config /data/ask4me.yaml`。
- 以 UID/GID `65532` 运行，`/data` 必须可写。
- 体积大致是“二进制 14MB + CA/zoneinfo”，压缩后约 10MB 级；实际以 `docker image ls` 为准。

`Dockerfile` 默认构建标准镜像；`--target slim` 才产出 slim。

## 6. `docker-compose.prod.yml`

不要直接改仓库里用官方镜像的 `docker-compose.yml`，新建一个生产文件。

### 标准镜像 + Caddy

```yaml
services:
  ask4me:
    build:
      context: .
      args:
        GOPROXY: https://goproxy.cn,direct
    image: ask4me:local
    container_name: ask4me
    restart: unless-stopped
    env_file: .env
    volumes:
      - ./data:/data
    expose:
      - "8080"
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

  caddy:
    image: caddy:2-alpine
    container_name: caddy
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    depends_on:
      - ask4me

volumes:
  caddy_data:
  caddy_config:
```

### 极轻量镜像 + Caddy

只改 `ask4me` 的 build/image，并注意 `data` 权限：

```yaml
  ask4me:
    build:
      context: .
      target: slim
      args:
        GOPROXY: https://goproxy.cn,direct
    image: ask4me:slim
    restart: unless-stopped
    env_file: .env
    volumes:
      - ./data:/data
    expose:
      - "8080"
```

```bash
mkdir -p data
sudo chown -R 65532:65532 data
```

如果权限不对，slim 容器会报 `unable to open database file`。

### Caddyfile

```caddyfile
ask.example.com {
    reverse_proxy ask4me:8080 {
        flush_interval -1
    }
}
```

`flush_interval -1` 关闭代理缓冲，SSE / 长轮询才能实时收到事件。不要给 SSE 加压缩中间件。

Caddy 默认用 HTTP-01 签证书，需要 80 端口可达；如果 80 被占用或不可达，改用 DNS challenge 或自有证书。

### 用 Nginx 的话

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

并设置 `ASK4ME_MULTI_USER_TRUST_PROXY=true`。如果 TLS 在云厂商负载均衡/CDN 上终止，也可以只让 Nginx/Caddy 走内网。

## 7. 启动和检查

```bash
cd /opt/ask4me
mkdir -p data
docker compose -f docker-compose.prod.yml up -d --build
docker compose -f docker-compose.prod.yml logs -f --tail=100 ask4me
```

标准镜像第一次启动后，如果想让备份方便：

```bash
sudo chown -R "$USER":"$USER" data
```

slim 镜像不要改成你的 UID，保持 `65532:65532`。

检查：

```bash
docker compose -f docker-compose.prod.yml ps
curl -I https://ask.example.com/
```

根路径返回 404 是正常的（没有 landing page），能完成 TLS 握手就说明 Caddy 和容器通了。

## 8. 发测试请求

### 多用户明文

```bash
KEY='a4m_trial-2026_把随机串贴这里'
curl -sS -N --max-time 30 -X POST 'https://ask.example.com/v1/ask?stream=true' \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "部署测试",
    "body": "收到请点 OK",
    "notify": { "serverchan_sendkey": "SCT..." },
    "mcd": ":::buttons\n- [OK](ok)\n- [Later](later)\n:::"
  }'
```

SSE 先返回 `request.created`（含 interaction_url），用户提交后返回 `user.submitted`。

### 可选 E2EE

```bash
export ASK4ME_E2EE_PASSWORD='和回答者另行约定的口令'
ask4me-cli -h https://ask.example.com -k "$KEY" \
  --title '加密审批' --body '这段正文会加密' \
  --notify-sendkey 'SCT...'
```

打开通知里的链接，输入口令解密；回答也会在浏览器里加密。

> 已发布的 npm 包可能还不包含 E2EE。用当前分支时，在仓库里 `npm install` 后跑 `node packages/cli/bin/ask4me-cli.js ...`，或等下一次 `ask4me-cli` / `ask4me-sdk` 发布。E2EE 只影响带 `e2ee` 的请求，其他用户不受影响。

## 9. 备份、升级、回滚

### 备份

SQLite 是单文件 + WAL。最稳的是短暂停容器再拷贝：

```bash
cd /opt/ask4me
mkdir -p backups
docker compose -f docker-compose.prod.yml stop ask4me
sudo cp data/ask4me.db* "backups/ask4me-$(date +%F-%H%M).db.bak"
docker compose -f docker-compose.prod.yml start ask4me
```

或者宿主机装 `sqlite3` 后在线备份：

```bash
sqlite3 data/ask4me.db ".backup 'backups/ask4me-$(date +%F-%H%M).db'"
```

### 升级

```bash
cd /opt/ask4me
# 先备份
git pull
docker compose -f docker-compose.prod.yml up -d --build
```

数据库迁移是加列/加表，向后兼容；升级前仍建议备份。

### 回滚

```bash
git checkout <上一个 tag 或 commit>
docker compose -f docker-compose.prod.yml up -d --build
```

数据库迁移通常只加不删；回滚前最好用升级前的备份。

## 10. 运维注意事项

1. **只跑一个 ask4me 实例**：SQLite 不适合多副本；不要横向扩容。
2. **SSE 反代必须关 buffering**：Caddy `flush_interval -1`，Nginx `proxy_buffering off`。
3. **E2EE 必须 HTTPS**：纯 IP + HTTP 下 WebCrypto 不可用。
4. **Apprise 不在镜像里**：默认 ServerChan；要 Apprise 就自定义镜像。
5. **slim 没有 shell**：不能 `docker exec sh`；看日志用 `docker logs`，排障用标准镜像。
6. **slim 的 `/data` 权限是 65532**：`chown -R 65532:65532 data`。
7. **SQLite 越来越大**：当前版本没有自动 retention，长期试用要定期清理旧请求/监控磁盘。
8. **公网试用防滥用**：`open` 模式 per-key 配额可被换 key 绕过；用每 IP / 全局限额，必要时加 WAF。
9. **日志轮转**：compose 已配 Docker 日志轮转；也可以接 systemd/journald。
10. **Go 版本**：Dockerfile 固定 1.27.3；宿主机直接编译需要 1.27.3+。

## 11. 最小检查清单

- [ ] 域名 A 记录指向公网 IP
- [ ] 22 / 80 / 443 放行，8080 未暴露
- [ ] `.env` 权限 600，`ASK4ME_BASE_URL` 是 `https://`
- [ ] 标准镜像或 slim 镜像构建成功
- [ ] slim 的 `data/` 属于 65532
- [ ] Caddy 签证书成功，`curl -I` 能通
- [ ] 多用户请求带 `notify`
- [ ] E2EE 只是 `enabled=true`，没有强制；需要的用户自己带 `e2ee`
- [ ] 已做一次备份恢复演练
