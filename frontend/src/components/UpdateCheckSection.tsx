import React, { useState } from 'react'
import { ArrowUpCircle, CheckCircle2, ExternalLink, Loader2, RefreshCw } from 'lucide-react'
import type { UpdateCheckResult } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'

interface Props {
  appVersion: string
  updateInfo: UpdateCheckResult | null
  checkOnStartup: boolean
  onToggleCheckOnStartup: (v: boolean) => void
  onUpdateFound: (info: UpdateCheckResult) => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const UpdateCheckSection: React.FC<Props> = ({
  appVersion,
  updateInfo,
  checkOnStartup,
  onToggleCheckOnStartup,
  onUpdateFound,
  onToast,
}) => {
  const [checking, setChecking] = useState(false)

  const handleCheck = async () => {
    setChecking(true)
    try {
      const res = await api.checkForUpdates()
      onUpdateFound(res)
      if (res.hasUpdate) onToast(`发现新版本 v${res.latestVersion}`)
      else onToast(`当前 v${res.currentVersion} 已是最新版本`)
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      setChecking(false)
    }
  }

  return (
    <section className="space-y-2.5">
      <div className="flex items-center gap-2 text-xs font-semibold text-amber-300 uppercase tracking-wider">
        <ArrowUpCircle className="w-3.5 h-3.5" />版本更新 (GitHub Releases)
      </div>
      <div className="rounded-xl bg-[#0d1017] border border-ag-border divide-y divide-ag-border/70">
        <label className="flex items-center justify-between p-3 cursor-pointer">
          <span className="text-xs text-ag-text">启动时自动检查 GitHub 新版本</span>
          <input type="checkbox" checked={checkOnStartup} onChange={(e) => onToggleCheckOnStartup(e.target.checked)} className="w-4 h-4 accent-indigo-500" />
        </label>
        <div className="p-3 flex items-center justify-between gap-3">
          <div className="text-xs text-ag-muted min-w-0">
            {updateInfo?.hasUpdate ? (
              <span className="text-emerald-300 font-medium">发现新版本 v{updateInfo.latestVersion}（当前 v{appVersion}）</span>
            ) : updateInfo ? (
              <span className="inline-flex items-center gap-1 text-emerald-300"><CheckCircle2 className="w-3.5 h-3.5" />当前 v{appVersion} 已是最新</span>
            ) : (
              <span>当前版本：v{appVersion}</span>
            )}
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            {updateInfo?.hasUpdate && (
              <button
                type="button"
                onClick={() => void api.openExternalURL(updateInfo.downloadUrl || updateInfo.releaseUrl)}
                className="ag-btn-primary text-xs py-1 px-2.5"
              >
                <ExternalLink className="w-3 h-3" />前往下载
              </button>
            )}
            <button type="button" disabled={checking} onClick={() => void handleCheck()} className="ag-btn-secondary text-xs py-1 px-2.5">
              {checking ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
              {checking ? '检查中...' : '检查更新'}
            </button>
          </div>
        </div>
      </div>
    </section>
  )
}
