import type { ServerSaveRequest, ServerView } from '../types'

export const COLOR_PRESETS = ['#6366f1', '#38bdf8', '#22c55e', '#f59e0b', '#f43f5e', '#a855f7']

export function makeInitialServerForm(editing: ServerView | null, defaultKeepAliveSeconds = 30): ServerSaveRequest {
  const keepAliveDefault = defaultKeepAliveSeconds > 0 ? defaultKeepAliveSeconds : 30
  if (!editing) {
    return {
      id: '',
      name: '',
      group: '默认分组',
      color: '#6366f1',
      host: '',
      port: 22,
      username: 'root',
      authType: 'password',
      password: '',
      keepExistingPassword: false,
      sshKeyId: '',
      proxy: {
        type: 'none',
        host: '127.0.0.1',
        port: 7890,
        username: '',
        password: '',
        keepExistingPassword: false,
      },
      jumpHost: {
        mode: 'none',
        serverId: '',
        host: '',
        port: 22,
        username: 'root',
        authType: 'password',
        password: '',
        keepExistingPassword: false,
        sshKeyId: '',
        useMainProxy: true,
      },
      timeoutSeconds: 15,
      keepAliveSeconds: keepAliveDefault,
      defaultRemotePath: '',
      startupCommand: '',
      notes: '',
    }
  }
  const authType = editing.authType || 'password'
  const proxyType = editing.proxy?.type || 'none'
  const jumpMode = editing.jumpHost?.mode || 'none'
  const jumpAuthType = editing.jumpHost?.authType || 'password'
  return {
    id: editing.id,
    name: editing.name,
    group: editing.group || '默认分组',
    color: editing.color || '#6366f1',
    host: editing.host,
    port: editing.port ?? 22,
    username: editing.username || 'root',
    authType,
    password: '',
    keepExistingPassword: editing.hasPassword && authType !== 'key',
    sshKeyId: editing.sshKeyId || '',
    proxy: {
      type: proxyType,
      host: editing.proxy?.host || '127.0.0.1',
      port: editing.proxy?.port ?? 7890,
      username: editing.proxy?.username || '',
      password: '',
      keepExistingPassword: !!editing.proxy?.hasPassword && proxyType !== 'none',
    },
    jumpHost: {
      mode: jumpMode,
      serverId: editing.jumpHost?.serverId || '',
      host: editing.jumpHost?.host || '',
      port: editing.jumpHost?.port ?? 22,
      username: editing.jumpHost?.username || 'root',
      authType: jumpAuthType,
      password: '',
      keepExistingPassword: !!editing.jumpHost?.hasPassword && jumpMode === 'custom' && jumpAuthType !== 'key',
      sshKeyId: editing.jumpHost?.sshKeyId || '',
      useMainProxy: editing.jumpHost?.useMainProxy ?? true,
    },
    timeoutSeconds: editing.timeoutSeconds ?? 15,
    keepAliveSeconds: editing.keepAliveSeconds ?? keepAliveDefault,
    defaultRemotePath: editing.defaultRemotePath || '',
    startupCommand: editing.startupCommand || '',
    notes: editing.notes || '',
  }
}

export function wouldCreateJumpCycle(
  editingServerId: string | null | undefined,
  candidateId: string,
  allServers: ServerView[]
): boolean {
  if (!candidateId) return false
  if (editingServerId && candidateId === editingServerId) return true
  const byId = new Map<string, ServerView>()
  for (const s of allServers) byId.set(s.id, s)

  const visited = new Set<string>()
  if (editingServerId) visited.add(editingServerId)

  let cur: string | undefined = candidateId
  while (cur) {
    if (visited.has(cur)) return true
    visited.add(cur)
    const srv = byId.get(cur)
    if (!srv || srv.jumpHost?.mode !== 'server' || !srv.jumpHost.serverId) break
    cur = srv.jumpHost.serverId
  }
  return false
}

export function getAvailableJumpServers(
  allServers: ServerView[],
  editingServerId?: string | null
): ServerView[] {
  return allServers.filter((s) => !wouldCreateJumpCycle(editingServerId, s.id, allServers))
}

export function isServerConnectionDirty(
  form: ServerSaveRequest,
  editingServer: ServerView | null,
  defaultKeepAliveSeconds = 30
): boolean {
  if (!editingServer) return true
  const init = makeInitialServerForm(editingServer, defaultKeepAliveSeconds)
  if (
    form.host.trim() !== init.host.trim() ||
    form.port !== init.port ||
    form.username.trim() !== init.username.trim() ||
    form.password !== '' ||
    form.timeoutSeconds !== init.timeoutSeconds
  ) {
    return true
  }
  if (form.authType !== 'key' && init.keepExistingPassword && !form.keepExistingPassword) {
    return true
  }
  if (form.proxy.type !== init.proxy.type) return true
  if (form.proxy.type !== 'none') {
    if (
      form.proxy.host.trim() !== init.proxy.host.trim() ||
      form.proxy.port !== init.proxy.port ||
      form.proxy.username.trim() !== init.proxy.username.trim() ||
      form.proxy.password !== '' ||
      form.proxy.keepExistingPassword !== init.proxy.keepExistingPassword
    ) {
      return true
    }
  }
  if (form.jumpHost.mode !== init.jumpHost.mode) return true
  if (form.jumpHost.mode === 'server') {
    if (form.jumpHost.serverId !== init.jumpHost.serverId) return true
  } else if (form.jumpHost.mode === 'custom') {
    if (
      form.jumpHost.host.trim() !== init.jumpHost.host.trim() ||
      form.jumpHost.port !== init.jumpHost.port ||
      form.jumpHost.username.trim() !== init.jumpHost.username.trim() ||
      form.jumpHost.authType !== init.jumpHost.authType ||
      form.jumpHost.sshKeyId !== init.jumpHost.sshKeyId ||
      form.jumpHost.password !== '' ||
      form.jumpHost.keepExistingPassword !== init.jumpHost.keepExistingPassword ||
      form.jumpHost.useMainProxy !== init.jumpHost.useMainProxy
    ) {
      return true
    }
  }
  return false
}

export function isServerFormDirty(
  form: ServerSaveRequest,
  editingServer: ServerView | null,
  defaultKeepAliveSeconds = 30
): boolean {
  const init = makeInitialServerForm(editingServer, defaultKeepAliveSeconds)
  if (
    form.name !== init.name ||
    form.group !== init.group ||
    form.color !== init.color ||
    form.host !== init.host ||
    form.port !== init.port ||
    form.username !== init.username ||
    form.authType !== init.authType ||
    form.password !== init.password ||
    form.keepExistingPassword !== init.keepExistingPassword ||
    form.sshKeyId !== init.sshKeyId ||
    form.timeoutSeconds !== init.timeoutSeconds ||
    form.keepAliveSeconds !== init.keepAliveSeconds ||
    form.defaultRemotePath !== init.defaultRemotePath ||
    form.startupCommand !== init.startupCommand ||
    form.notes !== init.notes
  ) {
    return true
  }
  if (form.proxy.type !== init.proxy.type) return true
  if (form.proxy.type !== 'none') {
    if (
      form.proxy.host !== init.proxy.host ||
      form.proxy.port !== init.proxy.port ||
      form.proxy.username !== init.proxy.username ||
      form.proxy.password !== init.proxy.password ||
      form.proxy.keepExistingPassword !== init.proxy.keepExistingPassword
    ) {
      return true
    }
  }
  if (form.jumpHost.mode !== init.jumpHost.mode) return true
  if (form.jumpHost.mode === 'server') {
    if (form.jumpHost.serverId !== init.jumpHost.serverId) return true
  } else if (form.jumpHost.mode === 'custom') {
    if (
      form.jumpHost.host !== init.jumpHost.host ||
      form.jumpHost.port !== init.jumpHost.port ||
      form.jumpHost.username !== init.jumpHost.username ||
      form.jumpHost.authType !== init.jumpHost.authType ||
      form.jumpHost.password !== init.jumpHost.password ||
      form.jumpHost.keepExistingPassword !== init.jumpHost.keepExistingPassword ||
      form.jumpHost.sshKeyId !== init.jumpHost.sshKeyId ||
      form.jumpHost.useMainProxy !== init.jumpHost.useMainProxy
    ) {
      return true
    }
  }
  return false
}