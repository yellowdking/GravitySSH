package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yellowdking/GravitySSH/internal/autocomplete"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshclient"
	"github.com/yellowdking/GravitySSH/internal/vault"
)

type wailsEventSink struct {
	ctx context.Context
}

func (w *wailsEventSink) Emit(eventName string, data ...any) {
	if w.ctx != nil {
		runtime.EventsEmit(w.ctx, eventName, data...)
	}
}

// App is the Wails-bound backend controller exposed to the React frontend.
type App struct {
	ctx     context.Context
	store   *vault.Store
	sshMgr  *sshclient.Manager
	initErr error
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	store, err := vault.Open("", nil)
	if err != nil {
		a.initErr = err
		return
	}
	a.store = store
	a.sshMgr = sshclient.NewManager(store, &wailsEventSink{ctx: ctx})
}

func (a *App) shutdown(ctx context.Context) {
	if a.sshMgr != nil {
		a.sshMgr.CloseAll()
	}
}

func (a *App) ensureReady() error {
	if a.initErr != nil {
		return fmt.Errorf("加密保险库初始化失败: %w", a.initErr)
	}
	if a.store == nil || a.sshMgr == nil {
		return errors.New("应用尚未初始化完成")
	}
	return nil
}

// ---------- Vault & Security ----------

func (a *App) GetVaultSecurityInfo() (models.VaultSecurityInfo, error) {
	if err := a.ensureReady(); err != nil {
		return models.VaultSecurityInfo{}, err
	}
	return a.store.SecurityInfo(), nil
}

func (a *App) GetAppVersion() string {
	return AppVersion
}

// ---------- Servers ----------

func (a *App) ListServers() ([]models.ServerView, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.store.ListServers(), nil
}

func (a *App) SaveServer(req models.ServerSaveRequest) (models.ServerView, error) {
	if err := a.ensureReady(); err != nil {
		return models.ServerView{}, err
	}
	return a.store.SaveServer(req)
}

func (a *App) DeleteServer(id string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.store.DeleteServer(id)
}

func (a *App) TestServerConnection(req models.ServerSaveRequest) (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	return a.sshMgr.TestConnection(a.ctx, req)
}

// ---------- SSH Keys (Encrypted Vault, Private Key Never Exposed) ----------

func (a *App) ListSSHKeys() ([]models.SSHKeyMeta, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.store.ListSSHKeys(), nil
}

func (a *App) ImportSSHKeyFromText(name, pemContent, passphrase, comment string) (models.SSHKeyMeta, error) {
	if err := a.ensureReady(); err != nil {
		return models.SSHKeyMeta{}, err
	}
	return a.store.ImportSSHKey(name, pemContent, passphrase, comment)
}

func (a *App) PickAndReadSSHKeyFile() (map[string]string, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	selected, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择 SSH 私钥文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "SSH 密钥文件 (*.pem, *.key, id_*)", Pattern: "*.pem;*.key;*.ppk;*"},
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return nil, err
	}
	if selected == "" {
		return map[string]string{"cancelled": "true"}, nil
	}
	raw, err := os.ReadFile(selected)
	if err != nil {
		return nil, fmt.Errorf("读取密钥文件失败: %w", err)
	}
	if len(raw) > 256*1024 {
		return nil, errors.New("文件体积过大，不是有效的 SSH 私钥文件")
	}
	return map[string]string{
		"cancelled": "false",
		"fileName":  filepath.Base(selected),
		"content":   string(raw),
	}, nil
}

func (a *App) GenerateSSHKey(name, algorithm, passphrase, comment string) (models.SSHKeyMeta, error) {
	if err := a.ensureReady(); err != nil {
		return models.SSHKeyMeta{}, err
	}
	return a.store.GenerateSSHKey(name, algorithm, passphrase, comment)
}

func (a *App) DeleteSSHKey(id string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.store.DeleteSSHKey(id)
}

// ---------- SSH Sessions & Terminal ----------

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

// ---------- Auto SFTP File Manager ----------

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
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}
	uploaded := 0
	for _, localPath := range files {
		if err := a.sshMgr.SFTPUpload(sessionID, localPath, remoteDir); err != nil {
			return uploaded, err
		}
		uploaded++
	}
	return uploaded, nil
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
	defaultName := path.Base(remotePath)
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "保存下载文件",
		DefaultFilename: defaultName,
	})
	if err != nil {
		return "", err
	}
	if savePath == "" {
		return "", nil
	}
	if err := a.sshMgr.SFTPDownload(sessionID, remotePath, savePath); err != nil {
		return "", err
	}
	return savePath, nil
}

// ---------- Command History & Encrypted Shortcuts ----------

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

// ---------- Smart Command Autocomplete ----------

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
		// Also include global recent commands
		history = append(history, a.store.ListHistory("", 150)...)
	}
	items := autocomplete.Suggest(
		input,
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

// ---------- Settings ----------

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
