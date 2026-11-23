import React from 'react'
import type { ProxyType, ServerSaveRequest, ServerView } from '../types'

interface Props {
  form: ServerSaveRequest
  editingServer: ServerView | null
  onChange: (next: ServerSaveRequest) => void
}

export const ServerProxyTab: React.FC<Props> = ({ form, editingServer, onChange }) => (
  <div className="space-y-4">
    <div className="p-3 rounded-xl bg-sky-500/8 border border-sky-500/20 text-xs text-sky-200/90 leading-relaxed">
      配置代理后，SSH 和 SFTP 流量都将通过指定的 SOCKS5 或 HTTP CONNECT 代理隧道连接，适合跨境直连丢包严重或受限网络环境。
    </div>

    <div>
      <label className="block text-xs text-ag-muted mb-1.5">代理协议</label>
      <div className="grid grid-cols-3 gap-2">
        {(
          [
            { id: 'none', label: '不使用代理' },
            { id: 'socks5', label: 'SOCKS5 代理' },
            { id: 'http', label: 'HTTP (CONNECT) 代理' },
          ] as { id: ProxyType; label: string }[]
        ).map((opt) => (
          <button
            key={opt.id}
            type="button"
            onClick={() => onChange({ ...form, proxy: { ...form.proxy, type: opt.id } })}
            className={`py-2 px-3 rounded-lg text-xs font-medium border transition ${
              form.proxy.type === opt.id
                ? 'bg-sky-500/15 border-sky-400/60 text-sky-200'
                : 'bg-[#0d1017] border-ag-border text-ag-muted hover:text-ag-text'
            }`}
          >
            {opt.label}
          </button>
        ))}
      </div>
    </div>

    {form.proxy.type !== 'none' && (
      <>
        <div className="grid grid-cols-12 gap-3">
          <div className="col-span-8">
            <label className="block text-xs text-ag-muted mb-1.5">代理服务器地址</label>
            <input
              className="ag-input font-mono"
              placeholder="127.0.0.1"
              value={form.proxy.host}
              onChange={(e) => onChange({ ...form, proxy: { ...form.proxy, host: e.target.value } })}
            />
          </div>
          <div className="col-span-4">
            <label className="block text-xs text-ag-muted mb-1.5">代理端口</label>
            <input
              type="number"
              className="ag-input font-mono"
              placeholder="7890"
              value={form.proxy.port}
              onChange={(e) => onChange({ ...form, proxy: { ...form.proxy, port: Number(e.target.value) || 0 } })}
            />
          </div>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-xs text-ag-muted mb-1.5">代理用户名（可选）</label>
            <input
              className="ag-input font-mono"
              value={form.proxy.username}
              onChange={(e) => onChange({ ...form, proxy: { ...form.proxy, username: e.target.value } })}
            />
          </div>
          <div>
            <label className="block text-xs text-ag-muted mb-1.5">
              代理密码（可选） {form.proxy.keepExistingPassword && <span className="text-emerald-400">(已保存)</span>}
            </label>
            <input
              type="password"
              className="ag-input font-mono"
              placeholder={form.proxy.keepExistingPassword ? '••••••••' : ''}
              value={form.proxy.password}
              onChange={(e) =>
                onChange({
                  ...form,
                  proxy: {
                    ...form.proxy,
                    password: e.target.value,
                    keepExistingPassword: e.target.value === '' && !!editingServer?.proxy?.hasPassword,
                  },
                })
              }
            />
          </div>
        </div>
      </>
    )}
  </div>
)
