package models

import "time"

type AuthType string

const (
	AuthPassword AuthType = "password"
	AuthKey      AuthType = "key"
	AuthBoth     AuthType = "both"
)

type ProxyType string

const (
	ProxyNone   ProxyType = "none"
	ProxySOCKS5 ProxyType = "socks5"
	ProxyHTTP   ProxyType = "http"
)

type JumpMode string

const (
	JumpNone   JumpMode = "none"
	JumpServer JumpMode = "server"
	JumpCustom JumpMode = "custom"
)

type ProxyConfig struct {
	Type     ProxyType `json:"type"`
	Host     string    `json:"host"`
	Port     int       `json:"port"`
	Username string    `json:"username"`
	Password string    `json:"password,omitempty"`
}

type JumpHostConfig struct {
	Mode         JumpMode `json:"mode"`
	ServerID     string   `json:"serverId,omitempty"`
	Host         string   `json:"host,omitempty"`
	Port         int      `json:"port,omitempty"`
	Username     string   `json:"username,omitempty"`
	AuthType     AuthType `json:"authType,omitempty"`
	Password     string   `json:"password,omitempty"`
	SSHKeyID     string   `json:"sshKeyId,omitempty"`
	UseMainProxy bool     `json:"useMainProxy,omitempty"`
}

type ServerConfig struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Group             string         `json:"group"`
	Color             string         `json:"color"`
	Host              string         `json:"host"`
	Port              int            `json:"port"`
	Username          string         `json:"username"`
	AuthType          AuthType       `json:"authType"`
	Password          string         `json:"password,omitempty"`
	SSHKeyID          string         `json:"sshKeyId,omitempty"`
	Proxy             ProxyConfig    `json:"proxy"`
	JumpHost          JumpHostConfig `json:"jumpHost"`
	TimeoutSeconds    int            `json:"timeoutSeconds"`
	KeepAliveSeconds  int            `json:"keepAliveSeconds"`
	DefaultRemotePath string         `json:"defaultRemotePath"`
	StartupCommand    string         `json:"startupCommand"`
	Notes             string         `json:"notes"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	LastConnectedAt   *time.Time     `json:"lastConnectedAt,omitempty"`
}

type ProxyView struct {
	Type        ProxyType `json:"type"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Username    string    `json:"username"`
	HasPassword bool      `json:"hasPassword"`
}

type JumpHostView struct {
	Mode         JumpMode `json:"mode"`
	ServerID     string   `json:"serverId,omitempty"`
	Host         string   `json:"host,omitempty"`
	Port         int      `json:"port,omitempty"`
	Username     string   `json:"username,omitempty"`
	AuthType     AuthType `json:"authType,omitempty"`
	HasPassword  bool     `json:"hasPassword"`
	SSHKeyID     string   `json:"sshKeyId,omitempty"`
	UseMainProxy bool     `json:"useMainProxy,omitempty"`
}

type ServerView struct {
	ID                string       `json:"id"`
	Name              string       `json:"name"`
	Group             string       `json:"group"`
	Color             string       `json:"color"`
	Host              string       `json:"host"`
	Port              int          `json:"port"`
	Username          string       `json:"username"`
	AuthType          AuthType     `json:"authType"`
	HasPassword       bool         `json:"hasPassword"`
	SSHKeyID          string       `json:"sshKeyId,omitempty"`
	Proxy             ProxyView    `json:"proxy"`
	JumpHost          JumpHostView `json:"jumpHost"`
	TimeoutSeconds    int          `json:"timeoutSeconds"`
	KeepAliveSeconds  int          `json:"keepAliveSeconds"`
	DefaultRemotePath string       `json:"defaultRemotePath"`
	StartupCommand    string       `json:"startupCommand"`
	Notes             string       `json:"notes"`
	CreatedAt         time.Time    `json:"createdAt"`
	UpdatedAt         time.Time    `json:"updatedAt"`
	LastConnectedAt   *time.Time   `json:"lastConnectedAt,omitempty"`
}

type ProxySaveReq struct {
	Type                 ProxyType `json:"type"`
	Host                 string    `json:"host"`
	Port                 int       `json:"port"`
	Username             string    `json:"username"`
	Password             string    `json:"password"`
	KeepExistingPassword bool      `json:"keepExistingPassword"`
}

type JumpSaveReq struct {
	Mode                 JumpMode `json:"mode"`
	ServerID             string   `json:"serverId"`
	Host                 string   `json:"host"`
	Port                 int      `json:"port"`
	Username             string   `json:"username"`
	AuthType             AuthType `json:"authType"`
	Password             string   `json:"password"`
	KeepExistingPassword bool     `json:"keepExistingPassword"`
	SSHKeyID             string   `json:"sshKeyId"`
	UseMainProxy         bool     `json:"useMainProxy"`
}

type ServerSaveRequest struct {
	ID                   string       `json:"id"`
	Name                 string       `json:"name"`
	Group                string       `json:"group"`
	Color                string       `json:"color"`
	Host                 string       `json:"host"`
	Port                 int          `json:"port"`
	Username             string       `json:"username"`
	AuthType             AuthType     `json:"authType"`
	Password             string       `json:"password"`
	KeepExistingPassword bool         `json:"keepExistingPassword"`
	SSHKeyID             string       `json:"sshKeyId"`
	Proxy                ProxySaveReq `json:"proxy"`
	JumpHost             JumpSaveReq  `json:"jumpHost"`
	TimeoutSeconds       int          `json:"timeoutSeconds"`
	KeepAliveSeconds     int          `json:"keepAliveSeconds"`
	DefaultRemotePath    string       `json:"defaultRemotePath"`
	StartupCommand       string       `json:"startupCommand"`
	Notes                string       `json:"notes"`
}
