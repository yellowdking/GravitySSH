package models

import "github.com/yellowdking/synctool/sdk/go/envelope"

// VaultSyncState stores synctool-v1 device identity, custom relay credentials,
// paired peer DeviceCards, 2-phase offline revocation tombstones, and replay state
// inside the encrypted vault.gsv envelope (never exported in SyncBundle).
type VaultSyncState struct {
	MasterSeedB64U      string                           `json:"masterSeedB64u,omitempty"`
	LocalLabel          string                           `json:"localLabel,omitempty"`
	ReplayWatermarkTS   int64                            `json:"replayWatermarkTs,omitempty"`
	CustomRelay         *envelope.RelayProfile           `json:"customRelay,omitempty"`
	Devices             map[string]envelope.DeviceCard   `json:"devices,omitempty"`
	PendingTokenDeletes []string                         `json:"pendingTokenDeletes,omitempty"`
	SeenMsgs            map[string]envelope.SeenMsgEntry `json:"seenMsgs,omitempty"`
}

// SyncBundle is the portable decrypted vault payload exchanged inside gravityssh.vault.v1 part_id=1.
type SyncBundle struct {
	SchemaVersion  int               `json:"schemaVersion"`
	ExportedAt     string            `json:"exportedAt"`
	SourcePlatform string            `json:"sourcePlatform"`
	SourceDevice   string            `json:"sourceDevice"`
	Servers        []ServerConfig    `json:"servers"`
	SSHKeys        []SSHKeyRecord    `json:"sshKeys"`
	Shortcuts      []CommandShortcut `json:"shortcuts"`
	Templates      []BuiltinTemplate `json:"templates"`
}

// SyncRelayStatus is the secret-redacted relay status DTO exposed to the frontend (never includes #k=).
type SyncRelayStatus struct {
	Mode                  string `json:"mode"` // "default" | "custom"
	Endpoint              string `json:"endpoint"`
	RedactedURI           string `json:"redactedUri"`
	RelayID               string `json:"relayId"`
	TLSMode               string `json:"tlsMode"`
	SPKIPinShort          string `json:"spkiPinShort"`
	CertExpiresAt         string `json:"certExpiresAt,omitempty"`
	CertRotateRecommended bool   `json:"certRotateRecommended,omitempty"`
}

// SyncPairedDevice represents a trusted paired peer device in UI overview/status DTOs.
type SyncPairedDevice struct {
	DeviceID string `json:"deviceId"`
	Label    string `json:"label"`
	Platform string `json:"platform"`
	RelayID  string `json:"relayId"`
	IssuedAt int64  `json:"issuedAt"`
	PairedAt int64  `json:"pairedAt,omitempty"`
}

// SyncOverview returns local sync identity, active relay status, and paired devices.
type SyncOverview struct {
	LocalDeviceID    string             `json:"localDeviceId"`
	LocalDeviceLabel string             `json:"localDeviceLabel"`
	Relay            SyncRelayStatus    `json:"relay"`
	PairedDevices    []SyncPairedDevice `json:"pairedDevices"`
}

// SyncPingInfo reports relay RTT, capability profile, spool backend, clock skew, and TLS cert health.
type SyncPingInfo struct {
	RTTMs                 int64  `json:"rttMs"`
	Profile               string `json:"profile"`
	SpoolMode             string `json:"spoolMode"`
	ClockOffsetSec        int64  `json:"clockOffsetSec"`
	CertExpiresAt         string `json:"certExpiresAt,omitempty"`
	CertRotateRecommended bool   `json:"certRotateRecommended,omitempty"`
}

// SyncInboxItem represents a Phase-1 decrypted Manifest summary in the Mode B spool inbox.
type SyncInboxItem struct {
	ItemID         string `json:"itemId"`
	MsgID          string `json:"msgId"`
	SenderDeviceID string `json:"senderDeviceId"`
	SenderLabel    string `json:"senderLabel"`
	SenderPlatform string `json:"senderPlatform"`
	CreatedAt      int64  `json:"createdAt"`
	ExpiresAt      int64  `json:"expiresAt"`
	TotalBytes     int64  `json:"totalBytes"`
	ServerCount    int    `json:"serverCount"`
	KeyCount       int    `json:"keyCount"`
	ShortcutCount  int    `json:"shortcutCount"`
	TemplateCount  int    `json:"templateCount"`
	PairingBroken  bool   `json:"pairingBroken,omitempty"`
	ErrorMessage   string `json:"errorMessage,omitempty"`
}

// SyncDirectSendResult reports the outcome of SendSyncToDevice ("auto" | "spool" | "pipe").
type SyncDirectSendResult struct {
	Status    string `json:"status"` // "sent_spool" | "suggest_pipe" | "pipe_offer_ready"
	MsgID     string `json:"msgId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	PairURI   string `json:"pairUri,omitempty"`
	RoomCode  string `json:"roomCode,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	Message   string `json:"message,omitempty"`
}

// SyncFlowSession describes a started interactive Mode A flow (bind, oneshot send, receive, or direct pipe).
type SyncFlowSession struct {
	SessionID     string `json:"sessionId"`
	Purpose       string `json:"purpose"`
	Role          string `json:"role"`
	Mode          int    `json:"mode"`
	State         string `json:"state"`
	PairURI       string `json:"pairUri,omitempty"`
	RoomCode      string `json:"roomCode,omitempty"`
	ExpiresAt     string `json:"expiresAt,omitempty"`
	ServerCount   int    `json:"serverCount,omitempty"`
	KeyCount      int    `json:"keyCount,omitempty"`
	ShortcutCount int    `json:"shortcutCount,omitempty"`
	TemplateCount int    `json:"templateCount,omitempty"`
}

// SyncFlowStatus reports real-time state transitions for an interactive sync session.
type SyncFlowStatus struct {
	SessionID            string              `json:"sessionId"`
	Purpose              string              `json:"purpose,omitempty"`
	Role                 string              `json:"role,omitempty"`
	Mode                 int                 `json:"mode,omitempty"`
	State                string              `json:"state"` // "waiting_peer"|"awaiting_sas_input"|"awaiting_sas_confirm"|"transferring"|"ready_preview"|"completed"|"error"|"cancelled"
	PairURI              string              `json:"pairUri,omitempty"`
	RoomCode             string              `json:"roomCode,omitempty"`
	ExpiresAt            string              `json:"expiresAt,omitempty"`
	SAS                  string              `json:"sas,omitempty"`
	SASAttemptsRemaining int                 `json:"sasAttemptsRemaining,omitempty"`
	BytesDone            int64               `json:"bytesDone,omitempty"`
	BytesTotal           int64               `json:"bytesTotal,omitempty"`
	PairedDevice         *SyncPairedDevice   `json:"pairedDevice,omitempty"`
	Preview              *SyncReceivePreview `json:"preview,omitempty"`
	Message              string              `json:"message,omitempty"`
}

// SyncReceivePreview summarizes a fetched & decrypted sync bundle staged in RAM before import.
type SyncReceivePreview struct {
	PreviewID      string `json:"previewId"`
	SchemaVersion  int    `json:"schemaVersion"`
	ExportedAt     string `json:"exportedAt"`
	SourcePlatform string `json:"sourcePlatform"`
	SourceDevice   string `json:"sourceDevice"`
	ServerCount    int    `json:"serverCount"`
	KeyCount       int    `json:"keyCount"`
	ShortcutCount  int    `json:"shortcutCount"`
	TemplateCount  int    `json:"templateCount"`
}

// SyncImportSummary reports how many records were added or updated during import.
type SyncImportSummary struct {
	Mode             string `json:"mode"`
	ServersAdded     int    `json:"serversAdded"`
	ServersUpdated   int    `json:"serversUpdated"`
	KeysAdded        int    `json:"keysAdded"`
	KeysUpdated      int    `json:"keysUpdated"`
	ShortcutsAdded   int    `json:"shortcutsAdded"`
	ShortcutsUpdated int    `json:"shortcutsUpdated"`
	TemplatesAdded   int    `json:"templatesAdded"`
	TemplatesUpdated int    `json:"templatesUpdated"`
}
