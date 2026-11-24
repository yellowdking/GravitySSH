import type { AppSettings, CommandShortcut, ServerView, SessionInfo } from '../types'
import { api } from '../wailsApi'
import type { SidebarTab } from '../components/Sidebar'

interface ActionDeps {
  activeNav: SidebarTab
  setActiveNav: (tab: SidebarTab) => void
  activeSessionId: string | null
  setActiveSessionId: (id: string | null) => void
  setSessions: React.Dispatch<React.SetStateAction<SessionInfo[]>>
  setSftpOpenBySession: React.Dispatch<React.SetStateAction<Record<string, boolean>>>
  setShortcuts: React.Dispatch<React.SetStateAction<CommandShortcut[]>>
  settings: AppSettings
  setSettings: React.Dispatch<React.SetStateAction<AppSettings>>
  estimatePty: () => { cols: number; rows: number }
  loadAll: () => Promise<void>
  showToast: (text: string, type?: 'info' | 'error') => void
}

export function createSessionActions(d: ActionDeps) {
  const handleConnectServer = async (srv: ServerView) => {
    try {
      d.setActiveNav('servers')
      const { cols, rows } = d.estimatePty()
      const sess = await api.connectServer(srv.id, cols, rows)
      d.setSessions((prev) => (prev.some((s) => s.sessionId === sess.sessionId) ? prev : [...prev, sess]))
      d.setActiveSessionId(sess.sessionId)
      if (d.settings.autoOpenSftp) d.setSftpOpenBySession((prev) => ({ ...prev, [sess.sessionId]: true }))
    } catch (err) {
      d.showToast(`发起连接失败: ${err}`, 'error')
    }
  }

  const handleCloseSession = async (sessionId: string) => {
    await api.disconnectSession(sessionId)
    d.setSessions((prev) => {
      const next = prev.filter((s) => s.sessionId !== sessionId)
      if (d.activeSessionId === sessionId) d.setActiveSessionId(next.length > 0 ? next[next.length - 1].sessionId : null)
      return next
    })
  }

  const handleDeleteServer = async (srv: ServerView) => {
    if (!window.confirm(`确定删除服务器 "${srv.name}" (${srv.host}) 吗？`)) return
    await api.deleteServer(srv.id)
    d.showToast(`已删除服务器 ${srv.name}`)
    void d.loadAll()
  }

  const handleToggleAutocomplete = async () => {
    const next = { ...d.settings, autocompleteEnabled: !d.settings.autocompleteEnabled }
    d.setSettings(next)
    try {
      const saved = await api.saveSettings(next)
      d.setSettings(saved)
      d.showToast(saved.autocompleteEnabled ? '已开启智能命令补齐' : '已关闭智能命令补齐')
    } catch (err) {
      d.showToast(String(err), 'error')
    }
  }

  const handleRunCommand = async (cmd: string, execute: boolean) => {
    if (!d.activeSessionId) return d.showToast('请先在左侧连接一台服务器', 'error')
    await api.writeTerminal(d.activeSessionId, execute && !/[\r\n]$/.test(cmd) ? cmd + '\r' : cmd)
    if (d.activeNav !== 'servers') d.setActiveNav('servers')
  }

  const handleRunShortcut = async (sc: CommandShortcut, execute: boolean) => {
    if (!d.activeSessionId) return d.showToast('请先在左侧连接一台服务器', 'error')
    try {
      await api.runShortcut(d.activeSessionId, sc.id, execute)
      if (d.activeNav !== 'servers') d.setActiveNav('servers')
      d.setShortcuts((await api.listShortcuts()) || [])
    } catch (err) {
      d.showToast(String(err), 'error')
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
