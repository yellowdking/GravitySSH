package vault

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"golang.org/x/crypto/ssh"
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
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return uuid.NewString()
	}
	return trimmed
}

func formatAuthorizedPubKey(basePub, comment string) string {
	basePub = strings.TrimSpace(basePub)
	if comment != "" {
		return basePub + " " + comment
	}
	return basePub
}

func parseStrictPublicKey(rawPub, comment string) (keyType, fingerprint, cleanPub string, ok bool) {
	if strings.ContainsAny(rawPub, "\x00\r\n") {
		return "", "", "", false
	}
	trimmed := strings.TrimSpace(rawPub)
	if trimmed == "" {
		return "", "", "", false
	}
	pub, parsedComment, opts, rest, err := ssh.ParseAuthorizedKey([]byte(trimmed))
	if err != nil || len(opts) > 0 || len(bytes.TrimSpace(rest)) > 0 {
		return "", "", "", false
	}
	base := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if base == "" {
		return "", "", "", false
	}
	c := sshkey.SanitizeComment(comment)
	if c == "" {
		c = sshkey.SanitizeComment(parsedComment)
	}
	return pub.Type(), ssh.FingerprintSHA256(pub), formatAuthorizedPubKey(base, c), true
}

func sanitizeIncomingSSHKey(raw models.SSHKeyRecord) (models.SSHKeyRecord, bool, bool) {
	k := raw
	k.ID = strings.TrimSpace(k.ID)
	k.Name = strings.TrimSpace(k.Name)
	k.KeyType = strings.TrimSpace(k.KeyType)
	k.Fingerprint = strings.TrimSpace(k.Fingerprint)
	k.PublicKey = strings.TrimSpace(k.PublicKey)
	k.PrivateKeyPEM = strings.TrimSpace(k.PrivateKeyPEM)
	k.Comment = sshkey.SanitizeComment(k.Comment)

	if k.PrivateKeyPEM != "" {
		parsed, err := sshkey.ParsePrivateKey(k.PrivateKeyPEM, k.Passphrase)
		if err != nil {
			return models.SSHKeyRecord{}, false, false
		}
		k.KeyType = parsed.KeyType
		k.Fingerprint = parsed.Fingerprint
		pubComment := k.Comment
		if pubComment == "" && k.PublicKey != "" && !strings.ContainsAny(k.PublicKey, "\x00\r\n") {
			if pub, cmt, opts, rest, pErr := ssh.ParseAuthorizedKey([]byte(k.PublicKey)); pErr == nil && len(opts) == 0 && len(bytes.TrimSpace(rest)) == 0 && ssh.FingerprintSHA256(pub) == parsed.Fingerprint {
				pubComment = sshkey.SanitizeComment(cmt)
			}
		}
		k.PublicKey = formatAuthorizedPubKey(parsed.PublicKey, pubComment)
		if k.Name == "" {
			k.Name = k.KeyType
		}
		return k, true, true
	}

	if k.PublicKey != "" {
		kt, fp, cleanPub, ok := parseStrictPublicKey(k.PublicKey, k.Comment)
		if !ok {
			return models.SSHKeyRecord{}, false, false
		}
		k.KeyType = kt
		k.Fingerprint = fp
		k.PublicKey = cleanPub
		return k, false, true
	}

	// Metadata-only update (no PEM, no PublicKey) is only usable if it can match an existing local key.
	if k.ID == "" && k.Fingerprint == "" {
		return models.SSHKeyRecord{}, false, false
	}
	return k, false, true
}

func (s *Store) mergeSSHKeys(incoming []models.SSHKeyRecord, sum *models.SyncImportSummary) map[string]string {
	idMap := make(map[string]string, len(incoming))
	for _, rawKey := range incoming {
		k, hasPEM, ok := sanitizeIncomingSSHKey(rawKey)
		if !ok {
			continue
		}
		origID := k.ID
		idx := slices.IndexFunc(s.data.SSHKeys, func(cur models.SSHKeyRecord) bool {
			return (origID != "" && cur.ID == origID) || (k.Fingerprint != "" && cur.Fingerprint == k.Fingerprint)
		})
		if idx >= 0 {
			local := s.data.SSHKeys[idx]
			k.ID = local.ID
			fillEmptyString(&k.Name, local.Name)
			if !hasPEM {
				// Preserve local private key & matching public identity so incoming metadata
				// cannot overwrite local.PublicKey/Fingerprint while keeping local.PrivateKeyPEM.
				k.PrivateKeyPEM = local.PrivateKeyPEM
				k.Passphrase = local.Passphrase
				if local.PrivateKeyPEM != "" {
					if local.KeyType != "" {
						k.KeyType = local.KeyType
					}
					if local.Fingerprint != "" {
						k.Fingerprint = local.Fingerprint
					}
					if local.PublicKey != "" {
						k.PublicKey = local.PublicKey
					}
				} else {
					fillEmptyString(&k.KeyType, local.KeyType)
					fillEmptyString(&k.Fingerprint, local.Fingerprint)
					fillEmptyString(&k.PublicKey, local.PublicKey)
				}
			}
			preserveZeroTime(&k.CreatedAt, local.CreatedAt)
			s.data.SSHKeys[idx] = k
			if origID != "" {
				idMap[origID] = local.ID
			}
			sum.KeysUpdated++
		} else {
			if !hasPEM {
				// Do not insert blank or private-key-less records as brand new vault SSH keys.
				continue
			}
			k.ID = recordIDOrNew(origID)
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

func sameProxyEndpoint(a, b models.ProxyConfig) bool {
	if a.Type == "" || a.Type == models.ProxyNone || a.Type != b.Type {
		return false
	}
	hostA := strings.TrimSpace(a.Host)
	if hostA == "" {
		return false
	}
	return strings.EqualFold(hostA, strings.TrimSpace(b.Host)) &&
		a.Port == b.Port &&
		strings.TrimSpace(a.Username) == strings.TrimSpace(b.Username)
}

func effectiveJumpPort(port int) int {
	if port <= 0 || port > 65535 {
		return 22
	}
	return port
}

func sameCustomJumpEndpoint(a, b models.JumpHostConfig) bool {
	if a.Mode != models.JumpCustom || b.Mode != models.JumpCustom {
		return false
	}
	hostA := strings.TrimSpace(a.Host)
	if hostA == "" {
		return false
	}
	return strings.EqualFold(hostA, strings.TrimSpace(b.Host)) &&
		effectiveJumpPort(a.Port) == effectiveJumpPort(b.Port) &&
		strings.TrimSpace(a.Username) == strings.TrimSpace(b.Username)
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
			if serverEndpointKey(local) == ep && srv.AuthType != models.AuthKey {
				fillEmptyString(&srv.Password, local.Password)
			}
			if sameProxyEndpoint(srv.Proxy, local.Proxy) {
				fillEmptyString(&srv.Proxy.Password, local.Proxy.Password)
			}
			if sameCustomJumpEndpoint(srv.JumpHost, local.JumpHost) && srv.JumpHost.AuthType != models.AuthKey {
				fillEmptyString(&srv.JumpHost.Password, local.JumpHost.Password)
			}
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
	breakJumpCycles(s.data.Servers, importedIndices)
}

func breakJumpCycles(servers []models.ServerConfig, indices []int) {
	for _, idx := range indices {
		if idx < 0 || idx >= len(servers) {
			continue
		}
		srv := &servers[idx]
		if srv.JumpHost.Mode != models.JumpServer {
			continue
		}
		jumpID := strings.TrimSpace(srv.JumpHost.ServerID)
		if jumpID == "" || jumpID == srv.ID || hasJumpCycle(servers, srv.ID, jumpID) {
			srv.JumpHost.Mode = models.JumpNone
			srv.JumpHost.ServerID = ""
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
				return cur.PresetID == tpl.PresetID || (cur.PresetID == "" && cur.ID == tpl.PresetID)
			})
		} else {
			// Custom templates (PresetID == "") must never match or overwrite built-in presets by ID or Name.
			idx = slices.IndexFunc(s.data.Templates, func(cur models.BuiltinTemplate) bool {
				return cur.PresetID == "" && !validPresets[cur.ID] && cur.ID == tpl.ID
			})
			if idx < 0 {
				name := strings.TrimSpace(tpl.Name)
				idx = slices.IndexFunc(s.data.Templates, func(cur models.BuiltinTemplate) bool {
					return cur.PresetID == "" && !validPresets[cur.ID] && strings.TrimSpace(cur.Name) == name
				})
			}
		}

		if idx >= 0 {
			local := s.data.Templates[idx]
			tpl.ID = local.ID
			// Always preserve local PresetID so custom templates cannot claim a preset's PresetID
			// and existing presets never lose their PresetID.
			if local.PresetID != "" {
				tpl.PresetID = local.PresetID
			} else if tpl.PresetID != "" && validPresets[tpl.PresetID] && local.ID == tpl.PresetID {
				tpl.PresetID = local.ID
			} else {
				tpl.PresetID = ""
			}
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
	seenID := make(map[string]bool, len(in))
	out := make([]models.SSHKeyRecord, 0, len(in))
	for _, rawKey := range in {
		k, hasPEM, ok := sanitizeIncomingSSHKey(rawKey)
		if !ok || !hasPEM {
			continue
		}
		k.ID = recordIDOrNew(k.ID)
		if seenID[k.ID] {
			k.ID = uuid.NewString()
		}
		seenID[k.ID] = true
		out = append(out, k)
	}
	return out
}

func sanitizeServer(srv models.ServerConfig, defaultKeepAlive int) (models.ServerConfig, bool) {
	srv.ID = recordIDOrNew(srv.ID)
	if !normalizeServerConfig(&srv, defaultKeepAlive) {
		return models.ServerConfig{}, false
	}
	return srv, true
}

func sanitizeServers(in []models.ServerConfig, defaultKeepAlive int) []models.ServerConfig {
	out := make([]models.ServerConfig, 0, len(in))
	indices := make([]int, 0, len(in))
	for _, srv := range in {
		if clean, ok := sanitizeServer(srv, defaultKeepAlive); ok {
			indices = append(indices, len(out))
			out = append(out, clean)
		}
	}
	breakJumpCycles(out, indices)
	return out
}

func sanitizeShortcuts(in []models.CommandShortcut) []models.CommandShortcut {
	out := make([]models.CommandShortcut, 0, len(in))
	for _, sc := range in {
		sc.Command = strings.TrimSpace(sc.Command)
		if sc.Command == "" {
			continue
		}
		sc.Name = strings.TrimSpace(sc.Name)
		if sc.Name == "" {
			sc.Name = sc.Command
		}
		sc.Category = strings.TrimSpace(sc.Category)
		sc.Description = strings.TrimSpace(sc.Description)
		sc.ID = recordIDOrNew(sc.ID)
		out = append(out, sc)
	}
	return out
}

func sanitizeTemplates(in []models.BuiltinTemplate) []models.BuiltinTemplate {
	validPresets := builtinPresetIDSet()
	seenPreset := make(map[string]bool)
	seenID := make(map[string]bool)
	out := make([]models.BuiltinTemplate, 0, len(in))
	for _, tpl := range in {
		tpl.Name = strings.TrimSpace(tpl.Name)
		tpl.Command = strings.TrimSpace(tpl.Command)
		if tpl.Name == "" || tpl.Command == "" {
			continue
		}
		tpl.ID = strings.TrimSpace(tpl.ID)
		tpl.PresetID = strings.TrimSpace(tpl.PresetID)
		if tpl.PresetID != "" {
			// Accept a preset identity only if it matches a genuine built-in preset, is not duplicated,
			// and its ID is either empty or matches that preset ID (prevents forging PresetID on custom IDs).
			if !validPresets[tpl.PresetID] || seenPreset[tpl.PresetID] || (tpl.ID != "" && tpl.ID != tpl.PresetID) {
				tpl.PresetID = ""
			} else {
				seenPreset[tpl.PresetID] = true
				tpl.ID = tpl.PresetID
			}
		}
		if tpl.PresetID == "" {
			// Custom templates must never retain a built-in preset ID or duplicate another template's ID.
			if tpl.ID == "" || validPresets[tpl.ID] || seenID[tpl.ID] {
				tpl.ID = uuid.NewString()
			}
		}
		seenID[tpl.ID] = true
		out = append(out, tpl)
	}
	return out
}
