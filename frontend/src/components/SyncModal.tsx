import React, { useEffect, useState } from 'react'
import { ArrowDownToLine, ArrowUpFromLine, RefreshCw, X } from 'lucide-react'
import type { AppSettings } from '../types'
import { api } from '../wailsApi'
import { ModalShell } from './ModalShell'
import { SyncRelayBar } from './SyncRelayBar'
import { SyncSendTab } from './SyncSendTab'
import { SyncReceiveTab } from './SyncReceiveTab'

interface SyncModalProps {
  open: boolean
  settings: AppSettings
  onSettingsSaved: (next: AppSettings) => void
  onClose: () => void
  onSynced: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

const FALLBACK_RELAY = 'https://107.172.98.169:54843'

export const SyncModal: React.FC<SyncModalProps> = ({
  open, settings, onSettingsSaved, onClose, onSynced, onToast,
}) => {
  const [tab, setTab] = useState<'send' | 'receive'>('send')
  const [defaultRelay, setDefaultRelay] = useState(FALLBACK_RELAY)
  const [relayInput, setRelayInput] = useState(settings.syncRelayUrl || '')

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setRelayInput(settings.syncRelayUrl || '')
    void api.getDefaultSyncRelayUrl().then((url) => {
      if (!cancelled && url) setDefaultRelay(url)
    }).catch(() => {})
    return () => { cancelled = true }
  }, [open, settings.syncRelayUrl])

  const activeRelay = relayInput.trim()

  return (
    <ModalShell open={open} onClose={onClose}>
      <div className="w-full max-w-xl bg-ag-panel border border-ag-borderStrong rounded-2xl shadow-ag overflow-hidden flex flex-col max-h-[88vh]">
        <div className="px-5 py-4 border-b border-ag-border flex items-center justify-between bg-[#11151f]">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300">
              <RefreshCw className="w-4 h-4" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-ag-text">多端同步</h2>
              <p className="text-[11px] text-ag-muted">端到端加密配置互传</p>
            </div>
          </div>
          <button type="button" onClick={onClose} aria-label="关闭同步窗口" className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="px-5 pt-4 space-y-3">
          <SyncRelayBar
            settings={settings}
            relayInput={relayInput}
            defaultRelay={defaultRelay}
            onRelayInputChange={setRelayInput}
            onSettingsSaved={onSettingsSaved}
            onToast={onToast}
          />

          <div className="grid grid-cols-2 gap-1.5 p-1 rounded-xl bg-[#0d111a] border border-ag-border">
            <button
              type="button"
              onClick={() => setTab('send')}
              className={`py-2 rounded-lg text-xs font-medium flex items-center justify-center gap-1.5 transition ${tab === 'send' ? 'bg-indigo-500/20 text-indigo-200 border border-indigo-500/35' : 'text-ag-muted hover:text-ag-text'}`}
            >
              <ArrowUpFromLine className="w-3.5 h-3.5" />
              发送到其他设备
            </button>
            <button
              type="button"
              onClick={() => setTab('receive')}
              className={`py-2 rounded-lg text-xs font-medium flex items-center justify-center gap-1.5 transition ${tab === 'receive' ? 'bg-indigo-500/20 text-indigo-200 border border-indigo-500/35' : 'text-ag-muted hover:text-ag-text'}`}
            >
              <ArrowDownToLine className="w-3.5 h-3.5" />
              从其他设备接收
            </button>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-5">
          <div className={tab === 'send' ? '' : 'hidden'}>
            <SyncSendTab relayUrl={activeRelay} onToast={onToast} />
          </div>
          <div className={tab === 'receive' ? '' : 'hidden'}>
            <SyncReceiveTab relayUrl={activeRelay} onSynced={onSynced} onToast={onToast} />
          </div>
        </div>
      </div>
    </ModalShell>
  )
}
