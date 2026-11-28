# GravitySSH

跨平台 SSH 与 SFTP 桌面客户端（Windows & macOS Universal），支持代理/多级跳板机、本地加密保险库、命令补齐与多端同步。

![Platform](https://img.shields.io/badge/Desktop-Windows%20x64%20%7C%20macOS%20Universal-6366f1?style=flat-square)
![Mobile](https://img.shields.io/badge/Mobile-Android%20%7C%20iOS-38bdf8?style=flat-square)

## 功能概览

- **服务器与网络隧道**：分组主机管理、独立 SSH 密钥库（导入、生成、一键部署公钥）、SOCKS5 / HTTP 代理、多级跳板机、主机指纹校验。
- **自动 SFTP 文件管理**：复用当前 SSH 会话挂载 SFTP，支持目录浏览、拖拽上传、流式打包下载、在线文本编辑、远程压缩/解压与权限对齐。
- **高效终端体验**：多标签会话、底部实时监控栏、命令历史与快捷命令、6 组运维预设模板、智能命令与路径补齐、粘贴安全拦截、日志关键词高亮。
- **本地加密保险库**：Windows（DPAPI 绑定程序同目录 `gsshconfig\vault.gsv`）与 macOS（Login Keychain 绑定 `~/Library/Application Support/GravitySSH/gsshconfig/vault.gsv`），支持可选自定义主密码锁定。
- **全平台多端同步（Windows ↔ macOS ↔ Android ↔ iOS）**：
  - 移动端客户端：[GravitySSH-Android](https://github.com/yellowdking/GravitySSH-Android) · [GravitySSH-iOS](https://github.com/yellowdking/GravitySSH-iOS)
  - 支持已配对设备推送、离线收件箱与扫码/房间码直传；内置官方默认中继，亦支持自建中继：
    ```bash
    docker run --rm -v synctool_data:/data bbplin/synctool:latest init --public-host <公网IP>
    docker run -d --name synctool-relay --restart unless-stopped -p 54843:54843 -v synctool_data:/data bbplin/synctool:latest
    ```

## 下载与运行

前往 **[Releases](https://github.com/yellowdking/GravitySSH/releases)** 下载对应平台安装包：

- **Windows 10 / 11 (x64)**：`GravitySSH-vX.Y.Z-windows-amd64.exe`（单文件便携版，双击即用）或 `-portable.zip`。
- **macOS 11+ (Apple Silicon & Intel Universal)**：`GravitySSH-vX.Y.Z-darwin-universal.dmg`（打开后将 `GravitySSH.app` 拖入 `Applications`；首次运行若提示未验证开发者，可在终端执行 `xattr -cr /Applications/GravitySSH.app` 或在「系统设置 → 隐私与安全性」中点击仍要打开）。

## 本地开发与构建

```bash
# 1. 前端单元测试与构建
cd frontend && npm install && npm test && npm run build && cd ..

# 2. 后端静态检查与单元测试
go vet ./...
go test -v -count=1 ./...

# 3. 生成图标并打包桌面端（Windows 或 macOS Universal）
python3 scripts/gen_icons.py
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1

# Windows:
wails build -clean -s -skipbindings -trimpath -ldflags "-s -w -X main.AppVersion=$(cat VERSION)"

# macOS Universal (.app):
wails build -platform darwin/universal -clean -s -skipbindings -trimpath -ldflags "-s -w -X main.AppVersion=$(cat VERSION)"
```
