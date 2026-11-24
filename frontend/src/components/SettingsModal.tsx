import React, { useEffect, useState } from 'react'
import { Settings, X } from 'lucide-react'
import type { AppSettings, UpdateCheckResult, VaultLockState, VaultSecurityInfo } from '../types'
import { api } from '../wailsApi'
import { SettingsToggles } from './SettingsToggles'
import { VaultPasswordSection } from './VaultPasswordSection'
import { UpdateCheckSection } from './UpdateCheckSection'

interface SettingsModalProps {
  open: boolean
  settings: AppSettings
  securityInfo: VaultSecurityInfo | null
  lockState: VaultLockState | null
  appVersion: string
  updateInfo: UpdateCheckResult | null
  onUpdateFound: (info: UpdateCheckResult) => void
  onClose: () => void
  onSaved: (next: AppSettings) => void
  onLockStateChanged: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SettingsModal: React.FC<SettingsModalProps> = ({
  open, settings, securityInfo, lockState, appVersion, updateInfo, onUpdateFound, onClose, onSaved, onLockStateChanged, onToast,
}) => {
  const [draft, setDraft] = useState<AppSettings>(settings)
  const [saving, setSaving] = useState(false)

  useEffect(() => { if (open) setDraft(settings) }, [open, settings])
  if (!open) return null

  const patch = (p: Partial<AppSettings>) => setDraft((prev) => ({ ...prev, ...p }))

  const handleSave = async () => {
    setSaving(true)
    try {
      const saved = await api.saveSettings(draft)
      onSaved(saved)
      onClose()
    } catch (err) {
      onToast(String(err), 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4">
      <div className="w-full max-w-xl bg-ag-panel border border-ag-borderStrong rounded-2xl shadow-ag overflow-hidden flex flex-col max-h-[88vh]">
        <div className="px-5 py-4 border-b border-ag-border flex items-center justify-between bg-[#11151f]">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300"><Settings className="w-4 h-4" /></div>
            <div>
              <h2 className="text-sm font-semibold text-ag-text">设置 · GravitySSH v{appVersion}</h2>
              <p className="text-[11px] text-ag-muted">自定义命令补齐、监控、SFTP、终端防护与更新</p>
            </div>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text"><X className="w-4 h-4" /></button>
        </div>

        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          <SettingsToggles draft={draft} patch={patch} />
          <UpdateCheckSection appVersion={appVersion} updateInfo={updateInfo} checkOnStartup={draft.checkUpdatesOnStartup} onToggleCheckOnStartup={(v) => patch({ checkUpdatesOnStartup: v })} onUpdateFound={onUpdateFound} onToast={onToast} />
          <VaultPasswordSection securityInfo={securityInfo} lockState={lockState} onLockStateChanged={onLockStateChanged} onToast={onToast} />
        </div>

        <div className="px-5 py-3.5 border-t border-ag-border flex justify-end gap-2 bg-[#11151f]">
          <button onClick={onClose} className="ag-btn-ghost">取消</button>
          <button onClick={() => void handleSave()} disabled={saving} className="ag-btn-primary">{saving ? '保存中...' : '保存设置'}</button>
        </div>
      </div>
    </div>
  )
}
