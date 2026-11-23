package main

import (
	"errors"
	"path"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yellowdking/GravitySSH/internal/autocomplete"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func (a *App) ConnectServer(serverID string, cols, rows int) (models.SessionInfo, error) {
	if err := a.ensureReady(); err != nil {
		return models.SessionInfo{}, err
	}
	return a.sshMgr.Connect(a.ctx, serverID, cols, rows)
}

func (a *App) DisconnectSession(sessionID string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	a.sshMgr.Disconnect(sessionID)
	return nil
}

func (a *App) ListSessions() ([]models.SessionInfo, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.sshMgr.ListSessions(), nil
}

func (a *App) WriteTerminal(sessionID, data string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.sshMgr.WriteInput(sessionID, data)
}

func (a *App) ResizeTerminal(sessionID string, cols, rows int) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.sshMgr.Resize(sessionID, cols, rows)
}

func (a *App) GetCurrentInputLine(sessionID string) (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	return a.sshMgr.CurrentInputLine(sessionID), nil
}

func (a *App) SFTPList(sessionID, remotePath string) (models.SFTPListResult, error) {
	if err := a.ensureReady(); err != nil {
		return models.SFTPListResult{}, err
	}
	return a.sshMgr.SFTPList(sessionID, remotePath)
}

func (a *App) SFTPMkdir(sessionID, remotePath string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.sshMgr.SFTPMkdir(sessionID, remotePath)
}

func (a *App) SFTPRename(sessionID, oldPath, newPath string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.sshMgr.SFTPRename(sessionID, oldPath, newPath)
}

func (a *App) SFTPDelete(sessionID, remotePath string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.sshMgr.SFTPDelete(sessionID, remotePath)
}

func (a *App) SFTPUploadDialog(sessionID, remoteDir string) (int, error) {
	if err := a.ensureReady(); err != nil {
		return 0, err
	}
	files, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择要上传到服务器的文件",
	})
	if err != nil || len(files) == 0 {
		return 0, err
	}
	return a.SFTPUploadFiles(sessionID, files, remoteDir)
}

func (a *App) SFTPUploadFiles(sessionID string, localPaths []string, remoteDir string) (int, error) {
	if err := a.ensureReady(); err != nil {
		return 0, err
	}
	uploaded := 0
	for _, localPath := range localPaths {
		if strings.TrimSpace(localPath) == "" {
			continue
		}
		if err := a.sshMgr.SFTPUpload(sessionID, localPath, remoteDir); err != nil {
			return uploaded, err
		}
		uploaded++
	}
	return uploaded, nil
}

func (a *App) SFTPDownloadDialog(sessionID, remotePath string) (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "保存下载文件",
		DefaultFilename: path.Base(remotePath),
	})
	if err != nil || savePath == "" {
		return "", err
	}
	if err := a.sshMgr.SFTPDownload(sessionID, remotePath, savePath); err != nil {
		return "", err
	}
	return savePath, nil
}

func (a *App) ListCommandHistory(serverID string, limit int) ([]models.CommandRecord, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.store.ListHistory(serverID, limit), nil
}

func (a *App) DeleteHistoryEntry(id string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.store.DeleteHistoryEntry(id)
}

func (a *App) ClearHistory(serverID string) error {
	if err := a.ensureReady(); err != nil {
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

func (a *App) SaveShortcut(sc models.CommandShortcut) (models.CommandShortcut, error) {
	if err := a.ensureReady(); err != nil {
		return models.CommandShortcut{}, err
	}
	return a.store.SaveShortcut(sc)
}

func (a *App) DeleteShortcut(id string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.store.DeleteShortcut(id)
}

func (a *App) RunShortcut(sessionID, shortcutID string, forceExecute bool) error {
	if err := a.ensureReady(); err != nil {
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
	payload := target.Command
	if forceExecute || target.AutoExecute {
		if !strings.HasSuffix(payload, "\n") && !strings.HasSuffix(payload, "\r") {
			payload += "\r"
		}
	}
	return a.sshMgr.WriteInput(sessionID, payload)
}

func (a *App) SuggestCommands(input string, serverID string) ([]models.CompletionItem, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	settings := a.store.GetSettings()
	if !settings.AutocompleteEnabled {
		return []models.CompletionItem{}, nil
	}
	shortcuts := a.store.ListShortcuts()
	history := a.store.ListHistory(serverID, 150)
	if len(history) < 30 && serverID != "" {
		history = append(history, a.store.ListHistory("", 150)...)
	}
	items := autocomplete.Suggest(input, shortcuts, history, settings.AutocompleteFromShortcut, settings.AutocompleteFromHistory, 8)
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
	if err := a.ensureReady(); err != nil {
		return models.AppSettings{}, err
	}
	return a.store.SaveSettings(settings)
}
