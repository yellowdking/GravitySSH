import React from 'react'
import { Check, Copy, Lock, Trash2 } from 'lucide-react'
import type { SSHKeyMeta } from '../types'

interface Props {
  item: SSHKeyMeta
  copied: boolean
  onCopyPublic: (k: SSHKeyMeta) => void
  onDelete: (k: SSHKeyMeta) => void
}

export const KeyCardItem: React.FC<Props> = ({ item: k, copied, onCopyPublic, onDelete }) => (
  <div className="rounded-xl bg-ag-panel border border-ag-border hover:border-ag-borderStrong p-4 transition">
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-semibold text-ag-text">{k.name}</span>
          <span className="text-[11px] font-mono px-2 py-0.5 rounded bg-indigo-500/15 text-indigo-300 border border-indigo-500/25">{k.keyType}</span>
          <span className="inline-flex items-center gap-1 text-[11px] px-2 py-0.5 rounded bg-emerald-500/12 text-emerald-300 border border-emerald-500/20">
            <Lock className="w-2.5 h-2.5" />
            DPAPI 已加密
          </span>
          {k.hasPassphrase && <span className="text-[11px] px-2 py-0.5 rounded bg-amber-500/12 text-amber-300 border border-amber-500/20">含口令保护</span>}
          <span className="text-[11px] text-ag-subtle">关联 {k.usageCount} 台服务器</span>
        </div>
        <div className="mt-2 text-xs font-mono text-ag-muted break-all">指纹：{k.fingerprint}</div>
        <div className="mt-1.5 text-[11px] font-mono text-ag-subtle bg-[#0b0d12] border border-ag-border/80 rounded-lg px-2.5 py-1.5 truncate">{k.publicKey}</div>
        {k.comment && <div className="mt-1.5 text-xs text-ag-muted">备注：{k.comment}</div>}
      </div>
      <div className="flex items-center gap-1.5 shrink-0">
        <button onClick={() => onCopyPublic(k)} className="ag-btn-secondary text-xs py-1.5 px-2.5">
          {copied ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
          复制公钥
        </button>
        <button onClick={() => onDelete(k)} className="p-2 rounded-lg hover:bg-rose-500/15 text-ag-muted hover:text-rose-400 transition" title="删除密钥">
          <Trash2 className="w-4 h-4" />
        </button>
      </div>
    </div>
  </div>
)
