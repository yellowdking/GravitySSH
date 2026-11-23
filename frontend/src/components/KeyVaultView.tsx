import React, { useState } from 'react'
import { FolderOpen, KeyRound, Plus, ShieldCheck, Sparkles } from 'lucide-react'
import type { SSHKeyMeta } from '../types'
import { api } from '../wailsApi'
import { KeyFormDrawer } from './KeyFormDrawer'
import { KeyCardItem } from './KeyCardItem'

interface KeyVaultViewProps {
  keys: SSHKeyMeta[]
  onRefresh: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const KeyVaultView: React.FC<KeyVaultViewProps> = ({ keys, onRefresh, onToast }) => {
  const [mode, setMode] = useState<'none' | 'import' | 'generate'>('none')
  const [name, setName] = useState('')
  const [pemContent, setPemContent] = useState('')
  const [passphrase, setPassphrase] = useState('')
  const [comment, setComment] = useState('')
  const [algorithm, setAlgorithm] = useState<'ed25519' | 'rsa4096'>('ed25519')
  const [busy, setBusy] = useState(false)
  const [copiedId, setCopiedId] = useState<string | null>(null)

  const resetForm = () => { setName(''); setPemContent(''); setPassphrase(''); setComment(''); setMode('none') }

  const handlePickKeyFile = async () => {
    try {
      const res = await api.pickAndReadSSHKeyFile()
      if (res.cancelled === 'true') return
      setPemContent(res.content || '')
      if (!name && res.fileName) setName(res.fileName.replace(/\.(pem|key)$/i, ''))
      setMode('import')
    } catch (err) {
      onToast(String(err), 'error')
    }
  }

  const handleImportSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.importSSHKeyFromText(name, pemContent, passphrase, comment)
      onToast('SSH 私钥已通过 DPAPI 加密写入本地保险库')
      resetForm()
      onRefresh()
    } catch (err) {
      onToast(String(err), 'error')
    } finally {
      setBusy(false)
    }
  }

  const handleGenerateSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.generateSSHKey(name, algorithm, passphrase, comment)
      onToast('已生成全新 SSH 密钥对并加密保存')
      resetForm()
      onRefresh()
    } catch (err) {
      onToast(String(err), 'error')
    } finally {
      setBusy(false)
    }
  }

  const handleDelete = async (k: SSHKeyMeta) => {
    if (!window.confirm(`确定要从加密保险库中永久删除密钥 "${k.name}" 吗？`)) return
    try {
      await api.deleteSSHKey(k.id)
      onToast(`已删除密钥 ${k.name}`)
      onRefresh()
    } catch (err) {
      onToast(String(err), 'error')
    }
  }

  const copyPub = async (k: SSHKeyMeta) => {
    await navigator.clipboard.writeText(k.publicKey)
    setCopiedId(k.id)
    onToast('已复制公钥（可粘贴至服务器的 ~/.ssh/authorized_keys）')
    setTimeout(() => setCopiedId(null), 1800)
  }

  return (
    <div className="h-full overflow-y-auto p-6 bg-ag-bg">
      <div className="max-w-4xl mx-auto space-y-5">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-2.5">
            <div className="w-9 h-9 rounded-xl bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300"><KeyRound className="w-5 h-5" /></div>
            <div>
              <h1 className="text-lg font-semibold text-ag-text">SSH 密钥加密保险库</h1>
              <p className="text-xs text-ag-muted">集中加载、生成与管理 SSH 私钥，一键关联任意服务器或跳板机</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button onClick={handlePickKeyFile} className="ag-btn-secondary"><FolderOpen className="w-4 h-4 text-sky-400" />从文件加载私钥</button>
            <button onClick={() => setMode('import')} className="ag-btn-secondary"><Plus className="w-4 h-4" />粘贴导入</button>
            <button onClick={() => setMode('generate')} className="ag-btn-primary"><Sparkles className="w-4 h-4" />生成新密钥对</button>
          </div>
        </div>

        <div className="rounded-xl bg-emerald-500/8 border border-emerald-500/25 px-4 py-3 flex items-start gap-3">
          <ShieldCheck className="w-5 h-5 text-emerald-400 shrink-0 mt-0.5" />
          <div className="text-xs text-emerald-100/90 leading-relaxed">
            <span className="font-semibold text-emerald-300">防拷贝硬件绑定加密已开启：</span>
            所有 SSH 私钥及口令均通过 <span className="font-mono text-emerald-200">Windows DPAPI (MachineGuid + 当前账户 + 应用独立熵) + AES-256-GCM</span> 加密封装，拷贝文件到其他电脑或账户无法解密，且永不导出明文。
          </div>
        </div>

        {mode !== 'none' && (
          <KeyFormDrawer mode={mode} name={name} pemContent={pemContent} passphrase={passphrase} comment={comment} algorithm={algorithm} busy={busy} onNameChange={setName} onPemChange={setPemContent} onPassphraseChange={setPassphrase} onCommentChange={setComment} onAlgorithmChange={setAlgorithm} onPickFile={handlePickKeyFile} onCancel={resetForm} onSubmitImport={handleImportSubmit} onSubmitGenerate={handleGenerateSubmit} />
        )}

        {keys.length === 0 ? (
          <div className="rounded-2xl bg-ag-panel/70 border border-dashed border-ag-border py-14 text-center">
            <KeyRound className="w-8 h-8 text-ag-subtle mx-auto mb-3" />
            <p className="text-sm text-ag-muted mb-1">保险库中还没有保存任何 SSH 密钥</p>
            <p className="text-xs text-ag-subtle mb-4">点击上方按钮从本地加载现有私钥，或直接生成新的 Ed25519 密钥对</p>
            <button onClick={handlePickKeyFile} className="ag-btn-primary text-xs"><FolderOpen className="w-3.5 h-3.5" />加载本地 SSH 私钥文件</button>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3">
            {keys.map((k) => (
              <KeyCardItem key={k.id} item={k} copied={copiedId === k.id} onCopyPublic={(item) => void copyPub(item)} onDelete={(item) => void handleDelete(item)} />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
