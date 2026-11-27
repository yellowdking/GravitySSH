package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/crypto"
	"github.com/yellowdking/GravitySSH/internal/models"
)

const (
	vaultSchemaVersion      = 5
	defaultRecordFlushDelay = 1500 * time.Millisecond
)

var errVaultLocked = errors.New("配置文件处于锁定状态，请先输入密码解锁")

type Store struct {
	mu          sync.RWMutex
	filePath    string
	enc         *crypto.EnvelopeCrypto
	data        models.VaultData
	locked      bool
	hasPassword bool
	pwSalt      []byte
	pwKey       []byte
	dirty       bool
	closed      bool
	flushTimer  *time.Timer
	flushDelay  time.Duration
}

func Open(filePath string, protector crypto.KeyProtector) (*Store, error) {
	if filePath == "" {
		var err error
		filePath, err = DefaultVaultPath()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return nil, fmt.Errorf("create vault directory: %w", err)
	}

	s := &Store{
		filePath:   filePath,
		enc:        crypto.NewEnvelopeCrypto(protector),
		flushDelay: defaultRecordFlushDelay,
		data: models.VaultData{
			Version:   vaultSchemaVersion,
			Servers:   []models.ServerConfig{},
			SSHKeys:   []models.SSHKeyRecord{},
			Shortcuts: []models.CommandShortcut{},
			Templates: []models.BuiltinTemplate{},
			History:   []models.CommandRecord{},
			Settings:  models.DefaultSettings(),
		},
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.data.Shortcuts = defaultShortcuts()
			s.data.Templates = BuiltinOpsTemplates()
			if err := s.saveLocked(); err != nil {
				return nil, fmt.Errorf("initialize encrypted vault: %w", err)
			}
			return s, nil
		}
		return nil, fmt.Errorf("read vault file: %w", err)
	}

	isPw, err := s.enc.IsPasswordProtected(raw)
	if err != nil {
		return nil, err
	}
	if isPw {
		s.locked = true
		s.hasPassword = true
		return s, nil
	}

	plain, err := s.enc.OpenWithPassword(raw, "")
	if err != nil {
		return nil, err
	}
	defer clear(plain)

	var loaded models.VaultData
	if err := json.Unmarshal(plain, &loaded); err != nil {
		return nil, fmt.Errorf("decode vault json: %w", err)
	}
	normalizeVault(&loaded)
	s.data = loaded
	return s, nil
}

func normalizeVault(v *models.VaultData) {
	if v.Version < 3 {
		v.Settings.MonitorBarEnabled = true
		v.Settings.SFTPFollowTerminal = true
		v.Settings.PasteGuardEnabled = true
		v.Settings.KeywordHighlightEnabled = true
		v.Settings.CheckUpdatesOnStartup = true
	}
	if v.Version < 4 && v.Templates == nil {
		v.Templates = BuiltinOpsTemplates()
	}
	if v.Version < 5 {
		v.Settings.AutoSwitchEnglishIME = true
	}
	if v.Version < vaultSchemaVersion {
		v.Version = vaultSchemaVersion
	}
	if v.Servers == nil {
		v.Servers = []models.ServerConfig{}
	}
	if v.SSHKeys == nil {
		v.SSHKeys = []models.SSHKeyRecord{}
	}
	if v.Shortcuts == nil {
		v.Shortcuts = []models.CommandShortcut{}
	}
	if v.Templates == nil {
		v.Templates = []models.BuiltinTemplate{}
	}
	if v.History == nil {
		v.History = []models.CommandRecord{}
	}
	d := models.DefaultSettings()
	if v.Settings.TerminalFontFamily == "" {
		v.Settings.TerminalFontFamily = d.TerminalFontFamily
	}
	if v.Settings.TerminalFontSize <= 0 {
		v.Settings.TerminalFontSize = d.TerminalFontSize
	}
	if v.Settings.DefaultKeepAliveSeconds <= 0 {
		v.Settings.DefaultKeepAliveSeconds = d.DefaultKeepAliveSeconds
	}
	if v.Settings.MaxHistoryEntries <= 0 {
		v.Settings.MaxHistoryEntries = d.MaxHistoryEntries
	}
}

func cloneVaultData(d models.VaultData) models.VaultData {
	out := d
	if d.Servers != nil {
		out.Servers = make([]models.ServerConfig, len(d.Servers))
		for i, srv := range d.Servers {
			if srv.LastConnectedAt != nil {
				t := *srv.LastConnectedAt
				srv.LastConnectedAt = &t
			}
			out.Servers[i] = srv
		}
	}
	if d.SSHKeys != nil {
		out.SSHKeys = make([]models.SSHKeyRecord, len(d.SSHKeys))
		copy(out.SSHKeys, d.SSHKeys)
	}
	if d.Shortcuts != nil {
		out.Shortcuts = make([]models.CommandShortcut, len(d.Shortcuts))
		copy(out.Shortcuts, d.Shortcuts)
	}
	if d.Templates != nil {
		out.Templates = make([]models.BuiltinTemplate, len(d.Templates))
		copy(out.Templates, d.Templates)
	}
	if d.History != nil {
		out.History = make([]models.CommandRecord, len(d.History))
		copy(out.History, d.History)
	}
	return out
}

func defaultShortcuts() []models.CommandShortcut {
	now := time.Now()
	return []models.CommandShortcut{
		{
			ID:          uuid.NewString(),
			Name:        "系统资源总览",
			Command:     "uname -a && free -h && df -h",
			Description: "查看内核、内存和磁盘占用",
			Category:    "系统监控",
			AutoExecute: true,
			Pinned:      true,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          uuid.NewString(),
			Name:        "查看监听端口",
			Command:     "ss -tulpn",
			Description: "列出当前 TCP/UDP 监听端口及进程",
			Category:    "网络排查",
			AutoExecute: true,
			Pinned:      true,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          uuid.NewString(),
			Name:        "Docker 容器状态",
			Command:     "docker ps -a --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'",
			Description: "以简洁表格查看所有 Docker 容器状态",
			Category:    "Docker",
			AutoExecute: true,
			Pinned:      false,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
}

func (s *Store) saveLocked() error {
	if s.locked {
		return errVaultLocked
	}
	if s.hasPassword && (len(s.pwSalt) == 0 || len(s.pwKey) == 0) {
		return errors.New("加密密钥状态异常，请重新解锁配置文件")
	}
	if s.data.Version < vaultSchemaVersion {
		s.data.Version = vaultSchemaVersion
	}
	plain, err := json.Marshal(s.data)
	if err != nil {
		return fmt.Errorf("marshal vault: %w", err)
	}
	defer clear(plain)

	var sealed []byte
	if s.hasPassword {
		sealed, err = s.enc.SealV2WithCachedKey(plain, s.pwSalt, s.pwKey)
	} else {
		sealed, err = s.enc.SealV2WithCachedKey(plain, nil, nil)
	}
	if err != nil {
		return fmt.Errorf("encrypt vault: %w", err)
	}

	if err := writeFileAtomic(s.filePath, sealed); err != nil {
		return err
	}
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}
	s.dirty = false
	return nil
}

// writeFileAtomic writes data to a unique temp file in the target directory,
// flushes it to disk via Sync+Close, and replaces targetPath without ever
// deleting the existing vault file before the new copy is safely in place.
func writeFileAtomic(targetPath string, data []byte) error {
	dir := filepath.Dir(targetPath)
	tmpFile, err := os.CreateTemp(dir, ".vault-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp vault: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod temp vault: %w", err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp vault: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync temp vault: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp vault: %w", err)
	}

	// Retry direct rename briefly to absorb transient Windows AV/indexer locks.
	var renameErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*20) * time.Millisecond)
		}
		renameErr = os.Rename(tmpPath, targetPath)
		if renameErr == nil {
			cleanupTmp = false
			return nil
		}
	}

	// Safe backup-and-swap fallback: move old file to a backup instead of deleting it,
	// and restore the backup if moving the new temp file into place fails.
	if _, statErr := os.Stat(targetPath); statErr == nil {
		bakPath := tmpPath + ".bak"
		if bakErr := os.Rename(targetPath, bakPath); bakErr == nil {
			if err2 := os.Rename(tmpPath, targetPath); err2 == nil {
				cleanupTmp = false
				_ = os.Remove(bakPath)
				return nil
			} else {
				_ = os.Rename(bakPath, targetPath)
				return fmt.Errorf("commit vault file: %w", err2)
			}
		}
	}
	return fmt.Errorf("commit vault file: %w", renameErr)
}

func (s *Store) scheduleFlushLocked() {
	s.dirty = true
	if s.flushTimer != nil || s.closed {
		return
	}
	delay := s.flushDelay
	if delay <= 0 {
		delay = defaultRecordFlushDelay
	}
	s.flushTimer = time.AfterFunc(delay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.flushTimer = nil
		if s.closed || !s.dirty || s.locked {
			return
		}
		_ = s.saveLocked()
	})
}

// Flush immediately persists any coalesced dirty writes (such as recent command history).
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}
	if !s.dirty || s.locked {
		return nil
	}
	return s.saveLocked()
}

// Close flushes pending writes, stops background timers, and zeros cached key material.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}
	var err error
	if s.dirty && !s.locked {
		err = s.saveLocked()
	}
	s.closed = true
	clear(s.pwKey)
	clear(s.pwSalt)
	s.pwKey = nil
	s.pwSalt = nil
	return err
}

func (s *Store) SecurityInfo() models.VaultSecurityInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := s.enc.Protector()
	return models.VaultSecurityInfo{
		Encrypted:     true,
		Engine:        p.EngineName(),
		MachineBound:  p.IsMachineBound(),
		UserBound:     p.IsUserBound(),
		VaultPath:     s.filePath,
		KeyCount:      len(s.data.SSHKeys),
		ShortcutCount: len(s.data.Shortcuts),
		ServerCount:   len(s.data.Servers),
	}
}
