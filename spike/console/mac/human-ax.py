#!/usr/bin/env python3
"""SPIKE #257 - throwaway: section 3 of spike/console/mac/CHECKLIST.md with real HID input (CGEvent) on one widget window.

  human.py PID WIDGET_LABEL OUTDIR [only=check,check]
The window must be running the spike route with lbl=ax (keys-rx mirrored to ~/vibe-spike-evidence).
"""
import glob, json, os, subprocess, sys, time

PID, W, OUT = sys.argv[1], sys.argv[2], sys.argv[3]
ONLY = set(sys.argv[4].split('=')[1].split(',')) if len(sys.argv) > 4 else None
HERE = os.path.dirname(os.path.abspath(__file__))
EV = os.path.expanduser('~/vibe-spike-evidence')
os.makedirs(OUT, exist_ok=True)
ENV = dict(os.environ, DRIVE_PID=PID)
K = dict(ret=36, up=126, down=125, left=123, right=124, home=115, end=119, esc=53, a=0, c=8, v=9, l=37, r=15, b=11, f=3, e=14, u=32, space=49)


def d(*args, check=True):
    r = subprocess.run([os.path.join(HERE, 'drive'), *map(str, args)], env=ENV, capture_output=True, text=True)
    if check and r.returncode != 0:
        raise SystemExit(f'drive {args} failed: {r.stderr.strip()}')
    return r.stdout.strip()


def front():
    if d('front') != PID:
        d('activate', PID, check=False); time.sleep(0.4)
    if d('front') != PID:
        raise SystemExit('app is not frontmost; stopping before any input')


def win():
    for _ in range(10):
        r = d('win', PID, check=False)
        if r: x, y, w, h = map(int, r.split()); return x, y, w, h
        time.sleep(0.5)
    raise SystemExit('window gone')


def rxfile():
    fs = [f for f in glob.glob(os.path.join(EV, '*-ax-keys-rx.json')) if f'{W}' in os.path.basename(f) or True]
    return max(fs, key=os.path.getmtime)


def rx():
    time.sleep(1.3)
    return json.load(open(rxfile()))


def to_screen(px, py, st=None):
    st = st or rx(); x, y, w, h = win()
    return x + (w - st['innerW']) + px, y + (h - st['innerH']) + py


def cell_xy(st, row, col):
    t = st['rects']['term']; c = st['cell']
    return to_screen(t['x'] + 2 + (col + 0.5) * c['w'], t['y'] + 2 + (row + 0.5) * c['h'], st)


def click_page(px, py, mode=''):
    x, y = to_screen(px, py); d('click', int(x), int(y), *([mode] if mode else []))


def click_term():
    st = rx(); t = st['rects']['term']; click_page(t['x'] + t['w'] // 2, t['y'] + t['h'] - 30)


def key(name, *mods):
    front(); d('key', K[name], *mods)


def typ(s):
    front(); d('type', s)


def shot(name):
    x, y, w, h = win(); p = os.path.join(OUT, f'{W}-{name}.png')
    subprocess.run(['screencapture', '-x', '-R', f'{x},{y},{w},{h}', p]); return p


results = []


def record(check, action, before, after, verdict, note, extra=None):
    new = after['rx'][len(before['rx']):]
    r = dict(check=check, action=action, rxNew=new, verdict=verdict, note=note, screenTail=[l for l in after['screen'] if l.strip()][-6:], **(extra or {}))
    results.append(r)
    json.dump(r, open(os.path.join(OUT, f'{W}-check{check}.json'), 'w'), indent=2)
    print(f'[{check}] {verdict}: {action} -> rx {new} | {note}', flush=True)


def bar(dx):  # toolbar controls sit left of the seed box on the same flex row
    sd = rx()['rects']['seed']; return sd['x'] + dx, sd['y'] + sd['h'] // 2


def snapshot():  # the page's own snapshot button (snapN.json/png)
    click_page(*bar(-58)); time.sleep(0.6); click_term(); time.sleep(0.2)  # give focus back to the terminal


def reset():  # fresh prompt: Ctrl+C cancels any pending line
    click_term(); key('c', 'ctrl'); time.sleep(0.3)


def want(c):
    return ONLY is None or c in ONLY


front(); click_term(); time.sleep(0.3)

if want('4'):
    b = rx(); key('e', 'opt'); key('e'); key('u', 'opt'); key('u'); a = rx()
    new = a['rx'][len(b['rx']):]
    ok = new == ['\\u{e9}', '\\u{fc}']
    record('4', 'Option+e e, Option+u u (US layout)', b, a, 'pass' if ok else 'fail', f'expected é then ü once each; got {new}')
    shot('4'); snapshot()

if want('5'):
    click_term(); b = rx()
    d('tis', 'enable', 'com.apple.inputmethod.Kotoeri.RomajiTyping', check=False)
    d('tis', 'enable', 'com.apple.inputmethod.Kotoeri.RomajiTyping.Japanese', check=False)
    sel = d('tis', 'select', 'com.apple.inputmethod.Kotoeri.RomajiTyping.Japanese', check=False)
    time.sleep(0.8); cur = d('tis', 'current', 'x')
    wx, wy, ww, wh = win(); typ('kanji'); time.sleep(0.8)
    p_compose = os.path.join(OUT, f'{W}-5-composing.png'); subprocess.run(['screencapture', '-x', '-R', f'{wx},{wy},{ww},{wh}', p_compose])
    panels = d('axdump', PID, 0, check=False); caret = rx(); mid = caret
    key('ret'); a = rx(); shot('5-committed')
    d('tis', 'select', 'com.apple.keylayout.US'); time.sleep(0.5)
    composing_rx = mid['rx'][len(b['rx']):]; committed = a['rx'][len(mid['rx']):]
    ok = composing_rx == [] and len(committed) == 1 and committed[0].startswith('\\u{')
    record('5', 'Japanese Romaji: type "kanji" (composing), then Return to commit', b, a, 'pass' if ok else 'check',
           f'input source={cur} (select rc {sel}); rx while composing={composing_rx}; rx on commit={committed}; composition position: see {os.path.basename(p_compose)}; AX windows while composing: {panels}; main window {wx},{wy}; terminal rect (page) {caret["rects"]["term"]}')
    snapshot()

if want('6a'):
    reset(); b = rx(); typ('help'); key('ret'); typ('table'); key('ret'); key('c', 'ctrl'); m = rx()
    key('b', 'opt'); key('f', 'opt'); a = rx()
    ctrlc = m['rx'][len(b['rx']):]; opt = a['rx'][len(m['rx']):]
    cancel = any('^C' in l for l in a['screen'])
    meta = opt == ['\\x1bb', '\\x1bf']; mac = opt == ['\\u{222b}', '\\u{192}']
    record('6a', 'help Enter, table Enter, Ctrl+C, Option+b, Option+f', b, a, 'pass' if ('\\x03' in ctrlc and cancel and (meta or mac)) else 'check',
           f'Ctrl+C rx tail={ctrlc[-1:]}, "^C (cancel)" on screen={cancel}; Option+b/f sent {opt} ({"meta" if meta else "macOS characters" if mac else "other"})')
    shot('6a'); snapshot()

saved_clip = subprocess.run(['pbpaste'], capture_output=True).stdout  # text only; restored on exit
import atexit
atexit.register(lambda: subprocess.run(['pbcopy'], input=saved_clip))

if want('6b'):
    subprocess.run(['pbcopy'], input=b'vibe-spike-sentinel')  # so a failed copy never records the person's clipboard
    b = rx(); click_term(); key('a', 'cmd'); key('c', 'cmd'); time.sleep(0.4)
    clip = subprocess.run(['pbpaste'], capture_output=True, text=True).stdout
    s = b['rects']['seed']; click_page(s['x'] + 20, s['y'] + 12); key('a', 'cmd'); key('v', 'cmd'); a = rx()
    seed = a['seed']; visible = [l.rstrip() for l in a['screen'] if l.strip()]
    ok = len(clip.strip()) > 0 and any(v and v in seed for v in visible[:3])
    record('6b', 'terminal Cmd+A, Cmd+C, paste into seed box', b, a, 'pass' if ok else 'fail',
           f'clipboard {len(clip)} chars, first line {clip.splitlines()[:1]}; seed box now {len(seed)} chars', dict(clipboardHead=clip[:300]))
    shot('6b'); snapshot()

if want('6c'):
    st = rx(); s = st['rects']['seed']; click_page(s['x'] + 20, s['y'] + 12)
    key('a', 'cmd'); typ('seed paste check'); key('a', 'cmd'); key('c', 'cmd'); time.sleep(0.3)
    reset(); b = rx(); key('v', 'cmd'); m = rx()
    key('c', 'ctrl'); typ('paste on'); key('ret'); m2 = rx(); key('v', 'cmd'); a = rx()
    plain = m['rx'][len(b['rx']):]; br = a['rx'][len(m2['rx']):]
    ok = plain == ['seed paste check'] and br == ['\\x1b[200~seed paste check\\x1b[201~']
    record('6c', 'seed text Cmd+C, terminal Cmd+V; then "paste on", Cmd+V', b, a, 'pass' if ok else 'check', f'plain paste rx={plain}; bracketed rx={br}')
    key('c', 'ctrl'); typ('paste off'); key('ret'); shot('6c'); snapshot()

if want('6d'):
    reset(); b = rx(); y0 = b['vy']
    for k, m in [('up', ''), ('down', ''), ('left', ''), ('right', ''), ('home', ''), ('end', ''), ('a', 'ctrl'), ('l', 'ctrl'), ('r', 'ctrl')]:
        key(k, *([m] if m else []))
    a = rx(); new = a['rx'][len(b['rx']):]
    exp = ['\\x1b[A', '\\x1b[B', '\\x1b[D', '\\x1b[C', '\\x1b[H', '\\x1b[F', '\\x01', '\\x0c', '\\x12']
    shot('6d')
    record('6d', 'Up Down Left Right Home End Ctrl+A Ctrl+L Ctrl+R', b, a, 'pass' if len(new) == 9 else 'check',
           f'{len(new)}/9 reached rx; expected roughly {exp}; status/route still the spike: {d("front") == PID}')
    snapshot()

if want('7'):
    # fill the grid so the table lands in the last 24 buffer lines (the page dumps the tail), then map dump index -> screen row
    reset(); typ('lines 80'); key('ret'); time.sleep(3.5); typ('size'); key('ret'); typ('table'); key('ret'); st = rx()
    import re as _re
    rows_n = int([_re.match(r'^(\d+)x(\d+)$', l.strip()).group(2) for l in st['screen'] if _re.match(r'^\d+x\d+$', l.strip())][-1])
    off = rows_n - len(st['screen'])
    tl = [i for i, l in enumerate(st['screen']) if l.strip() and not l.startswith('vibe >') and not _re.match(r'^(\d+x\d+|line \d+)', l.strip())]
    r0, r1 = tl[1], tl[2]
    x1, y1 = cell_xy(st, off + r0, 0); x2, y2 = cell_xy(st, off + r1, len(st['screen'][r1].rstrip()))
    d('drag', int(x1), int(y1), int(x2), int(y2)); time.sleep(0.3); a = rx()
    first = (a['selection'].splitlines() or [''])[0].rstrip()
    idx = [i for i, l in enumerate(st['screen']) if l.rstrip() == first and first]
    shift = (r0 - idx[0]) if idx else 0
    if shift:  # pointer->row mapping was off: redo the drag on the intended rows
        x1, y1 = cell_xy(st, off + r0 + shift, 0); x2, y2 = cell_xy(st, off + r1 + shift, len(st['screen'][r1].rstrip()))
        d('click', int(x1), int(y1)); d('drag', int(x1), int(y1), int(x2), int(y2)); time.sleep(0.3)
    off += shift
    key('c', 'cmd'); time.sleep(0.4)
    a = rx(); clip = subprocess.run(['pbpaste'], capture_output=True, text=True).stdout
    expect = '\n'.join(l.rstrip() for l in st['screen'][r0:r1 + 1])
    drag_ok = clip.rstrip() == expect.rstrip()
    sel_after_drag = a['selection']
    words = [(m.start(), m.group()) for m in _re.finditer(r'[A-Za-z_]{3,}', st['screen'][r0])]
    col, word = words[0] if words else (1, '')
    xw, yw = cell_xy(st, off + r0, col + 1); d('click', int(xw), int(yw), 'double'); a2 = rx()
    dbl = a2['selection']
    record('7', 'table; drag 2 lines; Cmd+C; double-click a word', st, a2, 'pass' if drag_ok and dbl.strip() == word else 'check',
           f'rows={rows_n}; row shift {shift}; page selection after drag={sel_after_drag!r}; clipboard==dragged rows: {drag_ok}; double-click selection={dbl!r} (word {word!r})', dict(clipboard=clip, expected=expect))
    shot('7'); snapshot()

if want('10'):
    reset(); typ('lines 10000'); key('ret')
    t0 = time.time(); st = rx(); x, y = cell_xy(st, 10, 20); d('move', int(x), int(y))
    samples = []
    for i in range(6):
        t = time.time(); d('scroll', 'trackpad', 40 if i % 2 == 0 else -25, 25); samples.append(round(time.time() - t, 2))
    for i in range(3):
        d('scroll', 'wheel', 3 if i % 2 == 0 else -3, 8)
    p = shot('10'); a = rx()
    record('10', 'lines 10000; synthetic trackpad (continuous, phased) and wheel scroll during and after drain', st, a,
           'pass' if max(samples) < 1.5 else 'check',
           f'scroll bursts (25 events @16 ms) took {samples}s each (no stall if ~0.4-0.6); viewportY now {a["vy"]}, scrollback {a["sbLines"]}; last line visible in tail: {any("10000" in l for l in a["screen"])}')
    snapshot()

if want('3'):
    reset(); typ('colors'); key('ret'); time.sleep(0.5); shot('3-before')
    click_page(*bar(-183)); time.sleep(1.0)
    pressed = d('menupress', PID, 'chalkboard', check=False)
    time.sleep(1.2); shot('3-after'); snapshot(); st = rx()
    record('3', 'theme dropdown schoolhouse-light -> chalkboard with output on screen', st, st, 'see png',
           f'menu: {pressed[:60]}; compare {W}-3-before.png and {W}-3-after.png (old output recoloured or not)')
    click_page(*bar(-183)); time.sleep(1.0); d('menupress', PID, 'schoolhouse-light', check=False); time.sleep(0.8)

if want('9'):
    import re as _re
    x, y, w, h = win(); out = []
    def fs_on(): d('fullscreen', PID, 1)
    for name, act in [('small', lambda: d('setframe', PID, x, y, 760, 520)), ('wide', lambda: d('setframe', PID, x, y, 1400, 700)),
                      ('zoom', lambda: d('zoom', PID)), ('fullscreen', fs_on)]:
        act(); time.sleep(3 if name == 'fullscreen' else 1.2)
        reset(); typ('size'); key('ret'); st = rx()
        sz = [l.strip() for l in st['screen'] if _re.match(r'^\d+x\d+$', l.strip())][-1:]
        t = st['rects']['term']; c = st['cell']
        out.append(dict(step=name, sizeCmd=sz, termRect=t, cell=c, fitWouldBe=[int((t['w'] - 4) // c['w']), int((t['h'] - 4) // c['h'])]))
        if name == 'fullscreen':
            p = os.path.join(OUT, f'{W}-9-fullscreen.png'); subprocess.run(['screencapture', '-x', p])
        else:
            shot(f'9-{name}')
    d('move', 300, 20); time.sleep(0.6); d('move', 62, 50); time.sleep(1.2); d('click', 62, 50); time.sleep(3)  # green button in the full-screen title bar
    d('setframe', PID, x, y, w, h); time.sleep(1); d('activate', PID, check=False)
    record('9', 'resize small/wide, zoom (maximise), full screen; `size` after each', rx(), rx(), 'see json+png',
           'the harness fits once at open (no ResizeObserver), so `size` reports the grid it opened with; screenshots show clipping/stale pixels', dict(steps=out))

if want('1'):
    reset(); typ('table'); key('ret'); typ('unicode'); key('ret'); time.sleep(0.5)
    p = shot('1-retina'); snapshot()
    record('1', 'Retina screenshot (2x) of table/unicode output for crispness', rx(), rx(), 'see png', p)

if want('15'):
    st = rx(); t = st['rects']['term']; click_page(t['x'] + 200, t['y'] + 200, 'right'); time.sleep(0.8); shot('15-rightclick')
    ctx = d('menupress', PID, 'Inspect', check=False)
    if 'pressed' not in ctx:
        key('esc') if False else None
        front(); d('key', 111, 'cmd', 'shift', 'fn'); time.sleep(3)
    wins = d('axdump', PID, 0, check=False)
    subprocess.run(['screencapture', '-x', os.path.join(OUT, f'{W}-15-inspector.png')])
    opened = 'Web Inspector' in wins
    d('closewin', PID, 'Web Inspector', check=False); time.sleep(0.8)
    record('15', 'right-click page -> Inspect Element; fallback Cmd+Shift+F12', st, st, 'pass' if opened else 'fail',
           f'context menu Inspect: {"yes" if "pressed" in ctx else "no menu (Wails disables the default context menu unless debug or EnableDefaultContextMenu)"}; inspector opened via {"menu" if "pressed" in ctx else "Cmd+Shift+F12"}: {opened}; windows: {wins}')

subprocess.run(['pbcopy'], input=saved_clip)
json.dump(results, open(os.path.join(OUT, f'{W}-human.json'), 'w'), indent=2)
