package syncsvc

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/synctool/sdk/go/envelope"
)

const (
	// AppID is the canonical synctool-v1 application identifier shared across Windows & Android.
	AppID = "com.yellowdking.gravityssh"
	// Schema is the canonical Always-2-Part vault sync parcel schema.
	Schema = "gravityssh.vault.v1"

	RoleVaultBundle     = "vault_bundle"
	RoleVaultDump       = "vault_dump"
	BlobNameVaultBundle = "vault_bundle.json.gz"
	BlobMIMEGzip        = "application/gzip"

	// MaxDecompressedBundleBytes caps gunzipped SyncBundle JSON at 16 MiB against zip-bombs.
	MaxDecompressedBundleBytes = 16 << 20
	// MaxInlineSummaryBytes enforces < 512 B metadata-only Manifest.InlinePayload.
	MaxInlineSummaryBytes = 512
)

// ZeroSyncBundle zeroes sensitive slices in a SyncBundle in memory.
func ZeroSyncBundle(b *models.SyncBundle) {
	if b == nil {
		return
	}
	clear(b.Servers)
	clear(b.SSHKeys)
	clear(b.Shortcuts)
	clear(b.Templates)
	*b = models.SyncBundle{}
}

func truncateRunes(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// BuildVaultParcel encodes a SyncBundle into the canonical Always-2-Part gravityssh.vault.v1 parcel:
//   - part_id = 0 (Manifest.InlinePayload, < 512 B): counts & source metadata ONLY (never secrets!)
//   - part_id = 1 (BlobInput): gzipped UTF-8 JSON of SyncBundle streamed directly from RAM (zero temp files)
//
// Callers MUST invoke the returned cleanup func once sending completes to zero the gzipped buffer.
func BuildVaultParcel(bundle models.SyncBundle) (envelope.ParcelInput, func(), error) {
	if bundle.SchemaVersion <= 0 {
		bundle.SchemaVersion = 1
	}
	if strings.TrimSpace(bundle.ExportedAt) == "" {
		bundle.ExportedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(bundle.SourcePlatform) == "" {
		bundle.SourcePlatform = "windows"
	}
	safeDevice := truncateRunes(bundle.SourceDevice, 48)
	if safeDevice == "" {
		safeDevice = "Windows-PC"
	}
	bundle.SourceDevice = safeDevice
	if bundle.Servers == nil {
		bundle.Servers = []models.ServerConfig{}
	}
	if bundle.SSHKeys == nil {
		bundle.SSHKeys = []models.SSHKeyRecord{}
	}
	if bundle.Shortcuts == nil {
		bundle.Shortcuts = []models.CommandShortcut{}
	}
	if bundle.Templates == nil {
		bundle.Templates = []models.BuiltinTemplate{}
	}

	inline := map[string]any{
		"bundleSchemaVersion": bundle.SchemaVersion,
		"exportedAt":          bundle.ExportedAt,
		"sourcePlatform":      bundle.SourcePlatform,
		"sourceDevice":        safeDevice,
		"serverCount":         len(bundle.Servers),
		"keyCount":            len(bundle.SSHKeys),
		"shortcutCount":       len(bundle.Shortcuts),
		"templateCount":       len(bundle.Templates),
	}
	inlineRaw, err := json.Marshal(inline)
	if err != nil {
		return envelope.ParcelInput{}, func() {}, envelope.WrapError(envelope.ErrCodeManifestInvalid, false, 0, "marshal inline summary", err)
	}
	if len(inlineRaw) >= MaxInlineSummaryBytes {
		return envelope.ParcelInput{}, func() {}, envelope.NewError(envelope.ErrCodeManifestInvalid, false, 0, fmt.Sprintf("inline summary %d bytes exceeds %d limit", len(inlineRaw), MaxInlineSummaryBytes), nil)
	}

	rawJSON, err := json.Marshal(bundle)
	if err != nil {
		return envelope.ParcelInput{}, func() {}, envelope.WrapError(envelope.ErrCodeManifestInvalid, false, 0, "marshal sync bundle", err)
	}
	var gzBuf bytes.Buffer
	gzw, err := gzip.NewWriterLevel(&gzBuf, gzip.BestSpeed)
	if err != nil {
		clear(rawJSON)
		return envelope.ParcelInput{}, func() {}, envelope.WrapError(envelope.ErrCodeStorageIO, false, 0, "init gzip writer", err)
	}
	_, wErr := gzw.Write(rawJSON)
	cErr := gzw.Close()
	clear(rawJSON)
	if wErr != nil {
		clear(gzBuf.Bytes())
		return envelope.ParcelInput{}, func() {}, envelope.WrapError(envelope.ErrCodeStorageIO, false, 0, "gzip sync bundle", wErr)
	}
	if cErr != nil {
		clear(gzBuf.Bytes())
		return envelope.ParcelInput{}, func() {}, envelope.WrapError(envelope.ErrCodeStorageIO, false, 0, "finalize gzip sync bundle", cErr)
	}

	gzBytes := append([]byte(nil), gzBuf.Bytes()...)
	clear(gzBuf.Bytes())

	blob := envelope.BlobFromReader(
		RoleVaultBundle,
		BlobNameVaultBundle,
		BlobMIMEGzip,
		int64(len(gzBytes)),
		func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(gzBytes)), nil
		},
	)
	blob.Encoding = "gzip"

	cleanup := func() {
		clear(gzBytes)
	}
	return envelope.ParcelJSON(AppID, Schema, inline, blob), cleanup, nil
}

func extractSummaryInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return max(0, v)
	case int64:
		return max(0, int(v))
	case float64:
		return max(0, int(v))
	case json.Number:
		if n, err := v.Int64(); err == nil && n > 0 {
			return int(n)
		}
	}
	return 0
}

func extractSummaryString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// ParseManifestSummary extracts preview counts and sender metadata from part_id=0 Manifest.
func ParseManifestSummary(m envelope.Manifest) (serverCount, keyCount, shortcutCount, templateCount int, exportedAt, sourcePlatform, sourceDevice string) {
	in := m.InlinePayload
	serverCount = extractSummaryInt(in, "serverCount")
	keyCount = extractSummaryInt(in, "keyCount")
	shortcutCount = extractSummaryInt(in, "shortcutCount")
	templateCount = extractSummaryInt(in, "templateCount")
	exportedAt = extractSummaryString(in, "exportedAt")
	sourcePlatform = extractSummaryString(in, "sourcePlatform")
	if sourcePlatform == "" {
		sourcePlatform = strings.TrimSpace(m.Sender.Platform)
	}
	sourceDevice = extractSummaryString(in, "sourceDevice")
	if sourceDevice == "" {
		sourceDevice = strings.TrimSpace(m.Sender.Label)
	}
	return
}

func wipeAndRemoveStagingFile(path string) {
	if path == "" {
		return
	}
	if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		if st, sErr := f.Stat(); sErr == nil && st.Mode().IsRegular() && st.Size() > 0 {
			zeros := make([]byte, min(int64(64*1024), st.Size()))
			rem := st.Size()
			var off int64
			for rem > 0 {
				chunk := min(int64(len(zeros)), rem)
				_, wErr := f.WriteAt(zeros[:chunk], off)
				if wErr != nil {
					break
				}
				off += chunk
				rem -= chunk
			}
			_ = f.Sync()
		}
		_ = f.Close()
	}
	_ = os.Remove(path)
}

// DecodeReceivedParcel validates, gunzips (bounded at 16 MiB), and unmarshals a verified
// gravityssh.vault.v1 ReceivedParcel, wiping any staging blob files before returning.
func DecodeReceivedParcel(p envelope.ReceivedParcel) (models.SyncBundle, error) {
	defer func() {
		for _, b := range p.Blobs {
			wipeAndRemoveStagingFile(b.SafePath)
		}
	}()

	if p.Manifest.AppID != AppID {
		return models.SyncBundle{}, envelope.NewError(envelope.ErrCodeManifestInvalid, false, 0, fmt.Sprintf("unexpected app_id %q", p.Manifest.AppID), nil)
	}
	if p.Manifest.Schema != Schema {
		return models.SyncBundle{}, envelope.NewError(envelope.ErrCodeManifestInvalid, false, 0, fmt.Sprintf("unexpected schema %q", p.Manifest.Schema), nil)
	}

	var targetBlob *envelope.ReceivedBlob
	for i := range p.Blobs {
		role := p.Blobs[i].Descriptor.Role
		if role == RoleVaultBundle || role == RoleVaultDump {
			targetBlob = &p.Blobs[i]
			break
		}
	}

	var rawJSON []byte
	if targetBlob != nil {
		raw, err := readReceivedBlobBounded(*targetBlob)
		if err != nil {
			return models.SyncBundle{}, err
		}
		rawJSON = raw
	} else if len(p.Blobs) == 0 && len(p.Manifest.InlinePayload) > 0 {
		// Defense-in-depth fallback for empty-blobs inline_payload parcels.
		raw, err := extractInlineFallbackJSON(p.Manifest.InlinePayload)
		if err != nil {
			return models.SyncBundle{}, err
		}
		rawJSON = raw
	} else {
		return models.SyncBundle{}, envelope.NewError(envelope.ErrCodeManifestInvalid, false, 0, "parcel missing vault_bundle attachment", nil)
	}
	defer clear(rawJSON)

	var bundle models.SyncBundle
	if err := json.Unmarshal(rawJSON, &bundle); err != nil {
		return models.SyncBundle{}, envelope.WrapError(envelope.ErrCodeManifestInvalid, false, 0, "decode sync bundle json", err)
	}
	if bundle.SchemaVersion < 1 || bundle.SchemaVersion > 2 {
		return models.SyncBundle{}, envelope.NewError(envelope.ErrCodeUnsupportedVersion, false, 0, fmt.Sprintf("unsupported bundle schemaVersion %d", bundle.SchemaVersion), nil)
	}
	if strings.TrimSpace(bundle.SourcePlatform) == "" {
		bundle.SourcePlatform = strings.TrimSpace(p.Manifest.Sender.Platform)
	}
	if strings.TrimSpace(bundle.SourceDevice) == "" {
		bundle.SourceDevice = strings.TrimSpace(p.Manifest.Sender.Label)
	}
	if strings.TrimSpace(bundle.ExportedAt) == "" && p.Manifest.CreatedAt > 0 {
		bundle.ExportedAt = time.Unix(p.Manifest.CreatedAt, 0).UTC().Format(time.RFC3339)
	}
	return bundle, nil
}

func readReceivedBlobBounded(b envelope.ReceivedBlob) ([]byte, error) {
	fi, err := os.Lstat(b.SafePath)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, envelope.WrapError(envelope.ErrCodeStorageIO, false, 0, "staging blob missing or irregular", err)
	}
	if fi.Size() > MaxDecompressedBundleBytes {
		return nil, envelope.NewError(envelope.ErrCodeManifestInvalid, false, 0, "compressed vault bundle exceeds 16 MiB cap", nil)
	}
	f, err := os.Open(b.SafePath)
	if err != nil {
		return nil, envelope.WrapError(envelope.ErrCodeStorageIO, false, 0, "open staging blob", err)
	}
	defer f.Close()

	var reader io.Reader
	switch b.Descriptor.Encoding {
	case "gzip":
		gzr, gzErr := gzip.NewReader(f)
		if gzErr != nil {
			return nil, envelope.WrapError(envelope.ErrCodeManifestInvalid, false, 0, "invalid gzip vault_bundle stream", gzErr)
		}
		defer gzr.Close()
		reader = gzr
	case "", "identity":
		reader = f
	default:
		return nil, envelope.NewError(envelope.ErrCodeManifestInvalid, false, 0, fmt.Sprintf("unsupported blob encoding %q", b.Descriptor.Encoding), nil)
	}

	raw, err := io.ReadAll(io.LimitReader(reader, MaxDecompressedBundleBytes+1))
	if err != nil {
		clear(raw)
		return nil, envelope.WrapError(envelope.ErrCodeManifestInvalid, false, 0, "read vault_bundle payload", err)
	}
	if len(raw) > MaxDecompressedBundleBytes {
		clear(raw)
		return nil, envelope.NewError(envelope.ErrCodeManifestInvalid, false, 0, "decompressed vault_bundle exceeds 16 MiB cap", nil)
	}
	return raw, nil
}

func extractInlineFallbackJSON(inline map[string]any) ([]byte, error) {
	if bVal, ok := inline["bundle"]; ok {
		switch v := bVal.(type) {
		case string:
			return []byte(v), nil
		case map[string]any:
			return json.Marshal(v)
		}
	}
	_, hasServers := inline["servers"]
	_, hasKeys := inline["sshKeys"]
	_, hasVer := inline["schemaVersion"]
	if hasServers || hasKeys || hasVer {
		return json.Marshal(inline)
	}
	return nil, envelope.NewError(envelope.ErrCodeManifestInvalid, false, 0, "inline_payload fallback does not contain a SyncBundle", nil)
}
