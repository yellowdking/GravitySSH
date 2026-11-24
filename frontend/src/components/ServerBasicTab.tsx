import React, { useEffect, useState } from 'react'
import { AlertCircle, CheckCircle2, Loader2, UploadCloud } from 'lucide-react'
import type { AuthType, ServerSaveRequest, ServerView, SSHKeyMeta } from '../types'
import { api } from '../wailsApi'
import { COLOR_PRESETS } from './serverForm'

interface Props {
  form: ServerSaveRequest
  editingServer: ServerView | null
  sshKeys: SSHKeyMeta[]
  onChange: (next: ServerSaveRequest) => void
  onGoToKeyVault: () => void
}

const AUTH_OPTIONS: { id: AuthType; label: string }[] = [
  { id: 'password', label: '密码登录' },
  { id: 'key', label: 'SSH 密钥登录' },
  { id: 'both', label: '密钥优先 + 密码备用' },
]

export const ServerBasicTab: React.FC<Props> = ({ form, editingServer, sshKeys, onChange, onGoToKeyVault }) => {
  const [deploying, setDeploying] = useState(false)
  const [deployMsg, setDeployMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const patch = (p: Partial<ServerSaveRequest>) => onChange({ ...form, ...p })

  useEffect(() => { setDeployMsg(null) }, [form.sshKeyId, editingServer?.id])

  const handleQuickDeploy = async () => {
    if (!editingServer || !form.sshKeyId) return
    setDeploying(true)
    setDeployMsg(null)
    try {
      const [first] = await api.deploySSHKeyToServers(form.sshKeyId, [editingServer.id], false)
      setDeployMsg(first ? { ok: first.ok, text: first.message } : { ok: true, text: '密钥部署完成' })
    } catch (err) {
      setDeployMsg({ ok: false, text: String(err) })
    } finally {
      setDeploying(false)
    }
  }

  return (
    <>
      <div className="grid grid-cols-2 gap-3.5">
        <div>
          <label className="block text-xs text-ag-muted mb-1.5">服务器名称</label>
          <input className="ag-input" placeholder="例如：香港生产机 / Tokyo-VPS" value={form.name} onChange={(e) => patch({ name: e.target.value })} />
        </div>
        <div>
          <label className="block text-xs text-ag-muted mb-1.5">分组</label>
          <input className="ag-input" placeholder="例如：生产环境 / 内网机器" value={form.group} onChange={(e) => patch({ group: e.target.value })} />
        </div>
      </div>

      <div className="grid grid-cols-12 gap-3">
        <div className="col-span-6">
          <label className="block text-xs text-ag-muted mb-1.5">主机地址 (IP / 域名) *</label>
          <input required className="ag-input font-mono" placeholder="192.168.1.10 或 server.example.com" value={form.host} onChange={(e) => patch({ host: e.target.value })} />
        </div>
        <div className="col-span-2">
          <label className="block text-xs text-ag-muted mb-1.5">端口</label>
          <input type="number" min={1} max={65535} className="ag-input font-mono" value={form.port} onChange={(e) => patch({ port: Number(e.target.value) || 22 })} />
        </div>
        <div className="col-span-4">
          <label className="block text-xs text-ag-muted mb-1.5">用户名</label>
          <input className="ag-input font-mono" placeholder="root" value={form.username} onChange={(e) => patch({ username: e.target.value })} />
        </div>
      </div>

      <div>
        <label className="block text-xs text-ag-muted mb-1.5">认证方式</label>
        <div className="grid grid-cols-3 gap-2">
          {AUTH_OPTIONS.map((opt) => (
            <button key={opt.id} type="button" onClick={() => patch({ authType: opt.id })} className={`py-2 px-3 rounded-lg text-xs font-medium border transition ${form.authType === opt.id ? 'bg-indigo-500/15 border-indigo-400/60 text-indigo-200' : 'bg-[#0d1017] border-ag-border text-ag-muted hover:text-ag-text'}`}>
              {opt.label}
            </button>
          ))}
        </div>
      </div>

      {(form.authType === 'key' || form.authType === 'both') && (
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <label className="text-xs text-ag-muted">选择已保存的 SSH 密钥</label>
            <button type="button" onClick={onGoToKeyVault} className="text-[11px] text-indigo-400 hover:text-indigo-300">+ 去密钥库加载/生成新密钥</button>
          </div>
          <select className="ag-input" value={form.sshKeyId} onChange={(e) => patch({ sshKeyId: e.target.value })}>
            <option value="">-- 请选择 SSH 密钥 --</option>
            {sshKeys.map((k) => <option key={k.id} value={k.id}>{k.name} ({k.keyType})</option>)}
          </select>

          {editingServer && form.sshKeyId && (
            <div className="space-y-2 pt-0.5">
              <button type="button" disabled={deploying} onClick={() => void handleQuickDeploy()} className="ag-btn-secondary text-xs py-1.5 px-3">
                {deploying ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <UploadCloud className="w-3.5 h-3.5 text-indigo-400" />}
                {deploying ? '正在部署密钥...' : '一键部署该密钥到此服务器'}
              </button>
              {deployMsg && (
                <div className={`flex items-start gap-2 rounded-lg px-3 py-2 text-xs border ${deployMsg.ok ? 'bg-emerald-500/10 border-emerald-500/25 text-emerald-300' : 'bg-rose-500/10 border-rose-500/25 text-rose-300'}`}>
                  {deployMsg.ok ? <CheckCircle2 className="w-3.5 h-3.5 shrink-0 mt-0.5" /> : <AlertCircle className="w-3.5 h-3.5 shrink-0 mt-0.5" />}
                  <span className="break-all">{deployMsg.text}</span>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {(form.authType === 'password' || form.authType === 'both') && (
        <div>
          <label className="block text-xs text-ag-muted mb-1.5">
            SSH 密码 {form.keepExistingPassword && <span className="text-emerald-400">(已保存，留空保持不变)</span>}
          </label>
          <input
            type="password"
            className="ag-input font-mono"
            placeholder={form.keepExistingPassword ? '••••••••（留空保持原密码）' : '输入登录密码'}
            value={form.password}
            onChange={(e) => patch({ password: e.target.value, keepExistingPassword: e.target.value === '' && !!editingServer?.hasPassword })}
          />
        </div>
      )}

      <div className="flex items-center gap-3 pt-1">
        <span className="text-xs text-ag-muted">节点标识色</span>
        <div className="flex items-center gap-2">
          {COLOR_PRESETS.map((c) => (
            <button key={c} type="button" onClick={() => patch({ color: c })} className={`w-5 h-5 rounded-full transition ${form.color === c ? 'ring-2 ring-white ring-offset-2 ring-offset-ag-panel scale-110' : 'opacity-70 hover:opacity-100'}`} style={{ backgroundColor: c }} />
          ))}
        </div>
      </div>
    </>
  )
}
