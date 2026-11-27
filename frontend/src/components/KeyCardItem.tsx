import React from 'react'
import { Check, CheckCircle2, Copy, Fingerprint, Trash2, UploadCloud } from 'lucide-react'
import type { SSHKeyMeta } from '../types'
import { useCopyFeedback } from '../hooks/useCopyFeedback'

interface Props {
  item: SSHKeyMeta
  onDeploy: (k: SSHKeyMeta) => void
  onDelete: (k: SSHKeyMeta) => void
  onToast?: (msg: string, type?: 'info' | 'error') => void
}

export const KeyCardItem: React.FC<Props> = React.memo(({ item: k, onDeploy, onDelete, onToast }) => {
  const { copied, copy } = useCopyFeedback()
  const hasPublicKey = Boolean(k.publicKey && k.publicKey.trim())

  const handleCopyPublicKey = async () => {
    if (!hasPublicKey) return
    const ok = await copy(k.publicKey.trim())
    if (ok) onToast?.('已复制 SSH 公钥')
    else onToast?.('复制 SSH 公钥失败', 'error')
  }

  return (
    <div className="rounded-xl bg-ag-panel border border-ag-border hover:border-ag-borderStrong px-4 py-3.5 transition">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-semibold text-ag-text truncate">{k.name}</span>
            <span className="text-[11px] font-mono px-2 py-0.5 rounded bg-indigo-500/15 text-indigo-300 border border-indigo-500/25">
              {k.keyType}
            </span>
            <span className="inline-flex items-center gap-1 text-[11px] px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-300 border border-emerald-500/20">
              <CheckCircle2 className="w-3 h-3" />
              已保存
            </span>
            {k.hasPassphrase && (
              <span className="text-[11px] px-2 py-0.5 rounded bg-amber-500/[0.12] text-amber-300 border border-amber-500/20">
                含口令
              </span>
            )}
            <span className="text-[11px] text-ag-subtle">关联 {k.usageCount} 台服务器</span>
          </div>
          {k.fingerprint && (
            <div className="mt-1.5 flex items-center gap-1.5 text-[11px] font-mono text-ag-muted min-w-0" title={k.fingerprint}>
              <Fingerprint className="w-3.5 h-3.5 text-sky-400/80 shrink-0" />
              <span className="truncate select-all">{k.fingerprint}</span>
            </div>
          )}
          {k.comment && <div className="mt-1 text-xs text-ag-muted truncate">备注：{k.comment}</div>}
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          {hasPublicKey && (
            <button
              type="button"
              onClick={() => void handleCopyPublicKey()}
              title="复制 OpenSSH 公钥"
              className="ag-btn-secondary text-xs py-1.5 px-2.5"
            >
              {copied ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5 text-sky-400" />}
              {copied ? '已复制' : '复制公钥'}
            </button>
          )}
          <button type="button" onClick={() => onDeploy(k)} className="ag-btn-secondary text-xs py-1.5 px-2.5">
            <UploadCloud className="w-3.5 h-3.5 text-indigo-400" />
            部署到服务器
          </button>
          <button
            type="button"
            onClick={() => onDelete(k)}
            className="p-2 rounded-lg hover:bg-rose-500/15 text-ag-muted hover:text-rose-400 transition"
            title="删除密钥"
            aria-label={`删除密钥 ${k.name}`}
          >
            <Trash2 className="w-4 h-4" />
          </button>
        </div>
      </div>
    </div>
  )
})
