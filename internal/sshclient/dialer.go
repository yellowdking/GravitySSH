package sshclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"golang.org/x/crypto/ssh"
)

const maxJumpDepth = 6

type CredentialResolver interface {
	GetServerInternal(id string) (*models.ServerConfig, error)
	GetSSHKeyInternal(id string) (*models.SSHKeyRecord, error)
}

type DialResult struct {
	Client      *ssh.Client
	JumpClients []*ssh.Client
	ViaSummary  string
}

func (d *DialResult) Close() {
	if d.Client != nil {
		_ = d.Client.Close()
	}
	for i := len(d.JumpClients) - 1; i >= 0; i-- {
		if d.JumpClients[i] != nil {
			_ = d.JumpClients[i].Close()
		}
	}
}

func DialServer(ctx context.Context, cfg *models.ServerConfig, resolver CredentialResolver) (*DialResult, error) {
	return dialServerRecursive(ctx, cfg, resolver, make(map[string]bool), 0)
}

func dialServerRecursive(
	ctx context.Context,
	cfg *models.ServerConfig,
	resolver CredentialResolver,
	visited map[string]bool,
	depth int,
) (*DialResult, error) {
	if cfg == nil {
		return nil, errors.New("服务器配置为空")
	}
	if depth > maxJumpDepth {
		return nil, fmt.Errorf("跳板机层级超过最大限制 (%d 层)", maxJumpDepth)
	}
	if cfg.ID != "" {
		if visited[cfg.ID] {
			return nil, fmt.Errorf("检测到循环跳板机配置: %s", cfg.Name)
		}
		visited[cfg.ID] = true
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	targetAddr := net.JoinHostPort(cfg.Host, strconv.Itoa(defaultPort(cfg.Port)))
	sshCfg, err := buildSSHClientConfig(cfg.Username, cfg.AuthType, cfg.Password, cfg.SSHKeyID, resolver, timeout)
	if err != nil {
		return nil, err
	}

	var (
		conn        net.Conn
		jumpClients []*ssh.Client
		viaParts    []string
	)

	switch cfg.JumpHost.Mode {
	case models.JumpServer:
		conn, jumpClients, viaParts, err = dialViaJumpServer(ctx, cfg, targetAddr, resolver, visited, depth, timeout)
	case models.JumpCustom:
		conn, jumpClients, viaParts, err = dialViaCustomJump(ctx, cfg, targetAddr, resolver, timeout)
	default:
		conn, viaParts, err = dialTargetConn(ctx, cfg.Proxy, targetAddr, timeout)
	}
	if err != nil {
		return nil, err
	}

	client, err := upgradeSSHConn(ctx, conn, targetAddr, sshCfg)
	if err != nil {
		_ = conn.Close()
		closeAll(jumpClients)
		return nil, fmt.Errorf("SSH 认证/握手失败 (%s): %w", targetAddr, err)
	}

	return &DialResult{
		Client:      client,
		JumpClients: jumpClients,
		ViaSummary:  strings.Join(viaParts, " → "),
	}, nil
}

func dialViaJumpServer(
	ctx context.Context,
	cfg *models.ServerConfig,
	targetAddr string,
	resolver CredentialResolver,
	visited map[string]bool,
	depth int,
	timeout time.Duration,
) (net.Conn, []*ssh.Client, []string, error) {
	if cfg.JumpHost.ServerID == "" {
		return nil, nil, nil, errors.New("已启用跳板机模式，但未选择跳板服务器")
	}
	jumpSrv, err := resolver.GetServerInternal(cfg.JumpHost.ServerID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("读取跳板机配置失败: %w", err)
	}
	nextVisited := make(map[string]bool, len(visited))
	for k, v := range visited {
		nextVisited[k] = v
	}
	jumpRes, err := dialServerRecursive(ctx, jumpSrv, resolver, nextVisited, depth+1)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("连接跳板机 [%s] 失败: %w", jumpSrv.Name, err)
	}
	jumpClients := append(jumpRes.JumpClients, jumpRes.Client)
	var viaParts []string
	if jumpRes.ViaSummary != "" {
		viaParts = append(viaParts, jumpRes.ViaSummary)
	}
	viaParts = append(viaParts, fmt.Sprintf("跳板(%s)", jumpSrv.Name))

	conn, err := dialContextOverSSH(ctx, jumpRes.Client, targetAddr, timeout)
	if err != nil {
		closeAll(jumpClients)
		return nil, nil, nil, fmt.Errorf("通过跳板机 [%s] 连接目标 %s 失败: %w", jumpSrv.Name, targetAddr, err)
	}
	return conn, jumpClients, viaParts, nil
}

func dialViaCustomJump(
	ctx context.Context,
	cfg *models.ServerConfig,
	targetAddr string,
	resolver CredentialResolver,
	timeout time.Duration,
) (net.Conn, []*ssh.Client, []string, error) {
	if strings.TrimSpace(cfg.JumpHost.Host) == "" {
		return nil, nil, nil, errors.New("自定义跳板机地址不能为空")
	}
	jumpAddr := net.JoinHostPort(cfg.JumpHost.Host, strconv.Itoa(defaultPort(cfg.JumpHost.Port)))
	jumpUser := cfg.JumpHost.Username
	if jumpUser == "" {
		jumpUser = cfg.Username
	}
	jumpAuthType := cfg.JumpHost.AuthType
	if jumpAuthType == "" {
		if cfg.JumpHost.SSHKeyID != "" {
			jumpAuthType = models.AuthKey
		} else {
			jumpAuthType = models.AuthPassword
		}
	}
	jumpSSHCfg, err := buildSSHClientConfig(jumpUser, jumpAuthType, cfg.JumpHost.Password, cfg.JumpHost.SSHKeyID, resolver, timeout)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("自定义跳板机认证配置错误: %w", err)
	}

	var (
		jumpRawConn net.Conn
		viaParts    []string
	)
	if cfg.JumpHost.UseMainProxy && cfg.Proxy.Type != "" && cfg.Proxy.Type != models.ProxyNone {
		jumpRawConn, err = dialViaProxy(ctx, cfg.Proxy, jumpAddr, timeout)
		viaParts = append(viaParts, formatProxyBadge(cfg.Proxy))
	} else {
		jumpRawConn, err = dialDirect(ctx, jumpAddr, timeout)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("连接自定义跳板机 %s 失败: %w", jumpAddr, err)
	}

	jumpClient, err := upgradeSSHConn(ctx, jumpRawConn, jumpAddr, jumpSSHCfg)
	if err != nil {
		_ = jumpRawConn.Close()
		return nil, nil, nil, fmt.Errorf("自定义跳板机 %s SSH 握手失败: %w", jumpAddr, err)
	}
	jumpClients := []*ssh.Client{jumpClient}
	viaParts = append(viaParts, fmt.Sprintf("跳板(%s)", cfg.JumpHost.Host))

	conn, err := dialContextOverSSH(ctx, jumpClient, targetAddr, timeout)
	if err != nil {
		closeAll(jumpClients)
		return nil, nil, nil, fmt.Errorf("通过自定义跳板机连接目标 %s 失败: %w", targetAddr, err)
	}
	return conn, jumpClients, viaParts, nil
}

func dialTargetConn(ctx context.Context, proxyCfg models.ProxyConfig, targetAddr string, timeout time.Duration) (net.Conn, []string, error) {
	if proxyCfg.Type != "" && proxyCfg.Type != models.ProxyNone {
		conn, err := dialViaProxy(ctx, proxyCfg, targetAddr, timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("通过 %s 代理连接 %s 失败: %w", strings.ToUpper(string(proxyCfg.Type)), targetAddr, err)
		}
		return conn, []string{formatProxyBadge(proxyCfg)}, nil
	}
	conn, err := dialDirect(ctx, targetAddr, timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("连接 %s 失败: %w", targetAddr, err)
	}
	return conn, nil, nil
}
