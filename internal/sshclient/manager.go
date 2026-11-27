package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/vault"
	"golang.org/x/crypto/ssh"
)

type EventSink interface {
	Emit(eventName string, data ...any)
}

type dirCacheEntry struct {
	entries   []models.SFTPFileEntry
	fetchedAt time.Time
}

type cpuSample struct {
	total uint64
	idle  uint64
}

type netSample struct {
	rx uint64
	tx uint64
}

type sessionState struct {
	mu                sync.Mutex
	writeMu           sync.Mutex
	info              models.SessionInfo
	cancelDial        context.CancelFunc
	pendingDial       *DialResult
	dialRes           *DialResult
	sshSession        *ssh.Session
	stdin             io.WriteCloser
	sftpClient        *sftp.Client
	recorder          *CommandRecorder
	stopCh            chan struct{}
	closed            bool
	userClosed        bool
	lastCols          int
	lastRows          int
	currentDir        string
	dirCache          map[string]dirCacheEntry
	dirCacheGen       uint64
	prevCPU           *cpuSample
	prevNet           *netSample
	prevSampleAt      time.Time
	cachedCores       int
	gpuChecked        bool
	hasGPU            bool
	metricsInFlight   atomic.Bool
	keepAliveInFlight atomic.Bool
	rttProbeInFlight  atomic.Bool
	sshSameFSChecked  bool
	sshSameFS         bool
}

type Manager struct {
	mu                sync.RWMutex
	store             *vault.Store
	events            EventSink
	sessions          map[string]*sessionState
	dirWarmupInFlight sync.Map
	sftpDial          func(sessionID string) (*sftp.Client, error)
	knownHostsPathFn  func() string
}

func NewManager(store *vault.Store, events EventSink) *Manager {
	return &Manager{
		store:    store,
		events:   events,
		sessions: make(map[string]*sessionState),
	}
}

func (m *Manager) emit(event string, payload any) {
	if m.events != nil {
		m.events.Emit(event, payload)
	}
}

func (m *Manager) emitTransfer(p models.TransferProgress) {
	m.emit("sftp:progress", p)
}

func (m *Manager) TestConnection(ctx context.Context, req models.ServerSaveRequest) (string, error) {
	cfg := &models.ServerConfig{
		ID:             req.ID,
		Name:           req.Name,
		Host:           strings.TrimSpace(req.Host),
		Port:           defaultPort(req.Port),
		Username:       strings.TrimSpace(req.Username),
		AuthType:       req.AuthType,
		Password:       req.Password,
		SSHKeyID:       req.SSHKeyID,
		TimeoutSeconds: req.TimeoutSeconds,
		Proxy: models.ProxyConfig{
			Type:     req.Proxy.Type,
			Host:     strings.TrimSpace(req.Proxy.Host),
			Port:     req.Proxy.Port,
			Username: strings.TrimSpace(req.Proxy.Username),
			Password: req.Proxy.Password,
		},
		JumpHost: models.JumpHostConfig{
			Mode:         req.JumpHost.Mode,
			ServerID:     req.JumpHost.ServerID,
			Host:         strings.TrimSpace(req.JumpHost.Host),
			Port:         req.JumpHost.Port,
			Username:     strings.TrimSpace(req.JumpHost.Username),
			AuthType:     req.JumpHost.AuthType,
			Password:     req.JumpHost.Password,
			SSHKeyID:     req.JumpHost.SSHKeyID,
			UseMainProxy: req.JumpHost.UseMainProxy,
		},
	}

	if req.ID != "" {
		if existing, err := m.store.GetServerInternal(req.ID); err == nil {
			if req.KeepExistingPassword && cfg.Password == "" {
				cfg.Password = existing.Password
			}
			if req.Proxy.KeepExistingPassword && cfg.Proxy.Password == "" {
				cfg.Proxy.Password = existing.Proxy.Password
			}
			if req.JumpHost.KeepExistingPassword && cfg.JumpHost.Password == "" {
				cfg.JumpHost.Password = existing.JumpHost.Password
			}
		}
	}

	if cfg.Host == "" {
		return "", errors.New("请输入服务器 IP 或域名")
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 12
	}

	start := time.Now()
	res, err := DialServer(ctx, cfg, m.store)
	if err != nil {
		return "", err
	}
	defer res.Close()

	elapsed := time.Since(start).Round(time.Millisecond)
	if res.ViaSummary != "" {
		return fmt.Sprintf("连接成功 (%s) · 路径: %s → %s", elapsed, res.ViaSummary, cfg.Host), nil
	}
	return fmt.Sprintf("连接成功 (%s) · %s@%s:%d", elapsed, cfg.Username, cfg.Host, cfg.Port), nil
}

func (m *Manager) Connect(ctx context.Context, serverID string, cols, rows int) (models.SessionInfo, error) {
	srv, err := m.store.GetServerInternal(serverID)
	if err != nil {
		return models.SessionInfo{}, err
	}
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}

	sessionID := uuid.NewString()
	dialCtx, cancelDial := context.WithCancel(ctx)
	sess := &sessionState{
		info: models.SessionInfo{
			SessionID:  sessionID,
			ServerID:   srv.ID,
			ServerName: srv.Name,
			Host:       srv.Host,
			Port:       defaultPort(srv.Port),
			Username:   srv.Username,
			Status:     "connecting",
			CreatedAt:  time.Now(),
		},
		cancelDial: cancelDial,
		stopCh:     make(chan struct{}),
		lastCols:   cols,
		lastRows:   rows,
		dirCache:   make(map[string]dirCacheEntry),
	}

	sess.recorder = NewCommandRecorder(func(cmd string) {
		m.updateCWDFromCommand(sess, cmd)
		rec, recErr := m.store.RecordCommand(srv.ID, srv.Name, sessionID, cmd)
		if recErr == nil && rec != nil {
			m.emit("command:recorded", rec)
		}
	})

	infoSnap := sess.info
	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()
	m.emit("session:status", infoSnap)

	go m.establishSession(dialCtx, sess, srv, cols, rows)
	return infoSnap, nil
}
