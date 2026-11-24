package autocomplete

import (
	"fmt"
	"path"
	"strings"

	"github.com/yellowdking/GravitySSH/internal/models"
)

var fileCentricCommands = map[string]bool{
	"cd": true, "ls": true, "ll": true, "la": true, "dir": true,
	"cat": true, "less": true, "more": true, "head": true, "tail": true,
	"nano": true, "vi": true, "vim": true, "nvim": true, "code": true,
	"rm": true, "cp": true, "mv": true, "ln": true, "touch": true,
	"mkdir": true, "rmdir": true, "chmod": true, "chown": true,
	"unzip": true, "tar": true, "gunzip": true, "7z": true, "gzip": true,
	"sh": true, "bash": true, "zsh": true, "source": true,
	"python": true, "python3": true, "node": true, "go": true,
	"grep": true, "stat": true, "file": true, "du": true, "wc": true,
	"scp": true, "rsync": true, "diff": true, "sed": true, "awk": true,
	"find": true, "tree": true, "pushd": true,
}

var dirPrimaryCommands = map[string]bool{
	"cd": true, "pushd": true, "rmdir": true, "tree": true,
}

// ExtractPathContext checks whether input is completing a file/directory path
// and returns the remote directory to list.
func ExtractPathContext(input, cwd string) (targetDir string, active bool) {
	query := strings.TrimLeft(input, " \t")
	if query == "" {
		return "", false
	}

	_, token, ok := splitPathToken(query)
	if !ok {
		return "", false
	}
	if strings.HasPrefix(token, "-") {
		return "", false
	}

	baseDir := cwd
	if baseDir == "" {
		baseDir = "/"
	}

	if slash := strings.LastIndex(token, "/"); slash >= 0 {
		dirPart := token[:slash+1]
		switch {
		case strings.HasPrefix(dirPart, "/"):
			return path.Clean(dirPart), true
		case dirPart == "~/", strings.HasPrefix(dirPart, "~/"):
			return dirPart, true
		default:
			return path.Clean(path.Join(baseDir, dirPart)), true
		}
	}
	return baseDir, true
}

func splitPathToken(query string) (cmdPrefix, token string, ok bool) {
	if idx := strings.LastIndexAny(query, " \t"); idx >= 0 {
		return query[:idx+1], query[idx+1:], true
	}
	if strings.HasPrefix(query, "./") || strings.HasPrefix(query, "../") || strings.HasPrefix(query, "/") || strings.HasPrefix(query, "~/") {
		return "", query, true
	}
	return "", "", false
}

func suggestFileCandidates(input, query, _ string, dirEntries []models.SFTPFileEntry) []models.CompletionItem {
	cmdPrefix, token, ok := splitPathToken(query)
	if !ok || strings.HasPrefix(token, "-") {
		return nil
	}

	firstWord := ""
	if fields := strings.Fields(query); len(fields) > 0 && cmdPrefix != "" {
		firstWord = strings.ToLower(fields[0])
	}

	tokenDir := ""
	namePrefix := token
	if slash := strings.LastIndex(token, "/"); slash >= 0 {
		tokenDir = token[:slash+1]
		namePrefix = token[slash+1:]
	}

	isFileCmd := fileCentricCommands[firstWord]
	isDirPrimary := dirPrimaryCommands[firstWord]
	hasExplicitPath := tokenDir != ""

	// Avoid flooding non-file commands like "apt " or "docker " when no filename prefix is typed.
	if !isFileCmd && !hasExplicitPath && namePrefix == "" {
		return nil
	}

	lowerPrefix := strings.ToLower(namePrefix)
	showHidden := strings.HasPrefix(namePrefix, ".")
	out := make([]models.CompletionItem, 0, len(dirEntries))

	for _, entry := range dirEntries {
		name := entry.Name
		if name == "" || name == "." || name == ".." {
			continue
		}
		if strings.HasPrefix(name, ".") && !showHidden {
			continue
		}
		if firstWord == "cd" && !entry.IsDir {
			continue
		}

		lowerName := strings.ToLower(name)
		matchScore := 0
		if lowerPrefix == "" {
			matchScore = 1000 - min(len(name), 120)
		} else if strings.HasPrefix(lowerName, lowerPrefix) {
			matchScore = 1050 - min(len(name), 120)
		} else if len(lowerPrefix) >= 2 {
			if idx := strings.Index(lowerName, lowerPrefix); idx > 0 {
				matchScore = 420 - idx
			}
		}
		if matchScore <= 0 {
			continue
		}

		entryToken := tokenDir + name
		if entry.IsDir {
			entryToken += "/"
		}
		fullCmd := cmdPrefix + entryToken
		if strings.EqualFold(strings.TrimSpace(fullCmd), strings.TrimSpace(query)) {
			continue
		}

		baseScore := 110
		if isDirPrimary && entry.IsDir {
			baseScore = 320
		} else if isFileCmd || hasExplicitPath {
			baseScore = 260
			if entry.IsDir {
				baseScore += 20
			}
		}

		source := "file"
		cat := "文件"
		desc := formatSizeDesc(entry.Size)
		if entry.IsDir {
			source = "dir"
			cat = "目录"
			desc = "当前目录文件夹"
		}

		out = append(out, models.CompletionItem{
			Command:     fullCmd,
			Description: desc,
			Category:    cat,
			Source:      source,
			InsertText:  ComputeInsertSuffix(input, fullCmd),
			Score:       baseScore + matchScore,
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
