export interface PasteWarning {
  kind: 'danger' | 'pipe-shell' | 'hidden-chars' | 'homoglyph' | 'multiline'
  label: string
  detail: string
}

export interface PasteInspection {
  needsConfirm: boolean
  hasHiddenChars: boolean
  warnings: PasteWarning[]
}

const HIDDEN_CHARS_RE = /[\u200B-\u200F\u202A-\u202E\u2060-\u2069\uFEFF]/g

const DANGEROUS_PATTERNS: { re: RegExp; detail: string }[] = [
  { re: /\brm\s+-[^\n]*[rR][^\n]*f[^\n]*\s+(\/(\s|$|\*)|~\/?\s|\*)/, detail: '包含高危递归强制删除 (rm -rf)' },
  { re: /\bmkfs(\.\w+)?\b/, detail: '包含磁盘格式化命令 (mkfs)' },
  { re: /\bdd\s+[^\n]*\bof=\/dev\//, detail: '包含直接覆写磁盘设备 (dd of=/dev/...)' },
  { re: /:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:/, detail: '包含 Fork 炸弹' },
  { re: /\bchmod\s+-R\s+777\s+\/(\s|$)/, detail: '包含根目录全权放开 (chmod -R 777 /)' },
  { re: />\s*\/dev\/(sd|nvme|vd|hd)[a-z]/, detail: '包含重定向覆写块设备' },
  { re: /\b(shutdown|poweroff|reboot|init\s+0)\b/, detail: '包含关机或重启系统命令' },
  { re: /\biptables\s+(-[A-Za-z]+\s+)*-F\b/, detail: '包含清空防火墙规则 (iptables -F)' },
]

const PIPE_SHELL_RE = /\b(curl|wget)\b[^\n|]*\|\s*(sudo\s+)?(sh|bash|zsh|ash)\b/i
const MIXED_LATIN_CYRILLIC_RE = /(?=[A-Za-z0-9_-]*[A-Za-z])(?=[A-Za-z0-9_\u0400-\u04FF-]*[\u0400-\u04FF])[A-Za-z0-9_\u0400-\u04FF-]+/

export function stripHiddenChars(text: string): string {
  return text.replace(HIDDEN_CHARS_RE, '')
}

export function inspectPasteSafety(text: string, bracketedPasteActive: boolean): PasteInspection {
  const warnings: PasteWarning[] = []
  let hasHiddenChars = false

  if (!text) return { needsConfirm: false, hasHiddenChars: false, warnings }

  if (HIDDEN_CHARS_RE.test(text)) {
    HIDDEN_CHARS_RE.lastIndex = 0
    hasHiddenChars = true
    warnings.push({
      kind: 'hidden-chars',
      label: '隐藏/双向控制字符',
      detail: '内容包含零宽空格或 BiDi 方向控制字符，可能隐藏真实执行的命令',
    })
  }

  if (MIXED_LATIN_CYRILLIC_RE.test(text)) {
    warnings.push({
      kind: 'homoglyph',
      label: '可疑同形字混用',
      detail: '同一单词中混用了拉丁字母与西里尔字母（可能为伪装字符）',
    })
  }

  for (const p of DANGEROUS_PATTERNS) {
    if (p.re.test(text)) {
      warnings.push({ kind: 'danger', label: '高危系统命令', detail: p.detail })
      break
    }
  }

  if (PIPE_SHELL_RE.test(text)) {
    warnings.push({
      kind: 'pipe-shell',
      label: '远端脚本直接执行',
      detail: '检测到 curl/wget 管道直接交给 shell 执行，请确认来源可信',
    })
  }

  const lines = text.split('\n').filter((l) => l.trim().length > 0)
  if (!bracketedPasteActive && lines.length > 1) {
    warnings.push({
      kind: 'multiline',
      label: `多行立即执行 (${lines.length} 行)`,
      detail: '当前远端未开启括号粘贴，多行内容会立即逐行回车执行',
    })
  }

  return {
    needsConfirm: warnings.length > 0,
    hasHiddenChars,
    warnings,
  }
}
