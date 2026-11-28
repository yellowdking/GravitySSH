# GravitySSH

> **Antigravity 风格的 Windows 现代化 SSH 客户端** — 集成防拷贝 DPAPI 加密保险库、代理/多级跳板机、自动 SFTP 文件管理、主机监控与运维预设、粘贴安全卫士、智能命令补齐以及基于 TLS 1.3 的零知识多端同步。

![Platform](https://img.shields.io/badge/Platform-Windows%2010%2F11%20x64-6366f1?style=flat-square)
![Tech](https://img.shields.io/badge/Stack-Go%20%2B%20Wails%20v2%20%2B%20React%20%2B%20xterm.js-38bdf8?style=flat-square)
![Security](https://img.shields.io/badge/Vault-Windows%20DPAPI%20%2B%20AES--256--GCM-22c55e?style=flat-square)

## 核心特性

- **Antigravity 极简深色工作台**：左侧导航栏与分组服务器树、多标签终端工作区、流畅的暗色终端配色与微交互、内置 GitHub Release 版本更新检查。
- **独立 SSH 密钥加密保险库**：
  - 专属密钥管理界面，支持**从本地文件加载私钥**、**粘贴导入**（OpenSSH / PEM / 带口令私钥）、**一键生成 Ed25519 / RSA-4096 密钥对**以及**一键安全部署公钥到远端主机**。
  - 私钥与口令入库后仅在软件内部内存中用于认证，永不落盘明文、不可导出。
- **灵活的网络与跳板机配置**：
  - 添加/编辑服务器除支持 IP、端口、用户名、密码、SSH 密钥外，内置 **SOCKS5 / HTTP CONNECT 代理** 与 **跳板机（支持选择已有服务器形成多级跳板链，或自定义独立跳板机）**。
  - 内置 **TOFU 主机密钥指纹校验**、心跳保活（KeepAlive）与连接测试，轻松直达弱网、跨境或内网服务器。
- **连接即通自动 SFTP 文件管理**：
  - SSH 会话连上后自动复用同一条加密隧道挂载 SFTP（穿过代理与跳板机同样开箱即用）。
  - 支持目录浏览、上传文件、**拖入文件直接上传**、下载文件（带实时进度条）、**在线文本编辑**、**远程压缩打包 / 解压**、`chmod` 权限修改、新建文件夹、重命名、删除以及一键在终端 `cd` 到当前目录。
- **终端体验增强与运维安全**：
  - **实时主机状态栏**：按需采集远端 CPU、内存、磁盘与运行时长，一目了然。
  - **操作命令记录 & 快捷方式 / 运维预设**：自动记录非敏感操作命令，支持多选历史一键组合保存为加密快捷方式；内置 **6 组常用运维预设**（如 BBR 加速、Swap 配置、防火墙/SSH 端口维护等，支持安全确认）。
  - **粘贴安全卫士（Paste Guard）**：粘贴前自动检测高危删除/格式化命令、多行隐式执行及零宽/控制逃逸字符，支持净化后再确认执行。
  - **终端关键字高亮 & 自动英文输入法切换**：高亮 `ERROR`/`WARN`/IP 等关键输出，聚焦终端时自动切换英文输入态。
  - **智能命令补齐（支持自主开关）**：内置 `apt`、`apk`、`dnf/yum`、`docker`、`docker compose`、`systemctl`、`journalctl`、`git`、`nginx`、`kubectl`、`ufw`、`ss`、`tar`、`rsync`、`tmux`、`pm2` 等常见运维命令推荐，融合快捷方式与历史记录，支持鼠标点击或 `↑`/`↓` + `Tab` 补齐。
- **硬件/账户绑定防拷贝加密存储**：
  - 所有 SSH 私钥、口令、服务器密码、代理密码、快捷命令与历史记录均统一保存在程序同目录 `gsshconfig\vault.gsv`（支持自定义**主密码双重加密与一键锁定**，版本升级平滑迁移）。
  - 采用 **Windows DPAPI (`CryptProtectData`) + `HKLM MachineGuid` + 应用独立高熵 HKDF-SHA256 + AES-256-GCM 认证加密**。
  - 即使他人拷贝 `vault.gsv` 到其他电脑或其他 Windows 账户，也**无法解密取走任何数据**。
- **零知识多端同步（`synctool-v1`，Windows ↔ Android）**：
  - 配套安卓客户端：[GravitySSH-Android](https://github.com/yellowdking/GravitySSH-Android) · 独立零知识中继协议与服务端：[synctool](https://github.com/yellowdking/synctool)。
  - 非云端托管：基于 **`synctool-v1` 端到端加密协议**（`X25519` 密钥协商 + `Ed25519` 设备身份签名 + `AES-256-GCM STREAM` 分块认证加密），同时支持**已配对设备 0-RTT 一键离线信箱同步（阅后签名物理销毁）**与**新机扫码（128-bit OOB）/ 8 位房间码 + 6 位 SAS 安全核验一次性直传**。
  - 默认内置中继节点（`43.155.231.14:54843`）强制 **TLS 1.3 + SPKI SHA-256 指纹固定**，中继全程盲态无法解密任何配置，接收端可先预览主机/密钥/快捷/模板数量再自由选择安全合并或全量覆盖。
  - 自建私有中继支持 `bbplin/synctool:latest` Docker 一键部署：
    ```bash
    docker volume create synctool_data
    docker run --rm -v synctool_data:/data bbplin/synctool:latest init --public-host YOUR_VPS_IP --listen :54843 --data-dir /data
    docker run -d --name synctool-relay --restart unless-stopped -p 54843:54843 -e SYNCTOOL_PROFILE=standard -e SYNCTOOL_SPOOL=disk -v synctool_data:/data bbplin/synctool:latest serve --data-dir /data
    ```
    部署后将输出的 `synctool://relay?v=1&u=...&pin=...#k=...` 填入客户端自定义中继栏（访问密钥仅保存在本地加密保险库 `vault.gsv` 内）并点击连接测试即可切换。

## 下载与使用

前往右侧 **[Releases](https://github.com/yellowdking/GravitySSH/releases)** 页面下载最新版本的：

- `GravitySSH-vX.Y.Z-windows-amd64.exe`（单文件绿色便携版，双击即用，无需安装）
- `GravitySSH-vX.Y.Z-windows-amd64-portable.zip`

> 运行环境：Windows 10 / Windows 11 (x64)，依赖系统 **Microsoft Edge WebView2 Evergreen 运行时**（Windows 11 默认自带，Windows 10 若缺失可自动/手动安装）。

## 本地开发与构建

```powershell
# 1. 运行后端静态检查与单元测试
go vet ./...
go test -v -count=1 ./...

# (Linux/macOS 或支持 CGO 的环境可运行竞态检测)
go test -race -count=1 ./internal/...

# 2. 运行前端单元测试与构建
cd frontend
npm install
npm test
npm run build
cd ..

# 3. 生成 Windows 应用图标并编译可执行文件
python scripts/gen_icons.py
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1
$ver = (Get-Content -Raw VERSION).Trim()
wails build -clean -s -skipbindings -trimpath -ldflags "-s -w -X main.AppVersion=$ver"
```
