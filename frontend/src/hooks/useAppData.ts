import { useCallback, useEffect, useRef, useState } from 'react'
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
import { errMsg } from '../utils/errors'
import { DEFAULT_TERMINAL_FONT } from '../components/terminalTheme'
import { pushTerminalData } from './terminalStream'

function sameSettings(a: AppSettings, b: AppSettings): boolean {
  const keys = Object.keys(a) as Array<keyof AppSettings>
  if (keys.length !== Object.keys(b).length) return false
  for (const k of keys) {
    if (a[k] !== b[k]) return false
  }
  return true
}

const DEFAULT_SETTINGS: AppSettings = {
  autocompleteEnabled: true,
  autocompleteFromHistory: true,
  autoOpenSftp: true,
  terminalFontFamily: DEFAULT_TERMINAL_FONT,
  terminalFontSize: 14,
  defaultKeepAliveSeconds: 30,
  maxHistoryEntries: 2000,
  recordCommands: true,
  monitorBarEnabled: true,
  sftpFollowTerminal: true,
  pasteGuardEnabled: true,
  keywordHighlightEnabled: true,
  autoSwitchEnglishIme: true,
  checkUpdatesOnStartup: true,
  syncRelayUrl: '',
}

export function useAppData(showToast: (text: string, type?: 'info' | 'error') => void) {
  const [servers, setServers] = useState<ServerView[]>([])
  const [sshKeys, setSshKeys] = useState<SSHKeyMeta[]>([])
  const [shortcuts, setShortcuts] = useState<CommandShortcut[]>([])
  const [history, setHistory] = useState<CommandRecord[]>([])
  const [settings, setSettings] = useState<AppSettings>(DEFAULT_SETTINGS)
  const [securityInfo, setSecurityInfo] = useState<VaultSecurityInfo | null>(null)
  const [lockState, setLockState] = useState<VaultLockState | null>(null)
  const [appVersion, setAppVersion] = useState('1.4.1')
  const [updateInfo, setUpdateInfo] = useState<UpdateCheckResult | null>(null)
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null)
  const [sftpOpenBySession, setSftpOpenBySession] = useState<Record<string, boolean>>({})

  const closedSessionIdsRef = useRef<Set<string>>(new Set())
  const autoOpenSftpRef = useRef(settings.autoOpenSftp)
  autoOpenSftpRef.current = settings.autoOpenSftp

  const reloadServers = useCallback(async () => {
    try { setServers((await api.listServers()) || []) }
    catch (err) { showToast(`加载服务器列表失败: ${errMsg(err)}`, 'error') }
  }, [showToast])

  const reloadKeys = useCallback(async () => {
    try { setSshKeys((await api.listSSHKeys()) || []) }
    catch (err) { showToast(`加载密钥列表失败: ${errMsg(err)}`, 'error') }
  }, [showToast])

  const reloadShortcuts = useCallback(async () => {
    try { setShortcuts((await api.listShortcuts()) || []) }
    catch (err) { showToast(`加载快捷命令失败: ${errMsg(err)}`, 'error') }
  }, [showToast])

  const reloadHistory = useCallback(async () => {
    try { setHistory((await api.listCommandHistory('', 400)) || []) }
    catch (err) { showToast(`加载命令历史失败: ${errMsg(err)}`, 'error') }
  }, [showToast])

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
      if (cfg) {
        const merged = { ...DEFAULT_SETTINGS, ...cfg }
        setSettings((prev) => (sameSettings(prev, merged) ? prev : merged))
      }
      if (sec) setSecurityInfo(sec)
    } catch (err) {
      showToast(`加载配置失败: ${errMsg(err)}`, 'error')
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
      if (closedSessionIdsRef.current.has(info.sessionId)) return
      setSessions((prev) => {
        const idx = prev.findIndex((s) => s.sessionId === info.sessionId)
        if (idx === -1) return [...prev, info]
        const next = [...prev]
        next[idx] = info
        return next
      })
      if (info.status === 'connected' && info.sftpReady && autoOpenSftpRef.current) {
        setSftpOpenBySession((prev) => ({ ...prev, [info.sessionId]: prev[info.sessionId] ?? true }))
      }
    })
    const unsubTermData = api.onEvent<{ sessionId: string; data: string }>('terminal:data', (p) => {
      if (!p?.sessionId || !p.data || closedSessionIdsRef.current.has(p.sessionId)) return
      pushTerminalData(p.sessionId, p.data)
    })
    const unsubCmd = api.onEvent<CommandRecord>('command:recorded', (rec) => {
      setHistory((prev) => [rec, ...prev].slice(0, 500))
    })
    return () => { unsubStatus(); unsubTermData(); unsubCmd() }
  }, [])

  return {
    servers, sshKeys, shortcuts, setShortcuts, history, settings, setSettings,
    securityInfo, lockState, appVersion, updateInfo, setUpdateInfo,
    sessions, setSessions, closedSessionIdsRef, activeSessionId, setActiveSessionId,
    sftpOpenBySession, setSftpOpenBySession,
    loadAll, reloadServers, reloadKeys, reloadShortcuts, reloadHistory,
  }
}
