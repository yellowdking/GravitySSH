package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshclient"
	"github.com/yellowdking/GravitySSH/internal/syncclient"
	"github.com/yellowdking/GravitySSH/internal/vault"
)

func newTestAppWithVault(t *testing.T) (*App, *vault.Store) {
	t.Helper()
	store, err := vault.Open(filepath.Join(t.TempDir(), "vault.gsv"), nil)
	if err != nil {
		t.Fatalf("vault.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	app := NewApp()
	app.mu.Lock()
	app.store = store
	app.sshMgr = sshclient.NewManager(store, nil)
	app.mu.Unlock()
	t.Cleanup(func() { app.shutdown(nil) })
	return app, store
}

func TestAppVersionLifecycleNilCtxGuardsAndReadSSHKeyFileBounded(t *testing.T) {
	rawVersion, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	wantVersion := strings.TrimSpace(string(rawVersion))
	if AppVersion != wantVersion || AppVersion != "1.4.3" {
		t.Fatalf("expected fallback AppVersion=%q (1.4.3), got %q", wantVersion, AppVersion)
	}

	app, store := newTestAppWithVault(t)

	// Nil a.ctx must be rejected cleanly by dialog-backed RPCs instead of panicking.
	if _, err := app.PickAndReadSSHKeyFile(); err == nil {
		t.Fatal("expected PickAndReadSSHKeyFile with nil ctx to return error")
	}
	if _, err := app.SFTPDownloadFolderDialog("s1", "/tmp"); err == nil {
		t.Fatal("expected SFTPDownloadFolderDialog with nil ctx to return error")
	}
	if _, err := app.SFTPUploadDialog("s1", "/tmp"); err == nil {
		t.Fatal("expected SFTPUploadDialog with nil ctx to return error")
	}
	if _, err := app.SFTPDownloadDialog("s1", "/tmp/a"); err == nil {
		t.Fatal("expected SFTPDownloadDialog with nil ctx to return error")
	}
	// OpenExternalURL with nil ctx must be a safe no-op.
	app.OpenExternalURL("https://example.com")

	// readSSHKeyFileBounded rejects dirs and oversized files and reads valid regular files.
	tmpDir := t.TempDir()
	if _, err := readSSHKeyFileBounded(tmpDir, maxSSHKeyFileSize); err == nil {
		t.Fatal("expected directory to be rejected by readSSHKeyFileBounded")
	}
	okFile := filepath.Join(tmpDir, "id_test")
	if err := os.WriteFile(okFile, []byte("PRIVATE-KEY-CONTENT"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readSSHKeyFileBounded(okFile, 64)
	if err != nil || got != "PRIVATE-KEY-CONTENT" {
		t.Fatalf("readSSHKeyFileBounded valid file: got=%q err=%v", got, err)
	}
	bigFile := filepath.Join(tmpDir, "big.key")
	if err := os.WriteFile(bigFile, []byte(strings.Repeat("A", 128)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSSHKeyFileBounded(bigFile, 64); err == nil {
		t.Fatal("expected oversized key file to be rejected")
	}

	// ClearSSHHostCache and ClearAllSSHCache must require unlocked vault (ensureUnlocked).
	if err := store.SetPassword("", "vault-pass"); err != nil {
		t.Fatal(err)
	}
	vaultPath := store.LockState().VaultPath
	_ = store.Close()

	lockedStore, err := vault.Open(vaultPath, nil)
	if err != nil || !lockedStore.IsLocked() {
		t.Fatalf("expected locked vault, err=%v", err)
	}
	defer lockedStore.Close()
	app.mu.Lock()
	app.store = lockedStore
	app.mu.Unlock()

	if _, err := app.ClearSSHHostCache("10.0.0.1", 22); err == nil {
		t.Fatal("expected ClearSSHHostCache rejected while vault is locked")
	}
	if _, err := app.ClearAllSSHCache(); err == nil {
		t.Fatal("expected ClearAllSSHCache rejected while vault is locked")
	}
}

func TestPollSyncSendInspectsPoppedApprovedFlagOnRace(t *testing.T) {
	app, _ := newTestAppWithVault(t)

	inPoll := make(chan struct{})
	releasePoll := make(chan struct{})
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-GSSH-Client") != syncclient.ClientHeaderValue ||
			r.Header.Get("X-GSSH-Sig") != syncclient.SignRequest(syncclient.AppGateKey, r.Method, r.URL.Path, r.Header.Get("X-GSSH-TS"), body) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/v1/pair/poll" {
			close(inPoll)
			<-releasePoll
			// Simulate slot burned-after-read by receiver while poll was in flight.
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	app.syncCache.putSend(&pendingSyncSendSession{
		sessionID:   "sess-race",
		channelID:   strings.Repeat("a", 64),
		senderToken: strings.Repeat("b", 64),
		sendCode:    "ABCD2345",
		relayURL:    srv.URL,
		bundle:      sampleTestBundle(),
		hasBundle:   true,
		expiresAt:   time.Now().Add(5 * time.Minute),
	})

	type pollRes struct {
		res models.SyncSendPollResult
		err error
	}
	ch := make(chan pollRes, 1)
	go func() {
		r, err := app.PollSyncSend("sess-race")
		ch <- pollRes{res: r, err: err}
	}()

	<-inPoll
	// While PollSyncSend holds a stale snapshot with approved=false, ApproveSyncSend finishes.
	if !app.syncCache.markSendApproved("sess-race", "req-live-42") {
		t.Fatal("markSendApproved failed")
	}
	close(releasePoll)

	got := <-ch
	if got.err != nil {
		t.Fatalf("PollSyncSend failed: %v", got.err)
	}
	if got.res.Status != "consumed" || got.res.RequestID != "req-live-42" {
		t.Fatalf("expected status='consumed' and requestId='req-live-42' from popped live session, got %+v", got.res)
	}
}

func TestApplySyncReceivePreservesPreviewOnErrorAndDeletesOnSuccess(t *testing.T) {
	app, store := newTestAppWithVault(t)

	bundle := models.SyncBundle{
		SchemaVersion:  1,
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		SourcePlatform: "android",
		SourceDevice:   "Pixel",
		Servers: []models.ServerConfig{
			{ID: "sync-srv-1", Name: "Synced", Host: "10.8.0.1", Port: 22, Username: "root", Password: "secret"},
		},
	}
	previewID := app.syncCache.put(bundle)

	// Invalid mode fails ImportSyncBundle -> preview MUST remain intact for retry.
	if _, err := app.ApplySyncReceive(previewID, "invalid-mode"); err == nil {
		t.Fatal("expected error for invalid import mode")
	}

	// Retry with valid mode MUST succeed using preserved preview.
	sum, err := app.ApplySyncReceive(previewID, "merge")
	if err != nil || sum.ServersAdded != 1 || len(store.ListServers()) != 1 {
		t.Fatalf("expected retry ApplySyncReceive to succeed, sum=%+v err=%v", sum, err)
	}

	// Second apply after success must fail because preview was deleted & zeroed on success.
	if _, err := app.ApplySyncReceive(previewID, "merge"); err == nil {
		t.Fatal("expected preview to be removed after successful import")
	}
}

func TestSyncClientsCacheBoundedAtMaxCapacity(t *testing.T) {
	app, _ := newTestAppWithVault(t)

	for i := 1; i <= maxCachedSyncClients+10; i++ {
		urlStr := fmt.Sprintf("https://198.51.100.%d:54843", i)
		if _, _, err := app.syncClient(urlStr); err != nil {
			t.Fatalf("syncClient(%s) failed: %v", urlStr, err)
		}
	}

	app.syncClientMu.Lock()
	size := len(app.syncClients)
	app.syncClientMu.Unlock()
	if size > maxCachedSyncClients {
		t.Fatalf("syncClients map exceeded cap %d: got %d", maxCachedSyncClients, size)
	}
	if size != maxCachedSyncClients {
		t.Fatalf("expected syncClients cap %d, got %d", maxCachedSyncClients, size)
	}
}
