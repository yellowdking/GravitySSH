package vault

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func (s *Store) mergeSSHKeys(incoming []models.SSHKeyRecord, sum *models.SyncImportSummary) map[string]string {
	idMap := make(map[string]string, len(incoming))
	for _, k := range incoming {
		origID := k.ID
		if strings.TrimSpace(k.ID) == "" {
			k.ID = uuid.NewString()
		}
		idx := -1
		for i, cur := range s.data.SSHKeys {
			if (origID != "" && cur.ID == origID) || (k.Fingerprint != "" && cur.Fingerprint == k.Fingerprint) {
				idx = i
				break
			}
		}
		if idx >= 0 {
			localID := s.data.SSHKeys[idx].ID
			k.ID = localID
			if k.PrivateKeyPEM == "" {
				k.PrivateKeyPEM = s.data.SSHKeys[idx].PrivateKeyPEM
			}
			if k.Passphrase == "" {
				k.Passphrase = s.data.SSHKeys[idx].Passphrase
			}
			s.data.SSHKeys[idx] = k
			if origID != "" {
				idMap[origID] = localID
			}
			sum.KeysUpdated++
		} else {
			s.data.SSHKeys = append(s.data.SSHKeys, k)
			if origID != "" {
				idMap[origID] = k.ID
			}
			sum.KeysAdded++
		}
	}
	return idMap
}

func serverEndpointKey(srv models.ServerConfig) string {
	port := srv.Port
	if port <= 0 {
		port = 22
	}
	return strings.ToLower(strings.TrimSpace(srv.Host)) + ":" + fmt.Sprintf("%d", port) + ":" + strings.TrimSpace(srv.Username)
}

func (s *Store) mergeServers(incoming []models.ServerConfig, keyMap map[string]string, sum *models.SyncImportSummary) {
	srvIDMap := make(map[string]string, len(incoming))
	prepared := sanitizeServers(incoming, s.data.Settings.DefaultKeepAliveSeconds)
	for i := range prepared {
		if mapped, ok := keyMap[prepared[i].SSHKeyID]; ok {
			prepared[i].SSHKeyID = mapped
		}
		if mapped, ok := keyMap[prepared[i].JumpHost.SSHKeyID]; ok {
			prepared[i].JumpHost.SSHKeyID = mapped
		}
		origID := incoming[i].ID
		ep := serverEndpointKey(prepared[i])
		idx := -1
		for j, cur := range s.data.Servers {
			if (origID != "" && cur.ID == origID) || (prepared[i].Host != "" && serverEndpointKey(cur) == ep) {
				idx = j
				break
			}
		}
		if idx >= 0 {
			localID := s.data.Servers[idx].ID
			prepared[i].ID = localID
			if origID != "" {
				srvIDMap[origID] = localID
			}
			s.data.Servers[idx] = prepared[i]
			sum.ServersUpdated++
		} else {
			if origID != "" {
				srvIDMap[origID] = prepared[i].ID
			}
			s.data.Servers = append(s.data.Servers, prepared[i])
			sum.ServersAdded++
		}
	}
	for i := range s.data.Servers {
		if s.data.Servers[i].JumpHost.Mode == models.JumpServer {
			if mapped, ok := srvIDMap[s.data.Servers[i].JumpHost.ServerID]; ok {
				s.data.Servers[i].JumpHost.ServerID = mapped
			}
		}
	}
}

func (s *Store) mergeShortcuts(incoming []models.CommandShortcut, sum *models.SyncImportSummary) {
	for _, sc := range sanitizeShortcuts(incoming) {
		idx := -1
		for i, cur := range s.data.Shortcuts {
			if cur.ID == sc.ID || (strings.TrimSpace(cur.Name) == strings.TrimSpace(sc.Name) && strings.TrimSpace(cur.Command) == strings.TrimSpace(sc.Command)) {
				idx = i
				break
			}
		}
		if idx >= 0 {
			sc.ID = s.data.Shortcuts[idx].ID
			s.data.Shortcuts[idx] = sc
			sum.ShortcutsUpdated++
		} else {
			s.data.Shortcuts = append(s.data.Shortcuts, sc)
			sum.ShortcutsAdded++
		}
	}
}

func (s *Store) mergeTemplates(incoming []models.BuiltinTemplate, sum *models.SyncImportSummary) {
	for _, tpl := range sanitizeTemplates(incoming) {
		idx := -1
		for i, cur := range s.data.Templates {
			if cur.ID == tpl.ID ||
				(tpl.PresetID != "" && cur.PresetID == tpl.PresetID) ||
				strings.TrimSpace(cur.Name) == strings.TrimSpace(tpl.Name) {
				idx = i
				break
			}
		}
		if idx >= 0 {
			tpl.ID = s.data.Templates[idx].ID
			if tpl.PresetID == "" {
				tpl.PresetID = s.data.Templates[idx].PresetID
			}
			s.data.Templates[idx] = tpl
			sum.TemplatesUpdated++
		} else {
			s.data.Templates = append(s.data.Templates, tpl)
			sum.TemplatesAdded++
		}
	}
}

func sanitizeKeys(in []models.SSHKeyRecord) []models.SSHKeyRecord {
	out := make([]models.SSHKeyRecord, 0, len(in))
	for _, k := range in {
		if strings.TrimSpace(k.ID) == "" {
			k.ID = uuid.NewString()
		}
		out = append(out, k)
	}
	return out
}

func sanitizeServers(in []models.ServerConfig, defaultKeepAlive int) []models.ServerConfig {
	out := make([]models.ServerConfig, 0, len(in))
	for _, srv := range in {
		if strings.TrimSpace(srv.ID) == "" {
			srv.ID = uuid.NewString()
		}
		if srv.Port <= 0 || srv.Port > 65535 {
			srv.Port = 22
		}
		if srv.KeepAliveSeconds <= 0 {
			srv.KeepAliveSeconds = defaultKeepAlive
		}
		if srv.TimeoutSeconds <= 0 {
			srv.TimeoutSeconds = 15
		}
		if srv.Proxy.Type == "" {
			srv.Proxy.Type = models.ProxyNone
		}
		if srv.JumpHost.Mode == "" {
			srv.JumpHost.Mode = models.JumpNone
		}
		out = append(out, srv)
	}
	return out
}

func sanitizeShortcuts(in []models.CommandShortcut) []models.CommandShortcut {
	out := make([]models.CommandShortcut, 0, len(in))
	for _, sc := range in {
		if strings.TrimSpace(sc.Command) == "" {
			continue
		}
		if strings.TrimSpace(sc.ID) == "" {
			sc.ID = uuid.NewString()
		}
		out = append(out, sc)
	}
	return out
}

func sanitizeTemplates(in []models.BuiltinTemplate) []models.BuiltinTemplate {
	out := make([]models.BuiltinTemplate, 0, len(in))
	for _, tpl := range in {
		if strings.TrimSpace(tpl.Name) == "" || strings.TrimSpace(tpl.Command) == "" {
			continue
		}
		if strings.TrimSpace(tpl.ID) == "" {
			tpl.ID = uuid.NewString()
		}
		out = append(out, tpl)
	}
	return out
}
