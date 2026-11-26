package autocomplete

import "strings"

type SubcmdSpec struct {
	Token, Desc string
	AppendSpace bool
}

func parseTree(raw string) map[string][]SubcmdSpec {
	m := make(map[string][]SubcmdSpec, 32)
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			m[k] = parseSpecs(v)
		}
	}
	m["apt-get"], m["yum"] = m["apt"], m["dnf"]
	return m
}

var (
	FileCentricCommands = wordSet("cd ls ll la dir cat less more head tail nano vi vim nvim code rm cp mv ln touch mkdir rmdir chmod chown unzip tar gunzip 7z gzip zcat sh bash zsh source . python python3 node go grep stat file du wc scp rsync diff sed awk find tree pushd")
	DirOnlyCommands     = wordSet("cd rmdir pushd tree")
	SubcmdRoots         = wordSet("docker systemctl journalctl apt apt-get apk dnf yum git kubectl pm2 tmux nginx ufw firewall-cmd ss ip curl df ps kill")
	FileFlags           = wordSet("-f --file -c --config -i -o --env-file")

	RootCommandList = parseSpecs("docker :容器;systemctl :服务;journalctl :日志;git :Git;kubectl :K8s;apt :APT;apt-get :APT;apk :APK;dnf :DNF;yum :YUM;pm2 :PM2;tmux :终端;nginx :Nginx;ufw :防火墙;firewall-cmd :防火墙;ss :端口;ip :网络;curl :HTTP;tar :归档;rsync :同步;chmod :权限;chown :属主;find :查找;grep :搜索;ls :列表;cd :目录;df :磁盘;du :大小;ps :进程;cat :查看;tail :末尾;head :开头;less :分页;nano :编辑;vim :编辑;rm :删除;cp :复制;mv :移动;mkdir :目录;scp :复制;ssh :SSH;sudo :提权;free :内存;top:监控;htop:监控;kill :终止;lsof :端口;ping :连通;unzip :解压;python3 :Py3;node :Node;npm :NPM;go :Go;make :构建;certbot :证书;crontab :定时")

	subcmdTree = parseTree(`
docker=ps -a:全部容器;compose :编排;logs -f --tail 100 :日志;exec -it :终端;images:镜像;restart :重启;pull :拉取;stats:统计;rm -f :强删;inspect :详情;network ls:网络;volume ls:数据卷;system prune -af:清理;build -t :构建
docker compose=up -d:后台启动;down:停止;pull:拉取;logs -f --tail 100:日志;restart:重启;ps:状态;build:构建;exec :进容器;up -d --force-recreate:强制重建
docker system=prune -af:清理;df:磁盘
docker network=ls:列表;inspect :详情
docker volume=ls:列表;prune:清理
systemctl=status :状态;restart :重启;reload :重载;start :启动;stop :停止;enable --now :自启;disable :禁启;daemon-reload:重载;list-units --failed:失败
journalctl=-u :服务日志;-xe --no-pager:错误;-f:跟踪;--since "1 hour ago":近1小时
apt=update:刷新;upgrade -y:升级;update && apt upgrade -y:刷新升级;install -y :安装;remove -y :卸载;autoremove -y:清依赖;search :搜索
apk=update:索引;upgrade:升级;add --no-cache :安装;del :卸载;search :搜索;info:已装
dnf=update -y:更新;install -y :安装;remove -y :卸载;search :搜索
git=status:状态;pull --rebase:变基拉取;push:推送;log --oneline -20:日志;diff:变更;checkout :切分支;branch -a:分支;add .:暂存;commit -m "":提交;stash:储藏
kubectl=get pods -A:Pod;get svc -A:Svc;get nodes:节点;logs -f --tail=100 :日志;describe pod :详情;apply -f :应用;exec -it :终端
kubectl get=pods -A:Pod;svc -A:Svc;deploy -A:Deploy;nodes:节点
pm2=list:列表;logs --lines 100:日志;restart :重启;reload :重载;stop :停止;monit:监控;save:保存
tmux=ls:列表;new -s :新建;attach -t :接入;kill-session -t :销毁
nginx=-t:校验;-s reload:重载
ufw=status numbered:规则;allow :放行;delete :删除;deny :拒绝;reload:重载;enable:启用
firewall-cmd=--list-all:规则;--add-port=:端口;--permanent :持久;--reload:重载
ss=-tulpn:监听;-antp:TCP;-s:统计
ip=a:IP;route:路由;link:链路;neigh:邻居
curl=-fsSL :拉取;-I :响应头;-k :免TLS;-X POST :POST;-s ifconfig.me:公网IP
tar=-xzvf :解压;-czvf :压缩;-tvf :预览
rsync=-avzP :同步;--delete :删多余
chmod=+x :执行;-R :递归;755 :755;644 :644
find=. -name :按名;. -type f :文件;. -mtime -1:24h内
grep=-rn :递归;-i :忽略大小写;-E :正则
ls=-la:全部;-lh:大小;-lt:时间
df=-h:磁盘;-Th:类型
du=-sh *:各项;-h --max-depth=1:一级
ps=aux:全部;aux | grep :筛选;-ef:完整`)
)

func NormalizedCmdPath(eff []string, endsWithSpace bool) string {
	if len(eff) == 0 || (len(eff) == 1 && !endsWithSpace) {
		return ""
	}
	pre := eff
	if !endsWithSpace {
		pre = eff[:len(eff)-1]
	}
	parts := make([]string, 0, len(pre))
	for i := 0; i < len(pre); i++ {
		low := strings.ToLower(pre[i])
		if i > 0 && strings.HasPrefix(low, "-") {
			if FileFlags[low] && i+1 < len(pre) {
				i++
			}
			continue
		}
		parts = append(parts, low)
	}
	return strings.Join(parts, " ")
}

func MatchSubcommands(eff []string, endsWithSpace bool) []SubcmdSpec {
	if len(eff) == 0 || (len(eff) == 1 && !endsWithSpace) {
		return nil
	}
	pre, cur := eff, ""
	if !endsWithSpace {
		pre, cur = eff[:len(eff)-1], strings.ToLower(eff[len(eff)-1])
	}
	if len(pre) > 1 && FileFlags[strings.ToLower(pre[len(pre)-1])] {
		return nil
	}
	specs := subcmdTree[NormalizedCmdPath(eff, endsWithSpace)]
	if len(specs) == 0 {
		return nil
	}
	out := make([]SubcmdSpec, 0, len(specs))
	for _, s := range specs {
		low := strings.ToLower(s.Token)
		if cur != "" && (!strings.HasPrefix(low, cur) || (low == cur && (!s.AppendSpace || endsWithSpace))) {
			continue
		}
		out = append(out, s)
	}
	return out
}
