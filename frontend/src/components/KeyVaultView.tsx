import React, { useCallback, useEffect, useRef, useState } from 'react'
import { FolderOpen, KeyRound, Loader2, Plus, SlidersHorizontal, Zap } from 'lucide-react'
import type { ServerView, SSHKeyMeta } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { useActionDialogs } from '../hooks/useActionDialogs'
import { KeyFormDrawer } from './KeyFormDrawer'
import { KeyCardItem } from './KeyCardItem'
import { KeyDeployModal } from './KeyDeployModal'

interface KeyVaultViewProps {
  keys: SSHKeyMeta[]
  servers: ServerView[]
  onRefresh: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const KeyVaultView: React.FC<KeyVaultViewProps> = ({ keys, servers, onRefresh, onToast }) => {
  const [mode, setMode] = useState<'none' | 'import' | 'generate'>('none')
  const [name, setName] = useState('')
  const [pemContent, setPemContent] = useState('')
  const [passphrase, setPassphrase] = useState('')
  const [comment, setComment] = useState('')
  const [algorithm, setAlgorithm] = useState<'ed25519' | 'rsa4096'>('ed25519')
  const [busy, setBusy] = useState(false)
  const [quickBusy, setQuickBusy] = useState(false)
  const [deployKey, setDeployKey] = useState<SSHKeyMeta | null>(null)
  const { confirm, dialogNode } = useActionDialogs()

  const mountedRef = useRef(true)
  const busyRef = useRef(false)
  const quickBusyRef = useRef(false)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  const clearFormFields = () => {
    setName('')
    setPemContent('')
    setPassphrase('')
    setComment('')
    setAlgorithm('ed25519')
  }

  const resetForm = (force = false) => {
    if (!force && busyRef.current) return
    clearFormFields()
    setMode('none')
  }

  const switchMode = (nextMode: 'import' | 'generate') => {
    if (busyRef.current) return
    if (mode !== nextMode) {
      clearFormFields()
      setMode(nextMode)
    }
  }

  const handleQuickGenerate = async () => {
    if (quickBusyRef.current) return
    quickBusyRef.current = true
    setQuickBusy(true)
    try {
      const created = await api.generateSSHKey('', 'ed25519', '', '')
      onRefresh()
      if (!mountedRef.current) return
      if (servers.length > 0) {
        onToast(`已生成密钥 "${created.name}"，请选择要部署的服务器`)
        setDeployKey(created)
      } else {
        onToast(`已一键生成密钥 "${created.name}"`)
      }
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    } finally {
      quickBusyRef.current = false
      if (mountedRef.current) setQuickBusy(false)
    }
  }

  const handlePickKeyFile = async () => {
    if (busyRef.current) return
    const hasNativeImport = typeof window.go?.main?.App?.ImportSSHKeyFromFile === 'function'
    if (hasNativeImport) {
      const fromImportDrawer = mode === 'import'
      const targetName = fromImportDrawer ? name.trim() : ''
      const targetPass = fromImportDrawer ? passphrase : ''
      const targetComment = fromImportDrawer ? comment.trim() : ''
      busyRef.current = true
      setBusy(true)
      try {
        const imported = await api.importSSHKeyFromFile(targetName, targetPass, targetComment)
        if (!imported) return
        onRefresh()
        if (!mountedRef.current) return
        onToast(`已导入 SSH 私钥 "${imported.name}"`)
        if (fromImportDrawer) resetForm(true)
      } catch (err) {
        if (!mountedRef.current) return
        const msg = errMsg(err)
        if (!fromImportDrawer && /口令|passphrase/i.test(msg)) {
          clearFormFields()
          setMode('import')
          onToast('该私钥受口令保护，请在下方填写 Passphrase 后点击「从文件直接导入」', 'error')
        } else {
          onToast(msg, 'error')
        }
      } finally {
        busyRef.current = false
        if (mountedRef.current) setBusy(false)
      }
      return
    }

    try {
      const res = await api.pickAndReadSSHKeyFile()
      if (!mountedRef.current || res.cancelled === 'true' || busyRef.current) return
      const wasImport = mode === 'import'
      setPassphrase('')
      setPemContent(res.content || '')
      if (!wasImport) {
        setComment('')
        setAlgorithm('ed25519')
        setName(res.fileName ? res.fileName.replace(/\.(pem|key)$/i, '') : '')
      } else if (!name && res.fileName) {
        setName(res.fileName.replace(/\.(pem|key)$/i, ''))
      }
      setMode('import')
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    }
  }

  const handleImportSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (busyRef.current) return
    busyRef.current = true
    setBusy(true)
    try {
      await api.importSSHKeyFromText(name, pemContent, passphrase, comment)
      onRefresh()
      if (!mountedRef.current) return
      onToast('SSH 私钥已保存')
      resetForm(true)
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    } finally {
      busyRef.current = false
      if (mountedRef.current) setBusy(false)
    }
  }

  const handleGenerateSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (busyRef.current) return
    busyRef.current = true
    setBusy(true)
    try {
      const created = await api.generateSSHKey(name, algorithm, passphrase, comment)
      onRefresh()
      if (!mountedRef.current) return
      onToast(`已生成密钥 "${created.name}"`)
      resetForm(true)
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    } finally {
      busyRef.current = false
      if (mountedRef.current) setBusy(false)
    }
  }

  const handleDelete = useCallback(async (k: SSHKeyMeta) => {
    const ok = await confirm({
      title: '删除 SSH 密钥',
      message: `确定要永久删除密钥 "${k.name}" 吗？`,
      confirmLabel: '删除',
    })
    if (!ok) return
    try {
      await api.deleteSSHKey(k.id)
      onToast(`已删除密钥 ${k.name}`)
      onRefresh()
    } catch (err) {
      onToast(errMsg(err), 'error')
    }
  }, [confirm, onRefresh, onToast])

  const handleSelectDeploy = useCallback((item: SSHKeyMeta) => setDeployKey(item), [])
  const handleDeleteCb = useCallback((item: SSHKeyMeta) => { void handleDelete(item) }, [handleDelete])

  return (
    <div className="h-full overflow-y-auto p-6 bg-ag-bg">
      <div className="max-w-4xl mx-auto space-y-5">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-2.5">
            <div className="w-9 h-9 rounded-xl bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300">
              <KeyRound className="w-5 h-5" />
            </div>
            <div>
              <h1 className="text-lg font-semibold text-ag-text">SSH 密钥库</h1>
              <p className="text-xs text-ag-muted">一键生成、导入密钥并快速部署到目标服务器</p>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <button onClick={() => void handleQuickGenerate()} disabled={quickBusy} className="ag-btn-primary">
              {quickBusy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Zap className="w-4 h-4" />}
              {quickBusy ? '生成中...' : '一键生成密钥'}
            </button>
            <button onClick={() => void handlePickKeyFile()} disabled={busy} className="ag-btn-secondary"><FolderOpen className="w-4 h-4 text-sky-400" />从文件加载私钥</button>
            <button onClick={() => switchMode('import')} disabled={busy} className="ag-btn-secondary"><Plus className="w-4 h-4" />粘贴导入</button>
            <button onClick={() => switchMode('generate')} disabled={busy} className="ag-btn-secondary"><SlidersHorizontal className="w-3.5 h-3.5 text-ag-muted" />自定义生成</button>
          </div>
        </div>

        {mode !== 'none' && (
          <KeyFormDrawer mode={mode} name={name} pemContent={pemContent} passphrase={passphrase} comment={comment} algorithm={algorithm} busy={busy} onNameChange={setName} onPemChange={setPemContent} onPassphraseChange={setPassphrase} onCommentChange={setComment} onAlgorithmChange={setAlgorithm} onPickFile={() => void handlePickKeyFile()} onCancel={() => resetForm()} onSubmitImport={handleImportSubmit} onSubmitGenerate={handleGenerateSubmit} />
        )}

        {keys.length === 0 ? (
          <div className="rounded-2xl bg-ag-panel/70 border border-dashed border-ag-border py-14 text-center">
            <KeyRound className="w-8 h-8 text-ag-subtle mx-auto mb-3" />
            <p className="text-sm text-ag-muted mb-1">还没有保存任何 SSH 密钥</p>
            <p className="text-xs text-ag-subtle mb-4">点击一键生成 Ed25519 密钥，或从本地文件导入现有私钥</p>
            <div className="flex items-center justify-center gap-2">
              <button onClick={() => void handleQuickGenerate()} disabled={quickBusy} className="ag-btn-primary text-xs"><Zap className="w-3.5 h-3.5" />一键生成密钥</button>
              <button onClick={() => void handlePickKeyFile()} disabled={busy} className="ag-btn-secondary text-xs"><FolderOpen className="w-3.5 h-3.5 text-sky-400" />从文件加载私钥</button>
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-2.5">
            {keys.map((k) => (
              <KeyCardItem key={k.id} item={k} onDeploy={handleSelectDeploy} onDelete={handleDeleteCb} onToast={onToast} />
            ))}
          </div>
        )}
      </div>

      {deployKey && (
        <KeyDeployModal
          key={deployKey.id}
          open
          sshKey={deployKey}
          servers={servers}
          onClose={() => setDeployKey(null)}
          onDeployed={onRefresh}
          onToast={onToast}
        />
      )}
      {dialogNode}
    </div>
  )
}
