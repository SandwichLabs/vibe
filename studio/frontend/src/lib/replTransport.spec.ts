import { describe, it, expect, vi } from 'vitest'
import { createReplTransport } from './replTransport'

const tick = () => new Promise((r) => setTimeout(r, 0))

describe('createReplTransport', () => {
  it('sends input one call at a time, in order', async () => {
    const seen: string[] = []
    let inflight = 0
    let maxInflight = 0
    const t = createReplTransport({
      input: async (d) => { inflight++; maxInflight = Math.max(maxInflight, inflight); await tick(); seen.push(d); inflight-- },
      ack: async () => {},
    }, () => {}, 1)
    t.send('a'); t.send('b'); t.send('c')
    await new Promise((r) => setTimeout(r, 30))
    expect(seen.join('')).toBe('abc')
    expect(maxInflight).toBe(1)
  })

  it('retries when the session queue is full', async () => {
    let n = 0
    const seen: string[] = []
    const t = createReplTransport({
      input: async (d) => { if (n++ < 2) throw new Error('console input queue is full'); seen.push(d) },
      ack: async () => {},
    }, () => {}, 1)
    t.send('x')
    await new Promise((r) => setTimeout(r, 40))
    expect(seen).toEqual(['x'])
    expect(n).toBe(3)
  })

  it('reports other errors and drops the queue', async () => {
    const onError = vi.fn()
    const t = createReplTransport({ input: async () => { throw new Error('unknown console') }, ack: async () => {} }, onError, 1)
    t.send('x')
    await new Promise((r) => setTimeout(r, 10))
    expect(onError).toHaveBeenCalledWith('unknown console')
  })

  it('coalesces acknowledgements to the highest seq with one in flight', async () => {
    const acks: number[] = []
    let release: () => void = () => {}
    const gate = new Promise<void>((r) => { release = r })
    const t = createReplTransport({
      input: async () => {},
      ack: async (s) => { acks.push(s); if (acks.length === 1) await gate },
    }, () => {})
    t.acked(1)
    t.acked(2); t.acked(3); t.acked(2)
    await tick()
    expect(acks).toEqual([1])
    release()
    await new Promise((r) => setTimeout(r, 10))
    expect(acks).toEqual([1, 3])
  })

  it('stops sending after stop()', async () => {
    const input = vi.fn(async () => {})
    const t = createReplTransport({ input, ack: async () => {} }, () => {})
    t.stop()
    t.send('x')
    await tick()
    expect(input).not.toHaveBeenCalled()
  })

  it('sends Ctrl+C at once and drops the queued backlog', async () => {
    const seen: string[] = []
    let n = 0
    const t = createReplTransport({
      input: async (d) => { seen.push(d); if (d !== '\x03' && n++ === 0) throw new Error('console input queue is full') },
      ack: async () => {},
    }, () => {}, 50)
    t.send('select 1')
    t.send('more typing')
    await tick()
    t.send('\x03')
    await new Promise((r) => setTimeout(r, 120))
    expect(seen).toContain('\x03')
    expect(seen.indexOf('\x03')).toBeLessThan(3)
    // nothing typed before the Ctrl+C is sent after it
    expect(seen.slice(seen.indexOf('\x03') + 1)).toEqual([])
  })

  it('never splits a surrogate pair at the chunk boundary', async () => {
    const seen: string[] = []
    const t = createReplTransport({ input: async (d) => { seen.push(d) }, ack: async () => {} }, () => {}, 1)
    const text = 'a'.repeat(16 * 1024 - 1) + '😀' + 'b'
    t.send(text)
    await new Promise((r) => setTimeout(r, 20))
    expect(seen.join('')).toBe(text)
    for (const chunk of seen) {
      const first = chunk.charCodeAt(0)
      const last = chunk.charCodeAt(chunk.length - 1)
      expect(first >= 0xdc00 && first <= 0xdfff).toBe(false)
      expect(last >= 0xd800 && last <= 0xdbff).toBe(false)
    }
  })

  it('splits a big paste around Ctrl+C: the Ctrl+C goes alone, never too large', async () => {
    const seen: string[] = []
    const t = createReplTransport({
      input: async (d) => { if (d.length > 64 * 1024) throw new Error('console input exceeds the per-call limit'); seen.push(d) },
      ack: async () => {},
    }, () => {}, 1)
    t.send('x'.repeat(200_000) + '\x03' + 'tail')
    await new Promise((r) => setTimeout(r, 30))
    expect(seen[0]).toBe('\x03')
    expect(seen.slice(1).join('')).toBe('tail')
  })

  it('keys typed right after Ctrl+C go after it, in order', async () => {
    const seen: string[] = []
    let release: () => void = () => {}
    const gate = new Promise<void>((r) => { release = r })
    const t = createReplTransport({
      input: async (d) => { seen.push(d); if (d === '\x03') await gate },
      ack: async () => {},
    }, () => {}, 1)
    t.send('\x03' + 'sel')
    t.send('ect')
    await new Promise((r) => setTimeout(r, 15))
    expect(seen).toEqual(['\x03']) // nothing overtakes the Ctrl+C
    release()
    await new Promise((r) => setTimeout(r, 15))
    expect(seen.join('|')).toBe('\x03|select')
  })
})
