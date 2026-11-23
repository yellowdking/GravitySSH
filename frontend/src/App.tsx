import React, { useCallback, useEffect, useState } from 'react'
import type { CommandShortcut, ServerView } from './types'
import { api } from './wailsApi'
import { useAppData } from './hooks/useAppData'
import { Sidebar, type SidebarTab } from './components/Sidebar'
import { ServerModal } from './components/ServerModal'
import { KeyVaultView } from './components/KeyVaultView'
import { TerminalPane } from './components/TerminalPane'
import { SftpPanel } from './components/SftpPanel'
import { HistoryPanel } from './components/HistoryPanel'
import { ShortcutsPanel } from './components/ShortcutsPanel'
import { SettingsModal } from './components/SettingsModal'
import { WelcomeView } from './components/WelcomeView'
import { SessionTabsBar } from './components/SessionTabsBar'

export default function App() {
  const [activeNav, setActiveNav] = useState<SidebarTab>('servers')
  const [rightDrawer, setRightDrawer] = useState<'none' | 'history' | 'shortcuts'>('none')
  const [serverModalOpen, setServerModalOpen] = useState(false)
  const [editingServer, setEditingServer] = useState<ServerView | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [toast, setToast] = useState<{ text: string; type: 'info' | 'error' } | null>(null)

  const showToast = useCallback((text: string, type: 'info' | 'error' = 'info') => setToast({ text, type }), [])
  useEffect(() => { if (!toast) return; const t = setTimeout(() => setToast(null), 3600); return () => clearTimeout(t) }, [toast])

  const {
    servers, sshKeys, shortcuts, setShortcuts, history, settings, setSettings, securityInfo, appVersion,
    sessions, setSessions, activeSessionId, setActiveSessionId, sftpOpenBySession, setSftpOpenBySession, loadAll,
  } = useAppData(showToast)

  const handleConnectServer = async (srv: ServerView) => {
    try {
      setActiveNav('servers')
      const sess = await api.connectServer(srv.id, 120, 32)
      setSessions((prev) => (prev.some((s) => s.sessionId === sess.sessionId) ? prev : [...prev, sess]))
      setActiveSessionId(sess.sessionId)
      if (settings.autoOpenSftp) setSftpOpenBySession((prev) => ({ ...prev, [sess.sessionId]: true }))
    } catch (err) { showToast(`发起连接失败: ${err}`, 'error') }
  }

  const handleCloseSession = async (sessionId: string) => {
    await api.disconnectSession(sessionId)
    setSessions((prev) => {
      const next = prev.filter((s) => s.sessionId !== sessionId)
      if (activeSessionId === sessionId) setActiveSessionId(next.length > 0 ? next[next.length - 1].sessionId : null)
      return next
    })
  }

  const handleDeleteServer = async (srv: ServerView) => {
    if (!window.confirm(`确定删除服务器 "${srv.name}" (${srv.host}) 吗？`)) return
    await api.deleteServer(srv.id); showToast(`已删除服务器 ${srv.name}`); void loadAll()
  }

  const handleToggleAutocompleteQuick = async () => {
    const next = { ...settings, autocompleteEnabled: !settings.autocompleteEnabled }
    setSettings(next)
    try {
      const saved = await api.saveSettings(next)
      setSettings(saved)
      showToast(saved.autocompleteEnabled ? '已开启智能命令补齐' : '已关闭智能命令补齐')
    } catch (err) { showToast(String(err), 'error') }
  }

  const handleRunCommandOnActive = async (cmd: string, execute: boolean) => {
    if (!activeSessionId) return showToast('请先在左侧连接一台服务器', 'error')
    await api.writeTerminal(activeSessionId, execute && !/[\r\n]$/.test(cmd) ? cmd + '\r' : cmd)
    if (activeNav !== 'servers') setActiveNav('servers')
  }

  const handleRunShortcut = async (sc: CommandShortcut, execute: boolean) => {
    if (!activeSessionId) return showToast('请先在左侧连接一台服务器', 'error')
    try {
      await api.runShortcut(activeSessionId, sc.id, execute)
      if (activeNav !== 'servers') setActiveNav('servers')
      setShortcuts((await api.listShortcuts()) || [])
    } catch (err) { showToast(String(err), 'error') }
  }

  const activeSession = sessions.find((s) => s.sessionId === activeSessionId) || null

  return (
    <div className="w-screen h-screen flex overflow-hidden bg-ag-bg text-ag-text">
      <Sidebar activeNav={activeNav} onSelectNav={setActiveNav} servers={servers} sessions={sessions} keyCount={sshKeys.length} shortcutCount={shortcuts.length} historyCount={history.length} onAddServer={() => { setEditingServer(null); setServerModalOpen(true) }} onEditServer={(s) => { setEditingServer(s); setServerModalOpen(true) }} onDeleteServer={handleDeleteServer} onConnectServer={handleConnectServer} onOpenSettings={() => setSettingsOpen(true)} />
      <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden">
        <SessionTabsBar sessions={sessions} activeSessionId={activeSessionId} isServersView={activeNav === 'servers'} onSelectSession={(id) => { setActiveNav('servers'); setActiveSessionId(id) }} onCloseSession={(id) => void handleCloseSession(id)} />
        <div className="flex-1 min-h-0 relative overflow-hidden">
          {sessions.map((sess) => {
            const isVisible = activeNav === 'servers' && activeSessionId === sess.sessionId
            const showSftp = !!sftpOpenBySession[sess.sessionId]
            return (
              <div key={sess.sessionId} className={`absolute inset-0 flex min-w-0 ${isVisible ? 'z-10 opacity-100' : 'z-0 opacity-0 pointer-events-none'}`}>
                <TerminalPane session={sess} active={isVisible} settings={settings} showSftp={showSftp} rightDrawer={rightDrawer} onToggleSftp={() => setSftpOpenBySession((p) => ({ ...p, [sess.sessionId]: !p[sess.sessionId] }))} onToggleRightDrawer={(m) => setRightDrawer((p) => (p === m ? 'none' : m))} onToggleAutocomplete={() => void handleToggleAutocompleteQuick()} onReconnect={(id) => { const t = servers.find((s) => s.id === id); if (t) void handleConnectServer(t) }} />
                {showSftp && <SftpPanel session={sess} active={isVisible} onCdInTerminal={(d) => { void api.writeTerminal(sess.sessionId, `cd ${JSON.stringify(d)}\r`) }} onToast={showToast} />}
                {rightDrawer === 'shortcuts' && <ShortcutsPanel compact shortcuts={shortcuts} activeSessionId={sess.sessionId} onRunShortcut={handleRunShortcut} onChanged={() => void loadAll()} onToast={showToast} />}
                {rightDrawer === 'history' && <HistoryPanel compact history={history} activeServerId={sess.serverId} activeSessionId={sess.sessionId} onRunCommand={handleRunCommandOnActive} onShortcutSaved={() => void loadAll()} onHistoryChanged={() => void loadAll()} onToast={showToast} />}
              </div>
            )
          })}
          {activeNav === 'servers' && sessions.length === 0 && <WelcomeView onAddServer={() => { setEditingServer(null); setServerModalOpen(true) }} onOpenKeys={() => setActiveNav('keys')} onOpenShortcuts={() => setActiveNav('shortcuts')} />}
          {activeNav === 'keys' && <div className="absolute inset-0 z-20 bg-ag-bg"><KeyVaultView keys={sshKeys} onRefresh={() => void loadAll()} onToast={showToast} /></div>}
          {activeNav === 'shortcuts' && <div className="absolute inset-0 z-20 bg-ag-bg flex"><ShortcutsPanel shortcuts={shortcuts} activeSessionId={activeSession?.sessionId} onRunShortcut={handleRunShortcut} onChanged={() => void loadAll()} onToast={showToast} /></div>}
          {activeNav === 'history' && <div className="absolute inset-0 z-20 bg-ag-bg flex"><HistoryPanel history={history} activeServerId={activeSession?.serverId} activeSessionId={activeSession?.sessionId} onRunCommand={handleRunCommandOnActive} onShortcutSaved={() => void loadAll()} onHistoryChanged={() => void loadAll()} onToast={showToast} /></div>}
        </div>
      </div>

      <ServerModal open={serverModalOpen} editingServer={editingServer} allServers={servers} sshKeys={sshKeys} onClose={() => setServerModalOpen(false)} onSaved={() => { showToast(editingServer ? '服务器配置已加密更新' : '新服务器已加密保存'); void loadAll() }} onGoToKeyVault={() => setActiveNav('keys')} />
      <SettingsModal open={settingsOpen} settings={settings} securityInfo={securityInfo} appVersion={appVersion} onClose={() => setSettingsOpen(false)} onSaved={(n) => { setSettings(n); showToast('设置已保存') }} />
      {toast && (
        <div className={`fixed bottom-5 right-5 z-50 px-4 py-2.5 rounded-xl text-xs font-medium shadow-ag border backdrop-blur-md flex items-center gap-2 ${toast.type === 'error' ? 'bg-rose-950/90 border-rose-500/40 text-rose-200' : 'bg-ag-elevated/95 border-indigo-500/40 text-ag-text'}`}>
          <span className={`w-2 h-2 rounded-full ${toast.type === 'error' ? 'bg-rose-400' : 'bg-emerald-400'}`} />
          <span>{toast.text}</span>
        </div>
      )}
    </div>
  )
}
