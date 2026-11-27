import React from 'react'
import { AlertCircle, CheckCircle2, Loader2, UploadCloud } from 'lucide-react'
import type { AuthType, ServerSaveRequest, ServerView, SSHKeyMeta } from '../types'
import { COLOR_PRESETS } from './serverForm'
import { NumericInput } from './NumericInput'

interface Props {
  form: ServerSaveRequest
  editingServer: ServerView | null
  sshKeys: SSHKeyMeta[]
  deploying: boolean
  deployMsg: { ok: boolean; text: string } | null
  connectionDirty: boolean
  disableQuickDeploy: boolean
  disableKeyVaultLink?: boolean
  onQuickDeploy: () => void
  onChange: (next: ServerSaveRequest) => void
  onGoToKeyVault: () => void
}

const AUTH_OPTIONS: { id: AuthType; label: string }[] = [
  { id: 'password', label: '密码登录' },
  { id: 'key', label: 'SSH 密钥登录' },
  { id: 'both', label: '密钥优先 + 密码备用' },
]

export const ServerBasicTab: React.FC<Props> = ({
  form,
  editingServer,
  sshKeys,
  deploying,
  deployMsg,
  connectionDirty,
  disableQuickDeploy,
  disableKeyVaultLink = false,
  onQuickDeploy,
  onChange,
  onGoToKeyVault,
}) => {
  const patch = (p: Partial<ServerSaveRequest>) => onChange({ ...form, ...p })
  const hasValidKey = Boolean(form.sshKeyId) && sshKeys.some((k) => k.id === form.sshKeyId)

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
          <input
            required
            className="ag-input font-mono"
            placeholder="192.168.1.10 或 server.example.com"
            value={form.host}
            onChange={(e) => {
              const nextHost = e.target.value
              const hostChanged = nextHost !== form.host
              patch({
                host: nextHost,
                ...(hostChanged && form.keepExistingPassword ? { keepExistingPassword: false } : {}),
              })
            }}
          />
        </div>
        <div className="col-span-2">
          <label className="block text-xs text-ag-muted mb-1.5">端口</label>
          <NumericInput min={1} max={65535} fallback={22} className="ag-input font-mono" value={form.port} onChange={(port) => patch({ port })} />
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
            <button
              key={opt.id}
              type="button"
              onClick={() =>
                patch({
                  authType: opt.id,
                  ...(opt.id === 'key' ? { password: '', keepExistingPassword: false } : {}),
                })
              }
              className={`py-2 px-3 rounded-lg text-xs font-medium border transition ${form.authType === opt.id ? 'bg-indigo-500/15 border-indigo-400/60 text-indigo-200' : 'bg-[#0d1017] border-ag-border text-ag-muted hover:text-ag-text'}`}
            >
              {opt.label}
            </button>
          ))}
        </div>
      </div>

      {(form.authType === 'key' || form.authType === 'both') && (
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <label className="text-xs text-ag-muted">选择已保存的 SSH 密钥</label>
            <button type="button" disabled={disableKeyVaultLink} onClick={onGoToKeyVault} className="text-[11px] text-indigo-400 hover:text-indigo-300 disabled:opacity-40 disabled:cursor-not-allowed">+ 去密钥库加载/生成新密钥</button>
          </div>
          <select className="ag-input" value={form.sshKeyId} onChange={(e) => patch({ sshKeyId: e.target.value })}>
            <option value="">{sshKeys.length === 0 ? '-- 暂无可用 SSH 密钥 --' : '-- 请选择 SSH 密钥 --'}</option>
            {form.sshKeyId && !hasValidKey && (
              <option value={form.sshKeyId}>-- 原关联密钥已删除/不可用 --</option>
            )}
            {sshKeys.map((k) => <option key={k.id} value={k.id}>{k.name} ({k.keyType})</option>)}
          </select>
          {!hasValidKey && (form.authType === 'key' || Boolean(form.sshKeyId) || (editingServer && (editingServer.authType === 'key' || editingServer.authType === 'both'))) && (
            <p className="text-[11px] text-amber-300/90">
              {form.sshKeyId ? '原关联的 SSH 密钥已删除或不可用，请重新选择或前往密钥库生成' : '尚未关联 SSH 密钥（若原密钥已删除，请重新选择或前往密钥库生成）'}
            </p>
          )}

          {editingServer && hasValidKey && (
            <div className="space-y-2 pt-0.5">
              <button type="button" disabled={disableQuickDeploy} onClick={onQuickDeploy} className="ag-btn-secondary text-xs py-1.5 px-3">
                {deploying ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <UploadCloud className="w-3.5 h-3.5 text-indigo-400" />}
                {deploying ? '正在部署密钥...' : '一键部署该密钥到此服务器'}
              </button>
              {connectionDirty && (
                <p className="text-[11px] text-amber-300/90">
                  连接参数（主机/端口/用户名/密码/代理/跳板机）已修改但尚未保存，请先保存服务器后再部署密钥
                </p>
              )}
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
          <div className="flex items-center justify-between gap-2 mb-1.5">
            <label className="block text-xs text-ag-muted">
              SSH 密码{' '}
              {form.keepExistingPassword ? (
                <span className="text-emerald-400">(已保存，留空保持不变)</span>
              ) : (
                editingServer?.hasPassword && !form.password && (
                  <span className="text-amber-400">(保存后将清除原密码)</span>
                )
              )}
            </label>
            {form.keepExistingPassword && (
              <button
                type="button"
                onClick={() => patch({ password: '', keepExistingPassword: false })}
                className="text-[11px] text-rose-400 hover:text-rose-300 transition"
              >
                清除已保存密码
              </button>
            )}
          </div>
          <input
            type="password"
            className="ag-input font-mono"
            placeholder={form.keepExistingPassword ? '••••••••（留空保持原密码）' : '输入登录密码'}
            value={form.password}
            onChange={(e) => patch({ password: e.target.value, keepExistingPassword: false })}
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
