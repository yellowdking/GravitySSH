import { describe, expect, it } from 'vitest'
import type { ServerView } from '../types'
import {
  getAvailableJumpServers,
  isServerConnectionDirty,
  makeInitialServerForm,
  wouldCreateJumpCycle,
} from './serverForm'

function makeServer(id: string, jumpServerId = ''): ServerView {
  return {
    id,
    name: `srv-${id}`,
    group: '默认分组',
    color: '#6366f1',
    host: `${id}.example.com`,
    port: 22,
    username: 'root',
    authType: 'password',
    hasPassword: true,
    sshKeyId: '',
    proxy: { type: 'none', host: '', port: 0, username: '', hasPassword: false },
    jumpHost: jumpServerId
      ? {
          mode: 'server',
          serverId: jumpServerId,
          host: '',
          port: 22,
          username: 'root',
          authType: 'password',
          hasPassword: false,
          sshKeyId: '',
          useMainProxy: true,
        }
      : {
          mode: 'none',
          serverId: '',
          host: '',
          port: 22,
          username: 'root',
          authType: 'password',
          hasPassword: false,
          sshKeyId: '',
          useMainProxy: true,
        },
    timeoutSeconds: 15,
    keepAliveSeconds: 30,
    defaultRemotePath: '',
    startupCommand: '',
    notes: '',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  }
}

describe('serverForm jump cycle detection', () => {
  it('filters self-reference and 2-hop / 3-hop jump cycles', () => {
    // Chain: A -> B -> C, plus independent D
    const a = makeServer('A', 'B')
    const b = makeServer('B', 'C')
    const c = makeServer('C')
    const d = makeServer('D')
    const all = [a, b, c, d]

    // Editing C: choosing A, B, or C creates a cycle; only D is safe
    expect(wouldCreateJumpCycle('C', 'C', all)).toBe(true)
    expect(wouldCreateJumpCycle('C', 'B', all)).toBe(true)
    expect(wouldCreateJumpCycle('C', 'A', all)).toBe(true)
    expect(wouldCreateJumpCycle('C', 'D', all)).toBe(false)
    expect(getAvailableJumpServers(all, 'C').map((s) => s.id)).toEqual(['D'])

    // Editing B: choosing A or B creates a cycle; C and D are safe
    expect(getAvailableJumpServers(all, 'B').map((s) => s.id)).toEqual(['C', 'D'])

    // Editing A: only A itself is excluded
    expect(getAvailableJumpServers(all, 'A').map((s) => s.id)).toEqual(['B', 'C', 'D'])
  })

  it('rejects candidates that already participate in an existing loop', () => {
    const x = makeServer('X', 'Y')
    const y = makeServer('Y', 'X')
    const z = makeServer('Z')
    const all = [x, y, z]

    expect(wouldCreateJumpCycle(null, 'X', all)).toBe(true)
    expect(wouldCreateJumpCycle('Z', 'Y', all)).toBe(true)
    expect(getAvailableJumpServers(all, null).map((s) => s.id)).toEqual(['Z'])
  })
})

describe('isServerConnectionDirty', () => {
  it('stays clean when only switching authType/sshKeyId for quick-deploy or editing metadata', () => {
    const srv = makeServer('A')
    const form = makeInitialServerForm(srv, 30)
    expect(isServerConnectionDirty(form, srv, 30)).toBe(false)

    const withKey = {
      ...form,
      name: 'Renamed',
      authType: 'key' as const,
      sshKeyId: 'key-1',
      password: '',
      keepExistingPassword: false,
    }
    expect(isServerConnectionDirty(withKey, srv, 30)).toBe(false)
  })

  it('detects dirty host, port, username, password, proxy, and jumpHost changes', () => {
    const srv = makeServer('A')
    const form = makeInitialServerForm(srv, 30)

    expect(isServerConnectionDirty({ ...form, host: '10.0.0.2' }, srv, 30)).toBe(true)
    expect(isServerConnectionDirty({ ...form, port: 2222 }, srv, 30)).toBe(true)
    expect(isServerConnectionDirty({ ...form, username: 'ubuntu' }, srv, 30)).toBe(true)
    expect(isServerConnectionDirty({ ...form, password: 'newpw' }, srv, 30)).toBe(true)
    expect(isServerConnectionDirty({ ...form, keepExistingPassword: false }, srv, 30)).toBe(true)
    expect(
      isServerConnectionDirty(
        { ...form, proxy: { ...form.proxy, type: 'socks5', host: '127.0.0.1', port: 7890 } },
        srv,
        30
      )
    ).toBe(true)
    expect(
      isServerConnectionDirty(
        { ...form, jumpHost: { ...form.jumpHost, mode: 'server', serverId: 'B' } },
        srv,
        30
      )
    ).toBe(true)
  })
})
