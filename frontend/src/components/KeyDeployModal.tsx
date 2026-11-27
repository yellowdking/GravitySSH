import React, { useEffect, useState } from 'react'
import { AlertCircle, CheckCircle2, KeyRound, Loader2, Server, UploadCloud, X } from 'lucide-react'
import type { KeyDeployResult, ServerView, SSHKeyMeta } from '../types'
import { api } from '../wailsApi'
import { ModalShell } from './ModalShell'

interface Props {
  open: boolean
  sshKey: SSHKeyMeta | null
  servers: ServerView[]
  onClose: () => void
  onDeployed: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const KeyDeployModal: React.FC<Props> = ({ open, sshKey, servers, onClose, onDeployed, onToast }) => {
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [bindAsAuth, setBindAsAuth] = useState(true)
  const [deploying, setDeploying] = useState(false)
  const [results, setResults] = useState<KeyDeployResult[] | null>(null)

  useEffect(() => {
    if (open) { setSelectedIds([]); setBindAsAuth(true); setResults(null) }
  }, [open, sshKey?.id])

  if (!open || !sshKey) return null

  const allSelected = servers.length > 0 && selectedIds.length === servers.length
  const toggleOne = (id: string) => setSelectedIds((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]))
  const toggleAll = () => setSelectedIds(allSelected ? [] : servers.map((s) => s.id))

  const handleDeploy = async () => {
    if (!selectedIds.length) return
    setDeploying(true)
    setResults(null)
    try {
      const res = await api.deploySSHKeyToServers(sshKey.id, selectedIds, bindAsAuth)
      setResults(res)
      const ok = res.filter((r) => r.ok).length
      const fail = res.length - ok
      onDeployed()
      onToast(
        fail === 0 ? `已将密钥 "${sshKey.name}" 部署到 ${ok} 台服务器` : `部署完成：${ok} 成功，${fail} 失败`,
        fail === 0 ? 'info' : 'error',
      )
    } catch (err) {
      onToast(String(err), 'error')
    } finally {
      setDeploying(false)
    }
  }

  return (
    <ModalShell open={open} onClose={onClose}>
      <div className="w-full max-w-lg rounded-2xl bg-ag-panel border border-ag-borderStrong shadow-ag overflow-hidden flex flex-col max-h-[85vh]">
        <div className="px-5 py-4 border-b border-ag-border flex items-center justify-between bg-[#11151f]">
          <div className="flex items-center gap-2.5 min-w-0">
            <div className="w-8 h-8 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300 shrink-0"><UploadCloud className="w-4 h-4" /></div>
            <div className="min-w-0">
              <h3 className="text-sm font-semibold text-ag-text truncate">部署密钥到服务器</h3>
              <p className="text-[11px] text-ag-muted flex items-center gap-1.5 truncate">
                <KeyRound className="w-3 h-3 text-indigo-400 shrink-0" />
                <span>{sshKey.name}</span>
                <span className="font-mono text-ag-subtle">({sshKey.keyType})</span>
              </p>
            </div>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text"><X className="w-4 h-4" /></button>
        </div>

        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          {servers.length === 0 ? (
            <div className="rounded-xl bg-[#0d1017] border border-dashed border-ag-border py-10 text-center">
              <Server className="w-7 h-7 text-ag-subtle mx-auto mb-2" />
              <p className="text-xs text-ag-muted">暂无已保存的服务器，请先添加服务器后再部署</p>
            </div>
          ) : (
            <>
              <div className="flex items-center justify-between">
                <span className="text-xs text-ag-muted">选择目标服务器 ({selectedIds.length}/{servers.length})</span>
                <button type="button" onClick={toggleAll} className="text-xs text-indigo-400 hover:text-indigo-300">{allSelected ? '取消全选' : '全选'}</button>
              </div>
              <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70 max-h-60 overflow-y-auto">
                {servers.map((s) => (
                  <label key={s.id} className="flex items-center gap-3 px-3.5 py-2.5 cursor-pointer hover:bg-ag-hover/40">
                    <input type="checkbox" checked={selectedIds.includes(s.id)} disabled={deploying} onChange={() => toggleOne(s.id)} className="w-4 h-4 accent-indigo-500 shrink-0" />
                    <span className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: s.color || '#6366f1' }} />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="text-xs font-medium text-ag-text truncate">{s.name}</span>
                        {s.sshKeyId === sshKey.id && <span className="text-[10px] px-1.5 rounded bg-emerald-500/[0.12] text-emerald-300 border border-emerald-500/20">已关联</span>}
                      </div>
                      <div className="text-[11px] font-mono text-ag-subtle truncate">{s.username}@{s.host}:{s.port}</div>
                    </div>
                  </label>
                ))}
              </div>
              <label className="flex items-center gap-2 text-xs text-ag-text cursor-pointer select-none">
                <input type="checkbox" checked={bindAsAuth} disabled={deploying} onChange={(e) => setBindAsAuth(e.target.checked)} className="w-4 h-4 accent-indigo-500" />
                <span>部署成功后自动将该服务器切换为密钥登录</span>
              </label>
            </>
          )}

          {results && results.length > 0 && (
            <div className="space-y-1.5 max-h-40 overflow-y-auto">
              {results.map((r) => (
                <div key={r.serverId} className={`flex items-start gap-2 rounded-xl px-3 py-2 text-xs border ${r.ok ? 'bg-emerald-500/10 border-emerald-500/25 text-emerald-200' : 'bg-rose-500/10 border-rose-500/25 text-rose-200'}`}>
                  {r.ok ? <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0 mt-0.5" /> : <AlertCircle className="w-4 h-4 text-rose-400 shrink-0 mt-0.5" />}
                  <div className="min-w-0 flex-1"><span className="font-medium">{r.serverName}</span> · <span className="break-all opacity-90">{r.message}</span></div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="px-5 py-3.5 border-t border-ag-border flex justify-end gap-2 bg-[#11151f]">
          <button type="button" onClick={onClose} className="ag-btn-ghost">关闭</button>
          <button type="button" disabled={deploying || !selectedIds.length} onClick={() => void handleDeploy()} className="ag-btn-primary">
            {deploying ? <Loader2 className="w-4 h-4 animate-spin" /> : <UploadCloud className="w-4 h-4" />}
            {deploying ? '正在部署...' : '一键加载部署到所选服务器'}
          </button>
        </div>
      </div>
    </ModalShell>
  )
}
