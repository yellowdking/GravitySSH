package vault

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func fillEmptyString(dst *string, fallback string) {
	if *dst == "" {
		*dst = fallback
	}
}

func preserveZeroTime(dst *time.Time, fallback time.Time) {
	if dst.IsZero() && !fallback.IsZero() {
		*dst = fallback
	}
}

func remapID(idMap map[string]string, id string) string {
	if mapped, ok := idMap[id]; ok {
		return mapped
	}
	return id
}

func recordIDOrNew(id string) string {
	if strings.TrimSpace(id) == "" {
		return uuid.NewString()
	}
	return id
}

func (s *Store) mergeSSHKeys(incoming []models.SSHKeyRecord, sum *models.SyncImportSummary) map[string]string {
	idMap := make(map[string]string, len(incoming))
	for _, k := range incoming {
		origID := k.ID
		k.ID = recordIDOrNew(k.ID)
		idx := slices.IndexFunc(s.data.SSHKeys, func(cur models.SSHKeyRecord) bool {
			return (origID != "" && cur.ID == origID) || (k.Fingerprint != "" && cur.Fingerprint == k.Fingerprint)
		})
		if idx >= 0 {
			local := s.data.SSHKeys[idx]
			k.ID = local.ID
			fillEmptyString(&k.PrivateKeyPEM, local.PrivateKeyPEM)
			fillEmptyString(&k.Passphrase, local.Passphrase)
			preserveZeroTime(&k.CreatedAt, local.CreatedAt)
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
		srv.SSHKeyID = remapID(keyMap, srv.SSHKeyID)
		srv.JumpHost.SSHKeyID = remapID(keyMap, srv.JumpHost.SSHKeyID)
		ep := serverEndpointKey(srv)
		idx := slices.IndexFunc(s.data.Servers, func(cur models.ServerConfig) bool {
			return (origID != "" && cur.ID == origID) || (srv.Host != "" && serverEndpointKey(cur) == ep)
		})
		if idx >= 0 {
			local := s.data.Servers[idx]
			srv.ID = local.ID
			fillEmptyString(&srv.Password, local.Password)
			fillEmptyString(&srv.Proxy.Password, local.Proxy.Password)
			fillEmptyString(&srv.JumpHost.Password, local.JumpHost.Password)
			if srv.LastConnectedAt == nil && local.LastConnectedAt != nil {
				t := *local.LastConnectedAt
				srv.LastConnectedAt = &t
			}
			preserveZeroTime(&srv.CreatedAt, local.CreatedAt)
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
			s.data.Servers[idx].JumpHost.ServerID = remapID(srvIDMap, s.data.Servers[idx].JumpHost.ServerID)
		}
	}
}

func (s *Store) mergeShortcuts(incoming []models.CommandShortcut, sum *models.SyncImportSummary) {
	for _, sc := range sanitizeShortcuts(incoming) {
		idx := slices.IndexFunc(s.data.Shortcuts, func(cur models.CommandShortcut) bool {
			return cur.ID == sc.ID || (strings.TrimSpace(cur.Name) == strings.TrimSpace(sc.Name) && strings.TrimSpace(cur.Command) == strings.TrimSpace(sc.Command))
		})
		if idx >= 0 {
			local := s.data.Shortcuts[idx]
			sc.ID = local.ID
			if sc.UseCount < local.UseCount {
				sc.UseCount = local.UseCount
			}
			preserveZeroTime(&sc.CreatedAt, local.CreatedAt)
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
			idx = slices.IndexFunc(s.data.Templates, func(cur models.BuiltinTemplate) bool {
				return cur.PresetID == tpl.PresetID || cur.ID == tpl.PresetID
			})
		}
		if idx < 0 {
			idx = slices.IndexFunc(s.data.Templates, func(cur models.BuiltinTemplate) bool {
				return cur.ID == tpl.ID
			})
		}
		// Match by Name only between custom templates so a custom template can never hijack a preset (or vice versa).
		if idx < 0 && tpl.PresetID == "" {
			name := strings.TrimSpace(tpl.Name)
			idx = slices.IndexFunc(s.data.Templates, func(cur models.BuiltinTemplate) bool {
				return cur.PresetID == "" && strings.TrimSpace(cur.Name) == name
			})
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
		k.ID = recordIDOrNew(k.ID)
		out = append(out, k)
	}
	return out
}

func sanitizeServer(srv models.ServerConfig, defaultKeepAlive int) (models.ServerConfig, bool) {
	if !normalizeServerConfig(&srv, defaultKeepAlive) {
		return models.ServerConfig{}, false
	}
	srv.ID = recordIDOrNew(srv.ID)
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
		sc.ID = recordIDOrNew(sc.ID)
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
		tpl.ID = recordIDOrNew(tpl.ID)
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
