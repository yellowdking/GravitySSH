import type { Dispatch, MutableRefObject, SetStateAction } from 'react'
import type { AppSettings, CommandShortcut, ServerView, SessionInfo } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import type { ConfirmOptions } from './useActionDialogs'
import type { SidebarTab } from '../components/Sidebar'
import { discardTerminalBuffer, type TerminalActions } from './terminalStream'

export interface AutocompleteToggleState {
  seq: number
  lastConfirmedSeq: number
  inFlight: number
  confirmedEnabled: boolean
}

interface ActionDeps {
  activeNav: SidebarTab
  setActiveNav: (tab: SidebarTab) => void
  activeSessionId: string | null
  setActiveSessionId: Dispatch<SetStateAction<string | null>>
  sessionsRef: MutableRefObject<SessionInfo[]>
  closedSessionIdsRef: MutableRefObject<Set<string>>
  terminalActionsRef: MutableRefObject<Map<string, TerminalActions>>
  setSessions: Dispatch<SetStateAction<SessionInfo[]>>
  setSftpOpenBySession: Dispatch<SetStateAction<Record<string, boolean>>>
  setShortcuts: Dispatch<SetStateAction<CommandShortcut[]>>
  settings: AppSettings
  settingsRef?: MutableRefObject<AppSettings>
  autocompleteToggleStateRef?: MutableRefObject<AutocompleteToggleState>
  setSettings: Dispatch<SetStateAction<AppSettings>>
  estimatePty: () => { cols: number; rows: number }
  reloadServers: () => Promise<void>
  confirm: (opts: ConfirmOptions) => Promise<boolean>
  showToast: (text: string, type?: 'info' | 'error') => void
}

export function createSessionActions(d: ActionDeps) {
  const handleConnectServer = async (srv: ServerView) => {
    try {
      d.setActiveNav('servers')
      const { cols, rows } = d.estimatePty()
      const sess = await api.connectServer(srv.id, cols, rows)
      if (d.closedSessionIdsRef.current.has(sess.sessionId)) {
        discardTerminalBuffer(sess.sessionId)
        return
      }
      d.setSessions((prev) => (prev.some((s) => s.sessionId === sess.sessionId) ? prev : [...prev, sess]))
      d.setActiveSessionId(sess.sessionId)
      if (d.settings.autoOpenSftp) {
        d.setSftpOpenBySession((prev) => ({ ...prev, [sess.sessionId]: prev[sess.sessionId] ?? true }))
      }
    } catch (err) {
      d.showToast(`发起连接失败: ${errMsg(err)}`, 'error')
    }
  }

  const handleCloseSession = async (sessionId: string) => {
    const closed = d.closedSessionIdsRef.current
    if (closed.has(sessionId)) return
    closed.add(sessionId)
    if (closed.size > 200) {
      const oldest = closed.values().next().value
      if (oldest) closed.delete(oldest)
    }
    discardTerminalBuffer(sessionId)
    d.setSessions((prev) => prev.filter((s) => s.sessionId !== sessionId))
    d.setSftpOpenBySession((prev) => {
      if (!(sessionId in prev)) return prev
      const next = { ...prev }
      delete next[sessionId]
      return next
    })
    d.setActiveSessionId((cur) => {
      if (cur !== sessionId) return cur
      const remaining = d.sessionsRef.current.filter(
        (s) => s.sessionId !== sessionId && !closed.has(s.sessionId)
      )
      return remaining.length > 0 ? remaining[remaining.length - 1].sessionId : null
    })
    try {
      await api.disconnectSession(sessionId)
    } catch (err) {
      d.showToast(`断开会话失败: ${errMsg(err)}`, 'error')
    }
  }

  const handleDeleteServer = async (srv: ServerView) => {
    const ok = await d.confirm({
      title: '删除服务器',
      message: `确定删除服务器 "${srv.name}" (${srv.host}) 吗？`,
      confirmLabel: '删除',
    })
    if (!ok) return
    try {
      await api.deleteServer(srv.id)
      d.showToast(`已删除服务器 ${srv.name}`)
      void d.reloadServers()
    } catch (err) {
      d.showToast(`删除服务器失败: ${errMsg(err)}`, 'error')
    }
  }

  const handleToggleAutocomplete = async () => {
    const state = d.autocompleteToggleStateRef?.current ?? {
      seq: 0,
      lastConfirmedSeq: 0,
      inFlight: 0,
      confirmedEnabled: (d.settingsRef?.current ?? d.settings).autocompleteEnabled,
    }
    const baseSettings = d.settingsRef?.current ?? d.settings
    if (state.inFlight === 0) {
      state.confirmedEnabled = baseSettings.autocompleteEnabled
    }
    const seq = ++state.seq
    state.inFlight++
    const next: AppSettings = { ...baseSettings, autocompleteEnabled: !baseSettings.autocompleteEnabled }
    if (d.settingsRef) d.settingsRef.current = next
    d.setSettings(next)
    try {
      const saved = await api.saveSettings(next)
      if (seq >= state.lastConfirmedSeq) {
        state.lastConfirmedSeq = seq
        state.confirmedEnabled = saved.autocompleteEnabled
      }
      if (seq !== state.seq) return
      if (d.settingsRef) d.settingsRef.current = saved
      d.setSettings(saved)
      d.showToast(saved.autocompleteEnabled ? '已开启智能命令补齐' : '已关闭智能命令补齐')
    } catch (err) {
      if (seq === state.seq) {
        const cur = d.settingsRef?.current ?? next
        const rolledBack: AppSettings = { ...cur, autocompleteEnabled: state.confirmedEnabled }
        if (d.settingsRef) d.settingsRef.current = rolledBack
        d.setSettings(rolledBack)
        d.showToast(errMsg(err), 'error')
      }
    } finally {
      state.inFlight = Math.max(0, state.inFlight - 1)
    }
  }

  const fillActiveTerminal = (raw: string): boolean => {
    if (!d.activeSessionId) {
      d.showToast('请先在左侧连接一台服务器', 'error')
      return false
    }
    const termAct = d.terminalActionsRef.current.get(d.activeSessionId)
    if (!termAct) {
      d.showToast('终端尚未就绪，请稍后重试', 'error')
      return false
    }
    const fillText = raw.replace(/[\r\n]+$/, '')
    if (d.activeNav !== 'servers') d.setActiveNav('servers')
    if (fillText) termAct.pasteText(fillText)
    return true
  }

  const handleRunCommand = async (cmd: string, execute: boolean): Promise<boolean> => {
    if (!d.activeSessionId) {
      d.showToast('请先在左侧连接一台服务器', 'error')
      return false
    }
    if (!execute) {
      return fillActiveTerminal(cmd)
    }
    try {
      d.terminalActionsRef.current.get(d.activeSessionId)?.resetInputLine()
      await api.writeTerminal(d.activeSessionId, !/[\r\n]$/.test(cmd) ? cmd + '\r' : cmd)
      if (d.activeNav !== 'servers') d.setActiveNav('servers')
      return true
    } catch (err) {
      d.showToast(`发送命令失败: ${errMsg(err)}`, 'error')
      return false
    }
  }

  const handleRunShortcut = async (sc: CommandShortcut, execute: boolean) => {
    if (!d.activeSessionId) return d.showToast('请先在左侧连接一台服务器', 'error')
    if (!execute) {
      fillActiveTerminal(sc.command)
      return
    }
    try {
      d.terminalActionsRef.current.get(d.activeSessionId)?.resetInputLine()
      await api.runShortcut(d.activeSessionId, sc.id, true)
      if (d.activeNav !== 'servers') d.setActiveNav('servers')
      d.setShortcuts((await api.listShortcuts()) || [])
    } catch (err) {
      d.showToast(errMsg(err), 'error')
    }
  }

  return {
    handleConnectServer,
    handleCloseSession,
    handleDeleteServer,
    handleToggleAutocomplete,
    handleRunCommand,
    handleRunShortcut,
  }
}
