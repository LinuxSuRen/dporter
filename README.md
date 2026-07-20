# dporter

Docker 容器端口转发工具 —— 为没有映射宿主机端口的容器提供按需 TCP 端口转发，附带 Web 管理界面。

## 功能

- **容器列表** —— 列出所有 Docker 容器及其网络信息、端口映射状态
- **按需转发** —— 为任意运行中的容器创建 `localhost` → 容器 IP:端口 的 TCP 转发
- **一键删除** —— 停止不再需要的端口转发
- **Web UI** —— 嵌入式前端，无需额外部署

## 快速开始

```bash
# 编译
go build -o dporter .

# 运行（默认监听 :8080）
./dporter

# 自定义端口
PORT=9090 ./dporter
```

打开浏览器访问 `http://localhost:8080`。

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/containers` | 列出所有容器 |
| `GET` | `/api/forwards` | 列出当前转发 |
| `POST` | `/api/forwards` | 创建转发 |
| `DELETE` | `/api/forwards/{id}` | 删除转发 |

### 创建转发

```json
POST /api/forwards
{
  "containerId": "my-container",
  "containerPort": 3306,
  "localPort": 3307
}
```

`containerId` 支持容器 ID 前缀、完整 ID 或容器名称。`localPort` 省略时默认使用 `containerPort`。

## 依赖

- Go 1.22+
- Docker daemon（通过 `unix:///var/run/docker.sock` 或 `DOCKER_HOST` 环境变量连接）

## License

MIT
