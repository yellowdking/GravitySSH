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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}, nil
}

func dialDirect(ctx context.Context, addr string, timeout time.Duration) (net.Conn, error) {
	d := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, "tcp", addr)
}

func dialContextOverSSH(ctx context.Context, client *ssh.Client, addr string, timeout time.Duration) (net.Conn, error) {
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := client.Dial("tcp", addr)
		ch <- res{c: c, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("dial %s via jump host timed out after %s", addr, timeout)
	case r := <-ch:
		return r.c, r.err
	}
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
		baseDialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
		socksDialer, err := proxy.SOCKS5("tcp", proxyAddr, auth, baseDialer)
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

func dialHTTPConnect(ctx context.Context, proxyAddr, username, password, targetAddr string, timeout time.Duration) (net.Conn, error) {
	conn, err := dialDirect(ctx, proxyAddr, timeout)
	if err != nil {
		return nil, fmt.Errorf("无法连接 HTTP 代理 %s: %w", proxyAddr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))

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
		_ = conn.Close()
		return nil, fmt.Errorf("发送 HTTP CONNECT 请求失败: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("读取 HTTP 代理响应失败: %w", err)
	}
	_ = resp.Body.Close()

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

func upgradeSSHConn(conn net.Conn, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	if cfg.Timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(cfg.Timeout))
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		return nil, err
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
