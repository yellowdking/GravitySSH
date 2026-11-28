import { describe, expect, it } from 'vitest'
import { syncApi } from '../wailsSyncApi'
import {
  certExpiryAlert,
  formatPingSummary,
  formatRelaySummary,
  formatRoomCodeDisplay,
  formatSasDisplay,
  formatSyncCodeOrUriInput,
  isReadySyncCodeOrUri,
  parseSyncTimestampMs,
  redactSyncUri,
} from './syncUri'

describe('syncUri helpers', () => {
  it('redacts #k= and #s= fragments from synctool URIs', () => {
    const raw = 'synctool://relay?v=1&u=https%3A%2F%2F1.2.3.4%3A54843#k=SECRET_ACCESS_KEY_32_CHARS_MINIMUM'
    expect(redactSyncUri(raw)).toBe('synctool://relay?v=1&u=https%3A%2F%2F1.2.3.4%3A54843#[REDACTED]')
    expect(redactSyncUri(raw)).not.toContain('SECRET_ACCESS_KEY')
  })

  it('formats minimal relay and connectivity summaries without raw default IP or protocol fluff', () => {
    expect(formatRelaySummary(null)).toBe('官方默认中继')
    expect(
      formatRelaySummary({
        mode: 'default',
        endpoint: 'https://43.155.231.14:54843',
        redactedUri: 'synctool://relay?v=1#[REDACTED]',
      })
    ).toBe('官方默认中继')
    expect(
      formatRelaySummary({
        mode: 'custom',
        endpoint: 'https://relay.example.com:54843',
        redactedUri: 'synctool://relay?v=1&u=https%3A%2F%2Frelay.example.com%3A54843#[REDACTED]',
      })
    ).toBe('https://relay.example.com:54843')
    expect(
      formatRelaySummary({
        mode: 'custom',
        endpoint: '',
        redactedUri: 'synctool://relay?v=1&u=https%3A%2F%2F1.2.3.4%3A54843#k=TOP_SECRET',
      })
    ).toBe('synctool://relay?v=1&u=https%3A%2F%2F1.2.3.4%3A54843#[REDACTED]')
    expect(formatPingSummary(1748)).toBe('已连通 1748ms')
  })

  it('preserves synctool://pair URIs verbatim while normalizing 8-char Crockford Base32 room codes', () => {
    const uri = 'synctool://pair?v=1&r=01AB9XYZ&m=1&p=oneshot#s=CaseSensitive_b64u-Secret'
    expect(formatSyncCodeOrUriInput(`  ${uri}  `)).toBe(uri)
    expect(isReadySyncCodeOrUri(uri)).toBe(true)

    // Crockford Base32 maps O->0, I/L->1 and keeps 0/1 intact!
    expect(formatSyncCodeOrUriInput('o1il-9k7m')).toBe('0111-9K7M')
    expect(isReadySyncCodeOrUri('0111-9K7M')).toBe(true)
    expect(isReadySyncCodeOrUri('0111')).toBe(false)
  })

  it('formats SAS, room codes, timestamps, and cert rotation alerts accurately', () => {
    expect(formatSasDisplay('482910')).toBe('482-910')
    expect(formatRoomCodeDisplay('4K9M2P7X')).toBe('4K9M-2P7X')
    expect(parseSyncTimestampMs(1764300000)).toBe(1764300000 * 1000)

    const urgentIso = new Date(Date.now() + 3 * 86_400_000).toISOString()
    expect(certExpiryAlert(urgentIso, false).level).toBe('urgent')

    const warnIso = new Date(Date.now() + 20 * 86_400_000).toISOString()
    expect(certExpiryAlert(warnIso, false).level).toBe('warn')

    const healthyIso = new Date(Date.now() + 180 * 86_400_000).toISOString()
    expect(certExpiryAlert(healthyIso, false).level).toBe('none')
  })

  it('maps all 21 syncApi methods to exact locked Go Wails RPC names and arguments', async () => {
    const calls: Array<[string, unknown[]]> = []
    const appProxy = new Proxy<Record<string, (...args: unknown[]) => Promise<unknown>>>(
      {},
      {
        get: (_target, prop: string) => (...args: unknown[]) => {
          calls.push([prop, args])
          return Promise.resolve({})
        },
      }
    )
    ;(globalThis as unknown as { window: Window }).window = {
      go: { main: { App: appProxy } },
    } as unknown as Window

    await syncApi.getSyncOverview()
    await syncApi.setCustomSyncRelay('synctool://relay?v=1&u=https%3A%2F%2Fexample.com#k=abc')
    await syncApi.testSyncRelay('')
    await syncApi.setSyncDeviceLabel('Office-PC')
    await syncApi.listSyncInbox()
    await syncApi.startWatchSyncInbox()
    await syncApi.stopWatchSyncInbox()
    await syncApi.sendSyncToDevice('dev-1', 'auto')
    await syncApi.applyInboxSync('item-1', 'merge')
    await syncApi.dismissInboxItem('item-2')
    await syncApi.revokeSyncDevice('dev-1')
    await syncApi.startSyncDeviceBind(1, true)
    await syncApi.acceptSyncDeviceBind('01AB-9XYZ')
    await syncApi.startSyncOneShotSend(2, false)
    await syncApi.startSyncReceive('synctool://pair?v=1&r=01AB9XYZ&m=1&p=oneshot#s=secret')
    await syncApi.getSyncFlowStatus('sess-1')
    await syncApi.submitSyncSasInput('sess-1', '482910')
    await syncApi.confirmSyncSas('sess-1', true)
    await syncApi.cancelSyncFlow('sess-1')
    await syncApi.applySyncReceive('prev-1', 'overwrite')
    await syncApi.clearSyncPreview('prev-1')

    expect(calls).toEqual([
      ['GetSyncOverview', []],
      ['SetCustomSyncRelay', ['synctool://relay?v=1&u=https%3A%2F%2Fexample.com#k=abc']],
      ['TestSyncRelay', ['']],
      ['SetSyncDeviceLabel', ['Office-PC']],
      ['ListSyncInbox', []],
      ['StartWatchSyncInbox', []],
      ['StopWatchSyncInbox', []],
      ['SendSyncToDevice', ['dev-1', 'auto']],
      ['ApplyInboxSync', ['item-1', 'merge']],
      ['DismissInboxItem', ['item-2']],
      ['RevokeSyncDevice', ['dev-1']],
      ['StartSyncDeviceBind', [1, true]],
      ['AcceptSyncDeviceBind', ['01AB-9XYZ']],
      ['StartSyncOneShotSend', [2, false]],
      ['StartSyncReceive', ['synctool://pair?v=1&r=01AB9XYZ&m=1&p=oneshot#s=secret']],
      ['GetSyncFlowStatus', ['sess-1']],
      ['SubmitSyncSASInput', ['sess-1', '482910']],
      ['ConfirmSyncSAS', ['sess-1', true]],
      ['CancelSyncFlow', ['sess-1']],
      ['ApplySyncReceive', ['prev-1', 'overwrite']],
      ['ClearSyncPreview', ['prev-1']],
    ])
  })
})
