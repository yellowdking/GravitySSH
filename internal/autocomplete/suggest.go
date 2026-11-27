package autocomplete

import (
	"path"
	"sort"
	"strings"

	"github.com/yellowdking/GravitySSH/internal/models"
)

type SuggestRequest struct {
	Input, CWD, HomeDir, ServerID string
	Parsed                        *ParsedLine
	CWDEntries, TargetEntries     []models.SFTPFileEntry
	History                       []models.CommandRecord
	IncludeHistory                bool
	Limit                         int
}

var rootDescByCmd = func() map[string]string {
	m := make(map[string]string, len(RootCommandList))
	for _, s := range RootCommandList {
		m[strings.ToLower(s.Token)] = s.Desc
	}
	return m
}()

func mkItem(cmd, desc, cat, src, ins string, score int) models.CompletionItem {
	return models.CompletionItem{Command: cmd, Description: desc, Category: cat, Source: src, InsertText: ins, Score: score}
}

func sortTopItems(out []models.CompletionItem, limit int) []models.CompletionItem {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Command < out[j].Command
	})
	if len(out) > limit {
		return out[:limit]
	}
	return out
}

func specInsert(prefix string, s SubcmdSpec) string {
	if s.AppendSpace {
		return prefix + s.Token + " "
	}
	return prefix + s.Token
}

func SuggestWithContext(req SuggestRequest) []models.CompletionItem {
	if strings.TrimSpace(req.Input) == "" {
		return nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	var p ParsedLine
	if req.Parsed != nil {
		p = *req.Parsed
	} else {
		p = ParseLine(req.Input, req.CWD, req.HomeDir)
	}
	if len(p.Tokens) == 0 && strings.TrimSpace(p.ActiveSeg) == "" {
		return nil
	}
	sig, cur := ScanFolderSignals(req.CWDEntries, req.CWD), p.CurTok.Unquoted
	isFlag := len(p.Effective) >= 2 && !p.EndsWithSpace && strings.HasPrefix(cur, "-") && !strings.Contains(cur, "/")
	isFirst := len(p.Effective) == 1 && !p.EndsWithSpace

	byKey := make(map[string]models.CompletionItem, limit*4)
	add := func(it models.CompletionItem) {
		var k string
		if it.Source == "file" || it.Source == "dir" {
			k = it.InsertText
		} else {
			k = strings.ToLower(normWS(it.InsertText))
		}
		if k != "" {
			if prev, ok := byKey[k]; !ok || it.Score > prev.Score {
				byKey[k] = it
			}
		}
	}

	// Stage 1: Files
	fileHits := 0
	if p.PathActive && !isFlag {
		cwd := req.CWD
		if cwd == "" {
			cwd = "/"
		}
		entries := req.TargetEntries
		if len(entries) == 0 && path.Clean(p.TargetDir) == path.Clean(cwd) {
			entries = req.CWDEntries
		}
		for _, it := range suggestFileCandidates(p, req.Input, entries) {
			add(it)
			fileHits++
		}
	}

	// Stage 2: Flags
	if isFlag {
		for i, s := range MatchFlags(p.Effective, cur) {
			add(mkItem(s.Token, s.Desc, "参数", "flag", specInsert(p.TokPrefix, s), min(660, max(560, 640-i*6))))
		}
	}

	// Stage 3: Subcommands
	if !isFlag && fileHits == 0 && !strings.Contains(cur, "/") && (len(p.Effective) >= 2 || (len(p.Effective) == 1 && p.EndsWithSpace)) {
		cmdPath := NormalizedCmdPath(p.Effective, p.EndsWithSpace)
		cat := rootDescByCmd[strings.ToLower(p.Effective[0])]
		if cat == "" {
			cat = "命令"
		}
		for i, s := range matchSubcommandsForPath(cmdPath, p.Effective, p.EndsWithSpace) {
			sc := min(680, max(480, 560-i*5+FolderSubcmdBoost(cmdPath, s.Token, sig)))
			add(mkItem(s.Token, s.Desc, cat, "subcmd", specInsert(p.TokPrefix, s), sc))
		}
	}

	// Stage 4: History + Folder-Context 1st-word + Stage-1 root builtins
	if req.IncludeHistory && len(req.History) > 0 {
		for _, it := range rankHistoryCandidates(p, req.Input, req.ServerID, req.History, limit) {
			add(it)
		}
	}
	if isFirst && !p.PathActive && cur != "" {
		loCur := strings.ToLower(cur)
		for i, s := range FolderRootSuggestions(cur, sig) {
			add(mkItem(s.Token, s.Desc, "目录上下文", "builtin", specInsert(p.TokPrefix, s), max(440, 470-i*10)))
		}
		rank := 0
		for _, s := range RootCommandList {
			loTok := strings.ToLower(s.Token)
			if !strings.HasPrefix(loTok, loCur) || (loTok == loCur && !s.AppendSpace) {
				continue
			}
			sc := 310 - rank*4 + rootFolderBonus(loTok, sig)
			if loTok == loCur {
				sc += 15
			}
			add(mkItem(s.Token, s.Desc, "命令", "builtin", specInsert(p.TokPrefix, s), min(340, max(240, sc))))
			rank++
		}
	}

	out := make([]models.CompletionItem, 0, len(byKey))
	for _, it := range byKey {
		out = append(out, it)
	}
	return sortTopItems(out, limit)
}

func rootFolderBonus(c string, s FolderSignals) int {
	if (c == "docker" && s&(HasCompose|HasDockerfile) != 0) || (c == "git" && s&HasGit != 0) ||
		((c == "npm" || c == "node" || c == "pm2") && s&HasNode != 0) ||
		(c == "make" && s&HasMake != 0) || (c == "go" && s&HasGo != 0) || (c == "nginx" && s&HasNginx != 0) {
		return 30
	}
	return 0
}