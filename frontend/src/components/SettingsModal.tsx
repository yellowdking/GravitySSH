import React, { useEffect, useState } from 'react'
import { FolderTree, Settings, Sparkles, Terminal, X } from 'lucide-react'
import type { AppSettings, VaultLockState, VaultSecurityInfo } from '../types'
import { api } from '../wailsApi'
import { VaultPasswordSection } from './VaultPasswordSection'

interface SettingsModalProps {
  open: boolean
  settings: AppSettings
  securityInfo: VaultSecurityInfo | null
  lockState: VaultLockState | null
  appVersion: string
  onClose: () => void
  onSaved: (next: AppSettings) => void
  onLockStateChanged: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

const ToggleRow: React.FC<{
  label: string
  sub?: string
  checked: boolean
  disabled?: boolean
  onChange: (v: boolean) => void
}> = ({ label, sub, checked, disabled, onChange }) => (
  <label className={`flex items-center justify-between ${sub ? 'p-3.5' : 'p-3'} cursor-pointer ${disabled ? 'opacity-45' : ''}`}>
    <div>
      <div className={sub ? 'text-sm font-medium text-ag-text' : 'text-xs text-ag-text'}>{label}</div>
      {sub && <div className="text-xs text-ag-muted mt-0.5">{sub}</div>}
    </div>
    <input type="checkbox" disabled={disabled} checked={checked} onChange={(e) => onChange(e.target.checked)} className="w-4 h-4 accent-indigo-500" />
  </label>
)

export const SettingsModal: React.FC<SettingsModalProps> = ({
  open, settings, securityInfo, lockState, appVersion, onClose, onSaved, onLockStateChanged, onToast,
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
              <p className="text-[11px] text-ag-muted">自定义命令补齐、SFTP 面板与配置文件加密</p>
            </div>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text"><X className="w-4 h-4" /></button>
        </div>

        <div className="flex-1 overflow-y-auto p-5 space-y-5">
          <section className="space-y-2.5">
            <div className="flex items-center gap-2 text-xs font-semibold text-indigo-300 uppercase tracking-wider">
              <Sparkles className="w-3.5 h-3.5" />智能命令补齐 (APT / APK / Docker / 快捷命令)
            </div>
            <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
              <ToggleRow label="启用命令智能推荐与点击补齐" sub="输入命令时实时推荐，点击或按 Tab 自动补齐" checked={draft.autocompleteEnabled} onChange={(v) => patch({ autocompleteEnabled: v })} />
              <ToggleRow label="包含已保存的快捷方式" checked={draft.autocompleteFromShortcut} disabled={!draft.autocompleteEnabled} onChange={(v) => patch({ autocompleteFromShortcut: v })} />
              <ToggleRow label="包含历史执行过的命令" checked={draft.autocompleteFromHistory} disabled={!draft.autocompleteEnabled} onChange={(v) => patch({ autocompleteFromHistory: v })} />
            </div>
          </section>

          <section className="space-y-2.5">
            <div className="flex items-center gap-2 text-xs font-semibold text-sky-300 uppercase tracking-wider">
              <FolderTree className="w-3.5 h-3.5" />SFTP 与命令记录
            </div>
            <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
              <ToggleRow label="连接服务器后自动打开 SFTP 文件面板" checked={draft.autoOpenSftp} onChange={(v) => patch({ autoOpenSftp: v })} />
              <ToggleRow label="自动记录每条操作命令" checked={draft.recordCommands} onChange={(v) => patch({ recordCommands: v })} />
            </div>
          </section>

          <section className="space-y-2.5">
            <div className="flex items-center gap-2 text-xs font-semibold text-ag-muted uppercase tracking-wider">
              <Terminal className="w-3.5 h-3.5" />终端外观与网络
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs text-ag-muted mb-1">终端字号 ({draft.terminalFontSize}px)</label>
                <input type="range" min={11} max={22} value={draft.terminalFontSize} onChange={(e) => patch({ terminalFontSize: Number(e.target.value) })} className="w-full accent-indigo-500" />
              </div>
              <div>
                <label className="block text-xs text-ag-muted mb-1">默认心跳保活 (秒)</label>
                <input type="number" min={5} max={300} className="ag-input font-mono py-1.5" value={draft.defaultKeepAliveSeconds} onChange={(e) => patch({ defaultKeepAliveSeconds: Number(e.target.value) || 30 })} />
              </div>
            </div>
          </section>

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
