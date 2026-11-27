package main

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func sampleTestBundle() models.SyncBundle {
	now := time.Now().UTC()
	return models.SyncBundle{
		SchemaVersion:  1,
		ExportedAt:     now.Format(time.RFC3339),
		SourcePlatform: "windows",
		SourceDevice:   "test-host",
		Servers: []models.ServerConfig{
			{
				ID:              "srv-1",
				Name:            "prod-1",
				Host:            "10.0.0.1",
				Port:            22,
				Username:        "root",
				Password:        "s3cr3t-password",
				LastConnectedAt: &now,
			},
		},
		SSHKeys: []models.SSHKeyRecord{
			{
				ID:            "key-1",
				Name:          "prod-key",
				PrivateKeyPEM: "-----BEGIN OPENSSH PRIVATE KEY-----\nSECRET\n-----END OPENSSH PRIVATE KEY-----",
				Passphrase:    "key-pass",
			},
		},
		Shortcuts: []models.CommandShortcut{
			{ID: "sc-1", Name: "top", Command: "htop"},
		},
		Templates: []models.BuiltinTemplate{
			{ID: "tpl-1", Name: "disk", Command: "df -h"},
		},
	}
}

func TestSyncPreviewCacheApproveIsolationAndZeroing(t *testing.T) {
	cache := newSyncPreviewCache()
	defer cache.close()

	orig := sampleTestBundle()
	cache.putSend(&pendingSyncSendSession{
		sessionID:   "sess-1",
		channelID:   "chan-1",
		senderToken: "tok-1",
		sendCode:    "ABCD1234",
		relayURL:    "https://relay.example.com",
		bundle:      orig,
		hasBundle:   true,
		expiresAt:   time.Now().Add(5 * time.Minute),
	})

	// Zeroing the caller's original bundle must not affect cached copy.
	zeroSyncBundle(&orig)

	// getSend must not expose bundle slices.
	meta, ok := cache.getSend("sess-1")
	if !ok {
		t.Fatal("expected getSend to find sess-1")
	}
	if len(meta.bundle.Servers) != 0 || len(meta.bundle.SSHKeys) != 0 {
		t.Fatalf("expected getSend to omit bundle slices, got servers=%d keys=%d", len(meta.bundle.Servers), len(meta.bundle.SSHKeys))
	}

	// beginSendApprove atomically locks the session and returns a private deep copy.
	approveMeta, bundleCopy, err := cache.beginSendApprove("sess-1")
	if err != nil {
		t.Fatalf("beginSendApprove failed: %v", err)
	}
	defer zeroSyncBundle(&bundleCopy)
	if approveMeta.channelID != "chan-1" {
		t.Fatalf("unexpected approveMeta: %+v", approveMeta)
	}

	// Duplicate concurrent beginSendApprove must be rejected while approving.
	if _, _, dupErr := cache.beginSendApprove("sess-1"); dupErr == nil {
		t.Fatal("expected duplicate beginSendApprove to fail while already approving")
	}

	// Concurrently cancelling/clearing/closing the cache while marshaling bundleCopy
	// must NOT race with or zero bundleCopy.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			raw, err := json.Marshal(bundleCopy)
			if err != nil {
				t.Errorf("marshal failed: %v", err)
				return
			}
			var decoded models.SyncBundle
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Errorf("unmarshal failed: %v", err)
				return
			}
			if len(decoded.Servers) != 1 || decoded.Servers[0].Password != "s3cr3t-password" ||
				len(decoded.SSHKeys) != 1 || decoded.SSHKeys[0].Passphrase != "key-pass" {
				t.Errorf("bundleCopy was corrupted/zeroed concurrently: %+v", decoded)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		cache.popSend("sess-1")
		cache.clear("")
	}()
	wg.Wait()

	if cache.isSendApproving("sess-1") {
		t.Fatal("expected isSendApproving to return false after popSend/clear")
	}
}

func TestSyncPreviewCacheAbortAndRetryApprove(t *testing.T) {
	cache := newSyncPreviewCache()
	defer cache.close()

	cache.putSend(&pendingSyncSendSession{
		sessionID:   "sess-retry",
		channelID:   "chan-r",
		senderToken: "tok-r",
		sendCode:    "WXYZ5678",
		relayURL:    "https://relay.example.com",
		bundle:      sampleTestBundle(),
		hasBundle:   true,
		expiresAt:   time.Now().Add(5 * time.Minute),
	})

	_, copy1, err := cache.beginSendApprove("sess-retry")
	if err != nil {
		t.Fatalf("first beginSendApprove failed: %v", err)
	}
	zeroSyncBundle(&copy1)
	cache.abortSendApprove("sess-retry")

	// Retry after abort must succeed and still have intact bundle data.
	_, copy2, err := cache.beginSendApprove("sess-retry")
	if err != nil {
		t.Fatalf("retry beginSendApprove failed: %v", err)
	}
	defer zeroSyncBundle(&copy2)
	if len(copy2.Servers) != 1 || copy2.Servers[0].Password != "s3cr3t-password" {
		t.Fatalf("retry bundle corrupted: %+v", copy2)
	}

	if !cache.markSendApproved("sess-retry", "req-1") {
		t.Fatal("expected markSendApproved to succeed")
	}
	if _, _, err := cache.beginSendApprove("sess-retry"); err == nil {
		t.Fatal("expected beginSendApprove to fail after markSendApproved")
	}
}

func TestSyncPreviewCacheClockSkewGrace(t *testing.T) {
	cache := newSyncPreviewCache()
	defer cache.close()

	now := time.Now()
	// Client clock leading relay by 119s (within allowed +-120s relay skew, under 135s grace):
	// relay expiresAt appears 119s in the past locally, but sessions MUST NOT be pruned yet.
	skewedExp := now.Add(-119 * time.Second)
	cache.putSend(&pendingSyncSendSession{
		sessionID:   "sess-skew-ok",
		channelID:   "chan-skew",
		senderToken: "tok-skew",
		sendCode:    "ABCD2345",
		relayURL:    "https://relay.example.com",
		bundle:      sampleTestBundle(),
		hasBundle:   true,
		expiresAt:   skewedExp,
	})
	cache.putReceive(&pendingSyncReceiveSession{
		waitID:    "wait-skew-ok",
		channelID: "chan-skew",
		sendCode:  "ABCD2345",
		recvCode:  "WXYZ6789",
		requestID: "req-skew",
		relayURL:  "https://relay.example.com",
		expiresAt: skewedExp,
	})

	if _, ok := cache.getSend("sess-skew-ok"); !ok {
		t.Fatal("expected pending send within 135s clock-skew grace to remain cached")
	}
	if _, ok := cache.getReceive("wait-skew-ok"); !ok {
		t.Fatal("expected pending receive within 135s clock-skew grace to remain cached")
	}

	// Past the 135s clock-skew grace window, both sessions MUST be pruned.
	expiredPastGrace := now.Add(-(syncClockSkewGrace + 2*time.Second))
	cache.putSend(&pendingSyncSendSession{
		sessionID:   "sess-expired",
		channelID:   "chan-exp",
		senderToken: "tok-exp",
		sendCode:    "EFGH2345",
		relayURL:    "https://relay.example.com",
		bundle:      sampleTestBundle(),
		hasBundle:   true,
		expiresAt:   expiredPastGrace,
	})
	cache.putReceive(&pendingSyncReceiveSession{
		waitID:    "wait-expired",
		channelID: "chan-exp",
		sendCode:  "EFGH2345",
		recvCode:  "JKLM6789",
		requestID: "req-exp",
		relayURL:  "https://relay.example.com",
		expiresAt: expiredPastGrace,
	})

	if _, ok := cache.getSend("sess-expired"); ok {
		t.Fatal("expected pending send past 135s skew grace to be pruned")
	}
	if _, ok := cache.getReceive("wait-expired"); ok {
		t.Fatal("expected pending receive past 135s skew grace to be pruned")
	}
	// Earlier skewed sessions still within 135s grace remain intact.
	if _, ok := cache.getSend("sess-skew-ok"); !ok {
		t.Fatal("expected valid skewed send session to survive pruning of expired sessions")
	}
	if _, ok := cache.getReceive("wait-skew-ok"); !ok {
		t.Fatal("expected valid skewed receive session to survive pruning of expired sessions")
	}
}
