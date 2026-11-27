package main

import (
	"path"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yellowdking/GravitySSH/internal/ime"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func (a *App) ConnectServer(serverID string, cols, rows int) (models.SessionInfo, error) {
	if err := a.ensureUnlocked(); err != nil {
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

func (a *App) SetSessionCWD(sessionID, remotePath string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	a.sshMgr.SetSessionCWD(sessionID, remotePath)
	return nil
}

func (a *App) GetSessionMetrics(sessionID string) (models.HostMetrics, error) {
	if err := a.ensureReady(); err != nil {
		return models.HostMetrics{}, err
	}
	return a.sshMgr.GetSessionMetrics(sessionID)
}

// SwitchTerminalIME switches Windows IME to English when enabled in settings.
func (a *App) SwitchTerminalIME() {
	if a.store != nil && !a.store.GetSettings().AutoSwitchEnglishIME {
		return
	}
	ime.SwitchToEnglish()
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

func (a *App) SFTPAlignPermissions(sessionID, remotePath string) (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	return a.sshMgr.SFTPAlignPermissions(sessionID, remotePath)
}

func (a *App) SFTPExtractArchive(sessionID, remotePath string) (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	return a.sshMgr.SFTPExtractArchive(sessionID, remotePath)
}

func (a *App) SFTPReadTextFile(sessionID, remotePath string) (models.RemoteFileContent, error) {
	if err := a.ensureReady(); err != nil {
		return models.RemoteFileContent{}, err
	}
	return a.sshMgr.SFTPReadTextFile(sessionID, remotePath)
}

func (a *App) SFTPWriteTextFile(sessionID, remotePath, content string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.sshMgr.SFTPWriteTextFile(sessionID, remotePath, content)
}

func (a *App) SFTPCompress(sessionID, remoteDir string, names []string, format string) (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	return a.sshMgr.SFTPCompress(sessionID, remoteDir, names, format)
}

func (a *App) SFTPDownloadFolderDialog(sessionID, remotePath string) (map[string]any, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择保存文件夹的目标本地目录",
	})
	if err != nil || dir == "" {
		return map[string]any{"cancelled": true}, err
	}
	count, err := a.sshMgr.SFTPDownloadPacked(sessionID, remotePath, dir)
	if err != nil {
		return nil, err
	}
	return map[string]any{"cancelled": false, "localDir": dir, "fileCount": count}, nil
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
