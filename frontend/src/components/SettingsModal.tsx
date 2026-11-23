import React, { useEffect, useState } from 'react'
import { FolderTree, Lock, Settings, ShieldCheck, Sparkles, Terminal, X } from 'lucide-react'
import type { AppSettings, VaultSecurityInfo } from '../types'
import { api } from '../wailsApi'

interface SettingsModalProps {
  open: boolean
  settings: AppSettings
  securityInfo: VaultSecurityInfo | null
  appVersion: string
  onClose: () => void
  onSaved: (next: AppSettings) => void
}

export const SettingsModal: React.FC<SettingsModalProps> = ({
  open, settings, securityInfo, appVersion, onClose, onSaved,
}) => {
  const [draft, setDraft] = useState<AppSettings>(settings)
  const [saving, setSaving] = useState(false)

  useEffect(() => { if (open) setDraft(settings) }, [open, settings])
  if (!open) return null

  const handleSave = async () => {
    setSaving(true)
    try { onSaved(await api.saveSettings(draft)); onClose() }
    finally { setSaving(false) }
  }

  return (
    <div className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4">
      <div className="w-full max-w-xl bg-ag-panel border border-ag-borderStrong rounded-2xl shadow-ag overflow-hidden flex flex-col max-h-[88vh]">
        <div className="px-5 py-4 border-b border-ag-border flex items-center justify-between bg-[#11151f]">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300"><Settings className="w-4 h-4" /></div>
            <div>
              <h2 className="text-sm font-semibold text-ag-text">设置 · GravitySSH v{appVersion}</h2>
              <p className="text-[11px] text-ag-muted">自定义智能命令补齐、SFTP 自动连接与本地加密保险库</p>
            </div>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text"><X className="w-4 h-4" /></button>
        </div>

        <div className="flex-1 overflow-y-auto p-5 space-y-5">
          <section className="space-y-2.5">
            <div className="flex items-center gap-2 text-xs font-semibold text-indigo-300 uppercase tracking-wider"><Sparkles className="w-3.5 h-3.5" />智能命令补齐 (APT / APK / Docker / 快捷命令)</div>
            <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
              <label className="flex items-center justify-between p-3.5 cursor-pointer">
                <div>
                  <div className="text-sm font-medium text-ag-text">启用命令智能推荐与点击补齐</div>
                  <div className="text-xs text-ag-muted mt-0.5">输入命令时实时推荐，点击或按 Tab 自动补齐</div>
                </div>
                <input type="checkbox" checked={draft.autocompleteEnabled} onChange={(e) => setDraft({ ...draft, autocompleteEnabled: e.target.checked })} className="w-4 h-4 accent-indigo-500 cursor-pointer" />
              </label>
              <label className={`flex items-center justify-between p-3 cursor-pointer ${!draft.autocompleteEnabled ? 'opacity-45' : ''}`}>
                <span className="text-xs text-ag-text">包含已保存的快捷方式</span>
                <input type="checkbox" disabled={!draft.autocompleteEnabled} checked={draft.autocompleteFromShortcut} onChange={(e) => setDraft({ ...draft, autocompleteFromShortcut: e.target.checked })} className="w-4 h-4 accent-indigo-500" />
              </label>
              <label className={`flex items-center justify-between p-3 cursor-pointer ${!draft.autocompleteEnabled ? 'opacity-45' : ''}`}>
                <span className="text-xs text-ag-text">包含历史执行过的命令</span>
                <input type="checkbox" disabled={!draft.autocompleteEnabled} checked={draft.autocompleteFromHistory} onChange={(e) => setDraft({ ...draft, autocompleteFromHistory: e.target.checked })} className="w-4 h-4 accent-indigo-500" />
              </label>
            </div>
          </section>

          <section className="space-y-2.5">
            <div className="flex items-center gap-2 text-xs font-semibold text-sky-300 uppercase tracking-wider"><FolderTree className="w-3.5 h-3.5" />SFTP 与命令记录</div>
            <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
              <label className="flex items-center justify-between p-3 cursor-pointer">
                <span className="text-xs text-ag-text">连接服务器后自动打开 SFTP 文件面板</span>
                <input type="checkbox" checked={draft.autoOpenSftp} onChange={(e) => setDraft({ ...draft, autoOpenSftp: e.target.checked })} className="w-4 h-4 accent-indigo-500" />
              </label>
              <label className="flex items-center justify-between p-3 cursor-pointer">
                <span className="text-xs text-ag-text">自动记录每条操作命令</span>
                <input type="checkbox" checked={draft.recordCommands} onChange={(e) => setDraft({ ...draft, recordCommands: e.target.checked })} className="w-4 h-4 accent-indigo-500" />
              </label>
            </div>
          </section>

          <section className="space-y-2.5">
            <div className="flex items-center gap-2 text-xs font-semibold text-ag-muted uppercase tracking-wider"><Terminal className="w-3.5 h-3.5" />终端外观与网络</div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs text-ag-muted mb-1">终端字号 ({draft.terminalFontSize}px)</label>
                <input type="range" min={11} max={22} value={draft.terminalFontSize} onChange={(e) => setDraft({ ...draft, terminalFontSize: Number(e.target.value) })} className="w-full accent-indigo-500" />
              </div>
              <div>
                <label className="block text-xs text-ag-muted mb-1">默认心跳保活 (秒)</label>
                <input type="number" min={5} max={300} className="ag-input font-mono py-1.5" value={draft.defaultKeepAliveSeconds} onChange={(e) => setDraft({ ...draft, defaultKeepAliveSeconds: Number(e.target.value) || 30 })} />
              </div>
            </div>
          </section>

          {securityInfo && (
            <section className="rounded-xl bg-emerald-500/8 border border-emerald-500/25 p-3.5 space-y-1.5 text-[11px] font-mono text-emerald-100/85">
              <div className="flex items-center gap-2 text-xs font-sans font-semibold text-emerald-300"><ShieldCheck className="w-4 h-4" />加密保险库安全状态 (仅限本机本账户本软件解密)</div>
              <div className="flex items-center gap-1.5"><Lock className="w-3 h-3 text-emerald-400" />{securityInfo.engine}</div>
              <div className="truncate text-emerald-200/75" title={securityInfo.vaultPath}>{securityInfo.vaultPath}</div>
              <div>受保护：{securityInfo.serverCount} 服务器 · {securityInfo.keyCount} SSH 私钥 · {securityInfo.shortcutCount} 快捷命令</div>
            </section>
          )}
        </div>

        <div className="px-5 py-3.5 border-t border-ag-border flex justify-end gap-2 bg-[#11151f]">
          <button onClick={onClose} className="ag-btn-ghost">取消</button>
          <button onClick={handleSave} disabled={saving} className="ag-btn-primary">{saving ? '保存中...' : '保存设置'}</button>
        </div>
      </div>
    </div>
  )
}
