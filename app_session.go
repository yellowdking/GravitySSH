package main

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yellowdking/GravitySSH/internal/ime"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func (a *App) ConnectServer(serverID string, cols, rows int) (models.SessionInfo, error) {
	_, sshMgr, ctx, err := a.unlockedDeps()
	if err != nil {
		return models.SessionInfo{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return sshMgr.Connect(ctx, serverID, cols, rows)
}

func (a *App) DisconnectSession(sessionID string) error {
	_, sshMgr, _, err := a.readyDeps()
	if err != nil {
		return err
	}
	a.cancelShortcutRun(sessionID)
	sshMgr.Disconnect(sessionID)
	return nil
}

func (a *App) WriteTerminal(sessionID, data string) error {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return sshMgr.WriteInput(sessionID, data)
}

func (a *App) ResizeTerminal(sessionID, cols, rows int) error {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return sshMgr.Resize(sessionID, cols, rows)
}

func (a *App) SetSessionCWD(sessionID, remotePath string) error {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	sshMgr.SetSessionCWD(sessionID, remotePath)
	return nil
}

func (a *App) GetSessionMetrics(sessionID string) (models.HostMetrics, error) {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return models.HostMetrics{}, err
	}
	return sshMgr.GetSessionMetrics(sessionID)
}

// SwitchTerminalIME switches Windows IME to English when unlocked and enabled in settings.
func (a *App) SwitchTerminalIME() {
	a.mu.RLock()
	store := a.store
	a.mu.RUnlock()
	if store == nil || store.IsLocked() || !store.GetSettings().AutoSwitchEnglishIME {
		return
	}
	ime.SwitchToEnglish()
}

func (a *App) SFTPList(sessionID, remotePath string) (models.SFTPListResult, error) {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return models.SFTPListResult{}, err
	}
	return sshMgr.SFTPList(sessionID, remotePath)
}

func (a *App) SFTPMkdir(sessionID, remotePath string) error {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return sshMgr.SFTPMkdir(sessionID, remotePath)
}

func (a *App) SFTPRename(sessionID, oldPath, newPath string) error {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return sshMgr.SFTPRename(sessionID, oldPath, newPath)
}

func (a *App) SFTPDelete(sessionID, remotePath string) error {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return sshMgr.SFTPDelete(sessionID, remotePath)
}

func (a *App) SFTPAlignPermissions(sessionID, remotePath string) (string, error) {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return "", err
	}
	return sshMgr.SFTPAlignPermissions(sessionID, remotePath)
}

func (a *App) SFTPExtractArchive(sessionID, remotePath string) (string, error) {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return "", err
	}
	return sshMgr.SFTPExtractArchive(sessionID, remotePath)
}

func (a *App) SFTPReadTextFile(sessionID, remotePath string) (models.RemoteFileContent, error) {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return models.RemoteFileContent{}, err
	}
	return sshMgr.SFTPReadTextFile(sessionID, remotePath)
}

func (a *App) SFTPWriteTextFile(sessionID, remotePath, content string) error {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return sshMgr.SFTPWriteTextFile(sessionID, remotePath, content)
}

func (a *App) SFTPCompress(sessionID, remoteDir string, names []string, format string) (string, error) {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return "", err
	}
	return sshMgr.SFTPCompress(sessionID, remoteDir, names, format)
}

func (a *App) SFTPDownloadFolderDialog(sessionID, remotePath string) (map[string]any, error) {
	_, sshMgr, ctx, err := a.unlockedDeps()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("应用上下文尚未就绪")
	}
	dir, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
		Title: "选择保存文件夹的目标本地目录",
	})
	if err != nil || dir == "" {
		return map[string]any{"cancelled": true}, err
	}
	count, err := sshMgr.SFTPDownloadPacked(sessionID, remotePath, dir)
	if err != nil {
		return nil, err
	}
	return map[string]any{"cancelled": false, "localDir": dir, "fileCount": count}, nil
}

func (a *App) SFTPUploadDialog(sessionID, remoteDir string) (int, error) {
	_, _, ctx, err := a.unlockedDeps()
	if err != nil {
		return 0, err
	}
	if ctx == nil {
		return 0, errors.New("应用上下文尚未就绪")
	}
	files, err := runtime.OpenMultipleFilesDialog(ctx, runtime.OpenDialogOptions{
		Title: "选择要上传到服务器的文件",
	})
	if err != nil || len(files) == 0 {
		return 0, err
	}
	return a.SFTPUploadFiles(sessionID, files, remoteDir)
}

func (a *App) SFTPUploadFiles(sessionID string, localPaths []string, remoteDir string) (int, error) {
	_, sshMgr, _, err := a.unlockedDeps()
	if err != nil {
		return 0, err
	}
	uploaded := 0
	for _, localPath := range localPaths {
		if strings.TrimSpace(localPath) == "" {
			continue
		}
		if err := sshMgr.SFTPUpload(sessionID, localPath, remoteDir); err != nil {
			return uploaded, err
		}
		uploaded++
	}
	return uploaded, nil
}

func (a *App) SFTPDownloadDialog(sessionID, remotePath string) (string, error) {
	_, sshMgr, ctx, err := a.unlockedDeps()
	if err != nil {
		return "", err
	}
	if ctx == nil {
		return "", errors.New("应用上下文尚未就绪")
	}
	savePath, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		Title:           "保存下载文件",
		DefaultFilename: path.Base(remotePath),
	})
	if err != nil || savePath == "" {
		return "", err
	}
	if err := sshMgr.SFTPDownload(sessionID, remotePath, savePath); err != nil {
		return "", err
	}
	return savePath, nil
}
