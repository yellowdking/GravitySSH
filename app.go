package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
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

func (a *App) ensureUnlocked() error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	if a.store.IsLocked() {
		return errors.New("配置文件已锁定，请先输入解锁密码")
	}
	return nil
}

func (a *App) GetVaultLockState() (models.VaultLockState, error) {
	if err := a.ensureReady(); err != nil {
		return models.VaultLockState{}, err
	}
	return a.store.LockState(), nil
}

func (a *App) UnlockVault(password string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.store.Unlock(password)
}

func (a *App) SetVaultPassword(currentPassword, newPassword string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return a.store.SetPassword(currentPassword, newPassword)
}

func (a *App) GetVaultSecurityInfo() (models.VaultSecurityInfo, error) {
	if err := a.ensureReady(); err != nil {
		return models.VaultSecurityInfo{}, err
	}
	return a.store.SecurityInfo(), nil
}

func (a *App) GetAppVersion() string {
	return AppVersion
}

func (a *App) ListServers() ([]models.ServerView, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.store.ListServers(), nil
}

func (a *App) SaveServer(req models.ServerSaveRequest) (models.ServerView, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.ServerView{}, err
	}
	return a.store.SaveServer(req)
}

func (a *App) DeleteServer(id string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return a.store.DeleteServer(id)
}

func (a *App) TestServerConnection(req models.ServerSaveRequest) (string, error) {
	if err := a.ensureUnlocked(); err != nil {
		return "", err
	}
	return a.sshMgr.TestConnection(a.ctx, req)
}

func (a *App) ListSSHKeys() ([]models.SSHKeyMeta, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.store.ListSSHKeys(), nil
}

func (a *App) ImportSSHKeyFromText(name, pemContent, passphrase, comment string) (models.SSHKeyMeta, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SSHKeyMeta{}, err
	}
	return a.store.ImportSSHKey(name, pemContent, passphrase, comment)
}

func (a *App) PickAndReadSSHKeyFile() (map[string]string, error) {
	if err := a.ensureUnlocked(); err != nil {
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
	if err := a.ensureUnlocked(); err != nil {
		return models.SSHKeyMeta{}, err
	}
	return a.store.GenerateSSHKey(name, algorithm, passphrase, comment)
}

func (a *App) DeleteSSHKey(id string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return a.store.DeleteSSHKey(id)
}

func (a *App) DeploySSHKeyToServers(keyID string, serverIDs []string, bindAsAuth bool) ([]models.KeyDeployResult, error) {
	if err := a.ensureUnlocked(); err != nil {
		return nil, err
	}
	return a.sshMgr.DeploySSHKeyToServers(a.ctx, keyID, serverIDs, bindAsAuth)
}
