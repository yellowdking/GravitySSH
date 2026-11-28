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

func TestSyncPreviewCacheCloneIsolationEvictionAndZeroing(t *testing.T) {
	cache := newSyncPreviewCache()
	defer cache.close()

	orig := sampleTestBundle()
	id1 := cache.put(orig)
	if id1 == "" {
		t.Fatal("expected non-empty preview ID")
	}
	// Zeroing caller's original bundle must not mutate cached copy.
	zeroSyncBundle(&orig)

	cloned, ok := cache.peekClone(id1)
	if !ok {
		t.Fatal("expected peekClone to find id1")
	}
	defer zeroSyncBundle(&cloned)
	if len(cloned.Servers) != 1 || cloned.Servers[0].Password != "s3cr3t-password" ||
		len(cloned.SSHKeys) != 1 || cloned.SSHKeys[0].Passphrase != "key-pass" {
		t.Fatalf("unexpected peekClone bundle: %+v", cloned)
	}

	// Concurrent clear/close while marshaling cloned copy must be 100% race-free.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			raw, err := json.Marshal(cloned)
			if err != nil {
				t.Errorf("marshal failed: %v", err)
				return
			}
			var dec models.SyncBundle
			if err := json.Unmarshal(raw, &dec); err != nil || len(dec.Servers) != 1 {
				t.Errorf("unmarshal mismatch: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		cache.clear(id1)
	}()
	wg.Wait()

	if _, ok := cache.peekClone(id1); ok {
		t.Fatal("expected id1 cleared")
	}

	// Verify maxSyncPreviewEntries cap evicts oldest preview cleanly.
	var ids []string
	for i := 0; i < maxSyncPreviewEntries+3; i++ {
		ids = append(ids, cache.put(sampleTestBundle()))
	}
	if _, ok := cache.peekClone(ids[0]); ok {
		t.Fatal("expected oldest preview evicted past maxSyncPreviewEntries")
	}
	if _, ok := cache.peekClone(ids[len(ids)-1]); !ok {
		t.Fatal("expected newest preview retained")
	}
}

func TestSyncPreviewCacheTTLExpiration(t *testing.T) {
	cache := newSyncPreviewCache()
	defer cache.close()

	id := cache.put(sampleTestBundle())
	cache.mu.Lock()
	e := cache.entries[id]
	e.expiresAt = time.Now().Add(-time.Second)
	cache.entries[id] = e
	cache.mu.Unlock()

	if _, ok := cache.peekClone(id); ok {
		t.Fatal("expected expired preview to be pruned on peekClone")
	}
}
