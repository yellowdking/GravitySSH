import React from 'react'
import { Bookmark, KeyRound, Plus, Terminal } from 'lucide-react'

interface Props {
  onAddServer: () => void
  onOpenKeys: () => void
  onOpenShortcuts: () => void
}

export const WelcomeView: React.FC<Props> = ({ onAddServer, onOpenKeys, onOpenShortcuts }) => (
  <div className="h-full overflow-y-auto flex items-center justify-center p-8 bg-ag-bg">
    <div className="max-w-md w-full text-center space-y-6">
      <div className="space-y-2.5">
        <div className="w-14 h-14 mx-auto rounded-2xl bg-gradient-to-br from-indigo-500/25 to-violet-500/25 border border-indigo-400/35 flex items-center justify-center shadow-glow">
          <Terminal className="w-7 h-7 text-indigo-300" />
        </div>
        <h1 className="text-2xl font-semibold tracking-tight text-ag-text">GravitySSH</h1>
      </div>

      <div className="flex items-center justify-center gap-3">
        <button onClick={onAddServer} className="ag-btn-primary px-5 py-2.5"><Plus className="w-4 h-4" />添加主机</button>
        <button onClick={onOpenKeys} className="ag-btn-secondary px-4 py-2.5"><KeyRound className="w-4 h-4 text-indigo-400" />密钥</button>
        <button onClick={onOpenShortcuts} className="ag-btn-secondary px-4 py-2.5"><Bookmark className="w-4 h-4 text-violet-400" />快捷命令</button>
      </div>
    </div>
  </div>
)
