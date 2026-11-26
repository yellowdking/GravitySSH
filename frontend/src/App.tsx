import React, { useCallback, useEffect, useRef, useState } from 'react'
import type { ServerView } from './types'
import { useAppData } from './hooks/useAppData'
import { useLayoutState } from './hooks/useLayoutState'
import { createSessionActions } from './hooks/useSessionActions'
import { Sidebar, type SidebarTab } from './components/Sidebar'
import { KeyVaultView } from './components/KeyVaultView'
import { HistoryPanel } from './components/HistoryPanel'
import { ShortcutsPanel } from './components/ShortcutsPanel'
import { WelcomeView } from './components/WelcomeView'
import { SessionTabsBar } from './components/SessionTabsBar'
import { SessionWorkspace } from './components/SessionWorkspace'
import { VaultLockScreen } from './components/VaultLockScreen'
import { AppOverlays } from './components/AppOverlays'

export default function App() {
  const [nav, setNav] = useState<SidebarTab>('servers')
  const [drw, setDrw] = useState<'none' | 'history' | 'shortcuts'>('none')
  const [srvM, setSrvM] = useState(false)
  const [editS, setEditS] = useState<ServerView | null>(null)
  const [cfgM, setCfgM] = useState(false)
  const [syncM, setSyncM] = useState(false)
  const [toast, setToast] = useState<{ text: string; type: 'info' | 'error' } | null>(null)
  const wsRef = useRef<HTMLDivElement | null>(null)

  const notify = useCallback((text: string, type: 'info' | 'error' = 'info') => setToast({ text, type }), [])
  useEffect(() => { if (!toast) return; const t = setTimeout(() => setToast(null), 3600); return () => clearTimeout(t) }, [toast])

  const lay = useLayoutState()
  const data = useAppData(notify)
  const { sessions, activeSessionId: sid, servers, sshKeys, shortcuts, history, settings, loadAll } = data
  const act = createSessionActions({
    activeNav: nav, setActiveNav: setNav, activeSessionId: sid, setActiveSessionId: data.setActiveSessionId,
    setSessions: data.setSessions, setSftpOpenBySession: data.setSftpOpenBySession, setShortcuts: data.setShortcuts,
    settings, setSettings: data.setSettings, loadAll, showToast: notify,
    estimatePty: () => lay.estimateInitialPty(wsRef.current, settings.terminalFontSize, settings.autoOpenSftp, drw !== 'none'),
  })

  if (data.lockState?.locked) return <VaultLockScreen vaultPath={data.lockState.vaultPath} onUnlocked={() => void loadAll()} />

  const cur = sessions.find((s) => s.sessionId === sid) || null
  const addSrv = () => { setEditS(null); setSrvM(true) }
  const reload = () => { void loadAll() }
  const toggleSftp = (id: string) => data.setSftpOpenBySession((p) => ({ ...p, [id]: !p[id] }))
  const reconn = (id: string) => { const t = servers.find((s) => s.id === id); if (t) void act.handleConnectServer(t) }
  const ov = 'absolute inset-0 z-20 bg-ag-bg'

  return (
    <div className="w-screen h-screen flex overflow-hidden bg-ag-bg text-ag-text">
      <Sidebar activeNav={nav} collapsed={lay.sidebarCollapsed} width={lay.sidebarWidth} onToggleCollapse={lay.toggleSidebar} onWidthChange={lay.setSidebarWidth} onWidthCommit={lay.commitSidebarWidth} onSelectNav={setNav} servers={servers} sessions={sessions} keyCount={sshKeys.length} shortcutCount={shortcuts.length} historyCount={history.length} onAddServer={addSrv} onEditServer={(s) => { setEditS(s); setSrvM(true) }} onDeleteServer={act.handleDeleteServer} onConnectServer={act.handleConnectServer} onOpenSync={() => setSyncM(true)} onOpenSettings={() => setCfgM(true)} />
      <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden">
        <SessionTabsBar sessions={sessions} activeSessionId={sid} isServersView={nav === 'servers'} updateInfo={data.updateInfo} onSelectSession={(id) => { setNav('servers'); data.setActiveSessionId(id) }} onCloseSession={(id) => void act.handleCloseSession(id)} />
        <div ref={wsRef} className="flex-1 min-h-0 relative overflow-hidden">
          {sessions.map((s) => (
            <SessionWorkspace key={s.sessionId} session={s} visible={nav === 'servers' && sid === s.sessionId} settings={settings} showSftp={!!data.sftpOpenBySession[s.sessionId]} sftpWidth={lay.sftpWidth} onSftpWidthChange={lay.setSftpWidth} onSftpWidthCommit={lay.commitSftpWidth} rightDrawer={drw} drawerWidth={lay.drawerWidth} onDrawerWidthChange={lay.setDrawerWidth} onDrawerWidthCommit={lay.commitDrawerWidth} shortcuts={shortcuts} history={history} onToggleSftp={() => toggleSftp(s.sessionId)} onToggleRightDrawer={(m) => setDrw((p) => (p === m ? 'none' : m))} onToggleAutocomplete={() => void act.handleToggleAutocomplete()} onReconnect={reconn} onRunShortcut={act.handleRunShortcut} onRunCommand={act.handleRunCommand} onReload={reload} onToast={notify} />
          ))}
          {nav === 'servers' && !sessions.length && <WelcomeView onAddServer={addSrv} onOpenKeys={() => setNav('keys')} onOpenShortcuts={() => setNav('shortcuts')} />}
          {nav === 'keys' && <div className={ov}><KeyVaultView keys={sshKeys} servers={servers} onRefresh={reload} onToast={notify} /></div>}
          {nav === 'shortcuts' && <div className={`${ov} flex`}><ShortcutsPanel shortcuts={shortcuts} activeSessionId={cur?.sessionId} onRunShortcut={act.handleRunShortcut} onRunCommand={act.handleRunCommand} onChanged={reload} onToast={notify} /></div>}
          {nav === 'history' && <div className={`${ov} flex`}><HistoryPanel history={history} activeServerId={cur?.serverId} activeSessionId={cur?.sessionId} onRunCommand={act.handleRunCommand} onShortcutSaved={reload} onHistoryChanged={reload} onToast={notify} /></div>}
        </div>
      </div>
      <AppOverlays serverModalOpen={srvM} editingServer={editS} settingsOpen={cfgM} syncModalOpen={syncM} toast={toast} data={data} onCloseServerModal={() => setSrvM(false)} onCloseSettings={() => setCfgM(false)} onOpenSync={() => setSyncM(true)} onCloseSync={() => setSyncM(false)} onSelectNav={setNav} onReload={reload} onToast={notify} />
    </div>
  )
}
