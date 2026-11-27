import React, { useCallback, useEffect, useRef } from 'react'
import type { AppSettings, SessionInfo } from '../types'
import type { TerminalActions } from '../hooks/terminalStream'
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
  onRegisterTerminal?: (sessionId: string, actions: TerminalActions | null) => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SessionWorkspace: React.FC<SessionWorkspaceProps> = React.memo(({
  session, visible, settings, showSftp, sftpWidth, onSftpWidthChange, onSftpWidthCommit,
  rightDrawer, onToggleSftp, onToggleRightDrawer, onToggleAutocomplete, onReconnect, onRegisterTerminal, onToast,
}) => {
  const rootRef = useRef<HTMLDivElement | null>(null)
  const localTerminalRef = useRef<TerminalActions | null>(null)

  useEffect(() => {
    const el = rootRef.current as (HTMLDivElement & { inert?: boolean }) | null
    if (!el) return
    el.inert = !visible
    if (!visible) {
      el.setAttribute('inert', '')
      if (document.activeElement && el.contains(document.activeElement)) {
        ;(document.activeElement as HTMLElement).blur?.()
      }
    } else {
      el.removeAttribute('inert')
    }
  }, [visible])

  const handleRegisterTerminal = useCallback(
    (sessionId: string, actions: TerminalActions | null) => {
      localTerminalRef.current = actions
      onRegisterTerminal?.(sessionId, actions)
    },
    [onRegisterTerminal]
  )

  const handleToggleSftp = useCallback(() => onToggleSftp(session.sessionId), [onToggleSftp, session.sessionId])
  const handleCdInTerminal = useCallback(
    (d: string) => {
      localTerminalRef.current?.resetInputLine()
      void api.writeTerminal(session.sessionId, `cd '${d.replace(/'/g, "'\\''")}'\r`).catch(() => {})
    },
    [session.sessionId]
  )

  return (
    <div
      ref={rootRef}
      aria-hidden={!visible}
      className={`absolute inset-0 flex min-w-0 ${visible ? 'z-10 opacity-100 visible' : 'z-0 opacity-0 invisible pointer-events-none'}`}
    >
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
        onRegisterTerminal={handleRegisterTerminal}
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
