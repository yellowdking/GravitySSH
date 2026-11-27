import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { startSyncPollingLoop } from './useSyncPolling'

describe('startSyncPollingLoop / useSyncPolling core', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('polls serially after each async poll resolves without overlapping inFlight calls', async () => {
    let resolveFirst!: (cont: boolean) => void
    let calls = 0
    const poll = vi.fn(() => {
      calls++
      if (calls === 1) {
        return new Promise<boolean>((r) => {
          resolveFirst = r
        })
      }
      return Promise.resolve(true)
    })

    const stop = startSyncPollingLoop({
      intervalMs: 1000,
      poll,
      isHidden: () => false,
      subscribeVisibility: () => () => {},
    })

    await vi.advanceTimersByTimeAsync(999)
    expect(poll).toHaveBeenCalledTimes(0)

    await vi.advanceTimersByTimeAsync(1)
    expect(poll).toHaveBeenCalledTimes(1)

    // Even after 5s, while first poll is in-flight, no overlapping poll runs
    await vi.advanceTimersByTimeAsync(5000)
    expect(poll).toHaveBeenCalledTimes(1)

    resolveFirst(true)
    await Promise.resolve()
    await Promise.resolve()

    await vi.advanceTimersByTimeAsync(1000)
    expect(poll).toHaveBeenCalledTimes(2)

    stop()
  })

  it('stops permanently when poll returns false and does not revive on visibilitychange', async () => {
    let hidden = false
    const visListeners: Array<() => void> = []
    const emitVisibility = () => {
      for (const cb of [...visListeners]) cb()
    }
    const poll = vi.fn().mockResolvedValueOnce(true).mockResolvedValueOnce(false)

    const stop = startSyncPollingLoop({
      intervalMs: 500,
      poll,
      isHidden: () => hidden,
      subscribeVisibility: (cb) => {
        visListeners.push(cb)
        return () => {
          const idx = visListeners.indexOf(cb)
          if (idx !== -1) visListeners.splice(idx, 1)
        }
      },
    })

    await vi.advanceTimersByTimeAsync(500)
    expect(poll).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(500)
    expect(poll).toHaveBeenCalledTimes(2)

    // Stopped terminally — further time and visibility toggles must NOT revive it
    hidden = true
    emitVisibility()
    hidden = false
    emitVisibility()

    await vi.advanceTimersByTimeAsync(3000)
    expect(poll).toHaveBeenCalledTimes(2)

    stop()
  })

  it('pauses when hidden and resumes promptly when visible again while active', async () => {
    let hidden = false
    const visListeners: Array<() => void> = []
    const emitVisibility = () => {
      for (const cb of [...visListeners]) cb()
    }
    const poll = vi.fn().mockResolvedValue(true)

    const stop = startSyncPollingLoop({
      intervalMs: 1500,
      poll,
      isHidden: () => hidden,
      subscribeVisibility: (cb) => {
        visListeners.push(cb)
        return () => {
          const idx = visListeners.indexOf(cb)
          if (idx !== -1) visListeners.splice(idx, 1)
        }
      },
    })

    await vi.advanceTimersByTimeAsync(500)
    hidden = true
    emitVisibility()

    await vi.advanceTimersByTimeAsync(5000)
    expect(poll).toHaveBeenCalledTimes(0)

    hidden = false
    emitVisibility()
    await vi.advanceTimersByTimeAsync(150)
    expect(poll).toHaveBeenCalledTimes(1)

    stop()
  })

  it('continues polling after transient errors and stops cleanly when cancelled mid-flight', async () => {
    let resolveThird!: (cont: boolean) => void
    const poll = vi
      .fn<() => Promise<boolean>>()
      .mockRejectedValueOnce(new Error('network timeout'))
      .mockResolvedValueOnce(true)
      .mockImplementationOnce(
        () =>
          new Promise<boolean>((r) => {
            resolveThird = r
          })
      )

    let unsubscribed = false
    const stop = startSyncPollingLoop({
      intervalMs: 400,
      poll,
      isHidden: () => false,
      subscribeVisibility: () => () => {
        unsubscribed = true
      },
    })

    await vi.advanceTimersByTimeAsync(400)
    expect(poll).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(400)
    expect(poll).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(400)
    expect(poll).toHaveBeenCalledTimes(3)

    // Simulate resetKey change / unmount while 3rd poll is still in flight
    stop()
    expect(unsubscribed).toBe(true)

    resolveThird(true)
    await vi.advanceTimersByTimeAsync(2000)
    expect(poll).toHaveBeenCalledTimes(3)
  })
})
