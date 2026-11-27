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
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

func buildSSHClientConfig(
	username string,
	authType models.AuthType,
	password string,
	sshKeyID string,
	resolver CredentialResolver,
	timeout time.Duration,
) (*ssh.ClientConfig, error) {
	if strings.TrimSpace(username) == "" {
		username = "root"
	}

	var methods []ssh.AuthMethod
	wantKey := authType == models.AuthKey || authType == models.AuthBoth
	wantPass := authType == models.AuthPassword || authType == models.AuthBoth

	if wantKey && sshKeyID != "" {
		if resolver == nil {
			return nil, errors.New("无法加载 SSH 密钥：vault resolver 为空")
		}
		keyRec, err := resolver.GetSSHKeyInternal(sshKeyID)
		if err != nil {
			return nil, err
		}
		parsed, err := sshkey.ParsePrivateKey(keyRec.PrivateKeyPEM, keyRec.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("解析已保存的 SSH 密钥 [%s] 失败: %w", keyRec.Name, err)
		}
		methods = append(methods, ssh.PublicKeys(parsed.Signer))
	}

	if wantPass && password != "" {
		pw := password
		methods = append(methods,
			ssh.Password(pw),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = pw
				}
				return answers, nil
			}),
		)
	}

	if len(methods) == 0 {
		return nil, errors.New("请配置密码或选择已保存的 SSH 密钥")
	}

	return &ssh.ClientConfig{
		User:            username,
		Auth:            methods,
		HostKeyCallback: defaultTOFUHostKeyCallback(),
		Timeout:         timeout,
	}, nil
}

const dialerFallbackDelay = 100 * time.Millisecond

func newNetDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout:       timeout,
		KeepAlive:     30 * time.Second,
		FallbackDelay: dialerFallbackDelay,
	}
}

func dialDirect(ctx context.Context, addr string, timeout time.Duration) (net.Conn, error) {
	return newNetDialer(timeout).DialContext(ctx, "tcp", addr)
}

func dialContextOverSSH(ctx context.Context, client *ssh.Client, addr string, timeout time.Duration) (net.Conn, error) {
	dialCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		dialCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return client.DialContext(dialCtx, "tcp", addr)
}

func dialViaProxy(ctx context.Context, p models.ProxyConfig, targetAddr string, timeout time.Duration) (net.Conn, error) {
	if strings.TrimSpace(p.Host) == "" || p.Port <= 0 {
		return nil, errors.New("代理地址或端口配置无效")
	}
	proxyAddr := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))

	switch p.Type {
	case models.ProxySOCKS5:
		var auth *proxy.Auth
		if p.Username != "" || p.Password != "" {
			auth = &proxy.Auth{User: p.Username, Password: p.Password}
		}
		socksDialer, err := proxy.SOCKS5("tcp", proxyAddr, auth, newNetDialer(timeout))
		if err != nil {
			return nil, fmt.Errorf("初始化 SOCKS5 代理失败: %w", err)
		}
		if ctxDialer, ok := socksDialer.(proxy.ContextDialer); ok {
			return ctxDialer.DialContext(ctx, "tcp", targetAddr)
		}
		return socksDialer.Dial("tcp", targetAddr)

	case models.ProxyHTTP:
		return dialHTTPConnect(ctx, proxyAddr, p.Username, p.Password, targetAddr, timeout)

	default:
		return dialDirect(ctx, targetAddr, timeout)
	}
}
