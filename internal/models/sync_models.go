package models

// SyncBundle is the portable decrypted vault payload exchanged via gssh-sync-v1.
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

// SyncSendSession describes an active drop uploaded to the relay.
type SyncSendSession struct {
	Code          string `json:"code"`
	QRText        string `json:"qrText"`
	ExpiresAt     string `json:"expiresAt"`
	RelayURL      string `json:"relayUrl"`
	ServerCount   int    `json:"serverCount"`
	KeyCount      int    `json:"keyCount"`
	ShortcutCount int    `json:"shortcutCount"`
	TemplateCount int    `json:"templateCount"`
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
