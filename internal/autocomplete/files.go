package autocomplete

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yellowdking/GravitySSH/internal/models"
)

// ExtractPathContextWithHome resolves active path completion including ~/ home expansion.
func ExtractPathContextWithHome(input, cwd, homeDir string) (targetDir string, active bool) {
	p := ParseLine(input, cwd, homeDir)
	if !p.PathActive {
		return "", false
	}
	return p.TargetDir, true
}

func suggestFileCandidates(p ParsedLine, input string, entries []models.SFTPFileEntry) []models.CompletionItem {
	if !p.PathActive || len(entries) == 0 {
		return nil
	}

	cmd := ""
	if len(p.Effective) > 0 {
		cmd = strings.ToLower(p.Effective[0])
	}
	isFirstTok := len(p.Effective) == 0 || (len(p.Effective) == 1 && !p.EndsWithSpace)
	lowerPref := strings.ToLower(p.NamePrefix)
	prefRunes := utf8.RuneCountInString(p.NamePrefix)
	prevTok := p.PrevTok
	showHidden := strings.HasPrefix(p.NamePrefix, ".") || prevTok == "--env-file"
	arcOrFileFlag := prevTok == "-f" || prevTok == "--file" || archiveCommands[cmd]

	out := make([]models.CompletionItem, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name
		if name == "" || name == "." || name == ".." {
			continue
		}
		if strings.HasPrefix(name, ".") && !showHidden {
			continue
		}
		if p.DirsOnly && !entry.IsDir {
			continue
		}

		lowerName := strings.ToLower(name)
		if lowerPref != "" && !strings.HasPrefix(lowerName, lowerPref) {
			continue
		}

		matchBonus := 80
		if lowerPref != "" {
			excess := max(0, utf8.RuneCountInString(name)-prefRunes)
			matchBonus = 100 - min(excess, 40)
		}

		dirBonus := 0
		if entry.IsDir {
			switch {
			case p.DirsOnly:
				dirBonus = 120
			case cmd == "chmod":
				dirBonus = 0
			case isFirstTok || arcOrFileFlag:
				dirBonus = 20
			default:
				dirBonus = 30
			}
		}

		extDelta := FileExtDelta(cmd, p.PrevTok, entry, isFirstTok)
		score := min(960, max(550, 700+dirBonus+matchBonus+extDelta))

		displayName := name
		if entry.IsDir {
			displayName += "/"
		}
		fullLine := p.TokPrefix + escapePathToken(p.TokenDir, name, entry.IsDir, p.CurTok.Quote, p.PreserveLeadingEnvVar)
		if fullLine == input || (!entry.IsDir && strings.TrimSpace(fullLine) == strings.TrimSpace(input)) {
			continue
		}

		source, cat, desc := "file", "文件", formatSizeDesc(entry.Size)
		if entry.IsDir {
			source, cat, desc = "dir", "目录", "当前目录文件夹"
		}

		out = append(out, models.CompletionItem{
			Command:     displayName,
			Description: desc,
			Category:    cat,
			Source:      source,
			InsertText:  fullLine,
			Score:       score,
		})
	}
	return out
}

func formatSizeDesc(size int64) string {
	switch {
	case size < 1024:
		return fmt.Sprintf("文件 · %d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("文件 · %.1f KB", float64(size)/1024)
	case size < 1024*1024*1024:
		return fmt.Sprintf("文件 · %.1f MB", float64(size)/(1024*1024))
	default:
		return fmt.Sprintf("文件 · %.2f GB", float64(size)/(1024*1024*1024))
	}
}
