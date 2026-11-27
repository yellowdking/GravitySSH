package vault

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

// ExportSyncBundle exports a decrypted portable copy of servers, SSH keys,
// shortcuts, and ops templates for multi-device zero-knowledge sync.
func (s *Store) ExportSyncBundle(sourceDevice string) (models.SyncBundle, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return models.SyncBundle{}, errVaultClosed
	}
	if s.locked {
		return models.SyncBundle{}, errors.New("配置文件已锁定，请先解锁后再同步")
	}
	servers := make([]models.ServerConfig, len(s.data.Servers))
	for i, srv := range s.data.Servers {
		if srv.LastConnectedAt != nil {
			t := *srv.LastConnectedAt
			srv.LastConnectedAt = &t
		}
		servers[i] = srv
	}
	keys := make([]models.SSHKeyRecord, len(s.data.SSHKeys))
	copy(keys, s.data.SSHKeys)
	shortcuts := make([]models.CommandShortcut, len(s.data.Shortcuts))
	copy(shortcuts, s.data.Shortcuts)
	templates := make([]models.BuiltinTemplate, len(s.data.Templates))
	copy(templates, s.data.Templates)

	return models.SyncBundle{
		SchemaVersion:  1,
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		SourcePlatform: "windows",
		SourceDevice:   strings.TrimSpace(sourceDevice),
		Servers:        servers,
		SSHKeys:        keys,
		Shortcuts:      shortcuts,
		Templates:      templates,
	}, nil
}

// ImportSyncBundle applies a received SyncBundle using either "merge" (default)
// or "overwrite" mode while keeping local settings and vault password intact.
func (s *Store) ImportSyncBundle(bundle models.SyncBundle, mode string) (models.SyncImportSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return models.SyncImportSummary{}, errVaultClosed
	}
	if s.locked {
		return models.SyncImportSummary{}, errors.New("配置文件已锁定，请先解锁后再同步")
	}
	if bundle.SchemaVersion < 1 || bundle.SchemaVersion > 2 {
		return models.SyncImportSummary{}, fmt.Errorf("不支持的同步数据版本: %d", bundle.SchemaVersion)
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "overwrite" {
		return models.SyncImportSummary{}, fmt.Errorf("无效的同步模式: %s", mode)
	}

	prevServers := cloneServers(s.data.Servers)
	prevKeys := cloneSSHKeys(s.data.SSHKeys)
	prevShortcuts := cloneShortcuts(s.data.Shortcuts)
	prevTemplates := cloneTemplates(s.data.Templates)
	var summary models.SyncImportSummary
	summary.Mode = mode

	if mode == "overwrite" {
		s.data.SSHKeys = sanitizeKeys(bundle.SSHKeys)
		s.data.Servers = sanitizeServers(bundle.Servers, s.data.Settings.DefaultKeepAliveSeconds)
		s.data.Shortcuts = sanitizeShortcuts(bundle.Shortcuts)
		s.data.Templates = sanitizeTemplates(bundle.Templates)
		summary.KeysAdded = len(s.data.SSHKeys)
		summary.ServersAdded = len(s.data.Servers)
		summary.ShortcutsAdded = len(s.data.Shortcuts)
		summary.TemplatesAdded = len(s.data.Templates)
	} else {
		keyMap := s.mergeSSHKeys(bundle.SSHKeys, &summary)
		s.mergeServers(bundle.Servers, keyMap, &summary)
		s.mergeShortcuts(bundle.Shortcuts, &summary)
		s.mergeTemplates(bundle.Templates, &summary)
	}

	if err := s.saveLocked(); err != nil {
		s.data.Servers = prevServers
		s.data.SSHKeys = prevKeys
		s.data.Shortcuts = prevShortcuts
		s.data.Templates = prevTemplates
		return models.SyncImportSummary{}, err
	}
	return summary, nil
}
