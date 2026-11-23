import React from 'react'
import type { AuthType, ServerSaveRequest, ServerView, SSHKeyMeta } from '../types'
import { COLOR_PRESETS } from './serverForm'

interface Props {
  form: ServerSaveRequest
  editingServer: ServerView | null
  sshKeys: SSHKeyMeta[]
  onChange: (next: ServerSaveRequest) => void
  onGoToKeyVault: () => void
}

export const ServerBasicTab: React.FC<Props> = ({ form, editingServer, sshKeys, onChange, onGoToKeyVault }) => (
  <>
    <div className="grid grid-cols-2 gap-3.5">
      <div>
        <label className="block text-xs text-ag-muted mb-1.5">服务器名称</label>
        <input
          className="ag-input"
          placeholder="例如：香港生产机 / Tokyo-VPS"
          value={form.name}
          onChange={(e) => onChange({ ...form, name: e.target.value })}
        />
      </div>
      <div>
        <label className="block text-xs text-ag-muted mb-1.5">分组</label>
        <input
          className="ag-input"
          placeholder="例如：生产环境 / 内网机器"
          value={form.group}
          onChange={(e) => onChange({ ...form, group: e.target.value })}
        />
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
          onChange={(e) => onChange({ ...form, host: e.target.value })}
        />
      </div>
      <div className="col-span-2">
        <label className="block text-xs text-ag-muted mb-1.5">端口</label>
        <input
          type="number"
          min={1}
          max={65535}
          className="ag-input font-mono"
          value={form.port}
          onChange={(e) => onChange({ ...form, port: Number(e.target.value) || 22 })}
        />
      </div>
      <div className="col-span-4">
        <label className="block text-xs text-ag-muted mb-1.5">用户名</label>
        <input
          className="ag-input font-mono"
          placeholder="root"
          value={form.username}
          onChange={(e) => onChange({ ...form, username: e.target.value })}
        />
      </div>
    </div>

    <div>
      <label className="block text-xs text-ag-muted mb-1.5">认证方式</label>
      <div className="grid grid-cols-3 gap-2">
        {(
          [
            { id: 'password', label: '密码登录' },
            { id: 'key', label: 'SSH 密钥登录' },
            { id: 'both', label: '密钥优先 + 密码备用' },
          ] as { id: AuthType; label: string }[]
        ).map((opt) => (
          <button
            key={opt.id}
            type="button"
            onClick={() => onChange({ ...form, authType: opt.id })}
            className={`py-2 px-3 rounded-lg text-xs font-medium border transition ${
              form.authType === opt.id
                ? 'bg-indigo-500/15 border-indigo-400/60 text-indigo-200'
                : 'bg-[#0d1017] border-ag-border text-ag-muted hover:text-ag-text'
            }`}
          >
            {opt.label}
          </button>
        ))}
      </div>
    </div>

    {(form.authType === 'key' || form.authType === 'both') && (
      <div>
        <div className="flex items-center justify-between mb-1.5">
          <label className="text-xs text-ag-muted">选择已加密保存的 SSH 密钥</label>
          <button type="button" onClick={onGoToKeyVault} className="text-[11px] text-indigo-400 hover:text-indigo-300">
            + 去密钥库加载/生成新密钥
          </button>
        </div>
        <select
          className="ag-input"
          value={form.sshKeyId}
          onChange={(e) => onChange({ ...form, sshKeyId: e.target.value })}
        >
          <option value="">-- 请选择 SSH 密钥 --</option>
          {sshKeys.map((k) => (
            <option key={k.id} value={k.id}>
              {k.name} ({k.keyType} · {k.fingerprint.slice(0, 18)}...)
            </option>
          ))}
        </select>
      </div>
    )}

    {(form.authType === 'password' || form.authType === 'both') && (
      <div>
        <label className="block text-xs text-ag-muted mb-1.5">
          SSH 密码 {form.keepExistingPassword && <span className="text-emerald-400">(已加密保存，留空保持不变)</span>}
        </label>
        <input
          type="password"
          className="ag-input font-mono"
          placeholder={form.keepExistingPassword ? '••••••••（留空保持原密码）' : '输入登录密码（将使用 DPAPI 加密存储）'}
          value={form.password}
          onChange={(e) =>
            onChange({
              ...form,
              password: e.target.value,
              keepExistingPassword: e.target.value === '' && !!editingServer?.hasPassword,
            })
          }
        />
      </div>
    )}

    <div className="flex items-center gap-3 pt-1">
      <span className="text-xs text-ag-muted">节点标识色</span>
      <div className="flex items-center gap-2">
        {COLOR_PRESETS.map((c) => (
          <button
            key={c}
            type="button"
            onClick={() => onChange({ ...form, color: c })}
            className={`w-5 h-5 rounded-full transition ${
              form.color === c ? 'ring-2 ring-white ring-offset-2 ring-offset-ag-panel scale-110' : 'opacity-70 hover:opacity-100'
            }`}
            style={{ backgroundColor: c }}
          />
        ))}
      </div>
    </div>
  </>
)
