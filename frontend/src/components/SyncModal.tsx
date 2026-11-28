import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowDownToLine, ArrowUpFromLine, Link2, RefreshCw, X } from 'lucide-react'
import type { SyncInboxItem, SyncOverview, SyncRelayStatus } from '../syncTypes'
import { errMsg } from '../utils/errors'
import { api } from '../wailsApi'
import { ModalShell } from './ModalShell'
import { SyncPairedTab } from './SyncPairedTab'
import { SyncReceiveTab } from './SyncReceiveTab'
import { SyncRelayBar } from './SyncRelayBar'
import { SyncSendTab } from './SyncSendTab'

interface SyncModalProps {
  open: boolean
  onClose: () => void
  onSynced: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

type SyncTabKey = 'paired' | 'send' | 'receive'

export const SyncModal: React.FC<SyncModalProps> = ({ open, onClose, onSynced, onToast }) => {
  const [tab, setTab] = useState<SyncTabKey>('send')
  const [overview, setOverview] = useState<SyncOverview | null>(null)
  const [inboxItems, setInboxItems] = useState<SyncInboxItem[]>([])
  const [loadingInbox, setLoadingInbox] = useState(false)
  const [sendBusy, setSendBusy] = useState(false)
  const [receiveBusy, setReceiveBusy] = useState(false)

  const mountedRef = useRef(true)
  const userPickedTabRef = useRef(false)
  const sendBusyRef = useRef(false)
  const receiveBusyRef = useRef(false)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const refreshInbox = useCallback(async () => {
    setLoadingInbox(true)
    try {
      const list = await api.listSyncInbox()
      if (mountedRef.current) setInboxItems(list || [])
    } catch {
      // Non-fatal when offline or relay unreachable on initial open
    } finally {
      if (mountedRef.current) setLoadingInbox(false)
    }
  }, [])

  const refreshOverview = useCallback(async () => {
    try {
      const ov = await api.getSyncOverview()
      if (!mountedRef.current || !ov) return
      setOverview(ov)
      if (!userPickedTabRef.current) {
        const hasPaired = Array.isArray(ov.pairedDevices) && ov.pairedDevices.length > 0
        setTab(hasPaired ? 'paired' : 'send')
      }
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    }
  }, [onToast])

  const refreshAll = useCallback(async () => {
    await Promise.all([refreshOverview(), refreshInbox()])
  }, [refreshOverview, refreshInbox])

  useEffect(() => {
    if (!open) {
      userPickedTabRef.current = false
      sendBusyRef.current = false
      receiveBusyRef.current = false
      setSendBusy(false)
      setReceiveBusy(false)
      return
    }

    void refreshAll()
    void api.startWatchSyncInbox().catch(() => {})
    const unsubInbox = api.onEvent<unknown>('sync:inbox_updated', () => {
      void refreshInbox()
    })

    return () => {
      unsubInbox()
      void api.stopWatchSyncInbox().catch(() => {})
    }
  }, [open, refreshAll, refreshInbox])

  const handleSelectTab = (next: SyncTabKey) => {
    userPickedTabRef.current = true
    setTab(next)
  }

  const handleRelayUpdated = useCallback(
    (nextRelay: SyncRelayStatus) => {
      setOverview((prev) => (prev ? { ...prev, relay: nextRelay } : prev))
      void refreshInbox()
    },
    [refreshInbox]
  )

  const handleDeviceLabelSaved = useCallback((label: string) => {
    setOverview((prev) => (prev ? { ...prev, localDeviceLabel: label } : prev))
  }, [])

  const handleSendBusyChange = useCallback((busy: boolean) => {
    sendBusyRef.current = busy
    setSendBusy(busy)
  }, [])

  const handleReceiveBusyChange = useCallback((busy: boolean) => {
    receiveBusyRef.current = busy
    setReceiveBusy(busy)
  }, [])

  const modalBusy = sendBusy || receiveBusy
  const handleClose = () => {
    if (sendBusyRef.current || receiveBusyRef.current) return
    onClose()
  }

  const pairedDevices = overview?.pairedDevices || []
  const unreadCount = inboxItems.length

  return (
    <ModalShell open={open} onClose={handleClose}>
      <div className="w-full max-w-2xl bg-ag-panel border border-ag-borderStrong rounded-2xl shadow-ag overflow-hidden flex flex-col max-h-[90vh]">
        <div className="px-5 py-3.5 border-b border-ag-border flex items-center justify-between bg-[#11151f]">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300">
              <RefreshCw className="w-4 h-4" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-ag-text">多端同步 · synctool-v1</h2>
              <p className="text-[11px] text-ag-muted">已配对设备一键零往返加密推送 · 离线收件箱 · 一次性扫码速传</p>
            </div>
          </div>
          <button
            type="button"
            disabled={modalBusy}
            onClick={handleClose}
            aria-label="关闭同步窗口"
            className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text disabled:opacity-40 disabled:cursor-not-allowed"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="px-5 pt-3.5 space-y-3">
          <SyncRelayBar
            relay={overview?.relay ?? null}
            localDeviceLabel={overview?.localDeviceLabel || ''}
            onRelayUpdated={handleRelayUpdated}
            onDeviceLabelSaved={handleDeviceLabelSaved}
            onToast={onToast}
          />

          <div className="grid grid-cols-3 gap-1.5 p-1 rounded-xl bg-[#0d111a] border border-ag-border">
            <button
              type="button"
              onClick={() => handleSelectTab('paired')}
              className={`py-2 px-2 rounded-lg text-xs font-medium flex items-center justify-center gap-1.5 transition ${
                tab === 'paired'
                  ? 'bg-indigo-500/20 text-indigo-200 border border-indigo-500/35'
                  : 'text-ag-muted hover:text-ag-text'
              }`}
            >
              <Link2 className="w-3.5 h-3.5 shrink-0" />
              <span>已配对设备</span>
              {unreadCount > 0 ? (
                <span className="px-1.5 py-0.5 rounded-full text-[10px] font-bold bg-emerald-500 text-[#081018] leading-none">
                  {unreadCount}
                </span>
              ) : pairedDevices.length > 0 ? (
                <span className="px-1.5 py-0.5 rounded-full text-[10px] bg-indigo-500/25 text-indigo-200 leading-none">
                  {pairedDevices.length}
                </span>
              ) : null}
            </button>

            <button
              type="button"
              onClick={() => handleSelectTab('send')}
              className={`py-2 px-2 rounded-lg text-xs font-medium flex items-center justify-center gap-1.5 transition ${
                tab === 'send'
                  ? 'bg-indigo-500/20 text-indigo-200 border border-indigo-500/35'
                  : 'text-ag-muted hover:text-ag-text'
              }`}
            >
              <ArrowUpFromLine className="w-3.5 h-3.5 shrink-0" />
              <span>一次性发送</span>
            </button>

            <button
              type="button"
              onClick={() => handleSelectTab('receive')}
              className={`py-2 px-2 rounded-lg text-xs font-medium flex items-center justify-center gap-1.5 transition ${
                tab === 'receive'
                  ? 'bg-indigo-500/20 text-indigo-200 border border-indigo-500/35'
                  : 'text-ag-muted hover:text-ag-text'
              }`}
            >
              <ArrowDownToLine className="w-3.5 h-3.5 shrink-0" />
              <span>一次性接收</span>
            </button>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-5">
          <div className={tab === 'paired' ? '' : 'hidden'}>
            <SyncPairedTab
              relay={overview?.relay ?? null}
              pairedDevices={pairedDevices}
              inboxItems={inboxItems}
              loadingInbox={loadingInbox}
              onRefreshAll={refreshAll}
              onRefreshInbox={refreshInbox}
              onSynced={onSynced}
              onToast={onToast}
            />
          </div>
          <div className={tab === 'send' ? '' : 'hidden'}>
            <SyncSendTab
              relay={overview?.relay ?? null}
              onBusyChange={handleSendBusyChange}
              onToast={onToast}
            />
          </div>
          <div className={tab === 'receive' ? '' : 'hidden'}>
            <SyncReceiveTab
              onBusyChange={handleReceiveBusyChange}
              onSynced={onSynced}
              onToast={onToast}
            />
          </div>
        </div>
      </div>
    </ModalShell>
  )
}
