# sshsync — GravitySSH 零知识一次性同步中继

`sshsync` 是 [GravitySSH](https://github.com/yellowdking/GravitySSH)（Windows）与 [GravitySSH-Android](https://github.com/yellowdking/GravitySSH-Android) 的配套零知识多端同步中继服务，使用纯 Go 标准库零外部依赖构建，多架构 `scratch` 镜像仅约 5.5 MB（支持 `linux/amd64` 与 `linux/arm64`）。

## 安全特性

- **零知识端到端双向确认（`gssh-sync-v2`）**：发送端生成 8 位一次性同步码 / 二维码；接收端先发起配对请求并生成 4 位核对码，发送端人工确认无误后，客户端本地使用双密钥 `PBKDF2-HMAC-SHA256 (200,000 次)` + `AES-256-GCM` 加密配置包上传。中继仅透传密文，无法解密任何主机、密码或私钥。
- **纯内存阅后即焚与防滥用配额**：密文仅存于进程内存中，**默认 5 分钟过期（上限 10 分钟）或被读取 1 次即立即清零销毁**，永不写入磁盘；全局最多 64 个活跃通道 / 16 MiB 内存上限，单 IP（IPv6 按 `/64` 归桶）最多同时持有 4 个通道。
- **强制 TLS 1.3 + 隐身防扫描**：拒绝 TLS 1.2 及以下协议；未携带合法 App Gate HMAC 签名的探测请求均返回静默空 `404`（无 `Server` 响应头并主动断开连接），内置单 IP 限流、并发重请求保护与 10 次异常自动封禁 15 分钟。
- **容器自检与资源隔离**：内置 `-healthcheck` 本地 TLS 1.3 `/v1/ping` 自检命令（适配 `scratch` 镜像 `HEALTHCHECK`）及 `GOMEMLIMIT=48MiB` 软内存上限。
- **独立专用端口**：默认监听 `54843`，不占用 `80`/`443`，可与 Nginx Proxy Manager 等反向代理和平共存。

## 快速部署（任一 VPS）

### 方式一：单条 Docker 命令（推荐，开箱即用）

```bash
docker run -d \
  --name sshsync \
  --restart unless-stopped \
  -p 54843:54843 \
  --read-only \
  --memory=96m \
  --pids-limit=64 \
  --cpus=0.5 \
  --cap-drop=ALL \
  --security-opt no-new-privileges:true \
  bbplin/sshsync:latest
```

> 启动时若未挂载证书，程序会在内存中自动签发 10 年期 ECDSA P-256 自签名 TLS 1.3 证书并使用内置默认 App Gate 密钥，无需任何额外配置文件。若通过 `--cert`/`--key`（或环境变量）指定自定义证书，二者必须同时提供且证书需处于有效期内（`NotBefore <= now < NotAfter`）。

### 方式二：Docker Compose（持久化 TLS 证书 + 日志轮转）

```bash
mkdir -p /opt/sshsync/certs && chown 65532:65532 /opt/sshsync/certs
cp docker-compose.yml /opt/sshsync/
cd /opt/sshsync && docker compose up -d
```

## 客户端接入

部署完成后，在 **GravitySSH (Windows)** 或 **GravitySSH-Android** 的「多端同步」页面：

1. 在「同步中继服务器 (IP:端口)」输入框填写 `你的VPS_IP:54843`（或已配置证书的 HTTPS 域名）；
2. 点击 **测试连接** 验证连通性与延迟并点击 **保存**；
3. 发送端生成 8 位一次性同步码 / 二维码，接收端输入或扫码发起配对，发送端核对 4 位确认码后点击同意，接收端即可预览并安全合并或覆盖导入。

## 环境变量说明

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SSHSYNC_LISTEN` | `:54843` | TLS 1.3 监听地址与端口（对应 `-listen`） |
| `SSHSYNC_DIR` | `/opt/sshsync` | 未设置 `--cert`/`--key` 时默认查找 `certs/server.{crt,key}` 与 `app-gate.key` 的基础目录（对应 `-dir`） |
| `SSHSYNC_SAN_IP` | `sshsync` | 自动生成自签名证书时写入的 SAN IP 或主机名（对应 `-san-ip`） |
| `SSHSYNC_CERT_FILE` | `/data/cert.pem`（镜像内） | TLS 证书 PEM 路径（对应 `-cert`，必须与 `SSHSYNC_KEY_FILE` 同时设置；二者均不存在时自动生成） |
| `SSHSYNC_KEY_FILE` | `/data/key.pem`（镜像内） | TLS 私钥 PEM 路径（对应 `-key`，必须与 `SSHSYNC_CERT_FILE` 同时设置；二者均不存在时自动生成） |
| `SSHSYNC_APP_GATE_KEY` | 内置默认密钥 | **标准 GravitySSH 客户端请保持默认勿修改**。当前 Windows / Android 官方客户端使用内置 App Gate 密钥且暂不支持自定义覆盖；修改此项会导致官方客户端全部收到静默 `404` 并被封禁 15 分钟，仅在自行编译匹配密钥的定制客户端时覆盖（64 位小写十六进制，对应 `-gate-key`）。 |

## 常用命令行模式

```bash
# 预先在指定目录初始化证书、SPKI 指纹与 app-gate.key
sshsync init -dir /opt/sshsync -san-ip 你的VPS_IP

# 执行本地健康检查（Docker HEALTHCHECK 使用，成功退出码为 0）
sshsync -healthcheck -listen :54843
```

## 本地构建与测试

```bash
go vet ./cmd/sshsync/...
go test -race -v -count=1 ./cmd/sshsync/...
docker build -t bbplin/sshsync:local ./cmd/sshsync
```
