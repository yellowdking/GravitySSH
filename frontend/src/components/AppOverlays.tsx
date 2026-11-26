import React from 'react'
import type { ServerView, UpdateCheckResult } from '../types'
import type { SidebarTab } from './Sidebar'
import { ServerModal } from './ServerModal'
import { SettingsModal } from './SettingsModal'
import { SyncModal } from './SyncModal'

interface Props {
  serverModalOpen: boolean
  editingServer: ServerView | null
  settingsOpen: boolean
  syncModalOpen: boolean
  toast: { text: string; type: 'info' | 'error' } | null
  data: {
    servers: ServerView[]
    sshKeys: import('../types').SSHKeyMeta[]
    settings: import('../types').AppSettings
    securityInfo: import('../types').VaultSecurityInfo | null
    lockState: import('../types').VaultLockState | null
    appVersion: string
    updateInfo: UpdateCheckResult | null
    setSettings: (s: import('../types').AppSettings) => void
    setUpdateInfo: (u: UpdateCheckResult | null) => void
  }
  onCloseServerModal: () => void
  onCloseSettings: () => void
  onOpenSync: () => void
  onCloseSync: () => void
  onSelectNav: (tab: SidebarTab) => void
  onReload: () => void
  onToast: (text: string, type?: 'info' | 'error') => void
}

export const AppOverlays: React.FC<Props> = ({
  serverModalOpen, editingServer, settingsOpen, syncModalOpen, toast, data,
  onCloseServerModal, onCloseSettings, onOpenSync, onCloseSync, onSelectNav, onReload, onToast,
}) => (
  <>
    <ServerModal
      open={serverModalOpen}
      editingServer={editingServer}
      allServers={data.servers}
      sshKeys={data.sshKeys}
      onClose={onCloseServerModal}
      onSaved={() => { onToast(editingServer ? '服务器配置已更新' : '新服务器已保存'); onReload() }}
      onGoToKeyVault={() => onSelectNav('keys')}
    />
    <SettingsModal
      open={settingsOpen}
      settings={data.settings}
      securityInfo={data.securityInfo}
      lockState={data.lockState}
      appVersion={data.appVersion}
      updateInfo={data.updateInfo}
      onUpdateFound={data.setUpdateInfo}
      onClose={onCloseSettings}
      onOpenSync={onOpenSync}
      onSaved={(n) => { data.setSettings(n); onToast('设置已保存') }}
      onLockStateChanged={onReload}
      onToast={onToast}
    />
    <SyncModal
      open={syncModalOpen}
      settings={data.settings}
      onSettingsSaved={data.setSettings}
      onClose={onCloseSync}
      onSynced={onReload}
      onToast={onToast}
    />
    {toast && (
      <div className={`fixed bottom-5 right-5 z-50 px-4 py-2.5 rounded-xl text-xs font-medium shadow-ag border backdrop-blur-md flex items-center gap-2 ${toast.type === 'error' ? 'bg-rose-950/90 border-rose-500/40 text-rose-200' : 'bg-ag-elevated/95 border-indigo-500/40 text-ag-text'}`}>
        <span className={`w-2 h-2 rounded-full ${toast.type === 'error' ? 'bg-rose-400' : 'bg-emerald-400'}`} />
        <span>{toast.text}</span>
      </div>
    )}
  </>
)
