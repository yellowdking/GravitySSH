package sshclient

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
		dialCtx := ctx
		var cancel context.CancelFunc
		if timeout > 0 {
			dialCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		if ctxDialer, ok := socksDialer.(proxy.ContextDialer); ok {
			return ctxDialer.DialContext(dialCtx, "tcp", targetAddr)
		}
		return socksDialer.Dial("tcp", targetAddr)

	case models.ProxyHTTP:
		return dialHTTPConnect(ctx, proxyAddr, p.Username, p.Password, targetAddr, timeout)

	default:
		return dialDirect(ctx, targetAddr, timeout)
	}
}

func dialHTTPConnect(ctx context.Context, proxyAddr, username, password, targetAddr string, timeout time.Duration) (net.Conn, error) {
	conn, err := dialDirect(ctx, proxyAddr, timeout)
	if err != nil {
		return nil, fmt.Errorf("无法连接 HTTP 代理 %s: %w", proxyAddr, err)
	}
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	if ctxDeadline, ok := ctx.Deadline(); ok && (deadline.IsZero() || ctxDeadline.Before(deadline)) {
		deadline = ctxDeadline
	}
	if !deadline.IsZero() {
		_ = conn.SetDeadline(deadline)
	}

	stopCancel := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})

	var reqBuilder strings.Builder
	reqBuilder.WriteString(fmt.Sprintf("CONNECT %s HTTP/1.1\r\n", targetAddr))
	reqBuilder.WriteString(fmt.Sprintf("Host: %s\r\n", targetAddr))
	reqBuilder.WriteString("Proxy-Connection: Keep-Alive\r\n")
	if username != "" || password != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		reqBuilder.WriteString(fmt.Sprintf("Proxy-Authorization: Basic %s\r\n", cred))
	}
	reqBuilder.WriteString("\r\n")

	if _, err := io.WriteString(conn, reqBuilder.String()); err != nil {
		stopped := stopCancel()
		_ = conn.Close()
		if !stopped || ctx.Err() != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, context.Canceled
		}
		return nil, fmt.Errorf("发送 HTTP CONNECT 请求失败: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	stopped := stopCancel()
	if err != nil {
		_ = conn.Close()
		if !stopped || ctx.Err() != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, context.Canceled
		}
		return nil, fmt.Errorf("读取 HTTP 代理响应失败: %w", err)
	}
	_ = resp.Body.Close()

	if !stopped || ctx.Err() != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, context.Canceled
	}
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("HTTP 代理拒绝连接: %s", resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})

	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

type bufferedConn struct {
	net.Conn
	r io.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

func upgradeSSHConn(ctx context.Context, conn net.Conn, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	var deadline time.Time
	if cfg.Timeout > 0 {
		deadline = time.Now().Add(cfg.Timeout)
	}
	if ctxDeadline, ok := ctx.Deadline(); ok && (deadline.IsZero() || ctxDeadline.Before(deadline)) {
		deadline = ctxDeadline
	}
	if !deadline.IsZero() {
		_ = conn.SetDeadline(deadline)
	}

	stopCancel := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})

	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	stopped := stopCancel()
	if err != nil {
		if !stopped || ctx.Err() != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, context.Canceled
		}
		return nil, err
	}
	if !stopped || ctx.Err() != nil {
		_ = c.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, context.Canceled
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

func defaultPort(p int) int {
	if p <= 0 || p > 65535 {
		return 22
	}
	return p
}

func formatProxyBadge(p models.ProxyConfig) string {
	return fmt.Sprintf("%s(%s:%d)", strings.ToUpper(string(p.Type)), p.Host, p.Port)
}

func closeAll(clients []*ssh.Client) {
	for i := len(clients) - 1; i >= 0; i-- {
		if clients[i] != nil {
			_ = clients[i].Close()
		}
	}
}
