import React, { useState } from 'react'
import { AlertCircle, Eye, EyeOff, FolderLock, Loader2, Lock, Terminal } from 'lucide-react'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'

interface VaultLockScreenProps {
  vaultPath: string
  onUnlocked: () => void
}

export const VaultLockScreen: React.FC<VaultLockScreenProps> = ({ vaultPath, onUnlocked }) => {
  const [password, setPassword] = useState('')
  const [showPwd, setShowPwd] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const handleUnlock = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!password) {
      setError('请输入解锁密码')
      return
    }
    setBusy(true)
    setError('')
    try {
      await api.unlockVault(password)
      setPassword('')
      onUnlocked()
    } catch (err) {
      setError(errMsg(err) || '密码错误，无法解锁配置文件')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="w-screen h-screen bg-ag-bg text-ag-text flex items-center justify-center p-6 relative overflow-hidden">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_50%_20%,rgba(99,102,241,0.14),transparent_55%)]" />
      <div className="w-full max-w-sm rounded-2xl bg-ag-panel/95 border border-ag-borderStrong shadow-ag p-6 relative z-10 space-y-5">
        <div className="flex flex-col items-center text-center space-y-2.5">
          <div className="w-12 h-12 rounded-2xl bg-gradient-to-br from-indigo-500/25 to-violet-500/25 border border-indigo-400/35 flex items-center justify-center shadow-glow">
            <Terminal className="w-6 h-6 text-indigo-300" />
          </div>
          <div>
            <h1 className="text-base font-semibold text-ag-text tracking-tight">GravitySSH 已锁定</h1>
            <p className="text-xs text-ag-muted mt-1">请输入自定义加密密码以解锁本地配置</p>
          </div>
        </div>

        <form onSubmit={handleUnlock} className="space-y-3.5">
          <div className="relative">
            <Lock className="w-4 h-4 text-ag-subtle absolute left-3 top-1/2 -translate-y-1/2" />
            <input
              type={showPwd ? 'text' : 'password'}
              autoFocus
              disabled={busy}
              className="ag-input pl-9 pr-9 font-mono"
              placeholder="输入加密密码"
              value={password}
              onChange={(e) => {
                setPassword(e.target.value)
                if (error) setError('')
              }}
            />
            <button
              type="button"
              tabIndex={-1}
              onClick={() => setShowPwd((v) => !v)}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 p-1 text-ag-subtle hover:text-ag-text transition"
              title={showPwd ? '隐藏密码' : '显示密码'}
            >
              {showPwd ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
            </button>
          </div>

          {error && (
            <div className="flex items-start gap-2 rounded-xl bg-rose-500/10 border border-rose-500/30 px-3 py-2 text-xs text-rose-300">
              <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
              <span className="break-all">{error}</span>
            </div>
          )}

          <button type="submit" disabled={busy || !password} className="ag-btn-primary w-full py-2.5">
            {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Lock className="w-4 h-4" />}
            {busy ? '正在解锁...' : '解锁进入'}
          </button>
        </form>

        {vaultPath && (
          <div
            className="pt-3 border-t border-ag-border/80 flex items-center gap-2 text-[11px] text-ag-subtle font-mono"
            title={vaultPath}
          >
            <FolderLock className="w-3.5 h-3.5 shrink-0 text-ag-muted" />
            <span className="truncate">{vaultPath}</span>
          </div>
        )}
      </div>
    </div>
  )
}
