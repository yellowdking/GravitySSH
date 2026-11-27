import React from 'react'
import type { ServerSaveRequest } from '../types'
import { NumericInput } from './NumericInput'

interface Props {
  form: ServerSaveRequest
  defaultKeepAliveSeconds?: number
  onChange: (next: ServerSaveRequest) => void
}

export const ServerAdvancedTab: React.FC<Props> = ({ form, defaultKeepAliveSeconds = 30, onChange }) => (
  <div className="space-y-4">
    <div className="grid grid-cols-2 gap-3.5">
      <div>
        <label className="block text-xs text-ag-muted mb-1.5">连接超时 (秒)</label>
        <NumericInput
          min={5}
          max={120}
          fallback={15}
          className="ag-input font-mono"
          value={form.timeoutSeconds}
          onChange={(timeoutSeconds) => onChange({ ...form, timeoutSeconds })}
        />
      </div>
      <div>
        <label className="block text-xs text-ag-muted mb-1.5">心跳保活间隔 (秒 · 防止弱网断连)</label>
        <NumericInput
          min={5}
          max={300}
          fallback={defaultKeepAliveSeconds > 0 ? defaultKeepAliveSeconds : 30}
          className="ag-input font-mono"
          value={form.keepAliveSeconds}
          onChange={(keepAliveSeconds) => onChange({ ...form, keepAliveSeconds })}
        />
      </div>
    </div>
    <div>
      <label className="block text-xs text-ag-muted mb-1.5">SFTP 默认打开目录（留空则自动进入用户主目录）</label>
      <input
        className="ag-input font-mono"
        placeholder="/var/www 或 /opt/app"
        value={form.defaultRemotePath}
        onChange={(e) => onChange({ ...form, defaultRemotePath: e.target.value })}
      />
    </div>
    <div>
      <label className="block text-xs text-ag-muted mb-1.5">连接后自动执行的启动命令（可选）</label>
      <input
        className="ag-input font-mono"
        placeholder="例如：cd /srv/app && ls -la"
        value={form.startupCommand}
        onChange={(e) => onChange({ ...form, startupCommand: e.target.value })}
      />
    </div>
    <div>
      <label className="block text-xs text-ag-muted mb-1.5">备注</label>
      <textarea
        rows={2}
        className="ag-input"
        placeholder="机房位置、用途说明..."
        value={form.notes}
        onChange={(e) => onChange({ ...form, notes: e.target.value })}
      />
    </div>
  </div>
)
