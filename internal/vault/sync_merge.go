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
			local := s.data.SSHKeys[idx]
			k.ID = local.ID
			if k.PrivateKeyPEM == "" {
				k.PrivateKeyPEM = local.PrivateKeyPEM
			}
			if k.Passphrase == "" {
				k.Passphrase = local.Passphrase
			}
			if k.CreatedAt.IsZero() && !local.CreatedAt.IsZero() {
				k.CreatedAt = local.CreatedAt
			}
			s.data.SSHKeys[idx] = k
			if origID != "" {
				idMap[origID] = local.ID
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
	importedIndices := make([]int, 0, len(incoming))
	defaultKeepAlive := s.data.Settings.DefaultKeepAliveSeconds

	for _, rawSrv := range incoming {
		origID := rawSrv.ID
		srv, ok := sanitizeServer(rawSrv, defaultKeepAlive)
		if !ok {
			continue
		}
		if mapped, ok := keyMap[srv.SSHKeyID]; ok {
			srv.SSHKeyID = mapped
		}
		if mapped, ok := keyMap[srv.JumpHost.SSHKeyID]; ok {
			srv.JumpHost.SSHKeyID = mapped
		}
		ep := serverEndpointKey(srv)
		idx := -1
		for j, cur := range s.data.Servers {
			if (origID != "" && cur.ID == origID) || (srv.Host != "" && serverEndpointKey(cur) == ep) {
				idx = j
				break
			}
		}
		if idx >= 0 {
			local := s.data.Servers[idx]
			srv.ID = local.ID
			if srv.Password == "" && local.Password != "" {
				srv.Password = local.Password
			}
			if srv.Proxy.Password == "" && local.Proxy.Password != "" {
				srv.Proxy.Password = local.Proxy.Password
			}
			if srv.JumpHost.Password == "" && local.JumpHost.Password != "" {
				srv.JumpHost.Password = local.JumpHost.Password
			}
			if srv.LastConnectedAt == nil && local.LastConnectedAt != nil {
				t := *local.LastConnectedAt
				srv.LastConnectedAt = &t
			}
			if srv.CreatedAt.IsZero() && !local.CreatedAt.IsZero() {
				srv.CreatedAt = local.CreatedAt
			}
			if origID != "" {
				srvIDMap[origID] = local.ID
			}
			s.data.Servers[idx] = srv
			importedIndices = append(importedIndices, idx)
			sum.ServersUpdated++
		} else {
			if origID != "" {
				srvIDMap[origID] = srv.ID
			}
			s.data.Servers = append(s.data.Servers, srv)
			importedIndices = append(importedIndices, len(s.data.Servers)-1)
			sum.ServersAdded++
		}
	}

	// Only remap JumpHost.ServerID on servers imported in this batch so untouched local servers are never mutated.
	for _, idx := range importedIndices {
		if s.data.Servers[idx].JumpHost.Mode == models.JumpServer {
			if mapped, ok := srvIDMap[s.data.Servers[idx].JumpHost.ServerID]; ok {
				s.data.Servers[idx].JumpHost.ServerID = mapped
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
			local := s.data.Shortcuts[idx]
			sc.ID = local.ID
			if sc.UseCount < local.UseCount {
				sc.UseCount = local.UseCount
			}
			if sc.CreatedAt.IsZero() && !local.CreatedAt.IsZero() {
				sc.CreatedAt = local.CreatedAt
			}
			s.data.Shortcuts[idx] = sc
			sum.ShortcutsUpdated++
		} else {
			s.data.Shortcuts = append(s.data.Shortcuts, sc)
			sum.ShortcutsAdded++
		}
	}
}

func (s *Store) mergeTemplates(incoming []models.BuiltinTemplate, sum *models.SyncImportSummary) {
	validPresets := builtinPresetIDSet()
	for _, tpl := range sanitizeTemplates(incoming) {
		idx := -1
		if tpl.PresetID != "" {
			for i, cur := range s.data.Templates {
				if cur.PresetID == tpl.PresetID || cur.ID == tpl.PresetID {
					idx = i
					break
				}
			}
		}
		if idx < 0 {
			for i, cur := range s.data.Templates {
				if cur.ID == tpl.ID {
					idx = i
					break
				}
			}
		}
		// Match by Name only between custom templates so a custom template can never hijack a preset (or vice versa).
		if idx < 0 && tpl.PresetID == "" {
			name := strings.TrimSpace(tpl.Name)
			for i, cur := range s.data.Templates {
				if cur.PresetID == "" && strings.TrimSpace(cur.Name) == name {
					idx = i
					break
				}
			}
		}

		if idx >= 0 {
			local := s.data.Templates[idx]
			tpl.ID = local.ID
			// Always preserve local PresetID so custom templates cannot claim a preset's PresetID
			// and existing presets never lose their PresetID.
			tpl.PresetID = local.PresetID
			s.data.Templates[idx] = tpl
			sum.TemplatesUpdated++
		} else {
			if tpl.PresetID != "" && (!validPresets[tpl.PresetID] || tpl.ID != tpl.PresetID) {
				tpl.PresetID = ""
			}
			if tpl.PresetID == "" && validPresets[tpl.ID] {
				tpl.ID = uuid.NewString()
			}
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

func sanitizeServer(srv models.ServerConfig, defaultKeepAlive int) (models.ServerConfig, bool) {
	srv.Host = strings.TrimSpace(srv.Host)
	if srv.Host == "" {
		return models.ServerConfig{}, false
	}
	srv.Username = strings.TrimSpace(srv.Username)
	if srv.Username == "" {
		srv.Username = "root"
	}
	srv.Name = strings.TrimSpace(srv.Name)
	if srv.Name == "" {
		srv.Name = fmt.Sprintf("%s@%s", srv.Username, srv.Host)
	}
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
	return srv, true
}

func sanitizeServers(in []models.ServerConfig, defaultKeepAlive int) []models.ServerConfig {
	out := make([]models.ServerConfig, 0, len(in))
	for _, srv := range in {
		if clean, ok := sanitizeServer(srv, defaultKeepAlive); ok {
			out = append(out, clean)
		}
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
	validPresets := builtinPresetIDSet()
	seenPreset := make(map[string]bool)
	out := make([]models.BuiltinTemplate, 0, len(in))
	for _, tpl := range in {
		if strings.TrimSpace(tpl.Name) == "" || strings.TrimSpace(tpl.Command) == "" {
			continue
		}
		if strings.TrimSpace(tpl.ID) == "" {
			tpl.ID = uuid.NewString()
		}
		if tpl.PresetID != "" {
			if !validPresets[tpl.PresetID] || seenPreset[tpl.PresetID] {
				tpl.PresetID = ""
			} else {
				seenPreset[tpl.PresetID] = true
			}
		}
		out = append(out, tpl)
	}
	return out
}
