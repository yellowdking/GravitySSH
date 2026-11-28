package syncsvc

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"github.com/yellowdking/GravitySSH/internal/vault"
	"github.com/yellowdking/synctool/sdk/go/envelope"
)

var testRelayBinPath string

func findGoExecutable() string {
	candidates := []string{
		`C:\Users\me\AppData\Local\GravitySSHTools\go\bin\go.exe`,
	}
	if goroot := runtime.GOROOT(); goroot != "" {
		exe := "go"
		if runtime.GOOS == "windows" {
			exe = "go.exe"
		}
		candidates = append(candidates, filepath.Join(goroot, "bin", exe))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return "go"
}

func TestMain(m *testing.M) {
	tmpBuildDir, err := os.MkdirTemp("", "gssh-synctool-relay-bin-*")
	if err == nil {
		goExe := findGoExecutable()
		binName := "synctool-relay"
		if runtime.GOOS == "windows" {
			binName += ".exe"
		}
		binPath := filepath.Join(tmpBuildDir, binName)
		buildCmd := exec.Command(goExe, "build", "-o", binPath, "github.com/yellowdking/synctool/cmd/synctool-relay")
		if bOut, bErr := buildCmd.CombinedOutput(); bErr == nil {
			testRelayBinPath = binPath
		} else {
			fmt.Fprintf(os.Stderr, "build synctool-relay failed: %v\n%s\n", bErr, string(bOut))
		}
	}

	code := m.Run()
	if tmpBuildDir != "" {
		_ = os.RemoveAll(tmpBuildDir)
	}
	os.Exit(code)
}

func startLiveRelay(t *testing.T, spoolMode string) string {
	t.Helper()
	if testRelayBinPath == "" {
		t.Fatal("synctool-relay binary was not built in TestMain")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	dataDir := t.TempDir()
	initCmd := exec.Command(testRelayBinPath, "init", "--public-host", "127.0.0.1", "--listen", addr, "--data-dir", dataDir)
	initOut, err := initCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("synctool-relay init failed: %v\n%s", err, string(initOut))
	}
	var relayURI string
	for _, line := range strings.Split(string(initOut), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "synctool://relay?") {
			relayURI = trimmed
			break
		}
	}
	if relayURI == "" {
		t.Fatalf("failed to find synctool://relay URI in init output:\n%s", string(initOut))
	}

	ctx, cancel := context.WithCancel(context.Background())
	serveCmd := exec.CommandContext(ctx, testRelayBinPath, "serve",
		"--listen", addr,
		"--public-host", "127.0.0.1",
		"--profile", "standard",
		"--spool", spoolMode,
		"--data-dir", dataDir,
	)
	if err := serveCmd.Start(); err != nil {
		cancel()
		t.Fatalf("start synctool-relay serve: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = serveCmd.Wait()
	})

	// Wait until Ping succeeds.
	mgr := NewManager(nil, nil)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, pErr := mgr.TestRelay(context.Background(), nil, relayURI); pErr == nil {
			return relayURI
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for live synctool-relay at %s", addr)
	return ""
}

type testPeer struct {
	store     *vault.Store
	mgr       *Manager
	mu        sync.Mutex
	previews  map[string]models.SyncBundle
	events    []models.SyncFlowStatus
	inboxHits int
}

func newTestPeer(t *testing.T, label string) *testPeer {
	t.Helper()
	store, err := vault.Open(filepath.Join(t.TempDir(), "vault.gsv"), nil)
	if err != nil {
		t.Fatalf("vault.Open: %v", err)
	}
	p := &testPeer{
		store:    store,
		previews: make(map[string]models.SyncBundle),
	}
	p.mgr = NewManager(
		func(eventName string, payload any) {
			p.mu.Lock()
			defer p.mu.Unlock()
			switch eventName {
			case "sync:flow_status":
				if st, ok := payload.(models.SyncFlowStatus); ok {
					p.events = append(p.events, st)
				}
			case "sync:inbox_updated":
				p.inboxHits++
			}
		},
		func(bundle models.SyncBundle) models.SyncReceivePreview {
			p.mu.Lock()
			defer p.mu.Unlock()
			id := fmt.Sprintf("pv-%d", len(p.previews)+1)
			p.previews[id] = bundle
			return models.SyncReceivePreview{
				PreviewID:      id,
				SchemaVersion:  bundle.SchemaVersion,
				ExportedAt:     bundle.ExportedAt,
				SourcePlatform: bundle.SourcePlatform,
				SourceDevice:   bundle.SourceDevice,
				ServerCount:    len(bundle.Servers),
				KeyCount:       len(bundle.SSHKeys),
				ShortcutCount:  len(bundle.Shortcuts),
				TemplateCount:  len(bundle.Templates),
			}
		},
	)
	if err := p.mgr.SetDeviceLabel(store, label); err != nil {
		t.Fatalf("SetDeviceLabel: %v", err)
	}
	t.Cleanup(func() {
		p.mgr.CloseAllOnLock()
		_ = store.Close()
	})
	return p
}

func seedVaultSample(t *testing.T, st *vault.Store) models.SSHKeyMeta {
	t.Helper()
	pemStr, _, err := sshkey.GenerateKeyPair("ed25519", "alice@pc", "")
	if err != nil {
		t.Fatal(err)
	}
	keyMeta, err := st.ImportSSHKey("Prod-Ed25519", pemStr, "", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveServer(models.ServerSaveRequest{
		Name:     "HK-Edge-1",
		Host:     "198.51.100.88",
		Port:     2222,
		Username: "ubuntu",
		AuthType: models.AuthBoth,
		Password: "edge-password-secret",
		SSHKeyID: keyMeta.ID,
	}); err != nil {
		t.Fatal(err)
	}
	return keyMeta
}

func waitFlowState(t *testing.T, mgr *Manager, sessionID string, wantStates ...string) models.SyncFlowStatus {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		st, err := mgr.GetFlowStatus(sessionID)
		if err != nil {
			t.Fatalf("GetFlowStatus(%s): %v", sessionID, err)
		}
		for _, w := range wantStates {
			if st.State == w {
				return st
			}
		}
		if st.State == "error" || st.State == "cancelled" {
			t.Fatalf("flow %s entered unexpected terminal state %q: %s", sessionID, st.State, st.Message)
		}
		time.Sleep(20 * time.Millisecond)
	}
	st, _ := mgr.GetFlowStatus(sessionID)
	t.Fatalf("timed out waiting for %v on flow %s (last=%+v)", wantStates, sessionID, st)
	return models.SyncFlowStatus{}
}

func TestParcelCodecAlwaysTwoPartRoleFallbacksAndBombGuard(t *testing.T) {
	bundle := models.SyncBundle{
		SchemaVersion:  1,
		ExportedAt:     "2026-11-28T00:00:00Z",
		SourcePlatform: "windows",
		SourceDevice:   "Alice-Desktop",
		Servers:        []models.ServerConfig{{ID: "s1", Name: "S1", Host: "10.0.0.1", Port: 22, Username: "root", Password: "TOP_SECRET_PASSWORD"}},
		SSHKeys:        []models.SSHKeyRecord{{ID: "k1", Name: "K1", PrivateKeyPEM: "PRIVATE_PEM_SECRET"}},
	}

	parcel, cleanup, err := BuildVaultParcel(bundle)
	if err != nil {
		t.Fatalf("BuildVaultParcel failed: %v", err)
	}
	defer cleanup()

	if parcel.AppID != AppID || parcel.Schema != Schema || len(parcel.Blobs) != 1 {
		t.Fatalf("unexpected parcel structure: %+v", parcel)
	}
	inlineBytes, _ := json.Marshal(parcel.InlinePayload)
	if len(inlineBytes) >= MaxInlineSummaryBytes {
		t.Fatalf("inline summary exceeded 512B: %d", len(inlineBytes))
	}
	if bytes.Contains(inlineBytes, []byte("TOP_SECRET_PASSWORD")) || bytes.Contains(inlineBytes, []byte("PRIVATE_PEM_SECRET")) {
		t.Fatal("CRITICAL: InlinePayload leaked secrets!")
	}
	if parcel.Blobs[0].Role != RoleVaultBundle || parcel.Blobs[0].Encoding != "gzip" || parcel.Blobs[0].MIME != BlobMIMEGzip {
		t.Fatalf("unexpected blob descriptor: %+v", parcel.Blobs[0])
	}

	// Write gzipped stream to a staging file and verify DecodeReceivedParcel gunzips AND wipes/deletes SafePath!
	rc, sz, err := parcel.Blobs[0].OpenStream()
	if err != nil || sz <= 0 {
		t.Fatalf("OpenStream failed: sz=%d err=%v", sz, err)
	}
	stagePath := filepath.Join(t.TempDir(), "part_1.verified")
	gzData := make([]byte, sz)
	_, _ = rc.Read(gzData)
	_ = rc.Close()
	if err := os.WriteFile(stagePath, gzData, 0o600); err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeReceivedParcel(envelope.ReceivedParcel{
		Manifest: envelope.Manifest{
			EnvelopeVersion: 1,
			AppID:           AppID,
			Schema:          Schema,
			InlinePayload:   parcel.InlinePayload,
		},
		Blobs: []envelope.ReceivedBlob{{
			Descriptor: envelope.BlobDescriptor{PartID: 1, Role: RoleVaultDump, Encoding: "gzip"},
			SafePath:   stagePath,
		}},
	})
	if err != nil {
		t.Fatalf("DecodeReceivedParcel (role=vault_dump, gzip) failed: %v", err)
	}
	if len(decoded.Servers) != 1 || decoded.Servers[0].Password != "TOP_SECRET_PASSWORD" {
		t.Fatalf("decoded bundle mismatch: %+v", decoded)
	}
	if _, statErr := os.Stat(stagePath); !os.IsNotExist(statErr) {
		t.Fatal("expected staging SafePath to be wiped and removed by DecodeReceivedParcel")
	}

	// Verify identity encoding + empty-blobs inline_payload fallback + >16MiB gzip bomb rejection.
	rawPath := filepath.Join(t.TempDir(), "raw.json")
	rawJSON, _ := json.Marshal(bundle)
	_ = os.WriteFile(rawPath, rawJSON, 0o600)
	if _, err := DecodeReceivedParcel(envelope.ReceivedParcel{
		Manifest: envelope.Manifest{AppID: AppID, Schema: Schema},
		Blobs: []envelope.ReceivedBlob{{
			Descriptor: envelope.BlobDescriptor{PartID: 1, Role: RoleVaultBundle, Encoding: "identity"},
			SafePath:   rawPath,
		}},
	}); err != nil {
		t.Fatalf("identity blob decode failed: %v", err)
	}

	if _, err := DecodeReceivedParcel(envelope.ReceivedParcel{
		Manifest: envelope.Manifest{
			AppID:         AppID,
			Schema:        Schema,
			InlinePayload: map[string]any{"bundle": string(rawJSON)},
		},
	}); err != nil {
		t.Fatalf("empty-blobs inline_payload fallback failed: %v", err)
	}

	// Gzip bomb (> 16 MiB zeros compressed into ~17 KB) must be rejected cleanly!
	var bombBuf bytes.Buffer
	gzw := gzip.NewWriter(&bombBuf)
	zeros := make([]byte, 1024*1024)
	for i := 0; i < 17; i++ {
		_, _ = gzw.Write(zeros)
	}
	_ = gzw.Close()
	bombPath := filepath.Join(t.TempDir(), "bomb.gz")
	_ = os.WriteFile(bombPath, bombBuf.Bytes(), 0o600)
	if _, err := DecodeReceivedParcel(envelope.ReceivedParcel{
		Manifest: envelope.Manifest{AppID: AppID, Schema: Schema},
		Blobs: []envelope.ReceivedBlob{{
			Descriptor: envelope.BlobDescriptor{PartID: 1, Role: RoleVaultBundle, Encoding: "gzip"},
			SafePath:   bombPath,
		}},
	}); err == nil {
		t.Fatal("expected >16MiB gzip bomb to be rejected")
	}
}

func TestLiveRelayE2EOneshotQRAndSASAndPairedSpoolWatchAckRevoke(t *testing.T) {
	relayURI := startLiveRelay(t, "disk")
	t.Setenv("SYNCTOOL_DEFAULT_RELAY_URI", relayURI)

	alice := newTestPeer(t, "Alice-PC")
	bob := newTestPeer(t, "Bob-Laptop")
	keyMeta := seedVaultSample(t, alice.store)

	// Ping check & secret redaction verification
	pingInfo, err := alice.mgr.TestRelay(context.Background(), alice.store, "")
	if err != nil || pingInfo.SpoolMode != "disk" || pingInfo.CertExpiresAt == "" {
		t.Fatalf("TestRelay failed: %+v err=%v", pingInfo, err)
	}
	ovA, err := alice.mgr.GetOverview(alice.store)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	ovBytes, _ := json.Marshal(ovA)
	if strings.Contains(string(ovBytes), "#k=") || !strings.Contains(ovA.Relay.RedactedURI, "#[REDACTED]") {
		t.Fatalf("GetOverview leaked #k= or missed #[REDACTED]: %s", string(ovBytes))
	}

	// 1. Mode 1 One-Shot QR (m=1)
	ctx := context.Background()
	send1, err := alice.mgr.StartOneShotSend(ctx, alice.store, 1, false)
	if err != nil || send1.PairURI == "" || send1.ServerCount != 1 || send1.KeyCount != 1 {
		t.Fatalf("StartOneShotSend m=1 failed: %+v err=%v", send1, err)
	}
	recv1, err := bob.mgr.StartReceive(ctx, bob.store, send1.PairURI)
	if err != nil {
		t.Fatalf("StartReceive m=1 failed: %v", err)
	}
	recv1Final := waitFlowState(t, bob.mgr, recv1.SessionID, "ready_preview")
	waitFlowState(t, alice.mgr, send1.SessionID, "completed")
	if recv1Final.Preview == nil || recv1Final.Preview.ServerCount != 1 {
		t.Fatalf("expected preview with 1 server, got %+v", recv1Final.Preview)
	}

	// 2. Mode 2 One-Shot SAS (m=2) with 1 wrong initiator attempt + mandatory Responder ConfirmSAS
	send2, err := alice.mgr.StartOneShotSend(ctx, alice.store, 2, false)
	if err != nil || send2.RoomCode == "" {
		t.Fatalf("StartOneShotSend m=2 failed: %+v err=%v", send2, err)
	}
	recv2, err := bob.mgr.StartReceive(ctx, bob.store, send2.RoomCode)
	if err != nil {
		t.Fatalf("StartReceive m=2 failed: %v", err)
	}
	bobSASState := waitFlowState(t, bob.mgr, recv2.SessionID, "awaiting_sas_confirm")
	aliceSASState := waitFlowState(t, alice.mgr, send2.SessionID, "awaiting_sas_input")
	if len(bobSASState.SAS) != 6 || aliceSASState.SASAttemptsRemaining != 3 {
		t.Fatalf("unexpected SAS states: bob=%+v alice=%+v", bobSASState, aliceSASState)
	}
	// Approve on Responder
	if err := bob.mgr.ConfirmSAS(recv2.SessionID, true); err != nil {
		t.Fatalf("ConfirmSAS(true) failed: %v", err)
	}
	// Submit 1 wrong SAS on Initiator -> must decrement attempts to 2 without burning room yet!
	wrongSAS := "000000"
	if bobSASState.SAS == wrongSAS {
		wrongSAS = "999999"
	}
	if err := alice.mgr.SubmitSASInput(send2.SessionID, wrongSAS); err != nil {
		t.Fatalf("SubmitSASInput wrong failed: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := alice.mgr.GetFlowStatus(send2.SessionID)
		if st.State == "awaiting_sas_input" && st.SASAttemptsRemaining == 2 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	// Now submit matching 6-digit SAS -> transfer completes!
	if err := alice.mgr.SubmitSASInput(send2.SessionID, bobSASState.SAS); err != nil {
		t.Fatalf("SubmitSASInput correct failed: %v", err)
	}
	waitFlowState(t, bob.mgr, recv2.SessionID, "ready_preview")
	waitFlowState(t, alice.mgr, send2.SessionID, "completed")

	// 3. Bidirectional Device Bind (custom relay 1-scan import via includeRelay=true!) + 0-RTT Spool + Watch + ACK + Dismiss + Revoke
	charlie := newTestPeer(t, "Charlie-Phone")
	// Configure Alice with explicit custom relay, leave Charlie on broken default env to prove 1-scan import!
	if _, err := alice.mgr.SetCustomRelay(alice.store, relayURI); err != nil {
		t.Fatalf("SetCustomRelay on Alice failed: %v", err)
	}
	t.Setenv("SYNCTOOL_DEFAULT_RELAY_URI", "")

	bindOffer, err := alice.mgr.StartDeviceBind(ctx, alice.store, 1, true)
	if err != nil {
		t.Fatalf("StartDeviceBind failed: %v", err)
	}
	bindAccept, err := charlie.mgr.AcceptDeviceBind(ctx, charlie.store, bindOffer.PairURI)
	if err != nil {
		t.Fatalf("AcceptDeviceBind failed: %v", err)
	}
	waitFlowState(t, alice.mgr, bindOffer.SessionID, "completed")
	charlieDone := waitFlowState(t, charlie.mgr, bindAccept.SessionID, "completed")
	if charlieDone.PairedDevice == nil || charlieDone.PairedDevice.Label != "Alice-PC" {
		t.Fatalf("unexpected paired device on Charlie: %+v", charlieDone.PairedDevice)
	}
	ovCharlie, err := charlie.mgr.GetOverview(charlie.store)
	if err != nil || ovCharlie.Relay.Mode != "custom" || len(ovCharlie.PairedDevices) != 1 {
		t.Fatalf("expected Charlie to auto-import custom relay and 1 paired device: %+v err=%v", ovCharlie, err)
	}

	// Start WatchInbox on Charlie before Alice pushes 0-RTT spool parcel!
	if err := charlie.mgr.StartWatchInbox(ctx, charlie.store); err != nil {
		t.Fatalf("StartWatchInbox failed: %v", err)
	}
	pushRes, err := alice.mgr.SendToDevice(ctx, alice.store, ovCharlie.LocalDeviceID, "auto")
	if err != nil || pushRes.Status != "sent_spool" || pushRes.MsgID == "" {
		t.Fatalf("SendToDevice spool failed: %+v err=%v", pushRes, err)
	}

	// Verify WatchInbox emitted "sync:inbox_updated".
	watchDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(watchDeadline) {
		charlie.mu.Lock()
		hits := charlie.inboxHits
		charlie.mu.Unlock()
		if hits > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	charlie.mu.Lock()
	gotInboxWatch := charlie.inboxHits
	charlie.mu.Unlock()
	if gotInboxWatch == 0 {
		t.Fatal("expected WatchInbox to emit sync:inbox_updated event")
	}

	// Phase-1 ListInbox returns counts without downloading blob; ApplyInboxItem imports & burns!
	items, err := charlie.mgr.ListInbox(ctx, charlie.store)
	if err != nil || len(items) != 1 || items[0].ServerCount != 1 || items[0].KeyCount != 1 || items[0].SenderLabel != "Alice-PC" {
		t.Fatalf("unexpected ListInbox items: %+v err=%v", items, err)
	}
	impSum, err := charlie.mgr.ApplyInboxItem(ctx, charlie.store, items[0].ItemID, "merge")
	if err != nil || impSum.ServersAdded != 1 || impSum.KeysAdded != 1 {
		t.Fatalf("ApplyInboxItem failed: %+v err=%v", impSum, err)
	}
	if len(charlie.store.ListServers()) != 1 || charlie.store.ListSSHKeys()[0].Fingerprint != keyMeta.Fingerprint {
		t.Fatal("imported Charlie vault contents mismatch")
	}
	afterApply, err := charlie.mgr.ListInbox(ctx, charlie.store)
	if err != nil || len(afterApply) != 0 {
		t.Fatalf("expected empty inbox after ACK burn, got %+v err=%v", afterApply, err)
	}

	// Push a second item and DismissInboxItem (zero-blob direct /ack burn)!
	if _, err := alice.mgr.SendToDevice(ctx, alice.store, ovCharlie.LocalDeviceID, "spool"); err != nil {
		t.Fatalf("second SendToDevice failed: %v", err)
	}
	items2, err := charlie.mgr.ListInbox(ctx, charlie.store)
	if err != nil || len(items2) != 1 {
		t.Fatalf("expected 1 item before dismiss, got %+v err=%v", items2, err)
	}
	if err := charlie.mgr.DismissInboxItem(ctx, charlie.store, items2[0].ItemID); err != nil {
		t.Fatalf("DismissInboxItem failed: %v", err)
	}
	if rem, err := charlie.mgr.ListInbox(ctx, charlie.store); err != nil || len(rem) != 0 {
		t.Fatalf("expected 0 items after DismissInboxItem, got %+v err=%v", rem, err)
	}

	// Revoke Alice on Charlie -> Alice's next spool push must be rejected by relay!
	ovAlice, _ := alice.mgr.GetOverview(alice.store)
	if err := charlie.mgr.RevokeDevice(ctx, charlie.store, ovAlice.LocalDeviceID); err != nil {
		t.Fatalf("RevokeDevice failed: %v", err)
	}
	if _, err := alice.mgr.SendToDevice(ctx, alice.store, ovCharlie.LocalDeviceID, "spool"); err == nil {
		t.Fatal("expected spool push using revoked deposit_token to be rejected")
	}
}

func TestLiveRelaySpoolOffSuggestPipeAndDirectPipeTransfer(t *testing.T) {
	noSpoolURI := startLiveRelay(t, "off")
	t.Setenv("SYNCTOOL_DEFAULT_RELAY_URI", noSpoolURI)

	alice := newTestPeer(t, "Alice-NoSpool")
	bob := newTestPeer(t, "Bob-NoSpool")
	seedVaultSample(t, alice.store)
	ctx := context.Background()

	// Pair over --spool=off relay
	bOffer, err := alice.mgr.StartDeviceBind(ctx, alice.store, 1, false)
	if err != nil {
		t.Fatalf("StartDeviceBind on --spool=off failed: %v", err)
	}
	bAcc, err := bob.mgr.AcceptDeviceBind(ctx, bob.store, bOffer.PairURI)
	if err != nil {
		t.Fatalf("AcceptDeviceBind on --spool=off failed: %v", err)
	}
	waitFlowState(t, alice.mgr, bOffer.SessionID, "completed")
	waitFlowState(t, bob.mgr, bAcc.SessionID, "completed")

	ovBob, _ := bob.mgr.GetOverview(bob.store)
	// "auto" on --spool=off must cleanly return status="suggest_pipe" instead of error!
	autoRes, err := alice.mgr.SendToDevice(ctx, alice.store, ovBob.LocalDeviceID, "auto")
	if err != nil || autoRes.Status != "suggest_pipe" {
		t.Fatalf("expected status='suggest_pipe' on --spool=off relay, got %+v err=%v", autoRes, err)
	}

	// Switching to "pipe" streams 0-RTT direct room transfer end-to-end!
	pipeRes, err := alice.mgr.SendToDevice(ctx, alice.store, ovBob.LocalDeviceID, "pipe")
	if err != nil || pipeRes.Status != "pipe_offer_ready" || pipeRes.PairURI == "" {
		t.Fatalf("SendToDevice('pipe') failed: %+v err=%v", pipeRes, err)
	}
	recvDirect, err := bob.mgr.StartReceive(ctx, bob.store, pipeRes.PairURI)
	if err != nil {
		t.Fatalf("StartReceive direct failed: %v", err)
	}
	directPreview := waitFlowState(t, bob.mgr, recvDirect.SessionID, "ready_preview")
	waitFlowState(t, alice.mgr, pipeRes.SessionID, "completed")
	if directPreview.Preview == nil || directPreview.Preview.ServerCount != 1 {
		t.Fatalf("unexpected direct preview: %+v", directPreview.Preview)
	}
}
