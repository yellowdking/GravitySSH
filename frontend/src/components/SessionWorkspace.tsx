import React from 'react'
import type { AppSettings, CommandRecord, CommandShortcut, SessionInfo } from '../types'
import { api } from '../wailsApi'
import { TerminalPane } from './TerminalPane'
import { SftpPanel } from './SftpPanel'
import { ShortcutsPanel } from './ShortcutsPanel'
import { HistoryPanel } from './HistoryPanel'
import { ResizeHandle } from './ResizeHandle'

interface SessionWorkspaceProps {
  session: SessionInfo
  visible: boolean
  settings: AppSettings
  showSftp: boolean
  sftpWidth: number
  onSftpWidthChange: (w: number) => void
  onSftpWidthCommit: (w: number) => void
  rightDrawer: 'none' | 'history' | 'shortcuts'
  drawerWidth: number
  onDrawerWidthChange: (w: number) => void
  onDrawerWidthCommit: (w: number) => void
  shortcuts: CommandShortcut[]
  history: CommandRecord[]
  onToggleSftp: () => void
  onToggleRightDrawer: (mode: 'history' | 'shortcuts') => void
  onToggleAutocomplete: () => void
  onReconnect: (serverId: string) => void
  onRunShortcut: (sc: CommandShortcut, execute: boolean) => void
  onRunCommand: (cmd: string, execute: boolean) => void
  onReload: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SessionWorkspace: React.FC<SessionWorkspaceProps> = ({
  session, visible, settings, showSftp, sftpWidth, onSftpWidthChange, onSftpWidthCommit,
  rightDrawer, drawerWidth, onDrawerWidthChange, onDrawerWidthCommit,
  shortcuts, history, onToggleSftp, onToggleRightDrawer, onToggleAutocomplete,
  onReconnect, onRunShortcut, onRunCommand, onReload, onToast,
}) => (
  <div className={`absolute inset-0 flex min-w-0 ${visible ? 'z-10 opacity-100' : 'z-0 opacity-0 pointer-events-none'}`}>
    <TerminalPane
      session={session}
      active={visible}
      settings={settings}
      showSftp={showSftp}
      rightDrawer={rightDrawer}
      onToggleSftp={onToggleSftp}
      onToggleRightDrawer={onToggleRightDrawer}
      onToggleAutocomplete={onToggleAutocomplete}
      onReconnect={onReconnect}
    />
    {showSftp && (
      <>
        <ResizeHandle
          width={sftpWidth}
          min={240}
          max={620}
          direction="left"
          onChange={onSftpWidthChange}
          onCommit={onSftpWidthCommit}
          title="拖动调整 SFTP 面板宽度"
        />
        <SftpPanel
          session={session}
          active={visible}
          width={sftpWidth}
          onCdInTerminal={(d) => { void api.writeTerminal(session.sessionId, `cd ${JSON.stringify(d)}\r`) }}
          onToast={onToast}
        />
      </>
    )}
    {rightDrawer !== 'none' && (
      <>
        <ResizeHandle
          width={drawerWidth}
          min={260}
          max={560}
          direction="left"
          onChange={onDrawerWidthChange}
          onCommit={onDrawerWidthCommit}
          title="拖动调整抽屉宽度"
        />
        <div style={{ width: drawerWidth }} className="shrink-0 h-full min-w-0 flex overflow-hidden [&>*]:!w-full [&>*]:min-w-0">
          {rightDrawer === 'shortcuts' && (
            <ShortcutsPanel compact shortcuts={shortcuts} activeSessionId={session.sessionId} onRunShortcut={onRunShortcut} onChanged={onReload} onToast={onToast} />
          )}
          {rightDrawer === 'history' && (
            <HistoryPanel compact history={history} activeServerId={session.serverId} activeSessionId={session.sessionId} onRunCommand={onRunCommand} onShortcutSaved={onReload} onHistoryChanged={onReload} onToast={onToast} />
          )}
        </div>
      </>
    )}
  </div>
)
