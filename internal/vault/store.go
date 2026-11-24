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

const vaultSchemaVersion = 5

type Store struct {
	mu          sync.RWMutex
	filePath    string
	enc         *crypto.EnvelopeCrypto
	data        models.VaultData
	locked      bool
	hasPassword bool
	password    string
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
		filePath: filePath,
		enc:      crypto.NewEnvelopeCrypto(protector),
		data: models.VaultData{
			Version:   vaultSchemaVersion,
			Servers:   []models.ServerConfig{},
			SSHKeys:   []models.SSHKeyRecord{},
			Shortcuts: defaultShortcuts(),
			Templates: BuiltinOpsTemplates(),
			History:   []models.CommandRecord{},
			Settings:  models.DefaultSettings(),
		},
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
		v.Settings.AutoReconnect = true
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
	if v.Settings.SFTPPanelPosition == "" {
		v.Settings.SFTPPanelPosition = d.SFTPPanelPosition
	}
	if v.Settings.Language == "" {
		v.Settings.Language = d.Language
	}
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
		return errors.New("配置文件处于锁定状态，请先输入密码解锁")
	}
	if s.data.Version < vaultSchemaVersion {
		s.data.Version = vaultSchemaVersion
	}
	plain, err := json.Marshal(s.data)
	if err != nil {
		return fmt.Errorf("marshal vault: %w", err)
	}
	sealed, err := s.enc.SealWithPassword(plain, s.password)
	if err != nil {
		return fmt.Errorf("encrypt vault: %w", err)
	}

	tmpPath := s.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, sealed, 0o600); err != nil {
		return fmt.Errorf("write temp vault: %w", err)
	}
	if err := os.Rename(tmpPath, s.filePath); err != nil {
		_ = os.Remove(s.filePath)
		if err2 := os.Rename(tmpPath, s.filePath); err2 != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("commit vault file: %w", err2)
		}
	}
	return nil
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
