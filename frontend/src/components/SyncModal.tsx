import React, { useEffect, useState } from 'react'
import { ArrowDownToLine, ArrowUpFromLine, ChevronDown, ChevronRight, RefreshCw, RotateCcw, Server, X } from 'lucide-react'
import { api } from '../wailsApi'
import { SyncSendTab } from './SyncSendTab'
import { SyncReceiveTab } from './SyncReceiveTab'

interface SyncModalProps {
  open: boolean
  onClose: () => void
  onSynced: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

const FALLBACK_RELAY = 'https://107.172.98.169:54843'

export const SyncModal: React.FC<SyncModalProps> = ({ open, onClose, onSynced, onToast }) => {
  const [tab, setTab] = useState<'send' | 'receive'>('send')
  const [defaultRelay, setDefaultRelay] = useState(FALLBACK_RELAY)
  const [relayUrl, setRelayUrl] = useState(FALLBACK_RELAY)
  const [advancedOpen, setAdvancedOpen] = useState(false)

  useEffect(() => {
    if (!open) return
    void api.getDefaultSyncRelayUrl().then((url) => {
      if (url) {
        setDefaultRelay(url)
        setRelayUrl((prev) => (prev === FALLBACK_RELAY ? url : prev))
      }
    }).catch(() => {})
  }, [open])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4">
      <div className="w-full max-w-xl bg-ag-panel border border-ag-borderStrong rounded-2xl shadow-ag overflow-hidden flex flex-col max-h-[88vh]">
        <div className="px-5 py-4 border-b border-ag-border flex items-center justify-between bg-[#11151f]">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300">
              <RefreshCw className="w-4 h-4" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-ag-text">多设备零知识同步</h2>
              <p className="text-[11px] text-ag-muted">Windows ↔ Android 端到端加密配置互传</p>
            </div>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="px-5 pt-4">
          <div className="grid grid-cols-2 gap-1.5 p-1 rounded-xl bg-[#0d111a] border border-ag-border">
            <button
              onClick={() => setTab('send')}
              className={`py-2 rounded-lg text-xs font-medium flex items-center justify-center gap-1.5 transition ${tab === 'send' ? 'bg-indigo-500/20 text-indigo-200 border border-indigo-500/35' : 'text-ag-muted hover:text-ag-text'}`}
            >
              <ArrowUpFromLine className="w-3.5 h-3.5" />
              发送到其他设备
            </button>
            <button
              onClick={() => setTab('receive')}
              className={`py-2 rounded-lg text-xs font-medium flex items-center justify-center gap-1.5 transition ${tab === 'receive' ? 'bg-indigo-500/20 text-indigo-200 border border-indigo-500/35' : 'text-ag-muted hover:text-ag-text'}`}
            >
              <ArrowDownToLine className="w-3.5 h-3.5" />
              从其他设备接收
            </button>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          {tab === 'send' ? (
            <SyncSendTab relayUrl={relayUrl} onToast={onToast} />
          ) : (
            <SyncReceiveTab relayUrl={relayUrl} onSynced={onSynced} onToast={onToast} />
          )}

          <div className="border-t border-ag-border/80 pt-3">
            <button
              type="button"
              onClick={() => setAdvancedOpen((v) => !v)}
              className="flex items-center gap-1.5 text-[11px] text-ag-muted hover:text-ag-text transition"
            >
              {advancedOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
              <Server className="w-3 h-3" />
              <span>中继地址（高级）</span>
              {relayUrl !== defaultRelay && (
                <span className="ml-1 px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-300 text-[10px]">自定义</span>
              )}
            </button>
            {advancedOpen && (
              <div className="mt-2.5 flex items-center gap-2">
                <input
                  type="text"
                  value={relayUrl}
                  onChange={(e) => setRelayUrl(e.target.value)}
                  placeholder={defaultRelay}
                  className="flex-1 rounded-lg bg-[#0b0d12] border border-ag-border px-3 py-1.5 font-mono text-xs text-ag-text focus:outline-none focus:border-indigo-500/60"
                />
                <button
                  type="button"
                  onClick={() => setRelayUrl(defaultRelay)}
                  title="恢复默认中继地址"
                  className="px-2.5 py-1.5 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs text-ag-muted hover:text-ag-text flex items-center gap-1"
                >
                  <RotateCcw className="w-3 h-3" />
                  重置
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
