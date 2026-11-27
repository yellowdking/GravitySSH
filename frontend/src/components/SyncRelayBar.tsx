import React, { useState } from 'react'
import { Check, ChevronDown, ChevronRight, Copy, Loader2, RotateCcw, Save, Server, Wifi } from 'lucide-react'
import type { AppSettings } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { useCopyFeedback } from '../hooks/useCopyFeedback'

interface Props {
  settings: AppSettings
  relayInput: string
  defaultRelay: string
  onRelayInputChange: (next: string) => void
  onSettingsSaved: (next: AppSettings) => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

const DOCKER_CMD = 'docker run -d --name sshsync --restart unless-stopped -p 54843:54843 bbplin/sshsync'

export const SyncRelayBar: React.FC<Props> = ({
  settings, relayInput, defaultRelay, onRelayInputChange, onSettingsSaved, onToast,
}) => {
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testRes, setTestRes] = useState<{ ok: boolean; text: string } | null>(null)
  const [showDocker, setShowDocker] = useState(false)
  const { copied: copiedCmd, copy } = useCopyFeedback()

  const trimmed = relayInput.trim()
  const isCustom = trimmed !== '' && trimmed !== defaultRelay

  const persistRelay = async (val: string, msg: string) => {
    setSaving(true)
    try {
      const saved = await api.saveSettings({ ...settings, syncRelayUrl: val })
      onSettingsSaved(saved)
      onRelayInputChange(saved.syncRelayUrl || '')
      onToast(msg)
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleTest = async () => {
    setTesting(true)
    setTestRes(null)
    try {
      const ms = await api.testSyncRelay(trimmed)
      setTestRes({ ok: true, text: `✓ 已连通 (${ms}ms)` })
    } catch (err) {
      setTestRes({ ok: false, text: errMsg(err) })
    } finally {
      setTesting(false)
    }
  }

  const handleReset = () => {
    onRelayInputChange('')
    setTestRes(null)
    if (settings.syncRelayUrl) void persistRelay('', '已恢复默认内置中继')
  }

  const copyDocker = async () => {
    const ok = await copy(DOCKER_CMD)
    if (ok) onToast('Docker 部署命令已复制')
    else onToast('复制失败，请手动复制', 'error')
  }

  return (
    <div className="rounded-xl bg-[#0d111a] border border-ag-border p-3 space-y-2">
      <div className="flex items-center justify-between gap-2 text-[11px]">
        <span className="flex items-center gap-1.5 text-ag-text font-medium">
          <Server className="w-3.5 h-3.5 text-indigo-300" />
          中继服务器
          <span className={`px-1.5 py-0.5 rounded text-[10px] ${isCustom ? 'bg-amber-500/15 text-amber-300' : 'bg-indigo-500/15 text-indigo-300'}`}>
            {isCustom ? '自定义 VPS' : '内置默认'}
          </span>
        </span>
        {testRes && (
          <span className={`text-[11px] truncate max-w-[260px] ${testRes.ok ? 'text-emerald-400' : 'text-rose-400'}`}>
            {testRes.text}
          </span>
        )}
      </div>

      <div className="flex items-center gap-1.5">
        <input
          type="text"
          value={relayInput}
          onChange={(e) => { onRelayInputChange(e.target.value); setTestRes(null) }}
          placeholder="默认内置中继，或输入新 VPS 地址如 1.2.3.4:54843"
          className="flex-1 min-w-0 rounded-lg bg-[#0b0d12] border border-ag-border px-2.5 py-1.5 font-mono text-xs text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-indigo-500/60"
        />
        <button type="button" disabled={saving} onClick={() => void persistRelay(trimmed, trimmed ? '自定义中继已保存' : '已恢复默认中继')} className="px-2.5 py-1.5 rounded-lg bg-indigo-500/20 hover:bg-indigo-500/30 border border-indigo-500/35 text-xs text-indigo-200 flex items-center gap-1 shrink-0 disabled:opacity-50">
          {saving ? <Loader2 className="w-3 h-3 animate-spin" /> : <Save className="w-3 h-3" />}保存
        </button>
        <button type="button" disabled={testing} onClick={() => void handleTest()} className="px-2.5 py-1.5 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs text-ag-text flex items-center gap-1 shrink-0 disabled:opacity-50">
          {testing ? <Loader2 className="w-3 h-3 animate-spin" /> : <Wifi className="w-3 h-3 text-cyan-300" />}测试连接
        </button>
        {(isCustom || settings.syncRelayUrl) && (
          <button type="button" disabled={saving} onClick={handleReset} title="恢复默认内置中继" className="p-1.5 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-ag-muted hover:text-ag-text shrink-0">
            <RotateCcw className="w-3.5 h-3.5" />
          </button>
        )}
      </div>

      <div>
        <button
          type="button"
          onClick={() => setShowDocker((v) => !v)}
          className="flex items-center gap-1 text-[10px] text-ag-muted hover:text-ag-text transition"
        >
          {showDocker ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
          <span>新 VPS 一键 Docker 部署命令</span>
        </button>
        {showDocker && (
          <div className="mt-1.5 flex items-center gap-1.5 rounded-lg bg-[#090c12] border border-ag-border/80 px-2.5 py-1.5">
            <code className="flex-1 min-w-0 font-mono text-[10px] text-cyan-300 truncate select-all">{DOCKER_CMD}</code>
            <button type="button" onClick={() => void copyDocker()} title="复制命令" className="p-1 rounded hover:bg-ag-hover text-ag-muted hover:text-ag-text shrink-0">
              {copiedCmd ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
