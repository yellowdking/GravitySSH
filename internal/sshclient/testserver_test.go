package sshclient

import (
	"bufio"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"golang.org/x/crypto/ssh"
)

type memResolver struct {
	servers map[string]*models.ServerConfig
	keys    map[string]*models.SSHKeyRecord
}

func (r *memResolver) GetServerInternal(id string) (*models.ServerConfig, error) {
	s, ok := r.servers[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return s, nil
}

func (r *memResolver) GetSSHKeyInternal(id string) (*models.SSHKeyRecord, error) {
	k, ok := r.keys[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return k, nil
}

func startTestSSHServer(t *testing.T, expectedUser, expectedPass string, allowedPubKey ssh.PublicKey) (string, int, func()) {
	t.Helper()
	hostPEM, _, err := sshkey.GenerateKeyPair("ed25519", "host", "")
	if err != nil {
		t.Fatalf("gen host key: %v", err)
	}
	hostSigner, err := ssh.ParsePrivateKey([]byte(hostPEM))
	if err != nil {
		t.Fatalf("parse host key: %v", err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == expectedUser && expectedPass != "" && string(pass) == expectedPass {
				return nil, nil
			}
			return nil, errors.New("invalid password")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == expectedUser && allowedPubKey != nil && string(key.Marshal()) == string(allowedPubKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("invalid key")
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				sConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				defer sConn.Close()
				go ssh.DiscardRequests(reqs)
				for newChan := range chans {
					if newChan.ChannelType() != "direct-tcpip" {
						_ = newChan.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					var payload struct {
						DestAddr string
						DestPort uint32
						OrigAddr string
						OrigPort uint32
					}
					if err := ssh.Unmarshal(newChan.ExtraData(), &payload); err != nil {
						_ = newChan.Reject(ssh.Prohibited, "bad payload")
						continue
					}
					dest := net.JoinHostPort(payload.DestAddr, strconv.Itoa(int(payload.DestPort)))
					remote, err := net.DialTimeout("tcp", dest, 5*time.Second)
					if err != nil {
						_ = newChan.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					ch, chReqs, err := newChan.Accept()
					if err != nil {
						_ = remote.Close()
						continue
					}
					go ssh.DiscardRequests(chReqs)
					go func() { _, _ = io.Copy(ch, remote); _ = ch.Close() }()
					go func() { _, _ = io.Copy(remote, ch); _ = remote.Close() }()
				}
			}(c)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, func() { _ = ln.Close() }
}

func startTestHTTPConnectProxy(t *testing.T, expectedUser, expectedPass string) (string, int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen proxy: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(clientConn net.Conn) {
				defer clientConn.Close()
				br := bufio.NewReader(clientConn)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != http.MethodConnect {
					return
				}
				if expectedUser != "" {
					want := "Basic " + base64.StdEncoding.EncodeToString([]byte(expectedUser+":"+expectedPass))
					if req.Header.Get("Proxy-Authorization") != want {
						_, _ = io.WriteString(clientConn, "HTTP/1.1 407 Proxy Authentication Required\r\n\r\n")
						return
					}
				}
				upstream, err := net.DialTimeout("tcp", req.Host, 5*time.Second)
				if err != nil {
					_, _ = io.WriteString(clientConn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
					return
				}
				defer upstream.Close()
				_, _ = io.WriteString(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
				go io.Copy(upstream, br)
				_, _ = io.Copy(clientConn, upstream)
			}(c)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, func() { _ = ln.Close() }
}
