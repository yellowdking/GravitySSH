import React, { useEffect, useRef, useState } from 'react'
import {
  AlertTriangle, Check, ChevronDown, ChevronRight, Copy, Edit3,
  Eye, EyeOff, Loader2, RotateCcw, Save, Server, ShieldCheck, Wifi,
} from 'lucide-react'
import type { SyncPingInfo, SyncRelayStatus } from '../syncTypes'
import { useCopyFeedback } from '../hooks/useCopyFeedback'
import { errMsg } from '../utils/errors'
import { certExpiryAlert, redactSyncUri } from '../utils/syncUri'
import { api } from '../wailsApi'

interface Props {
  relay: SyncRelayStatus | null
  localDeviceLabel: string
  onRelayUpdated: (next: SyncRelayStatus) => void
  onDeviceLabelSaved: (label: string) => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

const DOCKER_DEPLOY_CMD =
  'docker run -d --name synctool-relay --restart unless-stopped -p 54843:54843 -v synctool_data:/data -e SYNCTOOL_PUBLIC_HOST=<VPS_IP> -e SYNCTOOL_PROFILE=standard -e SYNCTOOL_SPOOL=disk bbplin/synctool:latest'

export const SyncRelayBar: React.FC<Props> = ({
  relay,
  localDeviceLabel,
  onRelayUpdated,
  onDeviceLabelSaved,
  onToast,
}) => {
  const [customUriInput, setCustomUriInput] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [showCustomForm, setShowCustomForm] = useState(false)
  const [showGuide, setShowGuide] = useState(false)
  const [savingRelay, setSavingRelay] = useState(false)
  const [testing, setTesting] = useState(false)
  const [pingInfo, setPingInfo] = useState<SyncPingInfo | null>(null)
  const [pingError, setPingError] = useState('')

  const [editingLabel, setEditingLabel] = useState(false)
  const [labelDraft, setLabelDraft] = useState(localDeviceLabel)
  const [savingLabel, setSavingLabel] = useState(false)

  const { copied, copy } = useCopyFeedback()
  const mountedRef = useRef(true)
  const testReqIdRef = useRef(0)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      testReqIdRef.current++
    }
  }, [])

  useEffect(() => {
    if (!editingLabel) setLabelDraft(localDeviceLabel)
  }, [localDeviceLabel, editingLabel])

  const isCustom = relay?.mode === 'custom'
  const certAlert = certExpiryAlert(
    pingInfo?.certExpiresAt || relay?.certExpiresAt,
    Boolean(pingInfo?.certRotateRecommended || relay?.certRotateRecommended)
  )

  const handleTest = async () => {
    const reqId = ++testReqIdRef.current
    setTesting(true)
    setPingError('')
    try {
      const info = await api.testSyncRelay(customUriInput.trim())
      if (!mountedRef.current || testReqIdRef.current !== reqId) return
      setPingInfo(info)
    } catch (err) {
      if (!mountedRef.current || testReqIdRef.current !== reqId) return
      setPingInfo(null)
      setPingError(redactSyncUri(errMsg(err)))
    } finally {
      if (mountedRef.current && testReqIdRef.current === reqId) setTesting(false)
    }
  }

  const handleSaveCustomRelay = async () => {
    const trimmed = customUriInput.trim()
    if (!trimmed) {
      onToast('请粘贴完整的 synctool://relay?...#k=... 链接', 'error')
      return
    }
    setSavingRelay(true)
    try {
      const updated = await api.setCustomSyncRelay(trimmed)
      if (!mountedRef.current) return
      // Wipe #k= from React input state immediately once persisted inside encrypted vault.gsv
      setCustomUriInput('')
      setShowSecret(false)
      setShowCustomForm(false)
      setPingInfo(null)
      setPingError('')
      onRelayUpdated(updated)
      onToast('自定义同步中继已加密保存')
    } catch (err) {
      if (mountedRef.current) onToast(redactSyncUri(errMsg(err)), 'error')
    } finally {
      if (mountedRef.current) setSavingRelay(false)
    }
  }

  const handleResetDefaultRelay = async () => {
    setSavingRelay(true)
    try {
      const updated = await api.setCustomSyncRelay('')
      if (!mountedRef.current) return
      setCustomUriInput('')
      setShowSecret(false)
      setShowCustomForm(false)
      setPingInfo(null)
      setPingError('')
      onRelayUpdated(updated)
      onToast('已恢复官方内置默认中继')
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    } finally {
      if (mountedRef.current) setSavingRelay(false)
    }
  }

  const handleSaveLabel = async (e: React.FormEvent) => {
    e.preventDefault()
    const next = labelDraft.trim()
    if (!next) {
      onToast('设备名称不能为空', 'error')
      return
    }
    setSavingLabel(true)
    try {
      await api.setSyncDeviceLabel(next)
      if (!mountedRef.current) return
      setEditingLabel(false)
      onDeviceLabelSaved(next)
      onToast('本机同步名称已更新')
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    } finally {
      if (mountedRef.current) setSavingLabel(false)
    }
  }

  const handleCopyDocker = async () => {
    const ok = await copy(DOCKER_DEPLOY_CMD)
    if (ok) onToast('synctool Docker 部署命令已复制')
    else onToast('复制失败，请手动复制', 'error')
  }

  const safeEndpointDisplay = isCustom
    ? relay?.endpoint || redactSyncUri(relay?.redactedUri || '') || '自定义中继'
    : `${relay?.endpoint || 'https://43.155.231.14:54843'} (TLS 1.3 + SPKI Pin)`

  return (
    <div className="rounded-xl bg-[#0d111a] border border-ag-border p-3 space-y-2.5">
      <div className="flex flex-wrap items-center justify-between gap-2 text-[11px]">
        <div className="flex items-center gap-1.5 min-w-0">
          <Server className="w-3.5 h-3.5 text-indigo-300 shrink-0" />
          <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium shrink-0 ${isCustom ? 'bg-amber-500/15 text-amber-300 border border-amber-500/30' : 'bg-indigo-500/15 text-indigo-300 border border-indigo-500/30'}`}>
            {isCustom ? '自定义中继' : '内置默认中继'}
          </span>
          <span className="font-mono text-ag-muted truncate" title={safeEndpointDisplay}>
            {safeEndpointDisplay}
          </span>
          {relay?.spkiPinShort && (
            <span className="hidden sm:inline-flex items-center gap-0.5 text-[10px] font-mono text-emerald-300/90 bg-emerald-500/10 border border-emerald-500/25 px-1.5 py-0.5 rounded shrink-0">
              <ShieldCheck className="w-3 h-3" />
              {relay.spkiPinShort}
            </span>
          )}
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          <button
            type="button"
            disabled={testing}
            onClick={() => void handleTest()}
            className="px-2 py-1 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-[11px] text-ag-text flex items-center gap-1 transition disabled:opacity-50"
          >
            {testing ? <Loader2 className="w-3 h-3 animate-spin" /> : <Wifi className="w-3 h-3 text-cyan-300" />}
            连接测试
          </button>
          <button
            type="button"
            onClick={() => setShowCustomForm((v) => !v)}
            className="px-2 py-1 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-[11px] text-ag-muted hover:text-ag-text transition"
          >
            {showCustomForm ? '收起配置' : isCustom ? '更换中继' : '自定义中继'}
          </button>
          {isCustom && (
            <button
              type="button"
              disabled={savingRelay}
              onClick={() => void handleResetDefaultRelay()}
              title="恢复内置默认中继"
              className="p-1 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-ag-muted hover:text-ag-text disabled:opacity-50"
            >
              <RotateCcw className="w-3.5 h-3.5" />
            </button>
          )}
        </div>
      </div>

      {/* Local device label bar */}
      <div className="flex items-center justify-between gap-2 text-[11px] pt-0.5 border-t border-ag-border/60">
        {editingLabel ? (
          <form onSubmit={(e) => void handleSaveLabel(e)} className="flex items-center gap-1.5 flex-1">
            <span className="text-ag-muted shrink-0">本机名称：</span>
            <input
              type="text"
              autoFocus
              maxLength={48}
              value={labelDraft}
              onChange={(e) => setLabelDraft(e.target.value)}
              className="flex-1 min-w-0 rounded-md bg-[#0b0d12] border border-indigo-500/50 px-2 py-1 text-xs text-ag-text focus:outline-none"
            />
            <button type="submit" disabled={savingLabel} className="px-2 py-1 rounded-md bg-indigo-500 hover:bg-indigo-400 text-white text-[11px]">
              保存
            </button>
            <button type="button" onClick={() => setEditingLabel(false)} className="px-2 py-1 rounded-md text-ag-muted hover:text-ag-text text-[11px]">
              取消
            </button>
          </form>
        ) : (
          <div className="flex items-center justify-between w-full gap-2">
            <span className="text-ag-muted truncate">
              本机标识：<strong className="text-ag-text font-medium">{localDeviceLabel || 'Windows PC'}</strong>
            </span>
            <button
              type="button"
              onClick={() => setEditingLabel(true)}
              className="inline-flex items-center gap-1 text-[10px] text-indigo-300 hover:text-indigo-200"
            >
              <Edit3 className="w-3 h-3" />
              修改名称
            </button>
          </div>
        )}
      </div>

      {/* Diagnostics result */}
      {pingInfo && (
        <div className="rounded-lg bg-emerald-500/10 border border-emerald-500/25 px-2.5 py-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-emerald-200 font-mono">
          <span>✓ RTT {pingInfo.rttMs}ms</span>
          <span>规格: {pingInfo.profile}</span>
          <span>暂存: {pingInfo.spoolMode}</span>
          <span>时钟偏差: {pingInfo.clockOffsetSec >= 0 ? `+${pingInfo.clockOffsetSec}` : pingInfo.clockOffsetSec}s</span>
        </div>
      )}
      {pingError && (
        <div className="rounded-lg bg-rose-500/10 border border-rose-500/30 px-2.5 py-1.5 text-[11px] text-rose-200 break-all">
          连接失败：{pingError}
        </div>
      )}

      {/* Cert rotation warning */}
      {certAlert.level !== 'none' && (
        <div
          className={`rounded-lg px-2.5 py-2 flex items-start gap-2 text-[11px] leading-relaxed border ${
            certAlert.level === 'urgent'
              ? 'bg-rose-500/15 border-rose-500/40 text-rose-200'
              : 'bg-amber-500/10 border-amber-500/35 text-amber-200'
          }`}
        >
          <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
          <span>
            中继 TLS 证书{certAlert.daysLeft !== null ? `剩余 ${certAlert.daysLeft} 天到期（${certAlert.formattedDate}）` : '建议轮换'}。
            自建节点请执行 <code className="font-mono">synctool-relay cert-rotate --reuse-key</code> 无缝续期。
          </span>
        </div>
      )}

      {/* Custom relay masked URI form */}
      {showCustomForm && (
        <div className="space-y-2 pt-1 border-t border-ag-border/70">
          {isCustom && relay?.redactedUri && (
            <div className="text-[10px] font-mono text-ag-muted truncate" title={redactSyncUri(relay.redactedUri)}>
              当前已保存（脱敏）：{redactSyncUri(relay.redactedUri)}
            </div>
          )}
          <div className="flex items-center gap-1.5">
            <div className="relative flex-1 min-w-0">
              <input
                type={showSecret ? 'text' : 'password'}
                autoComplete="off"
                spellCheck={false}
                value={customUriInput}
                onChange={(e) => {
                  testReqIdRef.current++
                  setTesting(false)
                  setCustomUriInput(e.target.value)
                  setPingInfo(null)
                  setPingError('')
                }}
                placeholder="粘贴 synctool://relay?v=1&u=https%3A%2F%2F...&pin=...#k=..."
                aria-label="自定义 synctool 中继 URI"
                className="w-full rounded-lg bg-[#0b0d12] border border-ag-border pl-2.5 pr-8 py-1.5 font-mono text-xs text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-indigo-500/60"
              />
              <button
                type="button"
                onClick={() => setShowSecret((v) => !v)}
                aria-label={showSecret ? '隐藏访问密钥' : '显示访问密钥'}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-ag-muted hover:text-ag-text"
              >
                {showSecret ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
              </button>
            </div>
            <button
              type="button"
              disabled={savingRelay || !customUriInput.trim()}
              onClick={() => void handleSaveCustomRelay()}
              className="px-2.5 py-1.5 rounded-lg bg-indigo-500 hover:bg-indigo-400 text-xs text-white font-medium flex items-center gap-1 shrink-0 disabled:opacity-50"
            >
              {savingRelay ? <Loader2 className="w-3 h-3 animate-spin" /> : <Save className="w-3 h-3" />}
              保存
            </button>
          </div>
          <p className="text-[10px] text-ag-muted">
            访问密钥 <code className="font-mono">#k=</code> 仅加密保存在本地保险库 <code className="font-mono">vault.gsv</code> 中，绝不明文落盘或回传前端。
          </p>
        </div>
      )}

      {/* Collapsible self-host Docker guide */}
      <div>
        <button
          type="button"
          onClick={() => setShowGuide((v) => !v)}
          className="flex items-center gap-1 text-[10px] text-ag-muted hover:text-ag-text transition"
        >
          {showGuide ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
          <span>自建私有中继一键 Docker 部署指南 (synctool)</span>
        </button>
        {showGuide && (
          <div className="mt-1.5 space-y-1.5 rounded-lg bg-[#090c12] border border-ag-border/80 p-2.5">
            <div className="flex items-center gap-1.5">
              <code className="flex-1 min-w-0 font-mono text-[10px] text-cyan-300 truncate select-all">
                {DOCKER_DEPLOY_CMD}
              </code>
              <button
                type="button"
                onClick={() => void handleCopyDocker()}
                title="复制部署命令"
                className="p-1 rounded hover:bg-ag-hover text-ag-muted hover:text-ag-text shrink-0"
              >
                {copied ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
              </button>
            </div>
            <p className="text-[10px] text-ag-muted leading-relaxed">
              启动后执行 <code className="font-mono text-indigo-300">docker logs synctool-relay</code> 获取输出的完整{' '}
              <code className="font-mono text-indigo-300">synctool://relay?...#k=...</code> 链接粘贴至上方即可。
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
