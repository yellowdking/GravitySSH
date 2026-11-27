import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AppSettings, CommandShortcut, ServerView, SessionInfo } from '../types'
import { createSessionActions, type AutocompleteToggleState } from './useSessionActions'
import type { TerminalActions } from './terminalStream'
import type { SidebarTab } from '../components/Sidebar'

const DEFAULT_SETTINGS: AppSettings = {
  autocompleteEnabled: true,
  autocompleteFromHistory: true,
  autoOpenSftp: true,
  terminalFontFamily: 'monospace',
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

function makeServer(id: string): ServerView {
  return {
    id,
    name: `srv-${id}`,
    group: '默认分组',
    color: '#6366f1',
    host: '198.51.100.10',
    port: 22,
    username: 'root',
    authType: 'password',
    hasPassword: true,
    proxy: { type: 'none', host: '', port: 0, username: '', hasPassword: false },
    jumpHost: { mode: 'none', hasPassword: false },
    timeoutSeconds: 15,
    keepAliveSeconds: 30,
    defaultRemotePath: '',
    startupCommand: '',
    notes: '',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  }
}

function makeSession(sessionId: string, serverId = 'srv-1'): SessionInfo {
  return {
    sessionId,
    serverId,
    serverName: serverId,
    host: '198.51.100.10',
    port: 22,
    username: 'root',
    status: 'connected',
    sftpReady: true,
    homeDir: '/root',
    createdAt: '2026-01-01T00:00:00Z',
  }
}

function createHarness(appMock: Record<string, (...args: unknown[]) => Promise<unknown>>) {
  ;(globalThis as unknown as { window: unknown }).window = {
    go: { main: { App: appMock } },
  }

  let activeNav: SidebarTab = 'servers'
  let activeSessionId: string | null = null
  let sessions: SessionInfo[] = []
  let sftpOpenBySession: Record<string, boolean> = {}
  let shortcuts: CommandShortcut[] = []
  let settings: AppSettings = { ...DEFAULT_SETTINGS }

  const activeNavRef: { current: SidebarTab } = { current: activeNav }
  const activeSessionIdRef: { current: string | null } = { current: activeSessionId }
  const sessionsRef: { current: SessionInfo[] } = { current: sessions }
  const closedSessionIdsRef = { current: new Set<string>() }
  const terminalActionsRef = { current: new Map<string, TerminalActions>() }
  const settingsRef: { current: AppSettings } = { current: settings }
  const autocompleteToggleStateRef = {
    current: {
      seq: 0,
      lastConfirmedSeq: 0,
      inFlight: 0,
      confirmedEnabled: settings.autocompleteEnabled,
    } as AutocompleteToggleState,
  }
  const toasts: Array<{ text: string; type?: 'info' | 'error' }> = []

  const setActiveSessionId = (updater: string | null | ((prev: string | null) => string | null)) => {
    activeSessionId = typeof updater === 'function' ? updater(activeSessionId) : updater
    activeSessionIdRef.current = activeSessionId
  }
  const setSessions = (updater: SessionInfo[] | ((prev: SessionInfo[]) => SessionInfo[])) => {
    sessions = typeof updater === 'function' ? updater(sessions) : updater
    sessionsRef.current = sessions
  }

  const actions = createSessionActions({
    activeNav,
    activeNavRef,
    setActiveNav: (tab) => {
      activeNav = tab
      activeNavRef.current = tab
    },
    activeSessionId,
    activeSessionIdRef,
    setActiveSessionId,
    sessionsRef,
    closedSessionIdsRef,
    terminalActionsRef,
    setSessions,
    setSftpOpenBySession: (updater) => {
      sftpOpenBySession = typeof updater === 'function' ? updater(sftpOpenBySession) : updater
    },
    setShortcuts: (updater) => {
      shortcuts = typeof updater === 'function' ? updater(shortcuts) : updater
    },
    settings,
    settingsRef,
    autocompleteToggleStateRef,
    setSettings: (updater) => {
      settings = typeof updater === 'function' ? updater(settings) : updater
      settingsRef.current = settings
    },
    estimatePty: () => ({ cols: 120, rows: 36 }),
    reloadServers: vi.fn().mockResolvedValue(undefined),
    confirm: vi.fn().mockResolvedValue(true),
    showToast: (text, type) => toasts.push({ text, type }),
  })

  return {
    actions,
    activeNavRef,
    activeSessionIdRef,
    sessionsRef,
    closedSessionIdsRef,
    terminalActionsRef,
    settingsRef,
    setSessions,
    setActiveSessionId,
    toasts,
    getState: () => ({ activeNav, activeSessionId, sessions, sftpOpenBySession, shortcuts, settings }),
  }
}

describe('useSessionActions', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    delete (globalThis as unknown as { window?: unknown }).window
  })

  it('connects a server and avoids resurrecting a session closed while ConnectServer is in flight', async () => {
    let resolveConnect!: (s: SessionInfo) => void
    const ConnectServer = vi.fn(
      () =>
        new Promise<SessionInfo>((r) => {
          resolveConnect = r
        })
    )
    const DisconnectSession = vi.fn().mockResolvedValue(undefined)
    const h = createHarness({ ConnectServer, DisconnectSession })

    const connectPromise = h.actions.handleConnectServer(makeServer('s1'))
    expect(ConnectServer).toHaveBeenCalledWith('s1', 120, 36)

    // User closes the connecting tab before ConnectServer resolves
    await h.actions.handleCloseSession('sess-race')
    resolveConnect(makeSession('sess-race', 's1'))
    await connectPromise

    expect(h.getState().sessions).toHaveLength(0)
    expect(h.getState().activeSessionId).toBeNull()
  })

  it('activates the previous remaining open tab when closing the active session and keeps focus when closing background tab', async () => {
    const DisconnectSession = vi.fn().mockResolvedValue(undefined)
    const h = createHarness({ DisconnectSession })
    const s1 = makeSession('sess-1', 'srv-1')
    const s2 = makeSession('sess-2', 'srv-2')
    const s3 = makeSession('sess-3', 'srv-3')
    h.setSessions([s1, s2, s3])
    h.setActiveSessionId('sess-3')

    // Close active tab -> switches to last remaining open tab (sess-2)
    await h.actions.handleCloseSession('sess-3')
    expect(DisconnectSession).toHaveBeenCalledWith('sess-3')
    expect(h.getState().activeSessionId).toBe('sess-2')
    expect(h.getState().sessions.map((s) => s.sessionId)).toEqual(['sess-1', 'sess-2'])

    // Close background tab (sess-1) -> active tab stays sess-2
    await h.actions.handleCloseSession('sess-1')
    expect(DisconnectSession).toHaveBeenCalledWith('sess-1')
    expect(h.getState().activeSessionId).toBe('sess-2')
    expect(h.getState().sessions.map((s) => s.sessionId)).toEqual(['sess-2'])
  })

  it('routes shortcut fill-only (execute=false) through pasteText without trailing newline, and execute=true to RunShortcut', async () => {
    const RunShortcut = vi.fn().mockResolvedValue(undefined)
    const ListShortcuts = vi.fn().mockResolvedValue([])
    const h = createHarness({ RunShortcut, ListShortcuts })

    const pasteText = vi.fn()
    const resetInputLine = vi.fn()
    h.terminalActionsRef.current.set('sess-1', { pasteText, resetInputLine })
    h.activeSessionIdRef.current = 'sess-1'
    h.activeNavRef.current = 'shortcuts'

    const sc: CommandShortcut = {
      id: 'sc-1',
      name: 'List',
      command: 'ls -la\n',
      description: '',
      category: '常用',
      autoExecute: true,
      pinned: false,
      useCount: 0,
      createdAt: '',
      updatedAt: '',
    }

    await h.actions.handleRunShortcut(sc, false)
    expect(pasteText).toHaveBeenCalledWith('ls -la')
    expect(RunShortcut).not.toHaveBeenCalled()
    expect(h.getState().activeNav).toBe('servers')

    await h.actions.handleRunShortcut(sc, true)
    expect(resetInputLine).toHaveBeenCalledTimes(1)
    expect(RunShortcut).toHaveBeenCalledWith('sess-1', 'sc-1', true)
  })

  it('rolls back autocomplete toggle on SaveSettings error and ignores stale out-of-order responses', async () => {
    let rejectFirst!: (err: Error) => void
    let resolveSecond!: (cfg: AppSettings) => void
    const SaveSettings = vi
      .fn()
      .mockImplementationOnce(
        () =>
          new Promise<AppSettings>((_res, rej) => {
            rejectFirst = rej
          })
      )
      .mockImplementationOnce(
        () =>
          new Promise<AppSettings>((res) => {
            resolveSecond = res
          })
      )
      .mockRejectedValueOnce(new Error('disk full'))

    const h = createHarness({ SaveSettings })

    // Rapid double toggle: 1st (true->false) fails later, 2nd (false->true) succeeds
    const p1 = h.actions.handleToggleAutocomplete()
    expect(h.getState().settings.autocompleteEnabled).toBe(false)
    const p2 = h.actions.handleToggleAutocomplete()
    expect(h.getState().settings.autocompleteEnabled).toBe(true)

    resolveSecond({ ...DEFAULT_SETTINGS, autocompleteEnabled: true })
    await p2
    rejectFirst(new Error('stale failure'))
    await p1
    // Stale 1st rejection must NOT roll back or toast over newer confirmed seq
    expect(h.getState().settings.autocompleteEnabled).toBe(true)

    // Now a fresh single toggle fails and MUST roll back to true
    await h.actions.handleToggleAutocomplete()
    expect(h.getState().settings.autocompleteEnabled).toBe(true)
    expect(h.toasts.some((t) => t.type === 'error' && t.text.includes('disk full'))).toBe(true)
  })
})
