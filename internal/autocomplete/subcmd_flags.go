package autocomplete

import "strings"

func wordSet(s string) map[string]bool {
	f := strings.Fields(s)
	m := make(map[string]bool, len(f))
	for _, w := range f {
		m[w] = true
	}
	return m
}

func parseSpecs(raw string) []SubcmdSpec {
	parts := strings.Split(raw, ";")
	out := make([]SubcmdSpec, 0, len(parts))
	for _, item := range parts {
		if tok, desc, ok := strings.Cut(item, ":"); ok {
			out = append(out, SubcmdSpec{strings.TrimRight(tok, " "), desc, strings.HasSuffix(tok, " ")})
		}
	}
	return out
}

var (
	dockerLogsFlags = parseSpecs("-f:实时跟踪;--tail 100:最后100行;--since 10m:近10分钟;-t:时间戳")

	subcmdFlags = map[string][]SubcmdSpec{
		"ls":                  parseSpecs("-la:全部含隐藏;-lh:人性化大小;-lt:时间倒序;-ltr:时间正序;-a:含隐藏文件"),
		"df":                  parseSpecs("-h:人性化容量;-Th:含文件类型"),
		"du":                  parseSpecs("-sh:汇总大小;-h --max-depth=1:一级目录"),
		"ps":                  parseSpecs("-ef:完整进程;-aux:BSD进程"),
		"free":                parseSpecs("-h:人类可读;-m:MB显示;-g:GB显示;-s 2:每2秒刷新;-w:宽格式"),
		"docker":              parseSpecs("--help:查看帮助;-v:查看版本"),
		"docker ps":           parseSpecs("-a:含已停止;--format :输出模板;-q:仅容器ID;--filter :条件筛选"),
		"docker logs":         dockerLogsFlags,
		"docker compose logs": dockerLogsFlags,
		"docker compose":      parseSpecs("-f :配置文件;--env-file :环境变量;-p :项目名称"),
		"docker compose up":   parseSpecs("-d:后台运行;--build:重建镜像;--force-recreate:强制重建;--remove-orphans:清理孤儿"),
		"docker compose down": parseSpecs("-v:删除命名卷;--remove-orphans:清理孤儿"),
		"curl":                parseSpecs("-fsSL :静默跟随;-I :仅响应头;-k :忽略TLS;-X POST :POST请求;-H :请求头;-d :请求体;-o :输出文件;-v:详细日志"),
		"tar":                 parseSpecs("-xzvf :解压tar.gz;-czvf :打包压缩;-tvf :列出归档;-C :解压目录"),
		"grep":                parseSpecs("-rn :递归行号;-i :忽略大小写;-E :扩展正则;-v :反向排除;-C 3 :前后3行;--include=:文件名模式"),
		"find":                parseSpecs("-name :按名称;-type f:普通文件;-type d:目录;-maxdepth :最大深度;-mtime :修改天数"),
		"journalctl":          parseSpecs("-u :指定服务;-xe:最新错误;-f:实时跟踪;--since :起始时间;--no-pager:不分页;-n 100:近100行"),
		"systemctl":           parseSpecs("--now:立即生效;--failed:失败单元;--type=service:仅服务;--no-pager:不分页"),
		"rsync":               parseSpecs("-avzP :归档进度;--delete :删除多余;--exclude=:排除路径;--dry-run:模拟演练"),
		"chmod":               parseSpecs("-R :递归修改"),
		"kubectl":             parseSpecs("-A:全部命名空间;-n :指定命名空间;-o wide:宽表输出;-f :清单文件"),
		"ss":                  parseSpecs("-tulpn:监听端口;-antp:全部TCP;-s:连接统计"),
		"nginx":               parseSpecs("-t:校验语法;-s reload:平滑重载"),
		"firewall-cmd":        parseSpecs("--list-all:全部规则;--permanent :持久化;--add-port=:开放端口;--reload:重载防火墙"),
	}
)

func MatchFlags(eff []string, curTok string) []SubcmdSpec {
	if len(eff) == 0 || !strings.HasPrefix(curTok, "-") || strings.Contains(curTok, "/") {
		return nil
	}
	prior := eff
	if len(prior) > 1 && prior[len(prior)-1] == curTok {
		prior = prior[:len(prior)-1]
	}
	if len(prior) == 0 {
		return nil
	}

	used := make(map[string]bool, len(prior))
	nonFlags := make([]string, 0, len(prior))
	nonFlags = append(nonFlags, strings.ToLower(prior[0]))
	for i := 1; i < len(prior); i++ {
		t := prior[i]
		if strings.HasPrefix(t, "-") {
			used[t] = true
			if eq := strings.IndexByte(t, '='); eq > 0 {
				used[t[:eq+1]], used[t[:eq]] = true, true
			} else if FileFlags[strings.ToLower(t)] && i+1 < len(prior) {
				i++
			}
		} else {
			nonFlags = append(nonFlags, strings.ToLower(t))
		}
	}

	var specs []SubcmdSpec
	for n := len(nonFlags); n >= 1; n-- {
		if list, ok := subcmdFlags[strings.Join(nonFlags[:n], " ")]; ok {
			specs = list
			break
		}
	}
	if len(specs) == 0 {
		return nil
	}

	dbl := strings.HasPrefix(curTok, "--")
	out := make([]SubcmdSpec, 0, len(specs))
	for _, s := range specs {
		if (dbl && !strings.HasPrefix(s.Token, "--")) || !strings.HasPrefix(s.Token, curTok) {
			continue
		}
		head := s.Token
		if sp := strings.IndexByte(head, ' '); sp > 0 {
			head = head[:sp]
		}
		if used[head] || used[s.Token] || (s.Token == curTok && !s.AppendSpace) {
			continue
		}
		out = append(out, s)
	}
	return out
}
