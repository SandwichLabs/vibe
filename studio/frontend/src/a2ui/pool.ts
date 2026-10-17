// The shared concurrency pool with a priority lane, behind every live query in
// the app: A2UI data-bound components, notebook sql cells and data artifacts.
// Gating concurrency in this one place bounds ALL of them at once, so a page with
// many data-bound components mounting together cannot saturate the file's one
// DuckDB connection and make every other open query wait behind them.
//
// Two priority lanes (round 2 review: a background component storm starved an
// explicit, user-initiated cell Run for ~15s). 'low' (every component's
// auto-fetch) may use at most LOW_SLOT_CAP of the MAX_CONCURRENT_QUERIES slots,
// so RESERVED_HIGH_SLOTS is always free for 'high' work (a person clicking Run)
// to start immediately. Artifact runs are 'low' too.
//
// A caller that aborts while still queued leaves the queue (it never takes a
// slot). One already running keeps its slot until the Go call returns, since
// the call itself cannot be cancelled; its result is reported as aborted.
export type QueryPriority = 'high' | 'low'

const MAX_CONCURRENT_QUERIES = 4
const RESERVED_HIGH_SLOTS = 1
const LOW_SLOT_CAP = MAX_CONCURRENT_QUERIES - RESERVED_HIGH_SLOTS

let activeHigh = 0
let activeLow = 0
const highQueue: Array<() => void> = []
const lowQueue: Array<() => void> = []

function pump(): void {
  while (highQueue.length && activeHigh + activeLow < MAX_CONCURRENT_QUERIES) {
    const task = highQueue.shift()!
    activeHigh++
    task()
  }
  while (lowQueue.length && activeLow < LOW_SLOT_CAP && activeHigh + activeLow < MAX_CONCURRENT_QUERIES) {
    const task = lowQueue.shift()!
    activeLow++
    task()
  }
}

// acquireSlot resolves true once a slot is held, or false when the signal
// aborted first (then nothing is held and nothing stays queued).
function acquireSlot(priority: QueryPriority, signal?: AbortSignal): Promise<boolean> {
  return new Promise((resolve) => {
    const queue = priority === 'high' ? highQueue : lowQueue
    const onAbort = () => {
      const i = queue.indexOf(task)
      if (i >= 0) queue.splice(i, 1)
      resolve(false)
    }
    const task = () => {
      signal?.removeEventListener('abort', onAbort)
      resolve(true)
    }
    signal?.addEventListener('abort', onAbort, { once: true })
    queue.push(task)
    pump()
  })
}

function releaseSlot(priority: QueryPriority): void {
  if (priority === 'high') activeHigh--
  else activeLow--
  pump()
}

export type Pooled<T> = { aborted: true } | { aborted: false; value: T }

// runPooled runs task once the pool has a slot for its lane, holding the slot
// until the task settles. A signal aborted before the task starts, or by the time
// it settles, reports { aborted: true } (a thrown error after an abort too); an
// error otherwise propagates after the slot is released.
export async function runPooled<T>(task: () => Promise<T>, signal?: AbortSignal, priority: QueryPriority = 'low'): Promise<Pooled<T>> {
  if (signal?.aborted) return { aborted: true }
  if (!(await acquireSlot(priority, signal))) return { aborted: true }
  try {
    if (signal?.aborted) return { aborted: true }
    const value = await task()
    if (signal?.aborted) return { aborted: true }
    return { aborted: false, value }
  } catch (e) {
    if (signal?.aborted) return { aborted: true }
    throw e
  } finally {
    releaseSlot(priority)
  }
}

// Exposed for tests only — the pool's queue state is module-level and otherwise
// unresettable between test cases.
export function _resetQueryPoolForTests(): void {
  activeHigh = 0
  activeLow = 0
  highQueue.length = 0
  lowQueue.length = 0
}
