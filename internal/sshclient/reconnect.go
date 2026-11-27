package sshclient

import (
	"context"
	"fmt"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func (m *Manager) Reconnect(ctx context.Context, sessionID string, cols, rows int) (models.SessionInfo, error) {
	sess := m.getSession(sessionID)
	if sess == nil {
		return models.SessionInfo{}, fmt.Errorf("会话不存在或已关闭")
	}
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}

	sess.mu.Lock()
	serverID := sess.info.ServerID
	oldCancel := sess.cancelDial
	sess.cancelDial = nil
	closeStopChLocked(sess)
	if sess.sftpClient != nil {
		_ = sess.sftpClient.Close()
		sess.sftpClient = nil
	}
	if sess.sshSession != nil {
		_ = sess.sshSession.Close()
		sess.sshSession = nil
	}
	if sess.dialRes != nil {
		sess.dialRes.Close()
		sess.dialRes = nil
	}
	sess.stdin = nil
	sess.dirCache = make(map[string]dirCacheEntry)
	sess.dirCacheGen++
	sess.prevCPU = nil
	sess.prevNet = nil
	sess.prevSampleAt = time.Time{}
	sess.cachedCores = 0
	sess.gpuChecked = false
	sess.hasGPU = false
	sess.sshSameFSChecked = false
	sess.sshSameFS = false
	sess.userClosed = false
	sess.closed = false
	sess.stopCh = make(chan struct{})
	sess.lastCols = cols
	sess.lastRows = rows
	sess.info.Status = "connecting"
	sess.info.SFTPReady = false
	sess.info.Error = ""
	dialCtx, cancelDial := context.WithCancel(ctx)
	sess.cancelDial = cancelDial
	infoSnap := sess.info
	sess.mu.Unlock()

	if oldCancel != nil {
		oldCancel()
	}

	srv, err := m.store.GetServerInternal(serverID)
	if err != nil {
		m.failSession(sess, "读取服务器配置失败: "+err.Error())
		return models.SessionInfo{}, err
	}

	m.sendTerminalText(sessionID, "\r\n\x1b[38;5;214m↻ 正在重新连接...\x1b[0m\r\n")
	m.emit("session:status", infoSnap)
	go m.establishSession(dialCtx, sess, srv, cols, rows)
	return infoSnap, nil
}
