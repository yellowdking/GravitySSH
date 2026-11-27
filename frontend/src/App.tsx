import { Suspense, lazy, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { ServerView } from './types'
import { useAppData } from './hooks/useAppData'
import { useLayoutState } from './hooks/useLayoutState'
import { createSessionActions, type AutocompleteToggleState } from './hooks/useSessionActions'
import type { TerminalActions } from './hooks/terminalStream'
import { useActionDialogs } from './hooks/useActionDialogs'
import { Sidebar, type SidebarTab } from './components/Sidebar'
import { HistoryPanel } from './components/HistoryPanel'
import { ShortcutsPanel } from './components/ShortcutsPanel'
import { WelcomeView } from './components/WelcomeView'
import { SessionTabsBar } from './components/SessionTabsBar'
import { SessionWorkspace } from './components/SessionWorkspace'
import { VaultLockScreen } from './components/VaultLockScreen'
import { AppOverlays } from './components/AppOverlays'
import { ResizeHandle } from './components/ResizeHandle'

const KeyVaultView = lazy(() => import('./components/KeyVaultView').then((m) => ({ default: m.KeyVaultView })))

export default function App() {
  const [nav, setNav] = useState<SidebarTab>('servers')
  const [drw, setDrw] = useState<'none' | 'history' | 'shortcuts'>('none')
  const [srvM, setSrvM] = useState(false)
  const [editS, setEditS] = useState<ServerView | null>(null)
  const [cfgM, setCfgM] = useState(false)
  const [syncM, setSyncM] = useState(false)
  const [toast, setToast] = useState<{ text: string; type: 'info' | 'error' } | null>(null)
  const wsRef = useRef<HTMLDivElement | null>(null)
  const drawerRef = useRef<HTMLDivElement | null>(null)

  const notify = useCallback((text: string, type: 'info' | 'error' = 'info') => setToast({ text, type }), [])
  useEffect(() => { if (!toast) return; const t = setTimeout(() => setToast(null), 3600); return () => clearTimeout(t) }, [toast])

  const lay = useLayoutState()
  const data = useAppData(notify)
  const { confirm, dialogNode } = useActionDialogs()
  const {
    sessions, activeSessionId: sid, servers, sshKeys, shortcuts, history, settings,
    loadAll, reloadServers, reloadKeys, reloadShortcuts, reloadHistory,
    setActiveSessionId, setSessions, setSftpOpenBySession, setShortcuts, setSettings,
    closedSessionIdsRef, lockState,
  } = data

  useEffect(() => {
    if (lockState?.locked) {
      setCfgM(false)
      setSrvM(false)
      setSyncM(false)
    }
  }, [lockState?.locked])

  useLayoutEffect(() => {
    wsRef.current?.style.setProperty('--gssh-sftp-width', `${lay.sftpWidth}px`)
  }, [lay.sftpWidth, lockState?.locked])

  useLayoutEffect(() => {
    if (drawerRef.current) drawerRef.current.style.width = `${lay.drawerWidth}px`
  }, [lay.drawerWidth, drw, nav, sessions.length, lockState?.locked])

  const navRef = useRef(nav)
  navRef.current = nav
  const sidRef = useRef(sid)
  sidRef.current = sid
  const drwRef = useRef(drw)
  drwRef.current = drw
  const sessionsRef = useRef(sessions)
  sessionsRef.current = sessions
  const serversRef = useRef(servers)
  serversRef.current = servers
  const settingsRef = useRef(settings)
  settingsRef.current = settings
  const autocompleteToggleStateRef = useRef<AutocompleteToggleState>({
    seq: 0,
    lastConfirmedSeq: 0,
    inFlight: 0,
    confirmedEnabled: settings.autocompleteEnabled,
  })
  if (autocompleteToggleStateRef.current.inFlight === 0) {
    autocompleteToggleStateRef.current.confirmedEnabled = settings.autocompleteEnabled
  }
  const terminalActionsRef = useRef<Map<string, TerminalActions>>(new Map())

  const registerTerminalActions = useCallback((sessionId: string, actions: TerminalActions | null) => {
    if (actions) terminalActionsRef.current.set(sessionId, actions)
    else terminalActionsRef.current.delete(sessionId)
  }, [])

  const estimatePty = useCallback(
    () =>
      lay.estimateInitialPty(
        wsRef.current,
        settingsRef.current.terminalFontSize,
        settingsRef.current.autoOpenSftp,
        drwRef.current !== 'none'
      ),
    [lay.estimateInitialPty]
  )

  const act = useMemo(
    () =>
      createSessionActions({
        activeNav: navRef.current,
        activeNavRef: navRef,
        setActiveNav: setNav,
        activeSessionId: sidRef.current,
        activeSessionIdRef: sidRef,
        setActiveSessionId,
        sessionsRef,
        closedSessionIdsRef,
        terminalActionsRef,
        setSessions,
        setSftpOpenBySession,
        setShortcuts,
        settings: settingsRef.current,
        settingsRef,
        autocompleteToggleStateRef,
        setSettings,
        reloadServers,
        confirm,
        showToast: notify,
        estimatePty,
      }),
    [
      setActiveSessionId,
      closedSessionIdsRef,
      setSessions,
      setSftpOpenBySession,
      setShortcuts,
      setSettings,
      reloadServers,
      confirm,
      notify,
      estimatePty,
    ]
  )

  const reload = useCallback(() => { void loadAll() }, [loadAll])
  const handleServersReload = useCallback(() => { void reloadServers() }, [reloadServers])
  const handleShortcutsReload = useCallback(() => { void reloadShortcuts() }, [reloadShortcuts])
  const handleHistoryReload = useCallback(() => { void reloadHistory() }, [reloadHistory])
  const handleKeysReload = useCallback(() => {
    void reloadKeys()
    void reloadServers()
  }, [reloadKeys, reloadServers])
  const toggleSftp = useCallback((id: string) => setSftpOpenBySession((p) => ({ ...p, [id]: !p[id] })), [setSftpOpenBySession])
  const toggleRightDrawer = useCallback((m: 'history' | 'shortcuts') => setDrw((p) => (p === m ? 'none' : m)), [])
  const handleToggleAutocomplete = useCallback(() => { void act.handleToggleAutocomplete() }, [act])
  const reconn = useCallback((id: string) => {
    const t = serversRef.current.find((s) => s.id === id)
    if (t) void act.handleConnectServer(t)
    else notify('该服务器已被删除，无法重新连接', 'error')
  }, [act, notify])

  const previewSftpWidth = useCallback((w: number) => {
    wsRef.current?.style.setProperty('--gssh-sftp-width', `${w}px`)
  }, [])
  const commitSftpWidth = useCallback((w: number) => {
    wsRef.current?.style.setProperty('--gssh-sftp-width', `${w}px`)
    lay.commitSftpWidth(w)
  }, [lay.commitSftpWidth])

  const previewDrawerWidth = useCallback((w: number) => {
    if (drawerRef.current) drawerRef.current.style.width = `${w}px`
  }, [])
  const commitDrawerWidth = useCallback((w: number) => {
    if (drawerRef.current) drawerRef.current.style.width = `${w}px`
    lay.commitDrawerWidth(w)
  }, [lay.commitDrawerWidth])

  const addSrv = useCallback(() => { setEditS(null); setSrvM(true) }, [])
  const handleEditServer = useCallback((s: ServerView) => { setEditS(s); setSrvM(true) }, [])
  const handleOpenSync = useCallback(() => setSyncM(true), [])
  const handleCloseSync = useCallback(() => setSyncM(false), [])
  const handleOpenSettings = useCallback(() => setCfgM(true), [])
  const handleCloseSettings = useCallback(() => setCfgM(false), [])
  const handleCloseServerModal = useCallback(() => setSrvM(false), [])
  const handleSelectSession = useCallback((id: string) => { setNav('servers'); setActiveSessionId(id) }, [setActiveSessionId])
  const handleCloseSession = useCallback((id: string) => { void act.handleCloseSession(id) }, [act])
  const handleOpenKeys = useCallback(() => setNav('keys'), [])
  const handleOpenShortcuts = useCallback(() => setNav('shortcuts'), [])

  if (lockState?.locked) return <VaultLockScreen vaultPath={lockState.vaultPath} onUnlocked={reload} />

  const cur = sessions.find((s) => s.sessionId === sid) || null
  const ov = 'absolute inset-0 z-20 bg-ag-bg'

  return (
    <div className="w-screen h-screen flex overflow-hidden bg-ag-bg text-ag-text">
      <Sidebar
        activeNav={nav}
        collapsed={lay.sidebarCollapsed}
        width={lay.sidebarWidth}
        onToggleCollapse={lay.toggleSidebar}
        onWidthCommit={lay.commitSidebarWidth}
        onSelectNav={setNav}
        servers={servers}
        sessions={sessions}
        keyCount={sshKeys.length}
        shortcutCount={shortcuts.length}
        onAddServer={addSrv}
        onEditServer={handleEditServer}
        onDeleteServer={act.handleDeleteServer}
        onConnectServer={act.handleConnectServer}
        onOpenSync={handleOpenSync}
        onOpenSettings={handleOpenSettings}
      />
      <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden">
        <SessionTabsBar
          sessions={sessions}
          activeSessionId={sid}
          isServersView={nav === 'servers'}
          updateInfo={data.updateInfo}
          onSelectSession={handleSelectSession}
          onCloseSession={handleCloseSession}
        />
        <div ref={wsRef} className="flex-1 min-h-0 flex relative overflow-hidden">
          <div className="flex-1 min-w-0 h-full relative overflow-hidden">
            {sessions.map((s) => {
              const isVisible = nav === 'servers' && sid === s.sessionId
              const isSftpOpen = !!data.sftpOpenBySession[s.sessionId]
              return (
                <SessionWorkspace
                  key={s.sessionId}
                  session={s}
                  visible={isVisible}
                  settings={settings}
                  showSftp={isSftpOpen}
                  sftpWidth={isVisible && isSftpOpen ? lay.sftpWidth : 0}
                  onSftpWidthChange={previewSftpWidth}
                  onSftpWidthCommit={commitSftpWidth}
                  rightDrawer={isVisible ? drw : 'none'}
                  onToggleSftp={toggleSftp}
                  onToggleRightDrawer={toggleRightDrawer}
                  onToggleAutocomplete={handleToggleAutocomplete}
                  onReconnect={reconn}
                  onRegisterTerminal={registerTerminalActions}
                  onToast={notify}
                />
              )
            })}
            {nav === 'servers' && !sessions.length && (
              <WelcomeView onAddServer={addSrv} onOpenKeys={handleOpenKeys} onOpenShortcuts={handleOpenShortcuts} />
            )}
          </div>
          {nav === 'servers' && sessions.length > 0 && drw !== 'none' && (
            <>
              <ResizeHandle
                width={lay.drawerWidth}
                min={260}
                max={560}
                direction="left"
                onChange={previewDrawerWidth}
                onCommit={commitDrawerWidth}
                title="拖动调整抽屉宽度"
              />
              <div ref={drawerRef} className="shrink-0 h-full min-w-0 flex overflow-hidden [&>*]:!w-full [&>*]:min-w-0">
                {drw === 'shortcuts' && (
                  <ShortcutsPanel compact shortcuts={shortcuts} activeSessionId={cur?.sessionId} onRunShortcut={act.handleRunShortcut} onRunCommand={act.handleRunCommand} onChanged={handleShortcutsReload} onToast={notify} />
                )}
                {drw === 'history' && (
                  <HistoryPanel compact history={history} activeServerId={cur?.serverId} activeSessionId={cur?.sessionId} onRunCommand={act.handleRunCommand} onShortcutSaved={handleShortcutsReload} onHistoryChanged={handleHistoryReload} onToast={notify} />
                )}
              </div>
            </>
          )}
          {nav === 'keys' && (
            <div className={ov}>
              <Suspense fallback={<div className="h-full flex items-center justify-center text-xs text-ag-muted">正在加载密钥库...</div>}>
                <KeyVaultView keys={sshKeys} servers={servers} onRefresh={handleKeysReload} onToast={notify} />
              </Suspense>
            </div>
          )}
          {nav === 'shortcuts' && <div className={`${ov} flex`}><ShortcutsPanel shortcuts={shortcuts} activeSessionId={cur?.sessionId} onRunShortcut={act.handleRunShortcut} onRunCommand={act.handleRunCommand} onChanged={handleShortcutsReload} onToast={notify} /></div>}
          {nav === 'history' && <div className={`${ov} flex`}><HistoryPanel history={history} activeServerId={cur?.serverId} activeSessionId={cur?.sessionId} onRunCommand={act.handleRunCommand} onShortcutSaved={handleShortcutsReload} onHistoryChanged={handleHistoryReload} onToast={notify} /></div>}
        </div>
      </div>
      <AppOverlays
        serverModalOpen={srvM}
        editingServer={editS}
        settingsOpen={cfgM}
        syncModalOpen={syncM}
        toast={toast}
        data={data}
        onCloseServerModal={handleCloseServerModal}
        onCloseSettings={handleCloseSettings}
        onOpenSync={handleOpenSync}
        onCloseSync={handleCloseSync}
        onSelectNav={setNav}
        onServerSaved={handleServersReload}
        onReload={reload}
        onToast={notify}
      />
      {dialogNode}
    </div>
  )
}
