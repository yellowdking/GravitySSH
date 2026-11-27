import React, { useState } from 'react'
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

  const resetForm = () => { setName(''); setPemContent(''); setPassphrase(''); setComment(''); setMode('none') }

  const handleQuickGenerate = async () => {
    setQuickBusy(true)
    try {
      const created = await api.generateSSHKey('', 'ed25519', '', '')
      onRefresh()
      if (servers.length > 0) {
        onToast(`已生成密钥 "${created.name}"，请选择要部署的服务器`)
        setDeployKey(created)
      } else {
        onToast(`已一键生成密钥 "${created.name}"`)
      }
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      setQuickBusy(false)
    }
  }

  const handlePickKeyFile = async () => {
    try {
      const res = await api.pickAndReadSSHKeyFile()
      if (res.cancelled === 'true') return
      setPemContent(res.content || '')
      if (!name && res.fileName) setName(res.fileName.replace(/\.(pem|key)$/i, ''))
      setMode('import')
    } catch (err) {
      onToast(errMsg(err), 'error')
    }
  }

  const handleImportSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.importSSHKeyFromText(name, pemContent, passphrase, comment)
      onToast('SSH 私钥已保存')
      resetForm()
      onRefresh()
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      setBusy(false)
    }
  }

  const handleGenerateSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const created = await api.generateSSHKey(name, algorithm, passphrase, comment)
      onToast(`已生成密钥 "${created.name}"`)
      resetForm()
      onRefresh()
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      setBusy(false)
    }
  }

  const handleDelete = async (k: SSHKeyMeta) => {
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
  }

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
            <button onClick={handlePickKeyFile} className="ag-btn-secondary"><FolderOpen className="w-4 h-4 text-sky-400" />从文件加载私钥</button>
            <button onClick={() => setMode('import')} className="ag-btn-secondary"><Plus className="w-4 h-4" />粘贴导入</button>
            <button onClick={() => setMode('generate')} className="ag-btn-secondary"><SlidersHorizontal className="w-3.5 h-3.5 text-ag-muted" />自定义生成</button>
          </div>
        </div>

        {mode !== 'none' && (
          <KeyFormDrawer mode={mode} name={name} pemContent={pemContent} passphrase={passphrase} comment={comment} algorithm={algorithm} busy={busy} onNameChange={setName} onPemChange={setPemContent} onPassphraseChange={setPassphrase} onCommentChange={setComment} onAlgorithmChange={setAlgorithm} onPickFile={handlePickKeyFile} onCancel={resetForm} onSubmitImport={handleImportSubmit} onSubmitGenerate={handleGenerateSubmit} />
        )}

        {keys.length === 0 ? (
          <div className="rounded-2xl bg-ag-panel/70 border border-dashed border-ag-border py-14 text-center">
            <KeyRound className="w-8 h-8 text-ag-subtle mx-auto mb-3" />
            <p className="text-sm text-ag-muted mb-1">还没有保存任何 SSH 密钥</p>
            <p className="text-xs text-ag-subtle mb-4">点击一键生成 Ed25519 密钥，或从本地文件导入现有私钥</p>
            <div className="flex items-center justify-center gap-2">
              <button onClick={() => void handleQuickGenerate()} disabled={quickBusy} className="ag-btn-primary text-xs"><Zap className="w-3.5 h-3.5" />一键生成密钥</button>
              <button onClick={handlePickKeyFile} className="ag-btn-secondary text-xs"><FolderOpen className="w-3.5 h-3.5 text-sky-400" />从文件加载私钥</button>
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-2.5">
            {keys.map((k) => (
              <KeyCardItem key={k.id} item={k} onDeploy={(item) => setDeployKey(item)} onDelete={(item) => void handleDelete(item)} />
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
