import React, { useEffect, useState } from 'react'
import { AlertCircle, CheckCircle2, Globe, KeyRound, Loader2, Server, Sliders, Waypoints, X } from 'lucide-react'
import type { ServerSaveRequest, ServerView, SSHKeyMeta } from '../types'
import { api } from '../wailsApi'
import { makeInitialServerForm } from './serverForm'
import { ServerBasicTab } from './ServerBasicTab'
import { ServerProxyTab } from './ServerProxyTab'
import { ServerJumpTab } from './ServerJumpTab'
import { ServerAdvancedTab } from './ServerAdvancedTab'

interface ServerModalProps {
  open: boolean
  editingServer: ServerView | null
  allServers: ServerView[]
  sshKeys: SSHKeyMeta[]
  onClose: () => void
  onSaved: (saved: ServerView) => void
  onGoToKeyVault: () => void
}

export const ServerModal: React.FC<ServerModalProps> = ({
  open, editingServer, allServers, sshKeys, onClose, onSaved, onGoToKeyVault,
}) => {
  const [tab, setTab] = useState<'basic' | 'proxy' | 'jump' | 'advanced'>('basic')
  const [form, setForm] = useState<ServerSaveRequest>(() => makeInitialServerForm(editingServer))
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testMsg, setTestMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    if (open) {
      setForm(makeInitialServerForm(editingServer))
      setTab('basic')
      setTestMsg(null)
      setError('')
    }
  }, [open, editingServer])

  if (!open) return null
  const availableJumpServers = allServers.filter((s) => s.id !== editingServer?.id)

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      const saved = await api.saveServer(form)
      onSaved(saved)
      onClose()
    } catch (err) {
      setError(String(err))
    } finally {
      setSaving(false)
    }
  }

  const handleTest = async () => {
    setTesting(true)
    setTestMsg(null)
    setError('')
    try {
      setTestMsg({ ok: true, text: await api.testServerConnection(form) })
    } catch (err) {
      setTestMsg({ ok: false, text: String(err) })
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4">
      <div className="w-full max-w-2xl bg-ag-panel border border-ag-borderStrong rounded-2xl shadow-ag overflow-hidden flex flex-col max-h-[90vh]">
        <div className="px-5 py-4 border-b border-ag-border flex items-center justify-between bg-[#11151f]">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300">
              <Server className="w-4 h-4" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-ag-text">
                {editingServer ? `编辑服务器 · ${editingServer.name}` : '添加服务器连接'}
              </h2>
              <p className="text-[11px] text-ag-muted">支持密码 / 加密 SSH 密钥 / SOCKS5 & HTTP 代理 / 多级跳板机</p>
            </div>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text"><X className="w-4 h-4" /></button>
        </div>

        <div className="px-5 pt-3 border-b border-ag-border flex items-center gap-1 bg-[#11151f]/60">
          {[
            { id: 'basic', label: '基本与认证', icon: <KeyRound className="w-3.5 h-3.5" /> },
            { id: 'proxy', label: `代理${form.proxy.type !== 'none' ? ` (${form.proxy.type.toUpperCase()})` : ''}`, icon: <Globe className="w-3.5 h-3.5" /> },
            { id: 'jump', label: `跳板机${form.jumpHost.mode !== 'none' ? ' (已启用)' : ''}`, icon: <Waypoints className="w-3.5 h-3.5" /> },
            { id: 'advanced', label: '高级选项', icon: <Sliders className="w-3.5 h-3.5" /> },
          ].map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setTab(t.id as typeof tab)}
              className={`flex items-center gap-1.5 px-3 py-2 text-xs font-medium rounded-t-lg border-b-2 transition ${
                tab === t.id ? 'border-indigo-400 text-indigo-200 bg-ag-panel' : 'border-transparent text-ag-muted hover:text-ag-text'
              }`}
            >
              {t.icon}
              {t.label}
            </button>
          ))}
        </div>

        <form onSubmit={handleSave} className="flex-1 overflow-y-auto p-5 space-y-4">
          {tab === 'basic' && <ServerBasicTab form={form} editingServer={editingServer} sshKeys={sshKeys} onChange={setForm} onGoToKeyVault={() => { onClose(); onGoToKeyVault() }} />}
          {tab === 'proxy' && <ServerProxyTab form={form} editingServer={editingServer} onChange={setForm} />}
          {tab === 'jump' && <ServerJumpTab form={form} editingServer={editingServer} availableJumpServers={availableJumpServers} sshKeys={sshKeys} onChange={setForm} />}
          {tab === 'advanced' && <ServerAdvancedTab form={form} onChange={setForm} />}

          {testMsg && (
            <div className={`flex items-start gap-2 p-3 rounded-xl text-xs border ${testMsg.ok ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-300' : 'bg-rose-500/10 border-rose-500/30 text-rose-300'}`}>
              {testMsg.ok ? <CheckCircle2 className="w-4 h-4 shrink-0 mt-0.5" /> : <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />}
              <span className="break-all">{testMsg.text}</span>
            </div>
          )}
          {error && (
            <div className="flex items-start gap-2 p-3 rounded-xl text-xs bg-rose-500/10 border border-rose-500/30 text-rose-300">
              <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
              <span>{error}</span>
            </div>
          )}

          <div className="pt-3 border-t border-ag-border flex items-center justify-between">
            <button type="button" onClick={handleTest} disabled={testing} className="ag-btn-secondary">
              {testing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />}
              测试连接
            </button>
            <div className="flex items-center gap-2">
              <button type="button" onClick={onClose} className="ag-btn-ghost">取消</button>
              <button type="submit" disabled={saving} className="ag-btn-primary">
                {saving && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                保存服务器
              </button>
            </div>
          </div>
        </form>
      </div>
    </div>
  )
}
