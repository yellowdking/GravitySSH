import React from 'react'
import { FolderTree, Sparkles, Terminal } from 'lucide-react'
import type { AppSettings } from '../types'
import { NumericInput } from './NumericInput'

interface Props {
  draft: AppSettings
  patch: (p: Partial<AppSettings>) => void
}

const ToggleRow: React.FC<{
  label: string
  sub?: string
  checked: boolean
  disabled?: boolean
  onChange: (v: boolean) => void
}> = ({ label, sub, checked, disabled, onChange }) => (
  <label className={`flex items-center justify-between ${sub ? 'p-3' : 'p-2.5'} cursor-pointer ${disabled ? 'opacity-45' : ''}`}>
    <div className="pr-3">
      <div className={sub ? 'text-xs font-medium text-ag-text' : 'text-xs text-ag-text'}>{label}</div>
      {sub && <div className="text-[11px] text-ag-muted mt-0.5">{sub}</div>}
    </div>
    <input type="checkbox" disabled={disabled} checked={checked} onChange={(e) => onChange(e.target.checked)} className="w-4 h-4 accent-indigo-500 shrink-0" />
  </label>
)

export const SettingsToggles: React.FC<Props> = ({ draft, patch }) => (
  <>
    <section className="space-y-2">
      <div className="flex items-center gap-2 text-xs font-semibold text-indigo-300 uppercase tracking-wider">
        <Sparkles className="w-3.5 h-3.5" />智能命令补齐
      </div>
      <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
        <ToggleRow label="启用命令与路径智能推荐" sub="实时推荐目录/文件、子命令、参数与本机历史（快捷命令请使用右侧面板）" checked={draft.autocompleteEnabled} onChange={(v) => patch({ autocompleteEnabled: v })} />
        <ToggleRow label="包含历史执行过的命令" checked={draft.autocompleteFromHistory} disabled={!draft.autocompleteEnabled} onChange={(v) => patch({ autocompleteFromHistory: v })} />
      </div>
    </section>

    <section className="space-y-2">
      <div className="flex items-center gap-2 text-xs font-semibold text-sky-300 uppercase tracking-wider">
        <FolderTree className="w-3.5 h-3.5" />连接、监控与 SFTP
      </div>
      <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
        <ToggleRow label="终端底部实时监控条" sub="显示网络延迟、CPU、内存、磁盘、网速、负载及 GPU（切后台自动暂停）" checked={draft.monitorBarEnabled} onChange={(v) => patch({ monitorBarEnabled: v })} />
        <ToggleRow label="连接服务器后自动打开 SFTP 面板" checked={draft.autoOpenSftp} onChange={(v) => patch({ autoOpenSftp: v })} />
        <ToggleRow label="SFTP 自动跟随终端 cd 切换目录" checked={draft.sftpFollowTerminal} onChange={(v) => patch({ sftpFollowTerminal: v })} />
        <ToggleRow label="自动记录每条操作命令" checked={draft.recordCommands} onChange={(v) => patch({ recordCommands: v })} />
      </div>
    </section>

    <section className="space-y-2">
      <div className="flex items-center gap-2 text-xs font-semibold text-ag-muted uppercase tracking-wider">
        <Terminal className="w-3.5 h-3.5" />终端安全、高亮与外观
      </div>
      <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
        <ToggleRow label="SSH 终端自动切换英文输入法" sub="进入、点击或切回 SSH 终端时自动切为英文，避免误输中文拼音（仍可手动按 Shift 切回中文）" checked={draft.autoSwitchEnglishIme} onChange={(v) => patch({ autoSwitchEnglishIme: v })} />
        <ToggleRow label="粘贴安全防护" sub="粘贴高危命令、curl|sh、隐藏/同形字符或非括号多行时弹窗确认" checked={draft.pasteGuardEnabled} onChange={(v) => patch({ pasteGuardEnabled: v })} />
        <ToggleRow label="终端日志关键词高亮" sub="自动为 error / warning / ok / IPv4 着色（nano/vim 全屏自动暂停）" checked={draft.keywordHighlightEnabled} onChange={(v) => patch({ keywordHighlightEnabled: v })} />
      </div>
      <div className="grid grid-cols-2 gap-3 pt-1">
        <div>
          <label className="block text-xs text-ag-muted mb-1">终端字号 ({draft.terminalFontSize}px)</label>
          <input type="range" min={11} max={22} value={draft.terminalFontSize} onChange={(e) => patch({ terminalFontSize: Number(e.target.value) })} className="w-full accent-indigo-500" />
        </div>
        <div>
          <label className="block text-xs text-ag-muted mb-1">默认心跳保活 (秒)</label>
          <NumericInput min={5} max={300} fallback={30} className="ag-input font-mono py-1.5" value={draft.defaultKeepAliveSeconds} onChange={(defaultKeepAliveSeconds) => patch({ defaultKeepAliveSeconds })} />
        </div>
      </div>
    </section>
  </>
)
