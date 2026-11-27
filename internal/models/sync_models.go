package models

// SyncBundle is the portable decrypted vault payload exchanged via gssh-sync-v1 / gssh-sync-v2.
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

// SyncSendSession describes an active pairing channel registered on the relay.
type SyncSendSession struct {
	SessionID     string `json:"sessionId"`
	Code          string `json:"code"`
	QRText        string `json:"qrText"`
	ExpiresAt     string `json:"expiresAt"`
	RelayURL      string `json:"relayUrl"`
	ServerCount   int    `json:"serverCount"`
	KeyCount      int    `json:"keyCount"`
	ShortcutCount int    `json:"shortcutCount"`
	TemplateCount int    `json:"templateCount"`
}

// SyncSendPollResult reports the sender-side mutual verification pairing status.
type SyncSendPollResult struct {
	Status           string `json:"status"`
	RequestID        string `json:"requestId,omitempty"`
	ReceiverCode     string `json:"receiverCode,omitempty"`
	ReceiverDevice   string `json:"receiverDevice,omitempty"`
	ReceiverPlatform string `json:"receiverPlatform,omitempty"`
	RequestedAt      string `json:"requestedAt,omitempty"`
	Message          string `json:"message,omitempty"`
}

// SyncReceiveWaitSession represents an active receiver-side verification wait.
type SyncReceiveWaitSession struct {
	WaitID       string `json:"waitId"`
	ReceiverCode string `json:"receiverCode"`
	ExpiresAt    string `json:"expiresAt"`
}

// SyncReceivePollResult reports the receiver-side mutual verification status.
type SyncReceivePollResult struct {
	Status  string              `json:"status"`
	Preview *SyncReceivePreview `json:"preview,omitempty"`
	Message string              `json:"message,omitempty"`
}

// SyncReceivePreview summarizes a fetched & decrypted sync bundle before import.
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
