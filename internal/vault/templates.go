package vault

import "github.com/yellowdking/GravitySSH/internal/models"

// BuiltinOpsTemplates returns curated, safety-hardened VPS initialization & ops shortcuts
// (BBR, SWAP, mirror switch, Docker install, safe SSH port change with rollback).
func BuiltinOpsTemplates() []models.BuiltinTemplate {
	return []models.BuiltinTemplate{
		{
			ID:          "tpl-bbr",
			Name:        "开启 BBR 网络加速",
			Category:    "网络优化",
			Description: "配置 fq 队列与 BBR 拥塞控制，独立写入 sysctl.d 并校验生效",
			SafetyNote:  "使用独立配置文件 /etc/sysctl.d/99-gravityssh-bbr.conf，可随时删除回退",
			Command: `sudo sh -c 'cat >/etc/sysctl.d/99-gravityssh-bbr.conf <<EOF
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
EOF
sysctl --system >/dev/null 2>&1 || sysctl -p /etc/sysctl.d/99-gravityssh-bbr.conf'
sysctl net.ipv4.tcp_congestion_control net.core.default_qdisc
lsmod | grep -i bbr || true`,
		},
		{
			ID:          "tpl-swap",
			Name:        "安全创建 2G SWAP 虚拟内存",
			Category:    "系统优化",
			Description: "检查现有 SWAP，创建 /swapfile (600 权限) 并备份后写入 /etc/fstab 持久化",
			SafetyNote:  "已存在 /swapfile 时自动跳过，修改 /etc/fstab 前自动生成 .bak 备份",
			Command: `SWAP_SIZE="2G"
if [ -f /swapfile ]; then echo "/swapfile 已存在，跳过创建"; swapon --show; exit 0; fi
sudo fallocate -l "$SWAP_SIZE" /swapfile || sudo dd if=/dev/zero of=/swapfile bs=1M count=2048 status=progress
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile
sudo cp -a /etc/fstab /etc/fstab.bak.$(date +%s)
grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab >/dev/null
free -h && swapon --show`,
		},
		{
			ID:          "tpl-mirror",
			Name:        "一键切换国内镜像源（自动识别系统）",
			Category:    "系统维护",
			Description: "自动识别 Debian / Ubuntu / Alpine，备份原软件源后切换为阿里云镜像并刷新缓存",
			SafetyNote:  "替换前自动备份原始源配置文件，可随时恢复",
			Command: `if [ -f /etc/os-release ]; then . /etc/os-release; fi
case "$ID" in
  debian)
    if [ -f /etc/apt/sources.list.d/debian.sources ]; then
      sudo cp -a /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list.d/debian.sources.bak
      sudo sed -i 's|deb.debian.org|mirrors.aliyun.com|g; s|security.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources
    elif [ -f /etc/apt/sources.list ]; then
      sudo cp -a /etc/apt/sources.list /etc/apt/sources.list.bak
      sudo sed -i 's|deb.debian.org|mirrors.aliyun.com|g; s|security.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list
    fi
    sudo apt-get update ;;
  ubuntu)
    if [ -f /etc/apt/sources.list.d/ubuntu.sources ]; then
      sudo cp -a /etc/apt/sources.list.d/ubuntu.sources /etc/apt/sources.list.d/ubuntu.sources.bak
      sudo sed -i 's|archive.ubuntu.com|mirrors.aliyun.com|g; s|security.ubuntu.com|mirrors.aliyun.com|g' /etc/apt/sources.list.d/ubuntu.sources
    elif [ -f /etc/apt/sources.list ]; then
      sudo cp -a /etc/apt/sources.list /etc/apt/sources.list.bak
      sudo sed -i 's|archive.ubuntu.com|mirrors.aliyun.com|g; s|security.ubuntu.com|mirrors.aliyun.com|g' /etc/apt/sources.list
    fi
    sudo apt-get update ;;
  alpine)
    sudo cp -a /etc/apk/repositories /etc/apk/repositories.bak
    sudo sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories
    sudo apk update ;;
  *) echo "未自动适配的系统: ${ID:-unknown}，请手动检查软件源" ;;
esac`,
		},
		{
			ID:          "tpl-docker",
			Name:        "一键安装 Docker 与 Compose",
			Category:    "Docker",
			Description: "安装 Docker Engine 与 Compose 插件，设置开机自启并验证版本",
			SafetyNote:  "已安装 Docker 时直接显示版本与状态，不重复覆盖",
			Command: `if command -v docker >/dev/null 2>&1; then
  echo "Docker 已安装：$(docker --version)"
else
  curl -fsSL https://get.docker.com | sudo sh || curl -fsSL https://get.docker.com | sudo sh -s -- --mirror Aliyun
fi
sudo systemctl enable --now docker 2>/dev/null || true
docker --version && docker compose version`,
		},
		{
			ID:          "tpl-ssh-port",
			Name:        "安全修改 SSH 端口（防锁死 10 分钟自动回滚）",
			Category:    "安全加固",
			Description: "新增新端口并保留旧端口、放行防火墙、语法校验后重载；10 分钟未确认自动回滚",
			SafetyNote:  "防失联机制：请先用新端口测试连接成功后再执行回显的取消回滚命令",
			Command: `printf "请输入新的 SSH 端口 (1024-65535): "; read NEW_PORT
case "$NEW_PORT" in ''|*[!0-9]*) echo "端口必须是数字"; exit 1 ;; esac
[ "$NEW_PORT" -lt 1024 ] || [ "$NEW_PORT" -gt 65535 ] && { echo "建议使用 1024-65535 之间的端口"; exit 1; }
BAK="/etc/ssh/sshd_config.bak.$(date +%s)"
sudo cp -a /etc/ssh/sshd_config "$BAK"
OLD_PORT=$(sudo sshd -T 2>/dev/null | awk '/^port /{print $2; exit}')
OLD_PORT=${OLD_PORT:-22}
sudo mkdir -p /etc/ssh/sshd_config.d
printf "Port %s\nPort %s\n" "$OLD_PORT" "$NEW_PORT" | sudo tee /etc/ssh/sshd_config.d/99-gravityssh-port.conf >/dev/null
if ! sudo sshd -t; then
  sudo rm -f /etc/ssh/sshd_config.d/99-gravityssh-port.conf
  echo "sshd 配置校验失败，已自动撤销"; exit 1
fi
command -v ufw >/dev/null 2>&1 && sudo ufw allow "${NEW_PORT}/tcp" >/dev/null 2>&1 || true
command -v firewall-cmd >/dev/null 2>&1 && sudo firewall-cmd --permanent --add-port="${NEW_PORT}/tcp" >/dev/null 2>&1 && sudo firewall-cmd --reload >/dev/null 2>&1 || true
sudo systemctl reload sshd 2>/dev/null || sudo systemctl reload ssh 2>/dev/null || sudo service ssh reload
echo "✓ 已同时监听旧端口 ${OLD_PORT} 与新端口 ${NEW_PORT}（备份: ${BAK}）"
(sleep 600 && [ -f /tmp/.gssh_port_pending ] && sudo rm -f /etc/ssh/sshd_config.d/99-gravityssh-port.conf /tmp/.gssh_port_pending && (sudo systemctl reload sshd 2>/dev/null || sudo systemctl reload ssh 2>/dev/null || true)) >/dev/null 2>&1 &
touch /tmp/.gssh_port_pending
echo ">>> 请立即新建会话测试新端口 ${NEW_PORT}！确认可连上后执行: rm -f /tmp/.gssh_port_pending"
echo ">>> 10 分钟内未确认将自动回滚至旧端口 ${OLD_PORT}。"`,
		},
	}
}
