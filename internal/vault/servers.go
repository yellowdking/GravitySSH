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
	if srv.AuthType == "" {
		if srv.SSHKeyID != "" {
			srv.AuthType = models.AuthKey
		} else {
			srv.AuthType = models.AuthPassword
		}
	}
	srv.Proxy.Host = strings.TrimSpace(srv.Proxy.Host)
	srv.Proxy.Username = strings.TrimSpace(srv.Proxy.Username)
	if srv.Proxy.Type == "" {
		srv.Proxy.Type = models.ProxyNone
	}
	srv.JumpHost.Host = strings.TrimSpace(srv.JumpHost.Host)
	srv.JumpHost.Username = strings.TrimSpace(srv.JumpHost.Username)
	if srv.JumpHost.Mode == "" {
		srv.JumpHost.Mode = models.JumpNone
	}
	if srv.TimeoutSeconds <= 0 {
		srv.TimeoutSeconds = 15
	}
	if srv.KeepAliveSeconds <= 0 {
		srv.KeepAliveSeconds = defaultKeepAlive
	}
	srv.DefaultRemotePath = strings.TrimSpace(srv.DefaultRemotePath)
	srv.StartupCommand = strings.TrimSpace(srv.StartupCommand)
	return true
}

func (s *Store) SaveServer(req models.ServerSaveRequest) (models.ServerView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return models.ServerView{}, errVaultLocked
	}

	if strings.TrimSpace(req.Host) == "" {
		return models.ServerView{}, errors.New("服务器地址 (IP / 域名) 不能为空")
	}
	if req.JumpHost.Mode == models.JumpServer && req.ID != "" && req.JumpHost.ServerID == req.ID {
		return models.ServerView{}, errors.New("跳板机不能指向服务器自身")
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
	if s.locked {
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
