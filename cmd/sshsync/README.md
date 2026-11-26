# sshsync — GravitySSH 零知识一次性同步中继

`sshsync` 是 [GravitySSH](https://github.com/yellowdking/GravitySSH)（Windows）与 [GravitySSH-Android](https://github.com/yellowdking/GravitySSH-Android) 的配套零知识多端同步中继服务，使用纯 Go 标准库零外部依赖构建，多架构 `scratch` 镜像仅约 5.5 MB（支持 `linux/amd64` 与 `linux/arm64`）。

## 安全特性

- **零知识端到端加密**：客户端本地使用 `PBKDF2-HMAC-SHA256 (200,000 次)` + `AES-256-GCM` 加密配置包，中继仅透传密文，无法解密任何主机、密码或私钥。
- **纯内存阅后即焚**：密文仅存于进程内存中，**5 分钟过期或被读取 1 次即立即擦除销毁**，永不写入磁盘。
- **强制 TLS 1.3 + 隐身防扫描**：拒绝 TLS 1.2 及以下协议；未携带合法 App Gate HMAC 签名的任何探测请求均返回静默空 `404`（无 `Server` 响应头），并内置单 IP 限流与 10 次异常自动封禁 15 分钟。
- **独立专用端口**：默认监听 `54843`，不占用 `80`/`443`，可与 Nginx Proxy Manager 等反向代理和平共存。

## 快速部署（任一 VPS）

### 方式一：单条 Docker 命令（推荐，开箱即用）

```bash
docker run -d \
  --name sshsync \
  --restart unless-stopped \
  -p 54843:54843 \
  --read-only \
  --memory=64m \
  --cap-drop=ALL \
  --security-opt no-new-privileges:true \
  bbplin/sshsync:latest
```

> 启动时若未挂载证书，程序会在内存中自动签发 10 年期 ECDSA P-256 自签名 TLS 1.3 证书并使用内置默认 App Gate 密钥，无需任何额外配置文件。

### 方式二：Docker Compose（持久化 TLS 证书）

```bash
mkdir -p /opt/sshsync/certs && chown 65532:65532 /opt/sshsync/certs
cp docker-compose.yml /opt/sshsync/
cd /opt/sshsync && docker compose up -d
```

## 客户端接入

部署完成后，在 **GravitySSH (Windows)** 或 **GravitySSH-Android** 的「多端同步」页面：

1. 在「同步中继服务器 (IP:端口)」输入框填写 `你的VPS_IP:54843`；
2. 点击 **测试连接** 验证延迟并点击 **保存**；
3. 一端生成 8 位一次性同步码或二维码，另一端输入或扫码即可完成同步。

## 环境变量说明

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SSHSYNC_LISTEN` | `:54843` | 监听地址与端口 |
| `SSHSYNC_CERT_FILE` | `/data/cert.pem` | TLS 证书路径（缺失时自动生成） |
| `SSHSYNC_KEY_FILE` | `/data/key.pem` | TLS 私钥路径（缺失时自动生成） |
| `SSHSYNC_APP_GATE_KEY` | 内置默认密钥 | 可选，自定义接入鉴权密钥 |

## 本地构建与测试

```bash
go vet ./cmd/sshsync/...
go test -v -count=1 ./cmd/sshsync/...
docker build -t bbplin/sshsync:local ./cmd/sshsync
```
