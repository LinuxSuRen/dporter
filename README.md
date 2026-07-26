# dporter

Docker 容器管理面板 —— 集容器运维、Web 终端、端口转发于一体的轻量工具，附带 Web 管理界面。

## 功能特色

### 容器管理
- **容器列表** —— 列出所有 Docker 容器，显示运行状态、网络 IP、端口映射、所属 Compose 项目
- **启停控制** —— 启动、停止、重启容器，支持批量重启
- **容器详情** —— 查看容器完整 inspect 信息
- **镜像拉取** —— 为容器拉取/更新镜像，支持 SSE 实时进度流

### Web 终端
- **交互式 Shell** —— 通过 WebSocket 进入容器内部 `/bin/sh`，基于 xterm.js
- **日志流** —— WebSocket 实时查看容器日志（跟随模式）
- 支持 xterm-256color，完整的终端体验

### 端口转发
- **按需转发** —— 为任意运行中的容器创建 `localhost` → 容器 IP:端口 的 TCP 转发
- **实时推送** —— 转发状态变更是通过 WebSocket 推送到前端
- **一键删除** —— 停止不再需要的端口转发

### Docker Compose 集成
- **按项目重启** —— 自动识别 Compose 项目，通过 `docker compose restart` 重启
- **重启并拉取** —— `compose pull` + `compose up -d` 一键更新
- **仅拉取镜像** —— 批量更新 Compose 项目的所有镜像

### 安全认证
- **Linux 用户认证** —— 通过 `-auth` 启用 HTTP Basic Auth，验证 `/etc/shadow` 中的 Linux 系统用户

### 代理模式
- **远程代理** —— 通过 `-api` 参数可将 API 请求转发到另一台运行 dporter 的主机，前端与后端分离部署

### 零依赖部署
- **单二进制** —— 前端静态资源通过 Go `embed` 编译进二进制文件，无需额外部署
- **仅需 Docker** —— 唯一外部依赖是 Docker daemon

## 快速开始

```bash
# 编译
go build -o dporter .

# 运行（默认监听 :8080）
./dporter

# 自定义端口
./dporter -p 9090

# 通过环境变量
PORT=9090 ./dporter
```

打开浏览器访问 `http://localhost:8080`。

### 启用认证

```bash
./dporter -auth
```

使用 Linux 系统用户名和密码登录。

### 远程代理模式

```bash
# 在远程 Docker 主机上运行
./dporter -p 8080

# 在本地运行（仅提供 Web UI，API 请求转发到远程）
./dporter -api http://remote-host:8080
```

本地浏览器打开 `http://localhost:8080`，所有 API 请求自动转发到远程主机。

### Docker 安装（无需 Go 环境）

从已发布的容器镜像中直接提取二进制文件安装：

```bash
docker create --name tmp ghcr.io/linuxsuren/dporter:latest \
  && docker cp tmp:/dporter /usr/local/bin/dporter \
  && docker rm tmp \
  && chmod +x /usr/local/bin/dporter

# 验证
dporter --help
```

镜像每次推送 `main` 分支和 `v*` 标签都会自动构建，支持 `linux/amd64` 和 `linux/arm64`。

## API

### 容器

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/containers` | 列出所有容器 |
| `GET` | `/api/containers/{id}/inspect` | 查看容器详情 |
| `GET` | `/api/containers/{id}/shell` | WebSocket 交互式 Shell |
| `GET` | `/api/containers/{id}/logs` | WebSocket 日志流 |
| `GET` | `/api/containers/{id}/pull` | SSE 拉取镜像 |
| `POST` | `/api/containers/{id}/restart` | 重启容器 |
| `POST` | `/api/containers/{id}/stop` | 停止容器 |
| `POST` | `/api/containers/{id}/start` | 启动容器 |
| `POST` | `/api/containers/batch/restart` | 批量重启 |

### 端口转发

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/forwards` | 列出当前转发 |
| `GET` | `/api/forwards/ws` | WebSocket 转发状态推送 |
| `POST` | `/api/forwards` | 创建转发 |
| `DELETE` | `/api/forwards/{id}` | 删除转发 |

创建转发示例：

```json
POST /api/forwards
{
  "containerId": "my-container",
  "containerPort": 3306,
  "localPort": 3307
}
```

`containerId` 支持容器 ID 前缀、完整 ID 或容器名称。`localPort` 省略时默认使用 `containerPort`。

### Compose

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/compose/restart?project=xxx` | SSE Compose 重启 |
| `GET` | `/api/compose/restart-pull?project=xxx` | SSE Compose pull + up |
| `GET` | `/api/compose/pull?project=xxx` | SSE Compose pull |

### 系统

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/version` | 版本信息 |
| `GET` | `/api/images/info` | 镜像信息 |

## 依赖

- Go 1.22+
- Docker daemon（通过 `unix:///var/run/docker.sock` 或 `DOCKER_HOST` 环境变量连接）

## License

MIT
