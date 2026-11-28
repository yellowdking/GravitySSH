import React from 'react'
import { FolderTree, Sparkles, Terminal } from 'lucide-react'
import type { AppSettings } from '../types'
import { isMacPlatform } from '../utils/errors'
import { NumericInput } from './NumericInput'

interface Props {
  draft: AppSettings
  patch: (p: Partial<AppSettings>) => void
}

const ToggleRow: React.FC<{
  label: string
  checked: boolean
  disabled?: boolean
  onChange: (v: boolean) => void
}> = ({ label, checked, disabled, onChange }) => (
  <label className={`flex items-center justify-between p-2.5 cursor-pointer ${disabled ? 'opacity-45' : ''}`}>
    <span className="pr-3 text-xs text-ag-text">{label}</span>
    <input type="checkbox" disabled={disabled} checked={checked} onChange={(e) => onChange(e.target.checked)} className="w-4 h-4 accent-indigo-500 shrink-0" />
  </label>
)

export const SettingsToggles: React.FC<Props> = ({ draft, patch }) => {
  const isMac = isMacPlatform()
  return (
    <>
      <section className="space-y-2">
        <div className="flex items-center gap-2 text-xs font-semibold text-indigo-300 uppercase tracking-wider">
          <Sparkles className="w-3.5 h-3.5" />命令补齐
        </div>
        <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
          <ToggleRow label="智能命令与路径补齐" checked={draft.autocompleteEnabled} onChange={(v) => patch({ autocompleteEnabled: v })} />
          <ToggleRow label="包含历史命令" checked={draft.autocompleteFromHistory} disabled={!draft.autocompleteEnabled} onChange={(v) => patch({ autocompleteFromHistory: v })} />
        </div>
      </section>

      <section className="space-y-2">
        <div className="flex items-center gap-2 text-xs font-semibold text-sky-300 uppercase tracking-wider">
          <FolderTree className="w-3.5 h-3.5" />监控与 SFTP
        </div>
        <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
          <ToggleRow label="底部状态监控栏" checked={draft.monitorBarEnabled} onChange={(v) => patch({ monitorBarEnabled: v })} />
          <ToggleRow label="连接后自动展开 SFTP" checked={draft.autoOpenSftp} onChange={(v) => patch({ autoOpenSftp: v })} />
          <ToggleRow label="SFTP 跟随终端目录" checked={draft.sftpFollowTerminal} onChange={(v) => patch({ sftpFollowTerminal: v })} />
          <ToggleRow label="记录执行命令历史" checked={draft.recordCommands} onChange={(v) => patch({ recordCommands: v })} />
        </div>
      </section>

      <section className="space-y-2">
        <div className="flex items-center gap-2 text-xs font-semibold text-ag-muted uppercase tracking-wider">
          <Terminal className="w-3.5 h-3.5" />终端与安全
        </div>
        <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
          {!isMac && (
            <ToggleRow label="自动切换英文输入法" checked={draft.autoSwitchEnglishIme} onChange={(v) => patch({ autoSwitchEnglishIme: v })} />
          )}
          <ToggleRow label="粘贴安全拦截" checked={draft.pasteGuardEnabled} onChange={(v) => patch({ pasteGuardEnabled: v })} />
          <ToggleRow label="终端日志关键词高亮" checked={draft.keywordHighlightEnabled} onChange={(v) => patch({ keywordHighlightEnabled: v })} />
        </div>
        <div className="grid grid-cols-2 gap-3 pt-1">
          <div>
            <label className="block text-xs text-ag-muted mb-1">终端字号：{draft.terminalFontSize}</label>
            <input type="range" min={11} max={22} value={draft.terminalFontSize} onChange={(e) => patch({ terminalFontSize: Number(e.target.value) })} className="w-full accent-indigo-500" />
          </div>
          <div>
            <label className="block text-xs text-ag-muted mb-1">默认心跳保活</label>
            <NumericInput min={5} max={300} fallback={30} className="ag-input font-mono py-1.5" value={draft.defaultKeepAliveSeconds} onChange={(defaultKeepAliveSeconds) => patch({ defaultKeepAliveSeconds })} />
          </div>
        </div>
      </section>
    </>
  )
}
