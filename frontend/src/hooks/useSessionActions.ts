import type { MutableRefObject } from 'react'
import type { AppSettings, CommandShortcut, ServerView, SessionInfo } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import type { ConfirmOptions } from './useActionDialogs'
import type { SidebarTab } from '../components/Sidebar'

interface ActionDeps {
  activeNav: SidebarTab
  setActiveNav: (tab: SidebarTab) => void
  activeSessionId: string | null
  setActiveSessionId: (id: string | null) => void
  sessionsRef: MutableRefObject<SessionInfo[]>
  closedSessionIdsRef: MutableRefObject<Set<string>>
  setSessions: React.Dispatch<React.SetStateAction<SessionInfo[]>>
  setSftpOpenBySession: React.Dispatch<React.SetStateAction<Record<string, boolean>>>
  setShortcuts: React.Dispatch<React.SetStateAction<CommandShortcut[]>>
  settings: AppSettings
  setSettings: React.Dispatch<React.SetStateAction<AppSettings>>
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
      d.closedSessionIdsRef.current.delete(sess.sessionId)
      d.setSessions((prev) => (prev.some((s) => s.sessionId === sess.sessionId) ? prev : [...prev, sess]))
      d.setActiveSessionId(sess.sessionId)
      if (d.settings.autoOpenSftp) d.setSftpOpenBySession((prev) => ({ ...prev, [sess.sessionId]: true }))
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
    try {
      await api.disconnectSession(sessionId)
    } catch (err) {
      d.showToast(`断开会话失败: ${errMsg(err)}`, 'error')
    }
    const remaining = d.sessionsRef.current.filter((s) => s.sessionId !== sessionId)
    d.setSessions((prev) => prev.filter((s) => s.sessionId !== sessionId))
    d.setSftpOpenBySession((prev) => {
      if (!(sessionId in prev)) return prev
      const next = { ...prev }
      delete next[sessionId]
      return next
    })
    if (d.activeSessionId === sessionId) {
      d.setActiveSessionId(remaining.length > 0 ? remaining[remaining.length - 1].sessionId : null)
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
    const next = { ...d.settings, autocompleteEnabled: !d.settings.autocompleteEnabled }
    d.setSettings(next)
    try {
      const saved = await api.saveSettings(next)
      d.setSettings(saved)
      d.showToast(saved.autocompleteEnabled ? '已开启智能命令补齐' : '已关闭智能命令补齐')
    } catch (err) {
      d.showToast(errMsg(err), 'error')
    }
  }

  const handleRunCommand = async (cmd: string, execute: boolean): Promise<boolean> => {
    if (!d.activeSessionId) {
      d.showToast('请先在左侧连接一台服务器', 'error')
      return false
    }
    try {
      await api.writeTerminal(d.activeSessionId, execute && !/[\r\n]$/.test(cmd) ? cmd + '\r' : cmd)
      if (d.activeNav !== 'servers') d.setActiveNav('servers')
      return true
    } catch (err) {
      d.showToast(`发送命令失败: ${errMsg(err)}`, 'error')
      return false
    }
  }

  const handleRunShortcut = async (sc: CommandShortcut, execute: boolean) => {
    if (!d.activeSessionId) return d.showToast('请先在左侧连接一台服务器', 'error')
    try {
      await api.runShortcut(d.activeSessionId, sc.id, execute)
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
