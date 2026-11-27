interface PasteWarning {
  kind: 'danger' | 'pipe-shell' | 'hidden-chars' | 'control-chars' | 'homoglyph' | 'multiline'
  label: string
  detail: string
}

export interface PasteInspection {
  needsConfirm: boolean
  hasHiddenChars: boolean
  warnings: PasteWarning[]
}

const HIDDEN_CHARS_TEST_RE = /[\u200B-\u200F\u202A-\u202E\u2060-\u2069\uFEFF]/
const HIDDEN_CHARS_STRIP_RE = /[\u200B-\u200F\u202A-\u202E\u2060-\u2069\uFEFF]/g
// Raw ESC (\x1b), C1 CSI (\x9b), DEL (\x7f), and other dangerous C0/C1 control chars (excluding \t, \n, \r).
const CONTROL_CHARS_TEST_RE = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]/
const CONTROL_CHARS_STRIP_RE = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]/g

function hasDangerousRm(text: string): boolean {
  const segments = text.split(/(?:\r\n|\r|\n|&&|\|\||[;|()])/)
  for (const seg of segments) {
    const tokens = seg.trim().split(/\s+/).filter(Boolean)
    for (let i = 0; i < tokens.length; i++) {
      const cmd = tokens[i].replace(/^['"]|['"]$/g, '')
      if (cmd !== 'rm' && cmd !== '/bin/rm' && cmd !== '/usr/bin/rm') continue
      let hasR = false
      let hasF = false
      let hasDangerousTarget = false
      let endOfOpts = false
      for (let j = i + 1; j < tokens.length; j++) {
        const rawTok = tokens[j]
        const tok = rawTok.replace(/^['"]|['"]$/g, '')
        if (!endOfOpts && tok === '--') {
          endOfOpts = true
          continue
        }
        if (!endOfOpts && tok.startsWith('--')) {
          if (tok === '--recursive') hasR = true
          else if (tok === '--force') hasF = true
          continue
        }
        if (!endOfOpts && tok.startsWith('-') && tok.length > 1) {
          if (/[rR]/.test(tok)) hasR = true
          if (tok.includes('f')) hasF = true
          continue
        }
        if (/^\/+\*?$/.test(tok) || /^~\/?\*?$/.test(tok) || tok === '*') {
          hasDangerousTarget = true
        }
      }
      if (hasR && hasF && hasDangerousTarget) return true
    }
  }
  return false
}

const DANGEROUS_PATTERNS: { test: (s: string) => boolean; detail: string }[] = [
  { test: hasDangerousRm, detail: '包含高危递归强制删除 (rm -rf)' },
  { test: (s) => /\bmkfs(\.\w+)?\b/.test(s), detail: '包含磁盘格式化命令 (mkfs)' },
  { test: (s) => /\bdd\s+[^\n]*\bof=\/dev\//.test(s), detail: '包含直接覆写磁盘设备 (dd of=/dev/...)' },
  { test: (s) => /:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:/.test(s), detail: '包含 Fork 炸弹' },
  { test: (s) => /\bchmod\s+-R\s+777\s+\/(\s|$)/.test(s), detail: '包含根目录全权放开 (chmod -R 777 /)' },
  { test: (s) => />\s*\/dev\/(sd|nvme|vd|hd)[a-z]/.test(s), detail: '包含重定向覆写块设备' },
  { test: (s) => /\b(shutdown|poweroff|reboot|init\s+0)\b/.test(s), detail: '包含关机或重启系统命令' },
  { test: (s) => /\biptables\s+(-[A-Za-z]+\s+)*-F\b/.test(s), detail: '包含清空防火墙规则 (iptables -F)' },
]

const PIPE_SHELL_RE = /\b(curl|wget)\b[^\n|]*\|\s*(sudo\s+)?(sh|bash|zsh|ash)\b/i
const MIXED_LATIN_CYRILLIC_RE = /(?=[A-Za-z0-9_-]*[A-Za-z])(?=[A-Za-z0-9_\u0400-\u04FF-]*[\u0400-\u04FF])[A-Za-z0-9_\u0400-\u04FF-]+/

export function stripHiddenChars(text: string): string {
  return text.replace(HIDDEN_CHARS_STRIP_RE, '')
}

export function inspectPasteSafety(text: string, bracketedPasteActive: boolean): PasteInspection {
  const warnings: PasteWarning[] = []
  let hasHiddenChars = false

  if (!text) return { needsConfirm: false, hasHiddenChars: false, warnings }

  if (HIDDEN_CHARS_TEST_RE.test(text)) {
    hasHiddenChars = true
    warnings.push({
      kind: 'hidden-chars',
      label: '隐藏/双向控制字符',
      detail: '内容包含零宽空格或 BiDi 方向控制字符，可能隐藏真实执行的命令',
    })
  }

  if (CONTROL_CHARS_TEST_RE.test(text)) {
    warnings.push({
      kind: 'control-chars',
      label: '终端转义/控制字符',
      detail: '内容包含 ESC/C0/C1 控制字符，可能干扰终端状态或绕过粘贴保护',
    })
  }

  const strippedHidden = hasHiddenChars ? stripHiddenChars(text) : text
  const strippedAll = strippedHidden.replace(CONTROL_CHARS_STRIP_RE, '')
  const candidates = [text]
  if (strippedHidden !== text) candidates.push(strippedHidden)
  if (strippedAll !== strippedHidden && strippedAll !== text) candidates.push(strippedAll)

  if (candidates.some((c) => MIXED_LATIN_CYRILLIC_RE.test(c))) {
    warnings.push({
      kind: 'homoglyph',
      label: '可疑同形字混用',
      detail: '同一单词中混用了拉丁字母与西里尔字母（可能为伪装字符）',
    })
  }

  for (const p of DANGEROUS_PATTERNS) {
    if (candidates.some((c) => p.test(c))) {
      warnings.push({ kind: 'danger', label: '高危系统命令', detail: p.detail })
      break
    }
  }

  if (candidates.some((c) => PIPE_SHELL_RE.test(c))) {
    warnings.push({
      kind: 'pipe-shell',
      label: '远端脚本直接执行',
      detail: '检测到 curl/wget 管道直接交给 shell 执行，请确认来源可信',
    })
  }

  const normalized = text.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
  const nonEmptyLines = normalized.split('\n').filter((l) => l.trim().length > 0)
  if (!bracketedPasteActive && normalized.includes('\n')) {
    const isMulti = nonEmptyLines.length > 1
    warnings.push({
      kind: 'multiline',
      label: isMulti ? `多行立即执行 (${nonEmptyLines.length} 行)` : '包含换行符将立即执行',
      detail: isMulti
        ? '当前远端未开启括号粘贴，多行内容会立即逐行回车执行'
        : '当前远端未开启括号粘贴，粘贴内容包含换行符，发送后会立即回车执行',
    })
  }

  return {
    needsConfirm: warnings.length > 0,
    hasHiddenChars,
    warnings,
  }
}
