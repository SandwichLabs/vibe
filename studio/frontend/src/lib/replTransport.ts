// The console's two return paths to Go, kept in order and bounded: typed bytes
// (one ReplInput in flight, so order holds, retried when the session's queue is
// full) and frame acknowledgements (coalesced to the highest seq, one in flight).

export interface ReplApi {
  input(data: string): Promise<void>
  ack(seq: number): Promise<void>
}

export interface ReplTransport {
  send(data: string): void
  acked(seq: number): void
  stop(): void
}

const FULL = 'queue is full'

// chunkEnd is where the next chunk of s ends: at most max code units, never
// between the halves of a surrogate pair.
function chunkEnd(s: string, max: number): number {
  if (s.length <= max) return s.length
  const last = s.charCodeAt(max - 1)
  return last >= 0xd800 && last <= 0xdbff ? max - 1 : max
}

export function createReplTransport(
  api: ReplApi,
  onError: (message: string) => void,
  retryMs = 20,
): ReplTransport {
  let queue = ''
  let epoch = 0 // bumps on Ctrl+C: what was queued is gone
  let barrier: Promise<void> = Promise.resolve() // the last Ctrl+C: later input waits for it
  let sending = false
  let stopped = false
  let ackPending = 0
  let ackBusy = false

  const message = (e: unknown): string => (e instanceof Error ? e.message : String(e))

  async function pump(): Promise<void> {
    if (sending) return
    sending = true
    while (queue && !stopped) {
      await barrier // keys typed right after a Ctrl+C go after it, never before
      if (!queue || stopped) break
      const chunk = queue.slice(0, chunkEnd(queue, 16 * 1024))
      const mine = epoch
      try {
        await api.input(chunk)
        if (mine === epoch) queue = queue.slice(chunk.length)
      } catch (e) {
        if (message(e).includes(FULL)) {
          await new Promise((r) => setTimeout(r, retryMs))
          continue
        }
        if (mine === epoch) queue = ''
        onError(message(e))
      }
    }
    sending = false
  }

  async function flushAck(): Promise<void> {
    if (ackBusy) return
    ackBusy = true
    while (ackPending > 0 && !stopped) {
      const seq = ackPending
      ackPending = 0
      try {
        await api.ack(seq)
      } catch (e) {
        onError(message(e))
      }
    }
    ackBusy = false
  }

  return {
    send(data) {
      if (stopped || !data) return
      const at = data.lastIndexOf('\x03')
      if (at >= 0) {
        // Ctrl+C jumps the queue: the backlog, and anything pasted before the
        // Ctrl+C, is dropped (Go discards what was typed ahead as well) and the
        // Ctrl+C goes now, alone (so a big paste can never make it "too large"),
        // not behind a retry loop. What follows it is ordinary input.
        epoch++
        queue = ''
        barrier = api.input('\x03').then(() => {}, (e) => onError(message(e)))
        data = data.slice(at + 1)
        if (!data) return
      }
      queue += data
      void pump()
    },
    acked(seq) {
      if (stopped) return
      ackPending = Math.max(ackPending, seq)
      void flushAck()
    },
    stop() {
      stopped = true
      queue = ''
    },
  }
}
