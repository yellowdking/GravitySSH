package vault

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func toServerView(sc models.ServerConfig) models.ServerView {
	return models.ServerView{
		ID:          sc.ID,
		Name:        sc.Name,
		Group:       sc.Group,
		Color:       sc.Color,
		Host:        sc.Host,
		Port:        sc.Port,
		Username:    sc.Username,
		AuthType:    sc.AuthType,
		HasPassword: sc.Password != "",
		SSHKeyID:    sc.SSHKeyID,
		Proxy: models.ProxyView{
			Type:        sc.Proxy.Type,
			Host:        sc.Proxy.Host,
			Port:        sc.Proxy.Port,
			Username:    sc.Proxy.Username,
			HasPassword: sc.Proxy.Password != "",
		},
		JumpHost: models.JumpHostView{
			Mode:         sc.JumpHost.Mode,
			ServerID:     sc.JumpHost.ServerID,
			Host:         sc.JumpHost.Host,
			Port:         sc.JumpHost.Port,
			Username:     sc.JumpHost.Username,
			AuthType:     sc.JumpHost.AuthType,
			HasPassword:  sc.JumpHost.Password != "",
			SSHKeyID:     sc.JumpHost.SSHKeyID,
			UseMainProxy: sc.JumpHost.UseMainProxy,
		},
		TimeoutSeconds:    sc.TimeoutSeconds,
		KeepAliveSeconds:  sc.KeepAliveSeconds,
		DefaultRemotePath: sc.DefaultRemotePath,
		StartupCommand:    sc.StartupCommand,
		Notes:             sc.Notes,
		CreatedAt:         sc.CreatedAt,
		UpdatedAt:         sc.UpdatedAt,
		LastConnectedAt:   sc.LastConnectedAt,
	}
}

func (s *Store) ListServers() []models.ServerView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.ServerView, 0, len(s.data.Servers))
	for _, srv := range s.data.Servers {
		out = append(out, toServerView(srv))
	}
	return out
}

func (s *Store) GetServerInternal(id string) (*models.ServerConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, srv := range s.data.Servers {
		if srv.ID == id {
			cp := srv
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("server not found: %s", id)
}

func normalizeAuthType(raw models.AuthType, sshKeyID string) models.AuthType {
	switch models.AuthType(strings.ToLower(strings.TrimSpace(string(raw)))) {
	case models.AuthPassword:
		return models.AuthPassword
	case models.AuthKey:
		return models.AuthKey
	case models.AuthBoth:
		return models.AuthBoth
	default:
		if strings.TrimSpace(sshKeyID) != "" {
			return models.AuthKey
		}
		return models.AuthPassword
	}
}

func hasJumpCycle(servers []models.ServerConfig, selfID, jumpServerID string) bool {
	jumpServerID = strings.TrimSpace(jumpServerID)
	if jumpServerID == "" {
		return false
	}
	byID := make(map[string]models.ServerConfig, len(servers))
	for _, srv := range servers {
		byID[srv.ID] = srv
	}
	visited := make(map[string]bool)
	if selfID = strings.TrimSpace(selfID); selfID != "" {
		visited[selfID] = true
	}
	cur := jumpServerID
	for cur != "" {
		if visited[cur] {
			return true
		}
		visited[cur] = true
		next, ok := byID[cur]
		if !ok || next.JumpHost.Mode != models.JumpServer {
			return false
		}
		cur = strings.TrimSpace(next.JumpHost.ServerID)
	}
	return false
}

func normalizeServerConfig(srv *models.ServerConfig, defaultKeepAlive int) bool {
	srv.Host = strings.TrimSpace(srv.Host)
	if srv.Host == "" {
		return false
	}
	srv.Name = strings.TrimSpace(srv.Name)
	srv.Group = strings.TrimSpace(srv.Group)
	srv.Username = strings.TrimSpace(srv.Username)
	if srv.Username == "" {
		srv.Username = "root"
	}
	if srv.Name == "" {
		srv.Name = fmt.Sprintf("%s@%s", srv.Username, srv.Host)
	}
	if srv.Port <= 0 || srv.Port > 65535 {
		srv.Port = 22
	}
	srv.SSHKeyID = strings.TrimSpace(srv.SSHKeyID)
	srv.AuthType = normalizeAuthType(srv.AuthType, srv.SSHKeyID)

	srv.Proxy.Host = strings.TrimSpace(srv.Proxy.Host)
	srv.Proxy.Username = strings.TrimSpace(srv.Proxy.Username)
	switch models.ProxyType(strings.ToLower(strings.TrimSpace(string(srv.Proxy.Type)))) {
	case models.ProxySOCKS5:
		srv.Proxy.Type = models.ProxySOCKS5
		if srv.Proxy.Port <= 0 || srv.Proxy.Port > 65535 {
			srv.Proxy.Port = 1080
		}
	case models.ProxyHTTP:
		srv.Proxy.Type = models.ProxyHTTP
		if srv.Proxy.Port <= 0 || srv.Proxy.Port > 65535 {
			srv.Proxy.Port = 8080
		}
	default:
		srv.Proxy.Type = models.ProxyNone
		if srv.Proxy.Port < 0 || srv.Proxy.Port > 65535 {
			srv.Proxy.Port = 0
		}
	}

	srv.JumpHost.ServerID = strings.TrimSpace(srv.JumpHost.ServerID)
	srv.JumpHost.Host = strings.TrimSpace(srv.JumpHost.Host)
	srv.JumpHost.Username = strings.TrimSpace(srv.JumpHost.Username)
	srv.JumpHost.SSHKeyID = strings.TrimSpace(srv.JumpHost.SSHKeyID)
	switch models.JumpMode(strings.ToLower(strings.TrimSpace(string(srv.JumpHost.Mode)))) {
	case models.JumpServer:
		if srv.JumpHost.ServerID == "" || (srv.ID != "" && srv.JumpHost.ServerID == srv.ID) {
			srv.JumpHost.Mode = models.JumpNone
			srv.JumpHost.ServerID = ""
		} else {
			srv.JumpHost.Mode = models.JumpServer
		}
		if srv.JumpHost.Port < 0 || srv.JumpHost.Port > 65535 {
			srv.JumpHost.Port = 0
		}
	case models.JumpCustom:
		srv.JumpHost.Mode = models.JumpCustom
		if srv.JumpHost.Port <= 0 || srv.JumpHost.Port > 65535 {
			srv.JumpHost.Port = 22
		}
		srv.JumpHost.AuthType = normalizeAuthType(srv.JumpHost.AuthType, srv.JumpHost.SSHKeyID)
	default:
		srv.JumpHost.Mode = models.JumpNone
		srv.JumpHost.ServerID = ""
		if srv.JumpHost.Port < 0 || srv.JumpHost.Port > 65535 {
			srv.JumpHost.Port = 0
		}
		if strings.TrimSpace(string(srv.JumpHost.AuthType)) != "" {
			srv.JumpHost.AuthType = normalizeAuthType(srv.JumpHost.AuthType, srv.JumpHost.SSHKeyID)
		}
	}

	if srv.TimeoutSeconds <= 0 {
		srv.TimeoutSeconds = 15
	}
	if srv.KeepAliveSeconds < 0 {
		if defaultKeepAlive <= 0 {
			defaultKeepAlive = 30
		}
		srv.KeepAliveSeconds = defaultKeepAlive
	}
	srv.DefaultRemotePath = strings.TrimSpace(srv.DefaultRemotePath)
	srv.StartupCommand = strings.TrimSpace(srv.StartupCommand)
	return true
}

func (s *Store) SaveServer(req models.ServerSaveRequest) (models.ServerView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return models.ServerView{}, errVaultClosed
	}
	if s.locked {
		return models.ServerView{}, errVaultLocked
	}

	if strings.TrimSpace(req.Host) == "" {
		return models.ServerView{}, errors.New("服务器地址 (IP / 域名) 不能为空")
	}
	jumpMode := models.JumpMode(strings.ToLower(strings.TrimSpace(string(req.JumpHost.Mode))))
	jumpServerID := strings.TrimSpace(req.JumpHost.ServerID)
	if jumpMode == models.JumpServer {
		if req.ID != "" && jumpServerID == req.ID {
			return models.ServerView{}, errors.New("跳板机不能指向服务器自身")
		}
		if hasJumpCycle(s.data.Servers, req.ID, jumpServerID) {
			return models.ServerView{}, errors.New("跳板机配置存在循环引用")
		}
	}

	now := time.Now()
	idx := -1
	var existing models.ServerConfig
	if req.ID != "" {
		for i, srv := range s.data.Servers {
			if srv.ID == req.ID {
				idx = i
				existing = srv
				break
			}
		}
	}

	password := req.Password
	if req.KeepExistingPassword && password == "" && idx >= 0 {
		password = existing.Password
	}
	proxyPassword := req.Proxy.Password
	if req.Proxy.KeepExistingPassword && proxyPassword == "" && idx >= 0 {
		proxyPassword = existing.Proxy.Password
	}
	jumpPassword := req.JumpHost.Password
	if req.JumpHost.KeepExistingPassword && jumpPassword == "" && idx >= 0 {
		jumpPassword = existing.JumpHost.Password
	}

	cfg := models.ServerConfig{
		ID:       req.ID,
		Name:     req.Name,
		Group:    req.Group,
		Color:    req.Color,
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		AuthType: req.AuthType,
		Password: password,
		SSHKeyID: req.SSHKeyID,
		Proxy: models.ProxyConfig{
			Type:     req.Proxy.Type,
			Host:     req.Proxy.Host,
			Port:     req.Proxy.Port,
			Username: req.Proxy.Username,
			Password: proxyPassword,
		},
		JumpHost: models.JumpHostConfig{
			Mode:         req.JumpHost.Mode,
			ServerID:     req.JumpHost.ServerID,
			Host:         req.JumpHost.Host,
			Port:         req.JumpHost.Port,
			Username:     req.JumpHost.Username,
			AuthType:     req.JumpHost.AuthType,
			Password:     jumpPassword,
			SSHKeyID:     req.JumpHost.SSHKeyID,
			UseMainProxy: req.JumpHost.UseMainProxy,
		},
		TimeoutSeconds:    req.TimeoutSeconds,
		KeepAliveSeconds:  req.KeepAliveSeconds,
		DefaultRemotePath: req.DefaultRemotePath,
		StartupCommand:    req.StartupCommand,
		Notes:             req.Notes,
		UpdatedAt:         now,
	}
	normalizeServerConfig(&cfg, s.data.Settings.DefaultKeepAliveSeconds)

	snapshot := cloneVaultData(s.data)
	if idx >= 0 {
		cfg.CreatedAt = existing.CreatedAt
		cfg.LastConnectedAt = existing.LastConnectedAt
		s.data.Servers[idx] = cfg
	} else {
		cfg.ID = uuid.NewString()
		cfg.CreatedAt = now
		s.data.Servers = append(s.data.Servers, cfg)
	}

	if err := s.saveLocked(); err != nil {
		s.data = snapshot
		return models.ServerView{}, err
	}
	return toServerView(cfg), nil
}

func (s *Store) DeleteServer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errVaultClosed
	}
	if s.locked {
		return errVaultLocked
	}
	for i, srv := range s.data.Servers {
		if srv.ID == id {
			snapshot := cloneVaultData(s.data)
			s.data.Servers = append(s.data.Servers[:i], s.data.Servers[i+1:]...)
			for j := range s.data.Servers {
				if s.data.Servers[j].JumpHost.Mode == models.JumpServer && s.data.Servers[j].JumpHost.ServerID == id {
					s.data.Servers[j].JumpHost.Mode = models.JumpNone
					s.data.Servers[j].JumpHost.ServerID = ""
				}
			}
			if err := s.saveLocked(); err != nil {
				s.data = snapshot
				return err
			}
			return nil
		}
	}
	return nil
}

// BindSSHKeyToServer attaches keyID to a saved server and switches authType to key/both
// while preserving any existing passwords.
func (s *Store) BindSSHKeyToServer(serverID, keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errVaultClosed
	}
	if s.locked {
		return errors.New("配置文件已锁定")
	}
	for i := range s.data.Servers {
		if s.data.Servers[i].ID == serverID {
			prev := s.data.Servers[i]
			s.data.Servers[i].SSHKeyID = keyID
			if s.data.Servers[i].Password != "" {
				s.data.Servers[i].AuthType = models.AuthBoth
			} else {
				s.data.Servers[i].AuthType = models.AuthKey
			}
			s.data.Servers[i].UpdatedAt = time.Now()
			if err := s.saveLocked(); err != nil {
				s.data.Servers[i] = prev
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("服务器不存在: %s", serverID)
}

func (s *Store) MarkServerConnected(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.locked {
		return
	}
	now := time.Now()
	for i := range s.data.Servers {
		if s.data.Servers[i].ID == id {
			s.data.Servers[i].LastConnectedAt = &now
			s.scheduleFlushLocked()
			return
		}
	}
}
