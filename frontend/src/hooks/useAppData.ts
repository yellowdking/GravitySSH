import { useCallback, useEffect, useState } from 'react'
import type {
  AppSettings,
  CommandRecord,
  CommandShortcut,
  ServerView,
  SessionInfo,
  SSHKeyMeta,
  VaultSecurityInfo,
} from '../types'
import { api } from '../wailsApi'

const DEFAULT_SETTINGS: AppSettings = {
  autocompleteEnabled: true,
  autocompleteFromHistory: true,
  autocompleteFromShortcut: true,
  autoOpenSftp: true,
  sftpPanelPosition: 'right',
  terminalFontFamily: 'Cascadia Mono, JetBrains Mono, Consolas, monospace',
  terminalFontSize: 14,
  defaultKeepAliveSeconds: 30,
  maxHistoryEntries: 2000,
  recordCommands: true,
  language: 'zh',
}

export function useAppData(showToast: (text: string, type?: 'info' | 'error') => void) {
  const [servers, setServers] = useState<ServerView[]>([])
  const [sshKeys, setSshKeys] = useState<SSHKeyMeta[]>([])
  const [shortcuts, setShortcuts] = useState<CommandShortcut[]>([])
  const [history, setHistory] = useState<CommandRecord[]>([])
  const [settings, setSettings] = useState<AppSettings>(DEFAULT_SETTINGS)
  const [securityInfo, setSecurityInfo] = useState<VaultSecurityInfo | null>(null)
  const [appVersion, setAppVersion] = useState('1.0.0')
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null)
  const [sftpOpenBySession, setSftpOpenBySession] = useState<Record<string, boolean>>({})

  const loadAll = useCallback(async () => {
    try {
      const [srvs, keys, scs, hist, cfg, sec, ver] = await Promise.all([
        api.listServers(), api.listSSHKeys(), api.listShortcuts(), api.listCommandHistory('', 400),
        api.getSettings(), api.getVaultSecurityInfo(), api.getAppVersion(),
      ])
      setServers(srvs || []); setSshKeys(keys || []); setShortcuts(scs || []); setHistory(hist || [])
      if (cfg) setSettings(cfg); if (sec) setSecurityInfo(sec); if (ver) setAppVersion(ver)
    } catch (err) { showToast(`加载保险库失败: ${err}`, 'error') }
  }, [showToast])

  useEffect(() => { void loadAll() }, [loadAll])

  useEffect(() => {
    const unsubStatus = api.onEvent<SessionInfo>('session:status', (info) => {
      setSessions((prev) => {
        const idx = prev.findIndex((s) => s.sessionId === info.sessionId)
        if (idx === -1) return [...prev, info]
        const next = [...prev]; next[idx] = info; return next
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
    servers, sshKeys, shortcuts, setShortcuts, history, settings, setSettings, securityInfo, appVersion,
    sessions, setSessions, activeSessionId, setActiveSessionId, sftpOpenBySession, setSftpOpenBySession, loadAll,
  }
}
