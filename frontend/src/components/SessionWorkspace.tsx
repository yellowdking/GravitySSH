import React, { useCallback } from 'react'
import type { AppSettings, SessionInfo } from '../types'
import { api } from '../wailsApi'
import { TerminalPane } from './TerminalPane'
import { SftpPanel } from './SftpPanel'
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
  onToggleSftp: (sessionId: string) => void
  onToggleRightDrawer: (mode: 'history' | 'shortcuts') => void
  onToggleAutocomplete: () => void
  onReconnect: (serverId: string) => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SessionWorkspace: React.FC<SessionWorkspaceProps> = React.memo(({
  session, visible, settings, showSftp, sftpWidth, onSftpWidthChange, onSftpWidthCommit,
  rightDrawer, onToggleSftp, onToggleRightDrawer, onToggleAutocomplete, onReconnect, onToast,
}) => {
  const handleToggleSftp = useCallback(() => onToggleSftp(session.sessionId), [onToggleSftp, session.sessionId])
  const handleCdInTerminal = useCallback(
    (d: string) => {
      void api.writeTerminal(session.sessionId, `cd '${d.replace(/'/g, "'\\''")}'\r`).catch(() => {})
    },
    [session.sessionId]
  )

  return (
    <div className={`absolute inset-0 flex min-w-0 ${visible ? 'z-10 opacity-100' : 'z-0 opacity-0 pointer-events-none'}`}>
      <TerminalPane
        session={session}
        active={visible}
        settings={settings}
        showSftp={showSftp}
        rightDrawer={rightDrawer}
        onToggleSftp={handleToggleSftp}
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
            followTerminal={settings.sftpFollowTerminal}
            onCdInTerminal={handleCdInTerminal}
            onToast={onToast}
          />
        </>
      )}
    </div>
  )
})
