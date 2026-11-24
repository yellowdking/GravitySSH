import React, { useCallback, useEffect, useRef, useState } from 'react'
import type { ServerView } from './types'
import { useAppData } from './hooks/useAppData'
import { useLayoutState } from './hooks/useLayoutState'
import { createSessionActions } from './hooks/useSessionActions'
import { Sidebar, type SidebarTab } from './components/Sidebar'
import { ServerModal } from './components/ServerModal'
import { KeyVaultView } from './components/KeyVaultView'
import { HistoryPanel } from './components/HistoryPanel'
import { ShortcutsPanel } from './components/ShortcutsPanel'
import { SettingsModal } from './components/SettingsModal'
import { WelcomeView } from './components/WelcomeView'
import { SessionTabsBar } from './components/SessionTabsBar'
import { SessionWorkspace } from './components/SessionWorkspace'
import { VaultLockScreen } from './components/VaultLockScreen'

export default function App() {
  const [activeNav, setActiveNav] = useState<SidebarTab>('servers')
  const [rightDrawer, setRightDrawer] = useState<'none' | 'history' | 'shortcuts'>('none')
  const [serverModalOpen, setServerModalOpen] = useState(false)
  const [editingServer, setEditingServer] = useState<ServerView | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [toast, setToast] = useState<{ text: string; type: 'info' | 'error' } | null>(null)
  const workspaceRef = useRef<HTMLDivElement | null>(null)

  const showToast = useCallback((text: string, type: 'info' | 'error' = 'info') => setToast({ text, type }), [])
  useEffect(() => { if (!toast) return; const t = setTimeout(() => setToast(null), 3600); return () => clearTimeout(t) }, [toast])

  const layout = useLayoutState()
  const data = useAppData(showToast)
  const actions = createSessionActions({
    activeNav, setActiveNav, activeSessionId: data.activeSessionId, setActiveSessionId: data.setActiveSessionId,
    setSessions: data.setSessions, setSftpOpenBySession: data.setSftpOpenBySession, setShortcuts: data.setShortcuts,
    settings: data.settings, setSettings: data.setSettings, loadAll: data.loadAll, showToast,
    estimatePty: () => layout.estimateInitialPty(workspaceRef.current, data.settings.terminalFontSize, data.settings.autoOpenSftp, rightDrawer !== 'none'),
  })

  if (data.lockState?.locked) {
    return <VaultLockScreen vaultPath={data.lockState.vaultPath} onUnlocked={() => void data.loadAll()} />
  }

  const activeSession = data.sessions.find((s) => s.sessionId === data.activeSessionId) || null
  const openAddServer = () => { setEditingServer(null); setServerModalOpen(true) }
  const reload = () => { void data.loadAll() }

  return (
    <div className="w-screen h-screen flex overflow-hidden bg-ag-bg text-ag-text">
      <Sidebar activeNav={activeNav} collapsed={layout.sidebarCollapsed} width={layout.sidebarWidth} onToggleCollapse={layout.toggleSidebar} onWidthChange={layout.setSidebarWidth} onWidthCommit={layout.commitSidebarWidth} onSelectNav={setActiveNav} servers={data.servers} sessions={data.sessions} keyCount={data.sshKeys.length} shortcutCount={data.shortcuts.length} historyCount={data.history.length} onAddServer={openAddServer} onEditServer={(s) => { setEditingServer(s); setServerModalOpen(true) }} onDeleteServer={actions.handleDeleteServer} onConnectServer={actions.handleConnectServer} onOpenSettings={() => setSettingsOpen(true)} />
      <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden">
        <SessionTabsBar sessions={data.sessions} activeSessionId={data.activeSessionId} isServersView={activeNav === 'servers'} onSelectSession={(id) => { setActiveNav('servers'); data.setActiveSessionId(id) }} onCloseSession={(id) => void actions.handleCloseSession(id)} />
        <div ref={workspaceRef} className="flex-1 min-h-0 relative overflow-hidden">
          {data.sessions.map((sess) => (
            <SessionWorkspace key={sess.sessionId} session={sess} visible={activeNav === 'servers' && data.activeSessionId === sess.sessionId} settings={data.settings} showSftp={!!data.sftpOpenBySession[sess.sessionId]} sftpWidth={layout.sftpWidth} onSftpWidthChange={layout.setSftpWidth} onSftpWidthCommit={layout.commitSftpWidth} rightDrawer={rightDrawer} drawerWidth={layout.drawerWidth} onDrawerWidthChange={layout.setDrawerWidth} onDrawerWidthCommit={layout.commitDrawerWidth} shortcuts={data.shortcuts} history={data.history} onToggleSftp={() => data.setSftpOpenBySession((p) => ({ ...p, [sess.sessionId]: !p[sess.sessionId] }))} onToggleRightDrawer={(m) => setRightDrawer((p) => (p === m ? 'none' : m))} onToggleAutocomplete={() => void actions.handleToggleAutocomplete()} onReconnect={(id) => { const t = data.servers.find((s) => s.id === id); if (t) void actions.handleConnectServer(t) }} onRunShortcut={actions.handleRunShortcut} onRunCommand={actions.handleRunCommand} onReload={reload} onToast={showToast} />
          ))}
          {activeNav === 'servers' && data.sessions.length === 0 && <WelcomeView onAddServer={openAddServer} onOpenKeys={() => setActiveNav('keys')} onOpenShortcuts={() => setActiveNav('shortcuts')} />}
          {activeNav === 'keys' && <div className="absolute inset-0 z-20 bg-ag-bg"><KeyVaultView keys={data.sshKeys} servers={data.servers} onRefresh={reload} onToast={showToast} /></div>}
          {activeNav === 'shortcuts' && <div className="absolute inset-0 z-20 bg-ag-bg flex"><ShortcutsPanel shortcuts={data.shortcuts} activeSessionId={activeSession?.sessionId} onRunShortcut={actions.handleRunShortcut} onChanged={reload} onToast={showToast} /></div>}
          {activeNav === 'history' && <div className="absolute inset-0 z-20 bg-ag-bg flex"><HistoryPanel history={data.history} activeServerId={activeSession?.serverId} activeSessionId={activeSession?.sessionId} onRunCommand={actions.handleRunCommand} onShortcutSaved={reload} onHistoryChanged={reload} onToast={showToast} /></div>}
        </div>
      </div>

      <ServerModal open={serverModalOpen} editingServer={editingServer} allServers={data.servers} sshKeys={data.sshKeys} onClose={() => setServerModalOpen(false)} onSaved={() => { showToast(editingServer ? '服务器配置已更新' : '新服务器已保存'); reload() }} onGoToKeyVault={() => setActiveNav('keys')} />
      <SettingsModal open={settingsOpen} settings={data.settings} securityInfo={data.securityInfo} lockState={data.lockState} appVersion={data.appVersion} onClose={() => setSettingsOpen(false)} onSaved={(n) => { data.setSettings(n); showToast('设置已保存') }} onLockStateChanged={reload} onToast={showToast} />
      {toast && (
        <div className={`fixed bottom-5 right-5 z-50 px-4 py-2.5 rounded-xl text-xs font-medium shadow-ag border backdrop-blur-md flex items-center gap-2 ${toast.type === 'error' ? 'bg-rose-950/90 border-rose-500/40 text-rose-200' : 'bg-ag-elevated/95 border-indigo-500/40 text-ag-text'}`}>
          <span className={`w-2 h-2 rounded-full ${toast.type === 'error' ? 'bg-rose-400' : 'bg-emerald-400'}`} />
          <span>{toast.text}</span>
        </div>
      )}
    </div>
  )
}
