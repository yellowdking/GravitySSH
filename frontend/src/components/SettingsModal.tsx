import React, { useCallback, useEffect, useRef, useState } from 'react'
import { RefreshCw, Settings, X } from 'lucide-react'
import type { AppSettings, UpdateCheckResult, VaultLockState, VaultSecurityInfo } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { ModalShell } from './ModalShell'
import { SettingsToggles } from './SettingsToggles'
import { SshCacheSection } from './SshCacheSection'
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
  onOpenSync: () => void
  onSaved: (next: AppSettings) => void
  onLockStateChanged: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SettingsModal: React.FC<SettingsModalProps> = ({
  open, settings, securityInfo, lockState, appVersion, updateInfo, onUpdateFound, onClose, onOpenSync, onSaved, onLockStateChanged, onToast,
}) => {
  const [draft, setDraft] = useState<AppSettings>(settings)
  const [saving, setSaving] = useState(false)
  const [vaultBusy, setVaultBusy] = useState(false)

  const mountedRef = useRef(true)
  const savingRef = useRef(false)
  const vaultBusyRef = useRef(false)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => { if (open) setDraft(settings) }, [open, settings])

  const handleVaultBusyChange = useCallback((busy: boolean) => {
    vaultBusyRef.current = busy
    if (mountedRef.current) setVaultBusy(busy)
  }, [])

  const modalBusy = saving || vaultBusy
  const handleClose = () => {
    if (savingRef.current || vaultBusyRef.current) return
    onClose()
  }

  const patch = (p: Partial<AppSettings>) => setDraft((prev) => ({ ...prev, ...p }))

  const handleSave = async () => {
    if (savingRef.current || vaultBusyRef.current) return
    savingRef.current = true
    setSaving(true)
    try {
      const saved = await api.saveSettings(draft)
      if (!mountedRef.current) return
      onSaved(saved)
      savingRef.current = false
      setSaving(false)
      onClose()
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    } finally {
      savingRef.current = false
      if (mountedRef.current) setSaving(false)
    }
  }

  return (
    <ModalShell open={open} onClose={handleClose}>
      <div className="w-full max-w-xl bg-ag-panel border border-ag-borderStrong rounded-2xl shadow-ag overflow-hidden flex flex-col max-h-[88vh]">
        <div className="px-5 py-4 border-b border-ag-border flex items-center justify-between bg-[#11151f]">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300"><Settings className="w-4 h-4" /></div>
            <div>
              <h2 className="text-sm font-semibold text-ag-text">设置 · GravitySSH v{appVersion}</h2>
              <p className="text-[11px] text-ag-muted">自定义命令补齐、监控、SFTP、多端同步与更新</p>
            </div>
          </div>
          <button type="button" disabled={modalBusy} onClick={handleClose} aria-label="关闭设置窗口" className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text disabled:opacity-40 disabled:cursor-not-allowed"><X className="w-4 h-4" /></button>
        </div>

        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          <div className="rounded-xl bg-[#0d111a] border border-cyan-500/25 p-3.5 flex items-center justify-between gap-3">
            <div>
              <div className="text-xs font-semibold text-ag-text">多端同步</div>
              <p className="text-[11px] text-ag-muted mt-0.5">
                通过一次性同步码或二维码端到端加密互传 · 当前中继：{settings.syncRelayUrl || '内置默认中继'}
              </p>
            </div>
            <button
              type="button"
              disabled={modalBusy}
              onClick={() => { if (!modalBusy) { onClose(); onOpenSync() } }}
              className="shrink-0 px-3 py-1.5 rounded-lg bg-cyan-500/15 hover:bg-cyan-500/25 text-cyan-200 border border-cyan-500/35 text-xs font-medium flex items-center gap-1.5 transition disabled:opacity-40 disabled:cursor-not-allowed"
            >
              <RefreshCw className="w-3.5 h-3.5" />
              打开同步
            </button>
          </div>
          <SettingsToggles draft={draft} patch={patch} />
          <UpdateCheckSection appVersion={appVersion} updateInfo={updateInfo} checkOnStartup={draft.checkUpdatesOnStartup} onToggleCheckOnStartup={(v) => patch({ checkUpdatesOnStartup: v })} onUpdateFound={onUpdateFound} onToast={onToast} />
          <SshCacheSection onToast={onToast} />
          <VaultPasswordSection securityInfo={securityInfo} lockState={lockState} onLockStateChanged={onLockStateChanged} onBusyChange={handleVaultBusyChange} onToast={onToast} />
        </div>

        <div className="px-5 py-3.5 border-t border-ag-border flex justify-end gap-2 bg-[#11151f]">
          <button type="button" disabled={modalBusy} onClick={handleClose} className="ag-btn-ghost">取消</button>
          <button type="button" onClick={() => void handleSave()} disabled={modalBusy} className="ag-btn-primary">{saving ? '保存中...' : '保存设置'}</button>
        </div>
      </div>
    </ModalShell>
  )
}
