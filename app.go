package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshclient"
	"github.com/yellowdking/GravitySSH/internal/syncsvc"
	"github.com/yellowdking/GravitySSH/internal/updater"
	"github.com/yellowdking/GravitySSH/internal/vault"
)

const maxSSHKeyFileSize = 256 * 1024

type wailsEventSink struct {
	ctx context.Context
}

func (w *wailsEventSink) Emit(eventName string, data ...any) {
	if w.ctx != nil {
		runtime.EventsEmit(w.ctx, eventName, data...)
	}
}

type App struct {
	mu              sync.RWMutex
	ctx             context.Context
	store           *vault.Store
	sshMgr          *sshclient.Manager
	initErr         error
	closing         bool
	syncCache       *syncPreviewCache
	syncMgr         *syncsvc.Manager
	shortcutMu      sync.Mutex
	shortcutRunners map[string]*sessionShortcutRunner
}

func NewApp() *App {
	app := &App{
		syncCache:       newSyncPreviewCache(),
		shortcutRunners: make(map[string]*sessionShortcutRunner),
	}
	app.syncMgr = syncsvc.NewManager(
		func(eventName string, payload any) {
			if ctx := app.appContext(); ctx != nil {
				runtime.EventsEmit(ctx, eventName, payload)
			}
		},
		app.buildSyncPreview,
	)
	return app
}

func (a *App) startup(ctx context.Context) {
	store, err := vault.Open("", nil)

	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		if store != nil {
			_ = store.Close()
		}
		return
	}
	a.ctx = ctx
	if err != nil {
		a.initErr = err
		a.mu.Unlock()
		return
	}
	a.store = store
	a.sshMgr = sshclient.NewManager(store, &wailsEventSink{ctx: ctx})
	a.mu.Unlock()
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	a.closing = true
	a.ctx = nil
	sshMgr := a.sshMgr
	store := a.store
	syncCache := a.syncCache
	syncMgr := a.syncMgr
	a.mu.Unlock()

	a.cancelAllShortcutRuns()
	if syncMgr != nil {
		syncMgr.CloseAllOnLock()
	}
	if sshMgr != nil {
		sshMgr.CloseAll()
	}
	if syncCache != nil {
		syncCache.close()
	}
	if store != nil {
		_ = store.Close()
	}
}

func (a *App) appContext() context.Context {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	return ctx
}

func (a *App) bgContext() context.Context {
	if ctx := a.appContext(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func (a *App) readyDeps() (*vault.Store, *sshclient.Manager, context.Context, error) {
	a.mu.RLock()
	closing := a.closing
	initErr := a.initErr
	store := a.store
	sshMgr := a.sshMgr
	ctx := a.ctx
	a.mu.RUnlock()
	if closing {
		return nil, nil, nil, errors.New("应用正在关闭")
	}
	if initErr != nil {
		return nil, nil, nil, fmt.Errorf("加密保险库初始化失败: %w", initErr)
	}
	if store == nil || sshMgr == nil {
		return nil, nil, nil, errors.New("应用尚未初始化完成")
	}
	return store, sshMgr, ctx, nil
}

func (a *App) unlockedDeps() (*vault.Store, *sshclient.Manager, context.Context, error) {
	store, sshMgr, ctx, err := a.readyDeps()
	if err != nil {
		return nil, nil, nil, err
	}
	if store.IsLocked() {
		return nil, nil, nil, errors.New("配置文件已锁定，请先输入解锁密码")
	}
	return store, sshMgr, ctx, nil
}

func (a *App) ensureReady() error {
	_, _, _, err := a.readyDeps()
	return err
}

func (a *App) ensureUnlocked() error {
	_, _, _, err := a.unlockedDeps()
	return err
}

func (a *App) GetVaultLockState() (models.VaultLockState, error) {
	store, _, _, err := a.readyDeps()
	if err != nil {
		return models.VaultLockState{}, err
	}
	return store.LockState(), nil
}

func (a *App) UnlockVault(password string) error {
	store, _, _, err := a.readyDeps()
	if err != nil {
		return err
	}
	return store.Unlock(password)
}

// LockVault immediately terminates active shortcut runners, sync flows, and SSH/SFTP sessions,
// flushes dirty vault state, and zeroes decrypted secrets from memory.
func (a *App) LockVault() error {
	store, sshMgr, _, err := a.readyDeps()
	if err != nil {
		return err
	}
	if !store.LockState().HasPassword {
		return store.Lock()
	}
	a.cancelAllShortcutRuns()
	a.cancelAllSyncOnLock()
	if sshMgr != nil {
		sshMgr.CloseAll()
	}
	return store.Lock()
}

func (a *App) SetVaultPassword(currentPassword, newPassword string) error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return store.SetPassword(currentPassword, newPassword)
}

func (a *App) GetVaultSecurityInfo() (models.VaultSecurityInfo, error) {
	store, _, _, err := a.readyDeps()
	if err != nil {
		return models.VaultSecurityInfo{}, err
	}
	return store.SecurityInfo(), nil
}

func (a *App) GetAppVersion() string {
	return AppVersion
}

func (a *App) CheckForUpdates() (models.UpdateCheckResult, error) {
	return updater.CheckLatestRelease(a.bgContext(), AppVersion)
}

func (a *App) OpenExternalURL(rawURL string) {
	ctx := a.appContext()
	if ctx == nil {
		return
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return
	}
	runtime.BrowserOpenURL(ctx, u.String())
}

func (a *App) ListServers() ([]models.ServerView, error) {
	store, _, _, err := a.readyDeps()
	if err != nil {
		return nil, err
	}
	return store.ListServers(), nil
}

func (a *App) SaveServer(req models.ServerSaveRequest) (models.ServerView, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.ServerView{}, err
	}
	return store.SaveServer(req)
}

func (a *App) DeleteServer(id string) error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return store.DeleteServer(id)
}

func (a *App) TestServerConnection(req models.ServerSaveRequest) (string, error) {
	_, sshMgr, ctx, err := a.unlockedDeps()
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return sshMgr.TestConnection(ctx, req)
}

func (a *App) ListSSHKeys() ([]models.SSHKeyMeta, error) {
	store, _, _, err := a.readyDeps()
	if err != nil {
		return nil, err
	}
	return store.ListSSHKeys(), nil
}

func (a *App) ImportSSHKeyFromText(name, pemContent, passphrase, comment string) (models.SSHKeyMeta, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SSHKeyMeta{}, err
	}
	return store.ImportSSHKey(name, pemContent, passphrase, comment)
}

func readSSHKeyFileBytesBounded(selected string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(selected)
	if err != nil {
		return nil, fmt.Errorf("读取密钥文件失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("读取密钥文件失败: %w", err)
	}
	if info.IsDir() {
		return nil, errors.New("所选路径是目录，不是有效的 SSH 私钥文件")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("所选路径不是普通文件，无法作为 SSH 私钥读取")
	}
	if info.Size() > maxBytes {
		return nil, errors.New("文件体积过大，不是有效的 SSH 私钥文件")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取密钥文件失败: %w", err)
	}
	if int64(len(raw)) > maxBytes {
		clear(raw)
		return nil, errors.New("文件体积过大，不是有效的 SSH 私钥文件")
	}
	return raw, nil
}

func readSSHKeyFileBounded(selected string, maxBytes int64) (string, error) {
	raw, err := readSSHKeyFileBytesBounded(selected, maxBytes)
	if err != nil {
		return "", err
	}
	defer clear(raw)
	return string(raw), nil
}

func (a *App) importSSHKeyFromPath(store *vault.Store, selected, name, passphrase, comment string) (*models.SSHKeyMeta, error) {
	raw, err := readSSHKeyFileBytesBounded(selected, maxSSHKeyFileSize)
	if err != nil {
		return nil, err
	}
	defer clear(raw)

	keyName := strings.TrimSpace(name)
	if keyName == "" {
		base := filepath.Base(selected)
		keyName = strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
		if keyName == "" {
			keyName = strings.TrimSpace(base)
		}
	}
	meta, err := store.ImportSSHKey(keyName, string(raw), passphrase, comment)
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

func sshKeyOpenDialogOptionsForGOOS(goos, homeDir string) runtime.OpenDialogOptions {
	if goos == "darwin" {
		var defaultDir string
		if homeDir != "" {
			sshDir := filepath.Join(homeDir, ".ssh")
			if info, sErr := os.Stat(sshDir); sErr == nil && info.IsDir() {
				defaultDir = sshDir
			}
		}
		return runtime.OpenDialogOptions{
			Title:            "选择 SSH 私钥文件",
			DefaultDirectory: defaultDir,
			Filters:          nil,
			ShowHiddenFiles:  true,
		}
	}
	return runtime.OpenDialogOptions{
		Title: "选择 SSH 私钥文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "SSH 密钥文件 (*.pem, *.key, id_*)", Pattern: "*.pem;*.key;*.ppk;*"},
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	}
}

func sshKeyOpenDialogOptions() runtime.OpenDialogOptions {
	home, _ := os.UserHomeDir()
	return sshKeyOpenDialogOptionsForGOOS(goruntime.GOOS, home)
}

func (a *App) ImportSSHKeyFromFile(name, passphrase, comment string) (*models.SSHKeyMeta, error) {
	store, _, ctx, err := a.unlockedDeps()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("应用上下文尚未就绪")
	}
	selected, err := runtime.OpenFileDialog(ctx, sshKeyOpenDialogOptions())
	if err != nil {
		return nil, err
	}
	if selected == "" {
		return nil, nil
	}
	return a.importSSHKeyFromPath(store, selected, name, passphrase, comment)
}

func (a *App) PickAndReadSSHKeyFile() (map[string]string, error) {
	_, _, ctx, err := a.unlockedDeps()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("应用上下文尚未就绪")
	}
	selected, err := runtime.OpenFileDialog(ctx, sshKeyOpenDialogOptions())
	if err != nil {
		return nil, err
	}
	if selected == "" {
		return map[string]string{"cancelled": "true"}, nil
	}
	content, err := readSSHKeyFileBounded(selected, maxSSHKeyFileSize)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"cancelled": "false",
		"fileName":  filepath.Base(selected),
		"content":   content,
	}, nil
}

func (a *App) GenerateSSHKey(name, algorithm, passphrase, comment string) (models.SSHKeyMeta, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SSHKeyMeta{}, err
	}
	return store.GenerateSSHKey(name, algorithm, passphrase, comment)
}

func (a *App) DeleteSSHKey(id string) error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return store.DeleteSSHKey(id)
}

func (a *App) DeploySSHKeyToServers(keyID string, serverIDs []string, bindAsAuth bool) ([]models.KeyDeployResult, error) {
	_, sshMgr, ctx, err := a.unlockedDeps()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return sshMgr.DeploySSHKeyToServers(ctx, keyID, serverIDs, bindAsAuth)
}
