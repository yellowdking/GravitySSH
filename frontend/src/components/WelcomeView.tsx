import React from 'react'
import { Bookmark, FolderTree, KeyRound, Plus, ShieldCheck, Sparkles, Terminal, Waypoints } from 'lucide-react'

interface Props {
  onAddServer: () => void
  onOpenKeys: () => void
  onOpenShortcuts: () => void
}

export const WelcomeView: React.FC<Props> = ({ onAddServer, onOpenKeys, onOpenShortcuts }) => (
  <div className="h-full overflow-y-auto flex items-center justify-center p-8 bg-ag-bg">
    <div className="max-w-2xl w-full space-y-6">
      <div className="text-center space-y-2.5">
        <div className="w-14 h-14 mx-auto rounded-2xl bg-gradient-to-br from-indigo-500/25 to-violet-500/25 border border-indigo-400/35 flex items-center justify-center shadow-glow">
          <Terminal className="w-7 h-7 text-indigo-300" />
        </div>
        <h1 className="text-2xl font-semibold tracking-tight text-ag-text">欢迎使用 GravitySSH</h1>
        <p className="text-sm text-ag-muted">Antigravity 风格 Windows 现代化 SSH 客户端 · 便携加密 · 高效运维</p>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="rounded-xl bg-ag-panel border border-ag-border p-4 space-y-1.5">
          <div className="flex items-center gap-2 text-xs font-semibold text-indigo-300"><Waypoints className="w-4 h-4" />代理 & 多级跳板机</div>
          <p className="text-xs text-ag-muted leading-relaxed">内置 SOCKS5 / HTTP 代理与跳板机隧道，轻松稳定直达弱网或内网服务器。</p>
        </div>
        <div className="rounded-xl bg-ag-panel border border-ag-border p-4 space-y-1.5">
          <div className="flex items-center gap-2 text-xs font-semibold text-sky-300"><FolderTree className="w-4 h-4" />连接即通 SFTP 文件管理</div>
          <p className="text-xs text-ag-muted leading-relaxed">SSH 连上自动挂载 SFTP，支持文件大小显示、一键对齐权限、双击智能解压与拖入上传。</p>
        </div>
        <div className="rounded-xl bg-ag-panel border border-ag-border p-4 space-y-1.5">
          <div className="flex items-center gap-2 text-xs font-semibold text-violet-300"><Sparkles className="w-4 h-4" />命令记录 · 快捷方式 · 智能补齐</div>
          <p className="text-xs text-ag-muted leading-relaxed">支持多行快捷命令脚本与目录/文件名感知补齐，常用运维操作一键触达。</p>
        </div>
        <div className="rounded-xl bg-ag-panel border border-ag-border p-4 space-y-1.5">
          <div className="flex items-center gap-2 text-xs font-semibold text-emerald-300"><ShieldCheck className="w-4 h-4" />便携加密配置</div>
          <p className="text-xs text-ag-muted leading-relaxed">同目录加密存储，平滑升级与可选密码保护，主机配置与私钥安全可靠。</p>
        </div>
      </div>

      <div className="flex items-center justify-center gap-3 pt-1">
        <button onClick={onAddServer} className="ag-btn-primary px-5 py-2.5"><Plus className="w-4 h-4" />添加服务器</button>
        <button onClick={onOpenKeys} className="ag-btn-secondary px-4 py-2.5"><KeyRound className="w-4 h-4 text-indigo-400" />管理 SSH 密钥库</button>
        <button onClick={onOpenShortcuts} className="ag-btn-secondary px-4 py-2.5"><Bookmark className="w-4 h-4 text-violet-400" />查看快捷命令</button>
      </div>
    </div>
  </div>
)
