package models

type AppSettings struct {
	AutocompleteEnabled     bool   `json:"autocompleteEnabled"`
	AutocompleteFromHistory bool   `json:"autocompleteFromHistory"`
	AutoOpenSFTP            bool   `json:"autoOpenSftp"`
	TerminalFontFamily      string `json:"terminalFontFamily"`
	TerminalFontSize        int    `json:"terminalFontSize"`
	DefaultKeepAliveSeconds int    `json:"defaultKeepAliveSeconds"`
	MaxHistoryEntries       int    `json:"maxHistoryEntries"`
	RecordCommands          bool   `json:"recordCommands"`
	MonitorBarEnabled       bool   `json:"monitorBarEnabled"`
	SFTPFollowTerminal      bool   `json:"sftpFollowTerminal"`
	PasteGuardEnabled       bool   `json:"pasteGuardEnabled"`
	KeywordHighlightEnabled bool   `json:"keywordHighlightEnabled"`
	AutoSwitchEnglishIME    bool   `json:"autoSwitchEnglishIme"`
	CheckUpdatesOnStartup   bool   `json:"checkUpdatesOnStartup"`
	SyncRelayURL            string `json:"syncRelayUrl,omitempty"`
}

func DefaultSettings() AppSettings {
	return AppSettings{
		AutocompleteEnabled:     true,
		AutocompleteFromHistory: true,
		AutoOpenSFTP:            true,
		TerminalFontFamily:      "Cascadia Mono, Consolas, Courier New, monospace",
		TerminalFontSize:        14,
		DefaultKeepAliveSeconds: 30,
		MaxHistoryEntries:       2000,
		RecordCommands:          true,
		MonitorBarEnabled:       true,
		SFTPFollowTerminal:      true,
		PasteGuardEnabled:       true,
		KeywordHighlightEnabled: true,
		AutoSwitchEnglishIME:    true,
		CheckUpdatesOnStartup:   true,
	}
}

type VaultData struct {
	Version   int               `json:"version"`
	Servers   []ServerConfig    `json:"servers"`
	SSHKeys   []SSHKeyRecord    `json:"sshKeys"`
	Shortcuts []CommandShortcut `json:"shortcuts"`
	Templates []BuiltinTemplate `json:"templates"`
	History   []CommandRecord   `json:"history"`
	Settings  AppSettings       `json:"settings"`
}

type VaultSecurityInfo struct {
	Encrypted     bool   `json:"encrypted"`
	Engine        string `json:"engine"`
	MachineBound  bool   `json:"machineBound"`
	UserBound     bool   `json:"userBound"`
	VaultPath     string `json:"vaultPath"`
	KeyCount      int    `json:"keyCount"`
	ShortcutCount int    `json:"shortcutCount"`
	ServerCount   int    `json:"serverCount"`
}
