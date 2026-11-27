package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshclient"
	"github.com/yellowdking/GravitySSH/internal/syncclient"
	"github.com/yellowdking/GravitySSH/internal/updater"
	"github.com/yellowdking/GravitySSH/internal/vault"
)

const (
	maxSSHKeyFileSize    = 256 * 1024
	maxCachedSyncClients = 16
)

type cachedSyncClient struct {
	client   *syncclient.Client
	lastUsed time.Time
}

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
	syncClientMu    sync.Mutex
	syncClients     map[string]*cachedSyncClient
	shortcutMu      sync.Mutex
	shortcutRunners map[string]*sessionShortcutRunner
}

func NewApp() *App {
	return &App{
		syncCache:       newSyncPreviewCache(),
		syncClients:     make(map[string]*cachedSyncClient),
		shortcutRunners: make(map[string]*sessionShortcutRunner),
	}
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
	a.mu.Unlock()

	a.cancelAllShortcutRuns()
	if sshMgr != nil {
		sshMgr.CloseAll()
	}
	if syncCache != nil {
		syncCache.close()
	}
	a.syncClientMu.Lock()
	for _, entry := range a.syncClients {
		if entry != nil && entry.client != nil {
			entry.client.CloseIdleConnections()
		}
	}
	clear(a.syncClients)
	a.syncClientMu.Unlock()
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

func readSSHKeyFileBounded(selected string, maxBytes int64) (string, error) {
	f, err := os.Open(selected)
	if err != nil {
		return "", fmt.Errorf("读取密钥文件失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("读取密钥文件失败: %w", err)
	}
	if info.IsDir() {
		return "", errors.New("所选路径是目录，不是有效的 SSH 私钥文件")
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("所选路径不是普通文件，无法作为 SSH 私钥读取")
	}
	if info.Size() > maxBytes {
		return "", errors.New("文件体积过大，不是有效的 SSH 私钥文件")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("读取密钥文件失败: %w", err)
	}
	defer clear(raw)
	if int64(len(raw)) > maxBytes {
		return "", errors.New("文件体积过大，不是有效的 SSH 私钥文件")
	}
	return string(raw), nil
}

func (a *App) PickAndReadSSHKeyFile() (map[string]string, error) {
	_, _, ctx, err := a.unlockedDeps()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("应用上下文尚未就绪")
	}
	selected, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
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
