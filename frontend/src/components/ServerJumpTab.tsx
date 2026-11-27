import React from 'react'
import type { AuthType, JumpMode, ServerSaveRequest, ServerView, SSHKeyMeta } from '../types'
import { NumericInput } from './NumericInput'

interface Props {
  form: ServerSaveRequest
  editingServer: ServerView | null
  availableJumpServers: ServerView[]
  sshKeys: SSHKeyMeta[]
  onChange: (next: ServerSaveRequest) => void
}

export const ServerJumpTab: React.FC<Props> = ({
  form,
  editingServer,
  availableJumpServers,
  sshKeys,
  onChange,
}) => (
  <div className="space-y-4">
    <div className="p-3 rounded-xl bg-amber-500/[0.08] border border-amber-500/20 text-xs text-amber-200/90 leading-relaxed">
      通过跳板机 (Bastion / ProxyJump) 建立加密 SSH 隧道中转到目标机器，适合访问内网机器或线路不佳的海外节点（支持选择已保存服务器形成多级跳板链）。
    </div>

    <div>
      <label className="block text-xs text-ag-muted mb-1.5">跳板机模式</label>
      <div className="grid grid-cols-3 gap-2">
        {(
          [
            { id: 'none', label: '不使用跳板机' },
            { id: 'server', label: '选择已有服务器' },
            { id: 'custom', label: '自定义跳板机' },
          ] as { id: JumpMode; label: string }[]
        ).map((opt) => (
          <button
            key={opt.id}
            type="button"
            onClick={() => onChange({ ...form, jumpHost: { ...form.jumpHost, mode: opt.id } })}
            className={`py-2 px-3 rounded-lg text-xs font-medium border transition ${
              form.jumpHost.mode === opt.id
                ? 'bg-amber-500/15 border-amber-400/60 text-amber-200'
                : 'bg-[#0d1017] border-ag-border text-ag-muted hover:text-ag-text'
            }`}
          >
            {opt.label}
          </button>
        ))}
      </div>
    </div>

    {form.jumpHost.mode === 'server' && (
      <div>
        <label className="block text-xs text-ag-muted mb-1.5">选择跳板服务器</label>
        <select
          className="ag-input"
          value={form.jumpHost.serverId}
          onChange={(e) => onChange({ ...form, jumpHost: { ...form.jumpHost, serverId: e.target.value } })}
        >
          <option value="">-- 请选择一台已保存的服务器作为跳板机 --</option>
          {availableJumpServers.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name} ({s.username}@{s.host}:{s.port})
            </option>
          ))}
        </select>
        <p className="text-[11px] text-ag-subtle mt-1.5">
          提示：如果所选跳板机自身也配置了代理或上一级跳板机，连接时会自动按完整链路中转。
        </p>
      </div>
    )}

    {form.jumpHost.mode === 'custom' && (
      <div className="space-y-3">
        <div className="grid grid-cols-12 gap-3">
          <div className="col-span-6">
            <label className="block text-xs text-ag-muted mb-1.5">跳板机地址 *</label>
            <input
              className="ag-input font-mono"
              placeholder="bastion.example.com"
              value={form.jumpHost.host}
              onChange={(e) => onChange({ ...form, jumpHost: { ...form.jumpHost, host: e.target.value } })}
            />
          </div>
          <div className="col-span-2">
            <label className="block text-xs text-ag-muted mb-1.5">端口</label>
            <NumericInput
              min={1}
              max={65535}
              fallback={22}
              className="ag-input font-mono"
              value={form.jumpHost.port}
              onChange={(port) => onChange({ ...form, jumpHost: { ...form.jumpHost, port } })}
            />
          </div>
          <div className="col-span-4">
            <label className="block text-xs text-ag-muted mb-1.5">跳板机用户名</label>
            <input
              className="ag-input font-mono"
              value={form.jumpHost.username}
              onChange={(e) => onChange({ ...form, jumpHost: { ...form.jumpHost, username: e.target.value } })}
            />
          </div>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-xs text-ag-muted mb-1.5">跳板机认证方式</label>
            <select
              className="ag-input"
              value={form.jumpHost.authType}
              onChange={(e) =>
                onChange({ ...form, jumpHost: { ...form.jumpHost, authType: e.target.value as AuthType } })
              }
            >
              <option value="password">密码</option>
              <option value="key">已保存 SSH 密钥</option>
              <option value="both">密钥 + 密码</option>
            </select>
          </div>
          <div>
            <label className="block text-xs text-ag-muted mb-1.5">跳板机 SSH 密钥</label>
            <select
              className="ag-input"
              value={form.jumpHost.sshKeyId}
              onChange={(e) => onChange({ ...form, jumpHost: { ...form.jumpHost, sshKeyId: e.target.value } })}
            >
              <option value="">-- 无 --</option>
              {sshKeys.map((k) => (
                <option key={k.id} value={k.id}>
                  {k.name} ({k.keyType})
                </option>
              ))}
            </select>
          </div>
        </div>

        <div>
          <label className="block text-xs text-ag-muted mb-1.5">
            跳板机密码 {form.jumpHost.keepExistingPassword && <span className="text-emerald-400">(已保存)</span>}
          </label>
          <input
            type="password"
            className="ag-input font-mono"
            placeholder={form.jumpHost.keepExistingPassword ? '••••••••' : '输入跳板机密码'}
            value={form.jumpHost.password}
            onChange={(e) =>
              onChange({
                ...form,
                jumpHost: {
                  ...form.jumpHost,
                  password: e.target.value,
                  keepExistingPassword: e.target.value === '' && !!editingServer?.jumpHost?.hasPassword,
                },
              })
            }
          />
        </div>

        <label className="flex items-center gap-2 text-xs text-ag-muted cursor-pointer">
          <input
            type="checkbox"
            checked={form.jumpHost.useMainProxy}
            onChange={(e) => onChange({ ...form, jumpHost: { ...form.jumpHost, useMainProxy: e.target.checked } })}
            className="rounded border-ag-border bg-[#0d1017] accent-indigo-500"
          />
          连接此自定义跳板机时，同时走上面配置的代理通道
        </label>
      </div>
    )}
  </div>
)
