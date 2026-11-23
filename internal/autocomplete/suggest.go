package autocomplete

import (
	"sort"
	"strings"

	"github.com/yellowdking/GravitySSH/internal/models"
)

var builtinCatalogOps = []builtinEntry{
	{"systemctl status ", "查看系统服务运行状态", "Systemd"},
	{"systemctl start ", "启动系统服务", "Systemd"},
	{"systemctl stop ", "停止系统服务", "Systemd"},
	{"systemctl restart ", "重启系统服务", "Systemd"},
	{"systemctl reload ", "平滑重载服务配置", "Systemd"},
	{"systemctl enable --now ", "设置开机自启并立即启动", "Systemd"},
	{"systemctl disable ", "取消服务开机自启", "Systemd"},
	{"systemctl daemon-reload", "重新加载 systemd unit 配置", "Systemd"},
	{"systemctl list-units --type=service --state=running", "列出所有运行中的服务", "Systemd"},
	{"journalctl -u ", "查看指定服务的日志", "Systemd"},
	{"journalctl -xe --no-pager", "查看最新系统错误日志", "Systemd"},
	{"journalctl --since \"1 hour ago\"", "查看最近 1 小时的系统日志", "Systemd"},
	{"journalctl --vacuum-size=100M", "清理日志至 100MB 以内", "Systemd"},

	{"git status", "查看当前仓库工作区状态", "Git"},
	{"git pull --rebase", "拉取远程更新并变基", "Git"},
	{"git push", "推送本地提交至远程仓库", "Git"},
	{"git log --oneline -20", "简洁查看最近 20 条提交记录", "Git"},
	{"git diff", "查看未暂存的代码变更", "Git"},
	{"git branch -a", "列出所有本地与远程分支", "Git"},
	{"git checkout ", "切换分支或恢复文件", "Git"},
	{"git stash", "暂存当前未提交的修改", "Git"},

	{"nginx -t", "测试 Nginx 配置文件语法", "Nginx"},
	{"nginx -s reload", "平滑重新加载 Nginx 配置", "Nginx"},
	{"certbot certificates", "查看已签发的 SSL 证书", "SSL"},
	{"certbot renew --dry-run", "模拟测试 SSL 证书续期", "SSL"},

	{"kubectl get pods -A", "查看所有命名空间的 Pod", "K8s"},
	{"kubectl get svc -A", "查看所有 Service", "K8s"},
	{"kubectl logs -f --tail=100 ", "实时跟踪 Pod 日志", "K8s"},
	{"kubectl describe pod ", "查看 Pod 详情与事件", "K8s"},
	{"kubectl apply -f ", "应用 YAML 资源清单", "K8s"},

	{"ss -tulpn", "查看所有 TCP/UDP 监听端口与进程", "网络"},
	{"ss -antp", "查看所有 TCP 连接状态", "网络"},
	{"ip a", "查看所有网卡与 IP 地址", "网络"},
	{"ip route", "查看系统路由表", "网络"},
	{"lsof -i :", "查看指定端口被哪个进程占用", "网络"},
	{"curl -I ", "仅请求并查看 HTTP 响应头", "网络"},
	{"curl -fsSL ", "静默拉取 URL 内容", "网络"},
	{"curl -s ifconfig.me", "查看当前服务器公网出口 IP", "网络"},
	{"ping -c 4 ", "测试目标连通性", "网络"},
	{"mtr -rwzc 10 ", "诊断路由跳数与丢包", "网络"},
	{"nc -zv ", "测试目标 TCP 端口连通性", "网络"},
	{"ufw status numbered", "查看 UFW 防火墙规则", "防火墙"},
	{"ufw allow ", "放行指定端口或服务", "防火墙"},

	{"ls -la", "列出当前目录所有文件（含隐藏）", "文件"},
	{"df -h", "查看各分区磁盘使用率", "系统"},
	{"du -sh *", "统计当前目录各文件大小", "系统"},
	{"free -h", "查看内存与 Swap 占用", "系统"},
	{"top", "实时监控系统进程与负载", "系统"},
	{"htop", "交互式进程与资源监视器", "系统"},
	{"ps aux | grep ", "按关键字检索运行中的进程", "系统"},
	{"tail -f -n 100 ", "实时追踪文件末尾 100 行", "文件"},
	{"grep -rn ", "递归搜索目录文本内容", "文件"},
	{"find . -name ", "按名称查找文件", "文件"},
	{"chmod +x ", "赋予文件可执行权限", "文件"},
	{"chown -R ", "递归修改文件所有者", "文件"},
	{"tar -czvf ", "打包并用 gzip 压缩", "压缩"},
	{"tar -xzvf ", "解压 .tar.gz 归档文件", "压缩"},
	{"rsync -avzP ", "增量同步文件并显示进度", "传输"},
	{"tmux new -s ", "创建新的命名 tmux 会话", "终端"},
	{"tmux attach -t ", "重新接入 tmux 会话", "终端"},
	{"tmux ls", "列出所有活跃 tmux 会话", "终端"},
	{"crontab -e", "编辑当前用户定时任务", "系统"},
	{"pm2 list", "查看 PM2 托管的进程列表", "Node/PM2"},
	{"pm2 logs --lines 100", "查看 PM2 实时日志", "Node/PM2"},
}

func ComputeInsertSuffix(currentInput, fullCommand string) string {
	trimmedInput := strings.TrimLeft(currentInput, " \t")
	if trimmedInput == "" {
		return fullCommand
	}
	if strings.HasPrefix(fullCommand, trimmedInput) {
		return fullCommand[len(trimmedInput):]
	}
	if len(fullCommand) >= len(trimmedInput) && strings.EqualFold(fullCommand[:len(trimmedInput)], trimmedInput) {
		return fullCommand[len(trimmedInput):]
	}
	return fullCommand
}

func Suggest(
	input string,
	shortcuts []models.CommandShortcut,
	history []models.CommandRecord,
	includeShortcuts bool,
	includeHistory bool,
	limit int,
) []models.CompletionItem {
	query := strings.TrimLeft(input, " \t")
	if query == "" {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}

	lowerQ := strings.ToLower(query)
	seen := make(map[string]bool)
	results := make([]models.CompletionItem, 0, limit*2)

	addCandidate := func(cmd, desc, cat, source string, baseScore int) {
		cmdTrim := strings.TrimSpace(cmd)
		if cmdTrim == "" || strings.EqualFold(cmdTrim, strings.TrimSpace(query)) {
			return
		}
		key := strings.ToLower(cmdTrim)
		if seen[key] {
			return
		}

		lowerCmd := strings.ToLower(cmd)
		score := 0
		if strings.HasPrefix(lowerCmd, lowerQ) {
			score = baseScore + 1000 - min(len(cmd), 120)
		} else if idx := strings.Index(lowerCmd, lowerQ); idx > 0 && len(lowerQ) >= 2 {
			score = baseScore + 300 - idx
		}
		if score <= 0 {
			return
		}

		seen[key] = true
		results = append(results, models.CompletionItem{
			Command:     cmd,
			Description: desc,
			Category:    cat,
			Source:      source,
			InsertText:  ComputeInsertSuffix(input, cmd),
			Score:       score,
		})
	}

	if includeShortcuts {
		for _, sc := range shortcuts {
			if strings.Contains(sc.Command, "\n") {
				continue
			}
			desc := sc.Name
			if sc.Description != "" {
				desc = sc.Name + " · " + sc.Description
			}
			bonus := 160
			if sc.Pinned {
				bonus += 40
			}
			addCandidate(sc.Command, desc, sc.Category, "shortcut", bonus)
		}
	}

	for _, b := range builtinCatalogCore {
		addCandidate(b.Command, b.Description, b.Category, "builtin", 120)
	}
	for _, b := range builtinCatalogOps {
		addCandidate(b.Command, b.Description, b.Category, "builtin", 120)
	}

	if includeHistory {
		for _, h := range history {
			if strings.Contains(h.Command, "\n") {
				continue
			}
			desc := "历史记录"
			if h.ServerName != "" {
				desc = "历史 · " + h.ServerName
			}
			addCandidate(h.Command, desc, "历史", "history", 90)
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Command < results[j].Command
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}
