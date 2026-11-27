import React, { useEffect, useState } from 'react'
import { AlertCircle, CheckCircle2, Globe, KeyRound, Loader2, Server, Sliders, Waypoints, X } from 'lucide-react'
import type { ServerSaveRequest, ServerView, SSHKeyMeta } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { ModalShell } from './ModalShell'
import { makeInitialServerForm } from './serverForm'
import { ServerBasicTab } from './ServerBasicTab'
import { ServerProxyTab } from './ServerProxyTab'
import { ServerJumpTab } from './ServerJumpTab'
import { ServerAdvancedTab } from './ServerAdvancedTab'
import { ClearHostCacheButton } from './SshCacheSection'

interface Props {
  open: boolean
  editingServer: ServerView | null
  allServers: ServerView[]
  sshKeys: SSHKeyMeta[]
  defaultKeepAliveSeconds?: number
  onClose: () => void
  onSaved: (saved: ServerView) => void
  onGoToKeyVault: () => void
}

export const ServerModal: React.FC<Props> = ({
  open, editingServer, allServers, sshKeys, defaultKeepAliveSeconds = 30, onClose, onSaved, onGoToKeyVault,
}) => {
  const [tab, setTab] = useState<'basic' | 'proxy' | 'jump' | 'advanced'>('basic')
  const [form, setForm] = useState<ServerSaveRequest>(() => makeInitialServerForm(editingServer, defaultKeepAliveSeconds))
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null)

  useEffect(() => {
    if (!open) return
    setForm(makeInitialServerForm(editingServer, defaultKeepAliveSeconds))
    setTab('basic')
    setNotice(null)
  }, [open, editingServer, defaultKeepAliveSeconds])

  const jumpServers = allServers.filter((s) => s.id !== editingServer?.id)

  const isValidInt = (n: number, min: number, max: number) =>
    Number.isInteger(n) && n >= min && n <= max

  const validateForm = (): boolean => {
    if (!form.host.trim()) {
      setTab('basic')
      setNotice({ ok: false, text: '请填写主机地址 (IP / 域名)' })
      return false
    }
    if (!isValidInt(form.port, 1, 65535)) {
      setTab('basic')
      setNotice({ ok: false, text: 'SSH 端口必须为 1–65535 之间的整数' })
      return false
    }
    if (form.proxy.type !== 'none') {
      if (!form.proxy.host.trim()) {
        setTab('proxy')
        setNotice({ ok: false, text: '请填写代理服务器地址' })
        return false
      }
      if (!isValidInt(form.proxy.port, 1, 65535)) {
        setTab('proxy')
        setNotice({ ok: false, text: '代理端口必须为 1–65535 之间的整数' })
        return false
      }
    }
    if (form.jumpHost.mode === 'custom') {
      if (!form.jumpHost.host.trim()) {
        setTab('jump')
        setNotice({ ok: false, text: '请填写自定义跳板机地址' })
        return false
      }
      if (!isValidInt(form.jumpHost.port, 1, 65535)) {
        setTab('jump')
        setNotice({ ok: false, text: '跳板机端口必须为 1–65535 之间的整数' })
        return false
      }
    }
    if (!isValidInt(form.timeoutSeconds, 5, 120)) {
      setTab('advanced')
      setNotice({ ok: false, text: '连接超时必须为 5–120 秒之间的整数' })
      return false
    }
    if (!isValidInt(form.keepAliveSeconds, 5, 300)) {
      setTab('advanced')
      setNotice({ ok: false, text: '心跳保活间隔必须为 5–300 秒之间的整数' })
      return false
    }
    return true
  }

  const buildSanitizedPayload = (): ServerSaveRequest => ({
    ...form,
    password: form.authType === 'key' ? '' : form.password,
    keepExistingPassword: form.authType === 'key' ? false : form.keepExistingPassword,
    proxy: {
      ...form.proxy,
      port: isValidInt(form.proxy.port, 1, 65535) ? form.proxy.port : 7890,
      password: form.proxy.type === 'none' ? '' : form.proxy.password,
      keepExistingPassword: form.proxy.type === 'none' ? false : form.proxy.keepExistingPassword,
    },
    jumpHost: {
      ...form.jumpHost,
      port: isValidInt(form.jumpHost.port, 1, 65535) ? form.jumpHost.port : 22,
      password: form.jumpHost.mode !== 'custom' || form.jumpHost.authType === 'key' ? '' : form.jumpHost.password,
      keepExistingPassword:
        form.jumpHost.mode !== 'custom' || form.jumpHost.authType === 'key'
          ? false
          : form.jumpHost.keepExistingPassword,
    },
  })

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setNotice(null)
    if (!validateForm()) return
    setSaving(true)
    try {
      onSaved(await api.saveServer(buildSanitizedPayload()))
      onClose()
    } catch (err) {
      setNotice({ ok: false, text: errMsg(err) })
    } finally {
      setSaving(false)
    }
  }

  const handleTest = async () => {
    setNotice(null)
    if (!validateForm()) return
    setTesting(true)
    try {
      setNotice({ ok: true, text: await api.testServerConnection(buildSanitizedPayload()) })
    } catch (err) {
      setNotice({ ok: false, text: errMsg(err) })
    } finally {
      setTesting(false)
    }
  }

  const tabs = [
    { id: 'basic', label: '基本与认证', icon: <KeyRound className="w-3.5 h-3.5" /> },
    { id: 'proxy', label: `代理${form.proxy.type !== 'none' ? ` (${form.proxy.type.toUpperCase()})` : ''}`, icon: <Globe className="w-3.5 h-3.5" /> },
    { id: 'jump', label: `跳板机${form.jumpHost.mode !== 'none' ? ' (已启用)' : ''}`, icon: <Waypoints className="w-3.5 h-3.5" /> },
    { id: 'advanced', label: '高级选项', icon: <Sliders className="w-3.5 h-3.5" /> },
  ] as const

  return (
    <ModalShell open={open} onClose={onClose}>
      <div className="w-full max-w-2xl bg-ag-panel border border-ag-borderStrong rounded-2xl shadow-ag overflow-hidden flex flex-col max-h-[90vh]">
        <div className="px-5 py-4 border-b border-ag-border flex items-center justify-between bg-[#11151f]">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300"><Server className="w-4 h-4" /></div>
            <div>
              <h2 className="text-sm font-semibold text-ag-text">{editingServer ? `编辑服务器 · ${editingServer.name}` : '添加服务器连接'}</h2>
              <p className="text-[11px] text-ag-muted">密码 / SSH 密钥 / SOCKS5 & HTTP 代理 / 多级跳板机</p>
            </div>
          </div>
          <button type="button" onClick={onClose} aria-label="关闭服务器配置窗口" className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text"><X className="w-4 h-4" /></button>
        </div>

        <div className="px-5 pt-3 border-b border-ag-border flex items-center gap-1 bg-[#11151f]/60">
          {tabs.map((t) => (
            <button key={t.id} type="button" onClick={() => setTab(t.id)} className={`flex items-center gap-1.5 px-3 py-2 text-xs font-medium rounded-t-lg border-b-2 transition ${tab === t.id ? 'border-indigo-400 text-indigo-200 bg-ag-panel' : 'border-transparent text-ag-muted hover:text-ag-text'}`}>
              {t.icon}{t.label}
            </button>
          ))}
        </div>

        <form noValidate onSubmit={handleSave} className="flex-1 overflow-y-auto p-5 space-y-4">
          {tab === 'basic' && <ServerBasicTab form={form} editingServer={editingServer} sshKeys={sshKeys} onChange={setForm} onGoToKeyVault={() => { onClose(); onGoToKeyVault() }} />}
          {tab === 'proxy' && <ServerProxyTab form={form} editingServer={editingServer} onChange={setForm} />}
          {tab === 'jump' && <ServerJumpTab form={form} editingServer={editingServer} availableJumpServers={jumpServers} sshKeys={sshKeys} onChange={setForm} />}
          {tab === 'advanced' && <ServerAdvancedTab form={form} defaultKeepAliveSeconds={defaultKeepAliveSeconds} onChange={setForm} />}

          {notice && (
            <div className={`flex items-start gap-2 p-3 rounded-xl text-xs border ${notice.ok ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-300' : 'bg-rose-500/10 border-rose-500/30 text-rose-300'}`}>
              {notice.ok ? <CheckCircle2 className="w-4 h-4 shrink-0 mt-0.5" /> : <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />}
              <span className="break-all">{notice.text}</span>
            </div>
          )}

          <div className="pt-3 border-t border-ag-border flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <button type="button" onClick={() => void handleTest()} disabled={testing} className="ag-btn-secondary">
                {testing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />}测试连接
              </button>
              {(editingServer || form.host.trim()) && <ClearHostCacheButton host={form.host} port={isValidInt(form.port, 1, 65535) ? form.port : 22} onResult={setNotice} />}
            </div>
            <div className="flex items-center gap-2">
              <button type="button" onClick={onClose} className="ag-btn-ghost">取消</button>
              <button type="submit" disabled={saving} className="ag-btn-primary">{saving && <Loader2 className="w-3.5 h-3.5 animate-spin" />}保存服务器</button>
            </div>
          </div>
        </form>
      </div>
    </ModalShell>
  )
}
