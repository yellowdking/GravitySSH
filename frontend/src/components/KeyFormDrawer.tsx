import React from 'react'
import { FileKey2, KeyRound, Sparkles, X } from 'lucide-react'

interface Props {
  mode: 'import' | 'generate'
  name: string
  pemContent: string
  passphrase: string
  comment: string
  algorithm: 'ed25519' | 'rsa4096'
  busy: boolean
  onNameChange: (v: string) => void
  onPemChange: (v: string) => void
  onPassphraseChange: (v: string) => void
  onCommentChange: (v: string) => void
  onAlgorithmChange: (v: 'ed25519' | 'rsa4096') => void
  onPickFile: () => void
  onCancel: () => void
  onSubmitImport: (e: React.FormEvent) => void
  onSubmitGenerate: (e: React.FormEvent) => void
}

export const KeyFormDrawer: React.FC<Props> = ({
  mode,
  name,
  pemContent,
  passphrase,
  comment,
  algorithm,
  busy,
  onNameChange,
  onPemChange,
  onPassphraseChange,
  onCommentChange,
  onAlgorithmChange,
  onPickFile,
  onCancel,
  onSubmitImport,
  onSubmitGenerate,
}) => (
  <div className="rounded-2xl bg-ag-panel border border-indigo-500/35 p-5 shadow-ag">
    <div className="flex items-center justify-between mb-4">
      <h3 className="text-sm font-semibold text-ag-text flex items-center gap-2">
        {mode === 'import' ? <FileKey2 className="w-4 h-4 text-indigo-400" /> : <Sparkles className="w-4 h-4 text-indigo-400" />}
        {mode === 'import' ? '导入 SSH 私钥' : '自定义生成 SSH 密钥对'}
      </h3>
      <button type="button" disabled={busy} onClick={onCancel} aria-label="关闭密钥表单" className="p-1 rounded-lg hover:bg-ag-hover text-ag-muted disabled:opacity-40 disabled:cursor-not-allowed">
        <X className="w-4 h-4" />
      </button>
    </div>

    {mode === 'import' ? (
      <form onSubmit={onSubmitImport} className="space-y-3.5">
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-xs text-ag-muted mb-1">密钥名称 *</label>
            <input required disabled={busy} className="ag-input disabled:opacity-60" placeholder="例如：生产集群主密钥" value={name} onChange={(e) => onNameChange(e.target.value)} />
          </div>
          <div>
            <label className="block text-xs text-ag-muted mb-1">私钥口令 Passphrase（可选）</label>
            <input type="password" disabled={busy} className="ag-input font-mono disabled:opacity-60" placeholder="无口令请留空" value={passphrase} onChange={(e) => onPassphraseChange(e.target.value)} />
          </div>
        </div>
        <div>
          <div className="flex items-center justify-between mb-1">
            <label className="text-xs text-ag-muted">私钥内容 *</label>
            <button
              type="button"
              disabled={busy}
              onClick={onPickFile}
              title="在后端直接读取并加密保存，私钥明文不经过界面"
              className="text-[11px] text-sky-400 hover:underline disabled:opacity-40 disabled:cursor-not-allowed"
            >
              从文件直接导入...
            </button>
          </div>
          <textarea
            required
            disabled={busy}
            rows={6}
            className="ag-input font-mono text-xs leading-relaxed disabled:opacity-60"
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
            value={pemContent}
            onChange={(e) => onPemChange(e.target.value)}
          />
        </div>
        <div>
          <label className="block text-xs text-ag-muted mb-1">备注（可选）</label>
          <input disabled={busy} className="ag-input disabled:opacity-60" placeholder="用于哪些服务器或云厂商" value={comment} onChange={(e) => onCommentChange(e.target.value)} />
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <button type="button" disabled={busy} onClick={onCancel} className="ag-btn-ghost">取消</button>
          <button type="submit" disabled={busy} className="ag-btn-primary">
            <KeyRound className="w-3.5 h-3.5" />
            {busy ? '保存中...' : '保存私钥'}
          </button>
        </div>
      </form>
    ) : (
      <form onSubmit={onSubmitGenerate} className="space-y-3.5">
        <div className="grid grid-cols-3 gap-3">
          <div>
            <label className="block text-xs text-ag-muted mb-1">密钥名称（留空自动命名）</label>
            <input disabled={busy} className="ag-input disabled:opacity-60" placeholder="例如：my-ed25519" value={name} onChange={(e) => onNameChange(e.target.value)} />
          </div>
          <div>
            <label className="block text-xs text-ag-muted mb-1">密钥类型</label>
            <select disabled={busy} className="ag-input disabled:opacity-60" value={algorithm} onChange={(e) => onAlgorithmChange(e.target.value as 'ed25519' | 'rsa4096')}>
              <option value="ed25519">Ed25519</option>
              <option value="rsa4096">RSA-4096</option>
            </select>
          </div>
          <div>
            <label className="block text-xs text-ag-muted mb-1">保护口令（可选）</label>
            <input type="password" disabled={busy} className="ag-input font-mono disabled:opacity-60" placeholder="可留空" value={passphrase} onChange={(e) => onPassphraseChange(e.target.value)} />
          </div>
        </div>
        <div>
          <label className="block text-xs text-ag-muted mb-1">备注（可选）</label>
          <input disabled={busy} className="ag-input font-mono disabled:opacity-60" placeholder="user@gravityssh" value={comment} onChange={(e) => onCommentChange(e.target.value)} />
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <button type="button" disabled={busy} onClick={onCancel} className="ag-btn-ghost">取消</button>
          <button type="submit" disabled={busy} className="ag-btn-primary">
            <Sparkles className="w-3.5 h-3.5" />
            {busy ? '正在生成...' : '生成并保存'}
          </button>
        </div>
      </form>
    )}
  </div>
)
