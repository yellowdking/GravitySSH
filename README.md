# GravitySSH

> **Antigravity 风格的 Windows 现代化 SSH 客户端** — 集成防拷贝 DPAPI 加密保险库、代理/多级跳板机、自动 SFTP 文件管理、操作命令记录与快捷方式、以及可开关的智能命令补齐。

![Platform](https://img.shields.io/badge/Platform-Windows%2010%2F11%20x64-6366f1?style=flat-square)
![Tech](https://img.shields.io/badge/Stack-Go%20%2B%20Wails%20v2%20%2B%20React%20%2B%20xterm.js-38bdf8?style=flat-square)
![Security](https://img.shields.io/badge/Vault-Windows%20DPAPI%20%2B%20AES--256--GCM-22c55e?style=flat-square)

## 核心特性

- **Antigravity 极简深色工作台**：左侧导航栏与分组服务器树、多标签终端工作区、流畅的暗色终端配色与微交互。
- **独立 SSH 密钥加密保险库**：
  - 专属密钥管理界面，支持**从本地文件加载私钥**、**粘贴导入**（OpenSSH / PEM / 带口令私钥）以及**一键生成 Ed25519 / RSA-4096 密钥对**。
  - 私钥与口令入库后仅在软件内部内存中用于认证，永不落盘明文、不可导出。
- **灵活的网络与跳板机配置**：
  - 添加/编辑服务器除支持 IP、端口、用户名、密码、SSH 密钥外，内置 **SOCKS5 / HTTP CONNECT 代理** 与 **跳板机（支持选择已有服务器形成多级跳板链，或自定义独立跳板机）**。
  - 支持心跳保活（KeepAlive）与连接测试，轻松直达弱网、跨境或内网服务器。
- **连接即通自动 SFTP 文件管理**：
  - SSH 会话连上后自动复用同一条加密隧道挂载 SFTP（穿过代理与跳板机同样开箱即用）。
  - 支持目录浏览、上传文件、**拖入文件直接上传**、下载文件（带实时进度条）、新建文件夹、重命名、删除以及一键在终端 `cd` 到当前目录。
- **操作命令记录 & 快捷方式**：
  - 自动记录每一次在终端执行的命令（支持按服务器筛选与搜索）。
  - 支持单选或**多选历史命令一键组合保存为加密快捷方式**，后续可一键立即执行或填入终端微调。
- **智能命令补齐（支持自主开关）**：
  - 内置 `apt`、`apk`、`dnf/yum`、`docker`、`docker compose`、`systemctl`、`journalctl`、`git`、`nginx`、`kubectl`、`ufw`、`ss`、`tar`、`rsync`、`tmux`、`pm2` 等常见运维命令推荐，并融合您的快捷方式与历史记录。
  - 输入时实时弹出候选列表，**鼠标点击任意推荐条目即可自动补齐**（亦支持 `↑`/`↓` + `Tab`）。
  - 可在**设置页面**或终端顶部工具栏随时开关，并独立控制是否结合快捷方式或历史记录。
- **硬件/账户绑定防拷贝加密存储**：
  - 所有 SSH 私钥、口令、服务器密码、代理密码、快捷命令与历史记录均统一保存在 `%APPDATA%\GravitySSH\vault.gsv`。
  - 采用 **Windows DPAPI (`CryptProtectData`) + `HKLM MachineGuid` + 应用独立高熵 HKDF-SHA256 + AES-256-GCM 认证加密**。
  - 即使他人拷贝 `vault.gsv` 到其他电脑或其他 Windows 账户，也**无法解密取走任何数据**。

## 下载与使用

前往右侧 **[Releases](https://github.com/yellowdking/GravitySSH/releases)** 页面下载最新版本的：

- `GravitySSH-vX.Y.Z-windows-amd64.exe`（单文件绿色便携版，双击即用，无需安装）
- `GravitySSH-vX.Y.Z-windows-amd64-portable.zip`

> 运行环境：Windows 10 / Windows 11 (x64)，内置 WebView2 运行时（Windows 11 默认自带）。

## 本地开发与构建

```powershell
# 1. 运行后端单元测试
go test -v ./...

# 2. 运行前端单元测试与构建
cd frontend
npm install
npm test
npm run build
cd ..

# 3. 编译 Windows 可执行文件
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1
wails build -clean -s -skipbindings
```
