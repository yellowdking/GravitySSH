package main

import (
	"errors"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/autocomplete"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/vault"
)

func (a *App) ListCommandHistory(serverID string, limit int) ([]models.CommandRecord, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.store.ListHistory(serverID, limit), nil
}

func (a *App) DeleteHistoryEntry(id string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return a.store.DeleteHistoryEntry(id)
}

func (a *App) ClearHistory(serverID string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return a.store.ClearHistory(serverID)
}

func (a *App) ListShortcuts() ([]models.CommandShortcut, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.store.ListShortcuts(), nil
}

func (a *App) ListBuiltinTemplates() []models.BuiltinTemplate {
	if err := a.ensureReady(); err != nil {
		return vault.BuiltinOpsTemplates()
	}
	return a.store.ListTemplates()
}

func (a *App) ListOpsTemplates() ([]models.BuiltinTemplate, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.store.ListTemplates(), nil
}

func (a *App) SaveOpsTemplate(tpl models.BuiltinTemplate) (models.BuiltinTemplate, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.BuiltinTemplate{}, err
	}
	return a.store.SaveTemplate(tpl)
}

func (a *App) DeleteOpsTemplate(id string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return a.store.DeleteTemplate(id)
}

func (a *App) LoadPresetTemplates() (models.PresetLoadResult, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.PresetLoadResult{}, err
	}
	return a.store.LoadPresetTemplates()
}

func (a *App) SaveShortcut(sc models.CommandShortcut) (models.CommandShortcut, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.CommandShortcut{}, err
	}
	return a.store.SaveShortcut(sc)
}

func (a *App) DeleteShortcut(id string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return a.store.DeleteShortcut(id)
}

// RunShortcut sends single-line or multi-line command scripts to the terminal.
// For multi-line execution, lines are paced at 35ms intervals to prevent remote TTY buffer overflow.
func (a *App) RunShortcut(sessionID, shortcutID string, forceExecute bool) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	var target *models.CommandShortcut
	for _, sc := range a.store.ListShortcuts() {
		if sc.ID == shortcutID {
			cp := sc
			target = &cp
			break
		}
	}
	if target == nil {
		return errors.New("快捷命令不存在")
	}
	a.store.IncrementShortcutUse(shortcutID)

	normalized := strings.ReplaceAll(target.Command, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	execute := forceExecute || target.AutoExecute
	if !execute {
		return a.sshMgr.WriteInput(sessionID, normalized)
	}

	rawLines := strings.Split(normalized, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil
	}
	if len(lines) == 1 {
		return a.sshMgr.WriteInput(sessionID, lines[0]+"\r")
	}

	// Send first line synchronously so immediate session/stdin errors surface right away.
	if err := a.sshMgr.WriteInput(sessionID, lines[0]+"\r"); err != nil {
		return err
	}
	rest := lines[1:]
	go func() {
		for _, line := range rest {
			time.Sleep(35 * time.Millisecond)
			if err := a.sshMgr.WriteInput(sessionID, line+"\r"); err != nil {
				return
			}
		}
	}()
	return nil
}

func (a *App) SuggestCommands(input string, serverID string) ([]models.CompletionItem, error) {
	return a.SuggestCommandsForSession(input, serverID, "")
}

func (a *App) SuggestCommandsForSession(input string, serverID string, sessionID string) ([]models.CompletionItem, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	settings := a.store.GetSettings()
	if !settings.AutocompleteEnabled {
		return []models.CompletionItem{}, nil
	}

	var (
		cwd        string
		dirEntries []models.SFTPFileEntry
	)
	if sessionID != "" {
		cwd = a.sshMgr.GetSessionCWD(sessionID)
		if targetDir, active := autocomplete.ExtractPathContext(input, cwd); active {
			dirEntries = a.sshMgr.CachedDirEntries(sessionID, targetDir)
		}
	}

	shortcuts := a.store.ListShortcuts()
	history := a.store.ListHistory(serverID, 150)
	if len(history) < 30 && serverID != "" {
		history = append(history, a.store.ListHistory("", 150)...)
	}

	items := autocomplete.SuggestWithFiles(
		input,
		cwd,
		dirEntries,
		shortcuts,
		history,
		settings.AutocompleteFromShortcut,
		settings.AutocompleteFromHistory,
		8,
	)
	if items == nil {
		return []models.CompletionItem{}, nil
	}
	return items, nil
}

func (a *App) GetSettings() (models.AppSettings, error) {
	if err := a.ensureReady(); err != nil {
		return models.AppSettings{}, err
	}
	return a.store.GetSettings(), nil
}

func (a *App) SaveSettings(settings models.AppSettings) (models.AppSettings, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.AppSettings{}, err
	}
	return a.store.SaveSettings(settings)
}
