// SPIKE #257 - never merge. A fake REPL that speaks VT to the widget and logs every byte it receives.
import type { TermAdapter } from './adapter'

export interface FrameStats { frames: number; p50: number; p95: number; max: number; over33: number; over50: number }
export interface LinesResult { n: number; drainMs: number; drain: FrameStats; scroll: FrameStats; combined: FrameStats; lastLinePresent: boolean; scrollbackAfter: number; lengthAfter: number }

export const esc = (s: string): string =>
  s.replace(/[\x00-\x1f\x7f]/g, (c) => (c === '\x1b' ? '\\x1b' : '\\x' + c.charCodeAt(0).toString(16).padStart(2, '0'))).replace(/[^\x20-\x7e\\]/g, (c) => '\\u{' + c.codePointAt(0)!.toString(16) + '}')

const C = (n: number) => `\x1b[${n}m`
const R = '\x1b[0m'
const PROMPT = `${C(1)}${C(36)}vibe${R} ${C(2)}>${R} `

function stats(ts: number[]): FrameStats {
  if (ts.length === 0) return { frames: 0, p50: 0, p95: 0, max: 0, over33: 0, over50: 0 }
  const s = [...ts].sort((a, b) => a - b)
  const q = (p: number) => Math.round(s[Math.min(s.length - 1, Math.floor(p * s.length))] * 10) / 10
  return { frames: s.length, p50: q(0.5), p95: q(0.95), max: Math.round(s[s.length - 1] * 10) / 10, over33: s.filter((x) => x > 33.4).length, over50: s.filter((x) => x > 50).length }
}

function rafFrames(): { stop(): number[] } {
  const ts: number[] = []
  let last = performance.now(); let on = true
  const tick = () => { if (!on) return; const now = performance.now(); ts.push(now - last); last = now; requestAnimationFrame(tick) }
  requestAnimationFrame(tick)
  return { stop() { on = false; return ts } }
}

export class Feed {
  rx: { t: number; raw: string }[] = []
  log: { t: number; cmd: string; result: unknown }[] = []
  lastResize = { cols: 0, rows: 0 }
  private line = ''
  constructor(private a: TermAdapter, private onSubmit?: (cmd: string) => void) {
    a.onData((d) => this.input(d))
    a.onResize((d) => { this.lastResize = d })
  }

  start(): void { this.a.write(`${C(1)}Vibe data console - R0 spike (${this.a.kind})${R}\r\ntype ${C(33)}help${R}\r\n${PROMPT}`) }

  /** Bytes the widget hands to Go, exactly. Echo printable text, run on CR. */
  input(d: string): void {
    this.rx.push({ t: Math.round(performance.now()), raw: d })
    const paste = d.startsWith('\x1b[200~')
    const body = paste ? d.replace('\x1b[200~', '').replace('\x1b[201~', '') : d
    if (paste || !d.startsWith('\x1b')) {
      for (const ch of body) {
        if (ch === '\r' && !paste) { this.a.write('\r\n'); const c = this.line.trim(); this.line = ''; void this.run(c).then(() => this.a.write(PROMPT)); continue }
        if (ch === '\x7f') { if (this.line) { this.line = this.line.slice(0, -1); this.a.write('\b \b') } continue }
        if (ch === '\x03') { this.line = ''; this.a.write(`${C(31)}^C (cancel)${R}\r\n${PROMPT}`); continue }
        if (ch === '\r' || ch === '\n') { this.line += '⏎'; this.a.write('⏎'); continue }
        if (ch >= ' ') { this.line += ch; this.a.write(ch) }
      }
    }
  }

  async run(cmd: string): Promise<void> {
    if (!cmd) return
    this.onSubmit?.(cmd)
    const [name, arg] = cmd.split(/\s+/)
    const w = (s: string) => this.a.write(s.replace(/\n/g, '\r\n'))
    let result: unknown = 'ok'
    switch (name) {
      case 'help': w(`commands: help table colors unicode wide lines N paste on|off size bell clear\n`); break
      case 'table': w(this.table()); break
      case 'colors': w(this.colors()); break
      case 'unicode': w(`CJK  你好世界 日本語 한글 |  emoji 😀👍🎉 |  combining é ñ |  deva किताब |\n12345678901234567890123456789012345678901234567890\n`); break
      case 'wide': w('W'.repeat(300) + '\n'); break
      case 'paste': this.a.write(arg === 'off' ? '\x1b[?2004l' : '\x1b[?2004h'); result = { bracketedPaste: this.a.hasBracketedPaste() }; w(`bracketed paste ${arg}: ${JSON.stringify(result)}\n`); break
      case 'size': result = { ...this.lastResize }; w(`${this.lastResize.cols}x${this.lastResize.rows}\n`); break
      case 'bell': this.a.write('\x07'); break
      case 'clear': this.a.write('\x1b[2J\x1b[3J\x1b[H'); break
      case 'lines': result = await this.lines(Number(arg) || 1000); break
      default: w(`${C(31)}unknown command: ${name}${R}\n`); result = 'unknown'
    }
    this.log.push({ t: Math.round(performance.now()), cmd, result })
  }

  private table(): string {
    const cols = ['id', 'customer', 'region', 'amount', 'ordered', 'status']
    const widths = [6, 14, 8, 12, 12, 10]
    const types = ['int64', 'varchar', 'varchar', 'decimal', 'date', 'varchar']
    const bar = (l: string, m: string, r: string) => l + widths.map((x) => '─'.repeat(x + 2)).join(m) + r
    const row = (cells: string[], right: number[] = [0, 3]) => '│' + cells.map((c, i) => ' ' + (right.includes(i) ? c.padStart(widths[i]) : c.padEnd(widths[i])) + ' ').join('│') + '│'
    const out = [bar('┌', '┬', '┐'), row(cols, []), row(types, []), bar('├', '┼', '┤')]
    for (let i = 1; i <= 6; i++) out.push(row([String(1000 + i), ['Acme', 'Globex', 'Initech', 'Umbrella', 'Hooli', 'Stark'][i - 1], ['EMEA', 'AMER', 'APAC'][i % 3], (i * 1234.5).toFixed(2), `2026-10-${10 + i}`, i % 2 ? 'paid' : 'open']))
    out.push(row(['·', '·', '·', '·', '·', '·'], []))
    out.push(row(['1012', 'Wonka', 'EMEA', '14814.00', '2026-10-28', 'paid']))
    out.push(bar('└', '┴', '┘'))
    out.push(`${C(2)}12 rows (3 shown)${R}`)
    return out.join('\r\n') + '\r\n'
  }

  private colors(): string {
    let s = ''
    for (let i = 0; i < 16; i++) s += `\x1b[${i < 8 ? 40 + i : 100 + i - 8}m  ${R}`
    s += '\r\n'
    for (let i = 0; i < 16; i++) s += `\x1b[${i < 8 ? 30 + i : 90 + i - 8}m##${R}`
    s += `\r\n${C(1)}bold${R} ${C(2)}dim${R} ${C(3)}italic${R} ${C(4)}underline${R} ${C(7)}inverse${R} ${C(9)}strike${R}\r\n`
    for (let i = 16; i < 232; i += 6) s += `\x1b[48;5;${i}m `
    s += `${R}\r\n`
    for (let i = 0; i < 48; i++) s += `\x1b[48;2;${i * 5};${255 - i * 5};128m `
    return s + R + '\r\n'
  }

  private lines(n: number): Promise<LinesResult> {
    return new Promise((resolve) => {
      const chunk: string[] = []; const chunks: string[] = []; let bytes = 0
      for (let i = 1; i <= n; i++) {
        const body = `line ${String(i).padStart(6, '0')} ` + 'the quick brown fox jumps over the lazy dog 0123456789 '.slice(0, 56)
        const l = (i % 10 === 0 ? `${C(32)}${body}${R}` : body) + '\r\n'
        chunk.push(l); bytes += l.length
        if (bytes > 65536 || i === n) { chunks.push(chunk.join('')); chunk.length = 0; bytes = 0 }
      }
      const fr = rafFrames(); const t0 = performance.now(); let pending = chunks.length
      for (const c of chunks) this.a.write(c, () => {
        if (--pending > 0) return
        const drainMs = Math.round(performance.now() - t0); const dts = fr.stop(); const drain = stats(dts)
        // Then 2 s of programmatic scrolling, three lines per frame.
        const sf = rafFrames(); const s0 = performance.now(); const dir = -3
        const step = () => {
          if (performance.now() - s0 > 2000) {
            const sts = sf.stop(); const scroll = stats(sts); const combined = stats(dts.concat(sts)); this.a.scrollToBottom()
            const d = this.a.dump(3)
            resolve({ n, drainMs, drain, scroll, combined, lastLinePresent: d.lines.some((x) => x.includes(`line ${String(n).padStart(6, '0')}`)), scrollbackAfter: d.scrollback, lengthAfter: d.length })
            return
          }
          this.a.scrollLines(dir)
          requestAnimationFrame(step)
        }
        requestAnimationFrame(step)
      })
    })
  }
}
