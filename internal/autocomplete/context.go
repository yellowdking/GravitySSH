package autocomplete

import (
	"strings"

	"github.com/yellowdking/GravitySSH/internal/models"
)

type FolderSignals uint32

const (
	HasCompose FolderSignals = 1 << iota
	HasDockerfile
	HasGit
	HasNode
	HasMake
	HasGo
	HasNginx
	HasSh
)

var (
	sigFiles = map[string]FolderSignals{
		"docker-compose.yml": HasCompose, "docker-compose.yaml": HasCompose, "compose.yml": HasCompose, "compose.yaml": HasCompose,
		"dockerfile": HasDockerfile, "package.json": HasNode, "makefile": HasMake, "gnumakefile": HasMake, "go.mod": HasGo, "nginx.conf": HasNginx,
	}
	arcE  = ".tar.gz .tar.xz .tar.bz2 .tgz .tar .zip .gz .bz2 .xz .7z"
	txtE  = ".yml .yaml .conf .cfg .ini .json .toml .env .log .txt .md .sh .py .go .js .ts .service .xml .sql"
	binE  = ".png .jpg .jpeg .gif .webp .zip .gz .tar .tgz .7z .bin .so .exe .pdf"
	edCmd = wordSet("nano vim vi nvim cat less more head tail grep sed awk")
	ctxRt = []struct {
		f FolderSignals
		s []SubcmdSpec
	}{
		{HasCompose, parseSpecs("docker compose up -d:启动;docker compose down:停止;docker compose logs -f --tail 100:日志;docker compose pull:拉取")},
		{HasDockerfile, parseSpecs("docker build -t :构建")},
		{HasGit, parseSpecs("git status:状态;git pull --rebase:拉取;git diff:变更;git log --oneline -20:提交")},
		{HasNode, parseSpecs("npm run :脚本;npm install:依赖;pm2 list:PM2")},
		{HasMake, parseSpecs("make :构建")},
		{HasGo, parseSpecs("go test ./...:测试;go build ./...:编译")},
		{HasNginx, parseSpecs("nginx -t:校验;nginx -s reload:重载")},
	}
	subBoost = map[string]int{"up -d": 90, "down": 80, "logs -f --tail 100": 75, "pull": 70, "status": 90, "pull --rebase": 85, "diff": 80, "log --oneline -20": 75, "-t": 95, "-s reload": 90}
	pathSig  = map[string]FolderSignals{"docker compose": HasCompose, "git": HasGit, "nginx": HasNginx}
)

func ScanFolderSignals(entries []models.SFTPFileEntry, cwd string) FolderSignals {
	var sig FolderSignals
	for _, e := range entries {
		lo := strings.ToLower(e.Name)
		if lo == ".git" {
			sig |= HasGit
		} else if !e.IsDir {
			if f := sigFiles[lo]; f != 0 {
				sig |= f
			} else if strings.HasSuffix(lo, ".sh") {
				sig |= HasSh
			}
		}
	}
	if strings.HasPrefix(strings.ToLower(cwd), "/etc/nginx") {
		sig |= HasNginx
	}
	return sig
}

func hasExt(lo, list string) bool {
	for _, x := range strings.Fields(list) {
		if strings.HasSuffix(lo, x) {
			return true
		}
	}
	return false
}

func FileExtDelta(cmd, prevTok string, entry models.SFTPFileEntry, isFirstToken bool) int {
	if entry.IsDir {
		return 0
	}
	lo, hp, hs, ct := strings.ToLower, strings.HasPrefix, strings.HasSuffix, strings.Contains
	name, c, p := lo(entry.Name), lo(cmd), lo(prevTok)
	switch {
	case isFirstToken:
		if hs(name, ".sh") {
			return 120
		}
		if hasExt(name, binE) {
			return -120
		}
		if ct(entry.Mode, "x") {
			return 80
		}
	case p == "--env-file":
		if hp(name, ".env") {
			return 140
		}
	case (p == "-f" || p == "--file") && (c == "docker" || c == "kubectl"):
		if hasExt(name, ".yml .yaml") {
			return 120
		}
	case (ct(" tar unzip gunzip 7z gzip zcat ", " "+c+" ") && hasExt(name, arcE)) ||
		(ct(" sh bash zsh source . ", " "+c+" ") && hasExt(name, ".sh .bash .zsh")) ||
		(hp(c, "python") && hs(name, ".py")) || (c == "node" && hasExt(name, ".js .mjs .ts")) ||
		(c == "go" && hs(name, ".go")) || (c == "nginx" && hs(name, ".conf")):
		return 120
	case edCmd[c]:
		if hp(name, ".env") || hasExt(name, txtE) {
			return 80
		}
		if hasExt(name, binE) {
			return -120
		}
	case c == "chmod":
		if hs(name, ".sh") {
			return 60
		}
		return 40
	}
	return 0
}

func FolderSubcmdBoost(cmdPath, token string, sig FolderSignals) int {
	if sig == 0 {
		return 0
	}
	p, tok := strings.ToLower(cmdPath), strings.ToLower(token)
	if p == "docker" {
		if sig&HasDockerfile != 0 && strings.HasPrefix(tok, "build") {
			return 110
		}
		if sig&HasCompose != 0 {
			if tok == "compose" {
				return 100
			}
			if strings.HasPrefix(tok, "compose ") {
				return 85
			}
		}
		return 0
	}
	if sig&pathSig[p] != 0 {
		return subBoost[tok]
	}
	return 0
}

func FolderRootSuggestions(prefix string, sig FolderSignals) []SubcmdSpec {
	lo := strings.ToLower(strings.TrimSpace(prefix))
	if len(lo) < 2 || sig == 0 {
		return nil
	}
	var out []SubcmdSpec
	for _, g := range ctxRt {
		if sig&g.f != 0 {
			for _, s := range g.s {
				if t := strings.ToLower(s.Token); strings.HasPrefix(t, lo) && (t != lo || s.AppendSpace) {
					out = append(out, s)
				}
			}
		}
	}
	return out
}
