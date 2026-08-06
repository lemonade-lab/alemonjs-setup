# Docker 部署 ALemonX

本文面向一个统一产品的两个部署场景：本地工作台用于开发和调试机器人；线上工作台用于运行、维护线上机器人。它们使用同一镜像和界面，但各自拥有独立、持久化的工作目录。

## 1. 持久化边界

Compose 将运行数据放在项目目录，便于备份、迁移与同步：

| 本机目录 | 容器路径 | 内容 |
| --- | --- | --- |
| `workspace/robots/` | `/workspace/robots/` | 机器人项目、Git 仓库、依赖与配置 |
| `state/` | `/home/alx/.config/` | 身份认证、Setup 插件、AI 等工作台状态 |
| `ssh/`（可选） | `/home/alx/.ssh/` | 仅供部署使用的 SSH 密钥与 known_hosts |

本地与线上不应默认共享这些目录：本地的机器人是开发资产，线上目录是部署资产。要发布机器人，请使用 Git、制品包或既有 CI/CD 将代码交付到线上 `workspace/robots/`，再由线上工作台完成依赖、运行与运维管理。不要迁移 `node_modules/`，可在目标服务器通过工作台重新安装依赖。

## 2. 直接用 Compose 启动

在任意一台已安装 Docker 的机器上，克隆或解压本仓库后即可启动：

```bash
docker compose up -d --build
docker compose ps
```

首次启动会创建 `workspace/`、`state/` 和 `ssh/`，入口脚本会处理 Linux Docker bind mount 常见的权限问题。访问 `http://127.0.0.1:17390` 即可进入工作台。

## 3. 本地构建离线镜像包

先启动 Docker Desktop / Docker daemon。Linux x86 服务器使用：

```bash
chmod +x docker-buildx.sh
VERSION=v0.1.0 PLATFORM=linux/amd64 ./docker-buildx.sh
```

Apple Silicon Linux 服务器改用：

```bash
VERSION=v0.1.0 PLATFORM=linux/arm64 ./docker-buildx.sh
```

构建完成会生成 OCI 镜像包，例如：

```text
dist/alx-v0.1.0-amd64.oci.tar
```

可在本机验证包能导入：

```bash
docker load -i dist/alx-v0.1.0-amd64.oci.tar
```

## 4. 上传并离线启动

将以下内容传到服务器的同一个目录：

- `docker-compose.yml`
- `dist/alx-v0.1.0-amd64.oci.tar`
- 线上环境自己的 `workspace/`、`state/`（可为空，由首次启动创建）
- `ssh/`（可选，且只放部署专用密钥）

服务器上执行：

```bash
docker load -i dist/alx-v0.1.0-amd64.oci.tar
alx_IMAGE=alemonx:v0.1.0 docker compose up -d
docker compose ps
docker compose logs -f alx
```

首次启动后立即开启身份认证：

```bash
docker compose exec alx alx auth enable \
  --account admin \
  --password '请使用高强度密码' \
  --confirm-password '请使用高强度密码'
```

默认端口仅映射到服务器本机 `127.0.0.1:17390`，因此适合交给 Nginx、Caddy 或 Traefik 做 HTTPS 反向代理。

## 5. 使用 Nginx 对外提供 HTTPS

在宿主机 Nginx 中创建站点配置，将 `setup.example.com` 改成你的域名。TLS 证书可由 Certbot 或现有证书管理系统提供。

```nginx
server {
    listen 80;
    server_name setup.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name setup.example.com;

    ssl_certificate     /etc/letsencrypt/live/setup.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/setup.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:17390;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

检查并加载配置：

```bash
nginx -t && systemctl reload nginx
```

务必同时保留 ALemonX 自身的身份认证；Nginx 的 HTTPS 只负责传输安全，不替代工作台登录保护。

## 6. 更新、备份与恢复

更新镜像后：

```bash
docker load -i dist/alx-v0.2.0-amd64.oci.tar
alx_IMAGE=alemonx:v0.2.0 docker compose up -d
```

备份时停止容器或确保机器人无写入任务，然后归档 `workspace/` 与 `state/`：

```bash
tar -czf alx-backup-$(date +%F).tar.gz workspace state
```

恢复时解压这两个目录，再启动 Compose。SSH 密钥应单独、加密保管；不要把个人电脑完整的 `~/.ssh` 挂载到生产容器。

## 7. 常用排障

```bash
docker compose ps
docker compose logs --tail=200 alx
docker compose exec alx alx auth status
docker compose exec alx git --version
docker compose exec alx node --version
```

若工作台无法添加目录，确认机器人目录位于 `workspace/robots/`。这是容器的安全边界；工作台不会管理容器的任意路径。
