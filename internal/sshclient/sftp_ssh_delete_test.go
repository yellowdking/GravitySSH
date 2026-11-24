package sshclient

import (
	"encoding/binary"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"golang.org/x/crypto/ssh"
)

func startMockSSHSFTPServer(t *testing.T, fs *mFS, execFn func(cmd string) (string, uint32)) *ssh.Client {
	t.Helper()
	pem, _, _ := sshkey.GenerateKeyPair("ed25519", "h", "")
	signer, _ := ssh.ParsePrivateKey([]byte(pem))
	scfg := &ssh.ServerConfig{NoClientAuth: true}
	scfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(nc net.Conn) {
				defer nc.Close()
				sc, chs, reqs, err := ssh.NewServerConn(nc, scfg)
				if err != nil {
					return
				}
				defer sc.Close()
				go ssh.DiscardRequests(reqs)
				for chReq := range chs {
					if chReq.ChannelType() != "session" {
						_ = chReq.Reject(ssh.UnknownChannelType, "")
						continue
					}
					ch, cr, err := chReq.Accept()
					if err != nil {
						continue
					}
					go func(ch ssh.Channel, cr <-chan *ssh.Request) {
						defer ch.Close()
						for r := range cr {
							switch r.Type {
							case "subsystem":
								if len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp" {
									_ = r.Reply(true, nil)
									srv := sftp.NewRequestServer(ch, sftp.Handlers{FileGet: fs, FilePut: fs, FileCmd: fs, FileList: fs})
									_ = srv.Serve()
									return
								}
								_ = r.Reply(false, nil)
							case "exec":
								cmd := ""
								if len(r.Payload) >= 4 {
									n := binary.BigEndian.Uint32(r.Payload[:4])
									if int(4+n) <= len(r.Payload) {
										cmd = string(r.Payload[4 : 4+n])
									}
								}
								_ = r.Reply(true, nil)
								out, code := execFn(cmd)
								_, _ = ch.Write([]byte(out))
								var st [4]byte
								binary.BigEndian.PutUint32(st[:], code)
								_, _ = ch.SendRequest("exit-status", false, st[:])
								return
							default:
								_ = r.Reply(false, nil)
							}
						}
					}(ch, cr)
				}
			}(c)
		}
	}()
	cli, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{User: "u", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

func TestSSHFastPathChrootSafetyAndMetricsSingleFlight(t *testing.T) {
	const d755 = os.ModeDir | 0o755
	fs := &mFS{m: map[string]*mNode{
		"/": {"/", "", d755}, "/r": {"r", "", d755},
		"/r/big": {"big", "", d755}, "/r/big/a": {"a", "", 0o644},
		"/r/fb": {"fb", "", d755}, "/r/fb/b": {"b", "", 0o644},
		"/r/fallback": {"fallback", "", d755}, "/r/fallback/c": {"c", "", 0o644},
	}}
	var sshRmCount atomic.Int32
	cli := startMockSSHSFTPServer(t, fs, func(cmd string) (string, uint32) {
		if strings.Contains(cmd, "pwd -P") {
			return "/r\n", 0
		}
		if strings.Contains(cmd, "rm -rf") {
			sshRmCount.Add(1)
			if strings.Contains(cmd, "/r/big") {
				fs.mu.Lock()
				for k := range fs.m {
					if k == "/r/big" || strings.HasPrefix(k, "/r/big/") {
						delete(fs.m, k)
					}
				}
				fs.mu.Unlock()
				return "", 0
			}
			return "restricted shell", 1
		}
		time.Sleep(120 * time.Millisecond)
		return "UNSUPPORTED\n", 0
	})

	m := NewManager(nil, nil)
	s := &sessionState{dialRes: &DialResult{Client: cli}, dirCache: map[string]dirCacheEntry{}}
	m.sessions["s1"] = s

	// Chroot safety: mock SFTP Getwd is "/", so sshMatchesSFTPRoot must be false
	if err := m.SFTPDelete("s1", "/r/fb"); err != nil {
		t.Fatalf("chroot-safe SFTP delete: %v", err)
	}
	if sshRmCount.Load() != 0 {
		t.Fatalf("expected 0 ssh rm calls when sftpWD=='/', got %d", sshRmCount.Load())
	}

	// Non-chrooted fast path
	s.mu.Lock()
	s.sshSameFSChecked, s.sshSameFS = true, true
	s.mu.Unlock()
	if err := m.SFTPDelete("s1", "/r/big"); err != nil {
		t.Fatalf("ssh fast-path dir delete: %v", err)
	}
	if sshRmCount.Load() != 1 || fs.m["/r/big"] != nil {
		t.Fatalf("expected 1 ssh rm fast-path delete, count=%d", sshRmCount.Load())
	}

	// SSH exec fails (restricted shell) -> transparently falls back to SFTP recursive delete
	if err := m.SFTPDelete("s1", "/r/fallback"); err != nil || fs.m["/r/fallback"] != nil {
		t.Fatalf("fallback to SFTP after SSH rm error: %v", err)
	}

	// Metrics single-flight guard prevents overlapping exec channels on high latency
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := m.GetSessionMetrics("s1")
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	busyCnt := 0
	for e := range errs {
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			busyCnt++
		}
	}
	if busyCnt != 1 {
		t.Fatalf("expected 1 concurrent metrics call rejected by single-flight guard, got %d", busyCnt)
	}
}
