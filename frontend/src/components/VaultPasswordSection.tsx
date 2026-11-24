import React, { useState } from 'react'
import { AlertCircle, FolderLock, KeyRound, Loader2, Lock, Unlock } from 'lucide-react'
import type { VaultLockState, VaultSecurityInfo } from '../types'
import { api } from '../wailsApi'

interface Props {
  securityInfo: VaultSecurityInfo | null
  lockState: VaultLockState | null
  onLockStateChanged: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const VaultPasswordSection: React.FC<Props> = ({
  securityInfo,
  lockState,
  onLockStateChanged,
  onToast,
}) => {
  const hasPassword = !!lockState?.hasPassword
  const vaultPath = lockState?.vaultPath || securityInfo?.vaultPath || ''

  const [action, setAction] = useState<'none' | 'set' | 'change' | 'remove'>('none')
  const [currentPwd, setCurrentPwd] = useState('')
  const [newPwd, setNewPwd] = useState('')
  const [confirmPwd, setConfirmPwd] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const reset = () => {
    setAction('none')
    setCurrentPwd('')
    setNewPwd('')
    setConfirmPwd('')
    setError('')
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (action === 'set' || action === 'change') {
      if (action === 'change' && !currentPwd) return setError('请输入当前加密密码')
      if (!newPwd) return setError('请输入新密码')
      if (newPwd !== confirmPwd) return setError('两次输入的新密码不一致')
    } else if (action === 'remove') {
      if (!currentPwd) return setError('请输入当前加密密码以确认移除')
    }

    setBusy(true)
    try {
      const cur = action === 'set' ? '' : currentPwd
      const next = action === 'remove' ? '' : newPwd
      await api.setVaultPassword(cur, next)
      onLockStateChanged()
      if (action === 'set') onToast('已设置配置文件加密密码，下次启动时将要求解锁')
      else if (action === 'change') onToast('配置文件加密密码已修改')
      else onToast('已移除自定义密码，恢复默认自动加密')
      reset()
    } catch (err) {
      setError(String(err).replace(/^Error:\s*/i, ''))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="space-y-2.5">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-xs font-semibold text-emerald-300 uppercase tracking-wider">
          <Lock className="w-3.5 h-3.5" />
          配置文件加密密码
        </div>
        <span
          className={`text-[11px] px-2 py-0.5 rounded border ${
            hasPassword
              ? 'bg-indigo-500/15 text-indigo-300 border-indigo-500/30'
              : 'bg-ag-elevated text-ag-muted border-ag-border'
          }`}
        >
          {hasPassword ? '已设置自定义密码' : '默认自动加密'}
        </span>
      </div>

      <div className="rounded-xl bg-[#0d1017] border border-ag-border p-3.5 space-y-3">
        {vaultPath && (
          <div className="flex items-center gap-2 text-[11px] font-mono text-ag-muted" title={vaultPath}>
            <FolderLock className="w-3.5 h-3.5 text-ag-subtle shrink-0" />
            <span className="truncate">{vaultPath}</span>
          </div>
        )}

        {action === 'none' ? (
          <div className="flex flex-wrap items-center gap-2">
            {!hasPassword ? (
              <button type="button" onClick={() => setAction('set')} className="ag-btn-secondary text-xs py-1.5 px-3">
                <KeyRound className="w-3.5 h-3.5 text-indigo-400" />设置自定义加密密码
              </button>
            ) : (
              <>
                <button type="button" onClick={() => setAction('change')} className="ag-btn-secondary text-xs py-1.5 px-3">
                  <KeyRound className="w-3.5 h-3.5 text-indigo-400" />修改密码
                </button>
                <button type="button" onClick={() => setAction('remove')} className="ag-btn-ghost text-xs text-rose-300 hover:text-rose-200">
                  <Unlock className="w-3.5 h-3.5" />移除密码
                </button>
              </>
            )}
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="space-y-2.5 pt-1">
            {(action === 'change' || action === 'remove') && (
              <input
                type="password"
                autoFocus
                className="ag-input font-mono text-xs py-1.5"
                placeholder="输入当前加密密码"
                value={currentPwd}
                onChange={(e) => { setCurrentPwd(e.target.value); setError('') }}
              />
            )}
            {(action === 'set' || action === 'change') && (
              <div className="grid grid-cols-2 gap-2">
                <input
                  type="password"
                  autoFocus={action === 'set'}
                  className="ag-input font-mono text-xs py-1.5"
                  placeholder="输入新密码"
                  value={newPwd}
                  onChange={(e) => { setNewPwd(e.target.value); setError('') }}
                />
                <input
                  type="password"
                  className="ag-input font-mono text-xs py-1.5"
                  placeholder="再次确认新密码"
                  value={confirmPwd}
                  onChange={(e) => { setConfirmPwd(e.target.value); setError('') }}
                />
              </div>
            )}

            {error && (
              <div className="flex items-center gap-1.5 text-xs text-rose-300">
                <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                <span className="break-all">{error}</span>
              </div>
            )}

            <div className="flex items-center justify-end gap-2 pt-1">
              <button type="button" onClick={reset} disabled={busy} className="ag-btn-ghost text-xs py-1">
                取消
              </button>
              <button type="submit" disabled={busy} className="ag-btn-primary text-xs py-1.5 px-3">
                {busy && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                {action === 'remove' ? '确认移除密码' : action === 'change' ? '确认修改' : '确认设置密码'}
              </button>
            </div>
          </form>
        )}
      </div>
    </section>
  )
}
