export type AuthType = 'password' | 'key' | 'both'
export type ProxyType = 'none' | 'socks5' | 'http'
export type JumpMode = 'none' | 'server' | 'custom'

export interface ProxyView {
  type: ProxyType
  host: string
  port: number
  username: string
  hasPassword: boolean
}

export interface JumpHostView {
  mode: JumpMode
  serverId?: string
  host?: string
  port?: number
  username?: string
  authType?: AuthType
  hasPassword: boolean
  sshKeyId?: string
  useMainProxy?: boolean
}

export interface ServerView {
  id: string
  name: string
  group: string
  color: string
  host: string
  port: number
  username: string
  authType: AuthType
  hasPassword: boolean
  sshKeyId?: string
  proxy: ProxyView
  jumpHost: JumpHostView
  timeoutSeconds: number
  keepAliveSeconds: number
  defaultRemotePath: string
  startupCommand: string
  notes: string
  createdAt: string
  updatedAt: string
  lastConnectedAt?: string
}

export interface ServerSaveRequest {
  id: string
  name: string
  group: string
  color: string
  host: string
  port: number
  username: string
  authType: AuthType
  password: string
  keepExistingPassword: boolean
  sshKeyId: string
  proxy: {
    type: ProxyType
    host: string
    port: number
    username: string
    password: string
    keepExistingPassword: boolean
  }
  jumpHost: {
    mode: JumpMode
    serverId: string
    host: string
    port: number
    username: string
    authType: AuthType
    password: string
    keepExistingPassword: boolean
    sshKeyId: string
    useMainProxy: boolean
  }
  timeoutSeconds: number
  keepAliveSeconds: number
  defaultRemotePath: string
  startupCommand: string
  notes: string
}

export interface SSHKeyMeta {
  id: string
  name: string
  keyType: string
  fingerprint: string
  publicKey: string
  hasPassphrase: boolean
  comment?: string
  usageCount: number
  createdAt: string
}

export interface CommandRecord {
  id: string
  command: string
  serverId: string
  serverName: string
  sessionId: string
  executedAt: string
}

export interface CommandShortcut {
  id: string
  name: string
  command: string
  description: string
  category: string
  autoExecute: boolean
  pinned: boolean
  useCount: number
  createdAt: string
  updatedAt: string
}

export interface AppSettings {
  autocompleteEnabled: boolean
  autocompleteFromHistory: boolean
  autocompleteFromShortcut: boolean
  autoOpenSftp: boolean
  sftpPanelPosition: 'right' | 'bottom'
  terminalFontFamily: string
  terminalFontSize: number
  defaultKeepAliveSeconds: number
  maxHistoryEntries: number
  recordCommands: boolean
  language: 'zh' | 'en'
}

export interface VaultSecurityInfo {
  encrypted: boolean
  engine: string
  machineBound: boolean
  userBound: boolean
  vaultPath: string
  keyCount: number
  shortcutCount: number
  serverCount: number
}

export interface VaultLockState {
  locked: boolean
  hasPassword: boolean
  vaultPath: string
  schemaVersion: number
}

export interface KeyDeployResult {
  serverId: string
  serverName: string
  ok: boolean
  message: string
}

export interface SFTPFileEntry {
  name: string
  path: string
  isDir: boolean
  isSymlink: boolean
  size: number
  mode: string
  modTime: string
}

export interface SFTPListResult {
  path: string
  parent: string
  entries: SFTPFileEntry[]
}

export interface TransferProgress {
  transferId: string
  sessionId: string
  direction: 'upload' | 'download'
  fileName: string
  remotePath: string
  localPath: string
  transferred: number
  total: number
  percent: number
  status: 'running' | 'done' | 'error'
  error?: string
}

export interface SessionInfo {
  sessionId: string
  serverId: string
  serverName: string
  host: string
  port: number
  username: string
  status: 'connecting' | 'connected' | 'disconnected' | 'error'
  sftpReady: boolean
  homeDir: string
  error?: string
  viaSummary?: string
  createdAt: string
}

export interface CompletionItem {
  command: string
  description: string
  category: string
  source: 'builtin' | 'shortcut' | 'history' | 'dir' | 'file'
  insertText: string
  score: number
}
