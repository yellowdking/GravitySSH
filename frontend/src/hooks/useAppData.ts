import { useCallback, useEffect, useState } from 'react'
import type {
  AppSettings,
  CommandRecord,
  CommandShortcut,
  ServerView,
  SessionInfo,
  SSHKeyMeta,
  UpdateCheckResult,
  VaultLockState,
  VaultSecurityInfo,
} from '../types'
import { api } from '../wailsApi'
import { DEFAULT_TERMINAL_FONT } from '../components/terminalTheme'

const DEFAULT_SETTINGS: AppSettings = {
  autocompleteEnabled: true,
  autocompleteFromHistory: true,
  autocompleteFromShortcut: true,
  autoOpenSftp: true,
  sftpPanelPosition: 'right',
  terminalFontFamily: DEFAULT_TERMINAL_FONT,
  terminalFontSize: 14,
  defaultKeepAliveSeconds: 30,
  maxHistoryEntries: 2000,
  recordCommands: true,
  language: 'zh',
  monitorBarEnabled: true,
  sftpFollowTerminal: true,
  pasteGuardEnabled: true,
  keywordHighlightEnabled: true,
  autoSwitchEnglishIme: true,
  checkUpdatesOnStartup: true,
}

export function useAppData(showToast: (text: string, type?: 'info' | 'error') => void) {
  const [servers, setServers] = useState<ServerView[]>([])
  const [sshKeys, setSshKeys] = useState<SSHKeyMeta[]>([])
  const [shortcuts, setShortcuts] = useState<CommandShortcut[]>([])
  const [history, setHistory] = useState<CommandRecord[]>([])
  const [settings, setSettings] = useState<AppSettings>(DEFAULT_SETTINGS)
  const [securityInfo, setSecurityInfo] = useState<VaultSecurityInfo | null>(null)
  const [lockState, setLockState] = useState<VaultLockState | null>(null)
  const [appVersion, setAppVersion] = useState('1.3.0')
  const [updateInfo, setUpdateInfo] = useState<UpdateCheckResult | null>(null)
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null)
  const [sftpOpenBySession, setSftpOpenBySession] = useState<Record<string, boolean>>({})

  const loadAll = useCallback(async () => {
    try {
      const [lock, ver] = await Promise.all([
        api.getVaultLockState().catch(() => null),
        api.getAppVersion().catch(() => ''),
      ])
      if (ver) setAppVersion(ver)
      if (lock) {
        setLockState(lock)
        if (lock.locked) return
      }
      const [srvs, keys, scs, hist, cfg, sec] = await Promise.all([
        api.listServers(),
        api.listSSHKeys(),
        api.listShortcuts(),
        api.listCommandHistory('', 400),
        api.getSettings(),
        api.getVaultSecurityInfo(),
      ])
      setServers(srvs || [])
      setSshKeys(keys || [])
      setShortcuts(scs || [])
      setHistory(hist || [])
      if (cfg) setSettings({ ...DEFAULT_SETTINGS, ...cfg })
      if (sec) setSecurityInfo(sec)
    } catch (err) {
      showToast(`加载配置失败: ${err}`, 'error')
    }
  }, [showToast])

  useEffect(() => { void loadAll() }, [loadAll])

  // Silent startup GitHub update check when enabled
  useEffect(() => {
    if (!settings.checkUpdatesOnStartup || lockState?.locked) return
    let cancelled = false
    const t = setTimeout(() => {
      void api.checkForUpdates().then((res) => { if (!cancelled && res) setUpdateInfo(res) }).catch(() => {})
    }, 2500)
    return () => { cancelled = true; clearTimeout(t) }
  }, [settings.checkUpdatesOnStartup, lockState?.locked])

  useEffect(() => {
    const unsubStatus = api.onEvent<SessionInfo>('session:status', (info) => {
      setSessions((prev) => {
        const idx = prev.findIndex((s) => s.sessionId === info.sessionId)
        if (idx === -1) return [...prev, info]
        const next = [...prev]
        next[idx] = info
        return next
      })
      if (info.status === 'connected' && info.sftpReady && settings.autoOpenSftp) {
        setSftpOpenBySession((prev) => ({ ...prev, [info.sessionId]: prev[info.sessionId] ?? true }))
      }
    })
    const unsubCmd = api.onEvent<CommandRecord>('command:recorded', (rec) => {
      setHistory((prev) => [rec, ...prev].slice(0, 500))
    })
    return () => { unsubStatus(); unsubCmd() }
  }, [settings.autoOpenSftp])

  return {
    servers, sshKeys, shortcuts, setShortcuts, history, settings, setSettings,
    securityInfo, lockState, appVersion, updateInfo, setUpdateInfo,
    sessions, setSessions, activeSessionId, setActiveSessionId,
    sftpOpenBySession, setSftpOpenBySession, loadAll,
  }
}
