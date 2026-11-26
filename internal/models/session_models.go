package models

import "time"

type SSHKeyRecord struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	KeyType       string    `json:"keyType"`
	Fingerprint   string    `json:"fingerprint"`
	PublicKey     string    `json:"publicKey"`
	PrivateKeyPEM string    `json:"privateKeyPem"`
	Passphrase    string    `json:"passphrase,omitempty"`
	Comment       string    `json:"comment,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type SSHKeyMeta struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	KeyType       string    `json:"keyType"`
	Fingerprint   string    `json:"fingerprint"`
	PublicKey     string    `json:"publicKey"`
	HasPassphrase bool      `json:"hasPassphrase"`
	Comment       string    `json:"comment,omitempty"`
	UsageCount    int       `json:"usageCount"`
	CreatedAt     time.Time `json:"createdAt"`
}

type CommandRecord struct {
	ID         string    `json:"id"`
	Command    string    `json:"command"`
	ServerID   string    `json:"serverId"`
	ServerName string    `json:"serverName"`
	SessionID  string    `json:"sessionId"`
	ExecutedAt time.Time `json:"executedAt"`
}

type CommandShortcut struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Command     string    `json:"command"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	AutoExecute bool      `json:"autoExecute"`
	Pinned      bool      `json:"pinned"`
	UseCount    int       `json:"useCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type SFTPFileEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	IsDir     bool      `json:"isDir"`
	IsSymlink bool      `json:"isSymlink"`
	Size      int64     `json:"size"`
	Mode      string    `json:"mode"`
	ModTime   time.Time `json:"modTime"`
}

type SFTPListResult struct {
	Path    string          `json:"path"`
	Parent  string          `json:"parent"`
	Entries []SFTPFileEntry `json:"entries"`
}

type TransferProgress struct {
	TransferID  string `json:"transferId"`
	SessionID   string `json:"sessionId"`
	Direction   string `json:"direction"`
	FileName    string `json:"fileName"`
	RemotePath  string `json:"remotePath"`
	LocalPath   string `json:"localPath"`
	Transferred int64  `json:"transferred"`
	Total       int64  `json:"total"`
	Percent     int    `json:"percent"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

type SessionInfo struct {
	SessionID  string    `json:"sessionId"`
	ServerID   string    `json:"serverId"`
	ServerName string    `json:"serverName"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Username   string    `json:"username"`
	Status     string    `json:"status"`
	SFTPReady  bool      `json:"sftpReady"`
	HomeDir    string    `json:"homeDir"`
	Error      string    `json:"error,omitempty"`
	ViaSummary string    `json:"viaSummary,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type VaultLockState struct {
	Locked        bool   `json:"locked"`
	HasPassword   bool   `json:"hasPassword"`
	VaultPath     string `json:"vaultPath"`
	SchemaVersion int    `json:"schemaVersion"`
}

type KeyDeployResult struct {
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
}

type CompletionItem struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Source      string `json:"source"`
	InsertText  string `json:"insertText"`
	Score       int    `json:"score"`
}
