package autocomplete

type builtinEntry struct {
	Command     string
	Description string
	Category    string
}

var builtinCatalogCore = []builtinEntry{
	{"apt update", "刷新软件源索引", "APT"},
	{"apt upgrade -y", "升级所有已安装的软件包", "APT"},
	{"apt update && apt upgrade -y", "刷新索引并自动升级系统", "APT"},
	{"apt install -y ", "安装指定软件包", "APT"},
	{"apt remove -y ", "卸载指定软件包", "APT"},
	{"apt autoremove -y", "清理不再需要的依赖包", "APT"},
	{"apt search ", "搜索软件仓库中的包", "APT"},
	{"apt list --installed", "列出已安装的所有软件包", "APT"},
	{"apt list --upgradable", "列出可升级的软件包", "APT"},
	{"apt-cache policy ", "查看软件包版本与来源优先级", "APT"},
	{"apt clean && apt autoclean", "清理本地下载的软件包缓存", "APT"},

	{"apk update", "更新 Alpine 软件源索引", "APK"},
	{"apk upgrade", "升级所有已安装的 Alpine 包", "APK"},
	{"apk add --no-cache ", "无缓存安装指定软件包", "APK"},
	{"apk del ", "卸载指定软件包", "APK"},
	{"apk search ", "搜索 Alpine 软件包", "APK"},
	{"apk info", "列出所有已安装软件包", "APK"},
	{"apk info -vv", "查看已安装包及详细描述", "APK"},

	{"dnf update -y", "更新所有软件包", "DNF/YUM"},
	{"dnf install -y ", "安装指定软件包", "DNF/YUM"},
	{"dnf remove -y ", "卸载指定软件包", "DNF/YUM"},
	{"dnf search ", "搜索软件包", "DNF/YUM"},
	{"yum update -y", "更新所有软件包", "DNF/YUM"},
	{"yum install -y ", "安装指定软件包", "DNF/YUM"},

	{"docker ps", "查看运行中的容器", "Docker"},
	{"docker ps -a", "查看所有容器（含已停止）", "Docker"},
	{"docker images", "列出本地所有镜像", "Docker"},
	{"docker logs -f --tail 100 ", "实时跟踪容器最近 100 行日志", "Docker"},
	{"docker exec -it ", "进入运行中容器的交互终端", "Docker"},
	{"docker restart ", "重启指定容器", "Docker"},
	{"docker stop ", "停止指定容器", "Docker"},
	{"docker rm -f ", "强制删除指定容器", "Docker"},
	{"docker rmi ", "删除指定镜像", "Docker"},
	{"docker pull ", "拉取远程镜像", "Docker"},
	{"docker stats --no-stream", "查看容器 CPU/内存实时占用快照", "Docker"},
	{"docker inspect ", "查看容器或镜像的详细 JSON 配置", "Docker"},
	{"docker network ls", "列出所有 Docker 网络", "Docker"},
	{"docker volume ls", "列出所有数据卷", "Docker"},
	{"docker system df", "查看 Docker 磁盘空间占用", "Docker"},
	{"docker system prune -af", "清理未使用的镜像/容器/缓存", "Docker"},
	{"docker compose up -d", "后台启动 Compose 服务编排", "Docker"},
	{"docker compose down", "停止并移除 Compose 容器与网络", "Docker"},
	{"docker compose logs -f --tail 100", "实时跟踪 Compose 服务日志", "Docker"},
	{"docker compose ps", "查看当前 Compose 服务状态", "Docker"},
	{"docker compose pull && docker compose up -d", "拉取最新镜像并滚动更新服务", "Docker"},
	{"docker compose restart", "重启 Compose 全部服务", "Docker"},
}
