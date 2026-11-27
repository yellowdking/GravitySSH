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
