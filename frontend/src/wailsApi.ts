import type {
  AppSettings,
  CommandRecord,
  CommandShortcut,
  CompletionItem,
  KeyDeployResult,
  ServerSaveRequest,
  ServerView,
  SessionInfo,
  SFTPListResult,
  SSHKeyMeta,
  VaultLockState,
  VaultSecurityInfo,
} from './types'

declare global {
  interface Window {
    go?: {
      main?: {
        App?: Record<string, (...args: unknown[]) => Promise<unknown>>
      }
    }
    runtime?: {
      EventsOn: (eventName: string, callback: (...data: unknown[]) => void) => () => void
      EventsOff: (...eventNames: string[]) => void
      OnFileDrop?: (callback: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean) => void
      OnFileDropOff?: () => void
    }
  }
}

function callGo<T>(method: string, ...args: unknown[]): Promise<T> {
  const fn = window.go?.main?.App?.[method]
  if (!fn) {
    return Promise.reject(new Error(`Wails runtime 未就绪: ${method}`))
  }
  return fn(...args) as Promise<T>
}

export const api = {
  getAppVersion: () => callGo<string>('GetAppVersion'),
  getVaultSecurityInfo: () => callGo<VaultSecurityInfo>('GetVaultSecurityInfo'),
  getVaultLockState: () => callGo<VaultLockState>('GetVaultLockState'),
  unlockVault: (password: string) => callGo<void>('UnlockVault', password),
  setVaultPassword: (currentPassword: string, newPassword: string) =>
    callGo<void>('SetVaultPassword', currentPassword, newPassword),

  // Servers
  listServers: () => callGo<ServerView[]>('ListServers'),
  saveServer: (req: ServerSaveRequest) => callGo<ServerView>('SaveServer', req),
  deleteServer: (id: string) => callGo<void>('DeleteServer', id),
  testServerConnection: (req: ServerSaveRequest) => callGo<string>('TestServerConnection', req),

  // SSH Keys
  listSSHKeys: () => callGo<SSHKeyMeta[]>('ListSSHKeys'),
  importSSHKeyFromText: (name: string, pemContent: string, passphrase: string, comment: string) =>
    callGo<SSHKeyMeta>('ImportSSHKeyFromText', name, pemContent, passphrase, comment),
  pickAndReadSSHKeyFile: () => callGo<{ cancelled: string; fileName?: string; content?: string }>('PickAndReadSSHKeyFile'),
  generateSSHKey: (name: string, algorithm: string, passphrase: string, comment: string) =>
    callGo<SSHKeyMeta>('GenerateSSHKey', name, algorithm, passphrase, comment),
  deleteSSHKey: (id: string) => callGo<void>('DeleteSSHKey', id),
  deploySSHKeyToServers: (keyId: string, serverIds: string[], bindAsAuth: boolean) =>
    callGo<KeyDeployResult[]>('DeploySSHKeyToServers', keyId, serverIds, bindAsAuth),

  // Sessions & Terminal
  connectServer: (serverId: string, cols: number, rows: number) =>
    callGo<SessionInfo>('ConnectServer', serverId, cols, rows),
  disconnectSession: (sessionId: string) => callGo<void>('DisconnectSession', sessionId),
  listSessions: () => callGo<SessionInfo[]>('ListSessions'),
  writeTerminal: (sessionId: string, data: string) => callGo<void>('WriteTerminal', sessionId, data),
  resizeTerminal: (sessionId: string, cols: number, rows: number) =>
    callGo<void>('ResizeTerminal', sessionId, cols, rows),
  setSessionCwd: (sessionId: string, remotePath: string) =>
    callGo<void>('SetSessionCWD', sessionId, remotePath),
  getCurrentInputLine: (sessionId: string) => callGo<string>('GetCurrentInputLine', sessionId),

  // Auto SFTP
  sftpList: (sessionId: string, remotePath: string) => callGo<SFTPListResult>('SFTPList', sessionId, remotePath),
  sftpMkdir: (sessionId: string, remotePath: string) => callGo<void>('SFTPMkdir', sessionId, remotePath),
  sftpRename: (sessionId: string, oldPath: string, newPath: string) =>
    callGo<void>('SFTPRename', sessionId, oldPath, newPath),
  sftpDelete: (sessionId: string, remotePath: string) => callGo<void>('SFTPDelete', sessionId, remotePath),
  sftpAlignPermissions: (sessionId: string, remotePath: string) =>
    callGo<string>('SFTPAlignPermissions', sessionId, remotePath),
  sftpExtractArchive: (sessionId: string, remotePath: string) =>
    callGo<string>('SFTPExtractArchive', sessionId, remotePath),
  sftpUploadDialog: (sessionId: string, remoteDir: string) =>
    callGo<number>('SFTPUploadDialog', sessionId, remoteDir),
  sftpUploadFiles: (sessionId: string, localPaths: string[], remoteDir: string) =>
    callGo<number>('SFTPUploadFiles', sessionId, localPaths, remoteDir),
  sftpDownloadDialog: (sessionId: string, remotePath: string) =>
    callGo<string>('SFTPDownloadDialog', sessionId, remotePath),

  // History & Shortcuts
  listCommandHistory: (serverId: string, limit: number) =>
    callGo<CommandRecord[]>('ListCommandHistory', serverId, limit),
  deleteHistoryEntry: (id: string) => callGo<void>('DeleteHistoryEntry', id),
  clearHistory: (serverId: string) => callGo<void>('ClearHistory', serverId),
  listShortcuts: () => callGo<CommandShortcut[]>('ListShortcuts'),
  saveShortcut: (sc: Partial<CommandShortcut>) => callGo<CommandShortcut>('SaveShortcut', sc),
  deleteShortcut: (id: string) => callGo<void>('DeleteShortcut', id),
  runShortcut: (sessionId: string, shortcutId: string, forceExecute: boolean) =>
    callGo<void>('RunShortcut', sessionId, shortcutId, forceExecute),

  // Autocomplete & Settings
  suggestCommands: (input: string, serverId: string) =>
    callGo<CompletionItem[]>('SuggestCommands', input, serverId),
  suggestCommandsForSession: (input: string, serverId: string, sessionId: string) =>
    callGo<CompletionItem[]>('SuggestCommandsForSession', input, serverId, sessionId),
  getSettings: () => callGo<AppSettings>('GetSettings'),
  saveSettings: (settings: AppSettings) => callGo<AppSettings>('SaveSettings', settings),

  // Runtime events
  onEvent: <T>(eventName: string, handler: (payload: T) => void): (() => void) => {
    if (!window.runtime?.EventsOn) return () => {}
    return window.runtime.EventsOn(eventName, (data: unknown) => handler(data as T))
  },

  onFileDrop: (handler: (paths: string[]) => void): (() => void) => {
    if (!window.runtime?.OnFileDrop) return () => {}
    window.runtime.OnFileDrop((_x, _y, paths) => {
      if (Array.isArray(paths) && paths.length > 0) {
        handler(paths)
      }
    }, false)
    return () => {
      window.runtime?.OnFileDropOff?.()
    }
  },
}
