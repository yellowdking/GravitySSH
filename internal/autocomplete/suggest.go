package autocomplete

import (
	"sort"
	"strings"

	"github.com/yellowdking/GravitySSH/internal/models"
)

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
	return SuggestWithFiles(input, "", nil, shortcuts, history, includeShortcuts, includeHistory, limit)
}

func SuggestWithFiles(
	input string,
	cwd string,
	dirEntries []models.SFTPFileEntry,
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

	// Live directory & filename candidates when typing path arguments or ./ / / paths.
	if len(dirEntries) > 0 {
		fileItems := suggestFileCandidates(input, query, cwd, dirEntries)
		for _, item := range fileItems {
			key := strings.ToLower(strings.TrimSpace(item.Command))
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, item)
		}
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
