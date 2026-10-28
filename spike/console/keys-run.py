#!/usr/bin/env python3
"""SPIKE #257 - throwaway. Real keyboard and pointer input (compositor level, private headless mutter) into the
Studio window for ONE widget configuration, with the bytes the widget handed over written to evidence.

  keys-run.py <tag> '<query without auto>' [--copykey] [--shots]       (run-webkitgtk.sh hw|hx must be running)

Needs bin/injectd (go build -o spike/console/bin/injectd ./spike/console/inject) and
evidence/headless.env. --shots takes X11 window PNGs (hx only).
Evidence: evidence/keys-<tag>-<step>.json (+ .png), evidence/keys-<tag>-summary.json
"""
import glob, json, os, shutil, subprocess, sys, time
here = os.path.dirname(os.path.abspath(__file__)); ev = os.path.join(here, 'evidence')
env = dict(os.environ)
for l in open(os.path.join(ev, 'headless.env')):
    k, _, v = l.strip().partition('='); env[k] = v
tag, query = sys.argv[1], sys.argv[2]
copykey = '--copykey' in sys.argv; shots = '--shots' in sys.argv
lbl = 'inj' + ('c' if copykey else '')
inj = subprocess.Popen([os.path.join(here, 'bin', 'injectd')], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, env=env, bufsize=1)
print(inj.stdout.readline().strip())
def send(line):
    inj.stdin.write(line + '\n'); inj.stdin.flush(); inj.stdout.readline()
def latest():
    fs = glob.glob(os.path.join(ev, f'*-{lbl}-keys-rx.json'))
    return json.load(open(max(fs, key=os.path.getmtime))) if fs else None
def snap(name, wait=1.3):
    time.sleep(wait)
    fs = glob.glob(os.path.join(ev, f'*-{lbl}-keys-rx.json'))
    if fs: shutil.copy(max(fs, key=os.path.getmtime), os.path.join(ev, f'keys-{tag}-{name}.json'))
    if shots:
        e = dict(env, DISPLAY=env['SPIKE_XDISPLAY'], XAUTHORITY=env['SPIKE_XAUTH'])
        subprocess.run([sys.executable, os.path.join(here, 'xshot.py'), os.path.join(ev, f'keys-{tag}-{name}.png')], env=e, capture_output=True)
    return latest()
for f in glob.glob(os.path.join(ev, f'*-{lbl}-*')): os.remove(f)
route = f"#/spike/console?{query}&lbl={lbl}{'&copykey=1' if copykey else ''}"
open(os.path.join(ev, 'route.txt'), 'w').write(route + '\t%d\n' % (time.time() * 1000))
time.sleep(10)
# calibrate: monitor coordinates -> page client coordinates, from the page's own mousemove
st = None
for attempt in range(6):
    send('abs 700 450'); time.sleep(0.4)
    for _ in range(4): send('rel 1 1'); time.sleep(0.15)
    st = snap('calib')
    if st and st['ptr']['x'] >= 0: break
if not st or st['ptr']['x'] < 0: sys.exit('pointer never reached the page; calib=' + json.dumps(st))
ox, oy = 704 - st['ptr']['x'], 454 - st['ptr']['y']
print('origin offset', ox, oy, 'inner', st['innerW'], st['innerH'])
def at(cx, cy): send(f'abs {int(cx + ox)} {int(cy + oy)}')
def center(r): return r['x'] + r['w'] // 2, r['y'] + r['h'] // 2
term = st['rects']['term']
def seedr(): return latest()['rects']['seed']   # the layout shifts as the status text changes: always read it fresh
def click_term(dx=0.4, dy=0.5): at(term['x'] + term['w'] * dx, term['y'] + term['h'] * dy); send('click'); time.sleep(1.0); send('key shift')  # focus settles; the first key after a click was dropped without the warm-up tap
def seed_set(text):
    at(*center(seedr())); send('click'); time.sleep(0.8); send('key shift'); send('key ctrl+a'); send('key BackSpace')
    for i, part in enumerate(text.split('\n')):
        if i: send('key Return')
        send('type ' + part)
    send('key ctrl+a'); send('key ctrl+c')
def seed_read():
    at(*center(seedr())); send('click'); time.sleep(0.8); send('key shift'); send('key ctrl+a'); send('key ctrl+v'); s = snap('seedread'); return s['seed'] if s else None
# 10k lines with wheel scrolling during the drain
res = {}
def wheel_block():
    click_term(0.4, 0.8); send('type clear'); send('key Return'); send('type lines 10000'); send('key Return')
    at(term['x'] + 300, term['y'] + 200)
    for _ in range(8): send('wheel up 5'); time.sleep(0.2)
    time.sleep(2.5); st = snap('lines-wheel'); res['discrete wheel up x40: [viewportY, scrollbackLines]'] = [st.get('vy'), st.get('sbLines')]
    send('wheel down 300'); st = snap('lines-bottom'); res['discrete wheel down x300'] = [st.get('vy'), st.get('sbLines')]; res['lines bottom'] = st['screen'][-3:]
    send('smooth up 40'); st = snap('smooth-up'); res['smooth scroll up x40 (pixel deltas)'] = [st.get('vy'), st.get('sbLines')]
    send('smooth down 400'); st = snap('smooth-down'); res['smooth scroll down x400'] = [st.get('vy'), st.get('sbLines')]
if '--only-wheel' in sys.argv:
    click_term(); wheel_block(); json.dump(res, open(os.path.join(ev, f'keys-{tag}-wheel.json'), 'w'), indent=2); send('quit'); print(json.dumps(res, indent=1)); sys.exit(0)
click_term(); time.sleep(0.5)
send('type help'); send('key Return'); res['typed'] = snap('type-enter')
send('type table'); send('key Return'); snap('table')
send('type paste on'); send('key Return'); snap('paste-on')
n0 = len(latest()['rx'])
send('key ctrl+c'); res['ctrl+c'] = snap('ctrl-c')['rx'][-1:]
send('key Left Right Up Down Home End Delete BackSpace Page_Up Page_Down'); res['nav'] = snap('nav')['rx'][-10:]
send('key alt+b alt+f ctrl+a ctrl+e ctrl+l ctrl+r ctrl+d ctrl+z Tab Escape'); res['emacs'] = snap('emacs')['rx'][-10:]
send('key ctrl+shift+a'); res['ctrl+shift+a'] = snap('ctrl-shift-a')['rx'][-1:]
# paste three ways, bracketed paste on, with a two-line clipboard made through the page's own textarea
seed_set('line-one\nline-two'); click_term(0.4, 0.8)
send('key ctrl+v'); res['ctrl+v on'] = snap('ctrl-v-on')['rx'][-1:]
send('key shift+Insert'); res['shift+Insert on'] = snap('shift-insert-on')['rx'][-1:]
send('key ctrl+shift+v'); res['ctrl+shift+v on'] = snap('ctrl-shift-v-on')['rx'][-1:]
send('type paste off'); send('key Return'); send('key ctrl+v'); res['ctrl+v off'] = snap('ctrl-v-off')['rx'][-1:]
# copy: drag across terminal text, Ctrl+Shift+C, Ctrl+C with a selection; read the clipboard back through the textarea
send('type clear'); send('key Return'); send('type table'); send('key Return'); time.sleep(0.8)
def drag(): at(term['x'] + 20, term['y'] + 40); send('down'); at(term['x'] + 330, term['y'] + 70); send('up'); time.sleep(0.4)
seed_set('untouched'); drag(); res['selection after drag'] = snap('selected')['selection']
res['A: clipboard after drag only (auto-copy?)'] = seed_read()
seed_set('untouched'); drag(); send('key ctrl+shift+c'); time.sleep(0.5)
res['copykey log'] = (latest() or {}).get('log', [])
res['B: clipboard after drag + ctrl+shift+c'] = seed_read()
seed_set('untouched'); drag(); n = len(latest()['rx']); send('key ctrl+c'); st = snap('ctrl-c-with-selection')
res['C: rx after ctrl+c with a selection'] = st['rx'][n:]
res['C: clipboard after drag + ctrl+c'] = seed_read()
dbl = []
cw, chh = latest()['cell']['w'], latest()['cell']['h']
for (px, py) in [(cw * 3.5, chh * 2.5), (cw * 12.5, chh * 2.5), (cw * 12.5, chh * 3.5), (cw * 38.5, chh * 5.5)]:  # text cells of the table header, 2 rows, a data row
    click_term(0.4, 0.8); at(term['x'] + px, term['y'] + py); send('click left 2'); dbl.append(snap('dblclick-%d-%d' % (px, py))['selection'])
res['dblclick selections at 4 points (a word each, or whitespace)'] = dbl
seed_set('middle-paste'); click_term(0.4, 0.8)
at(term['x'] + 200, term['y'] + 300); send('click middle'); res['middle-click'] = snap('middle-click')['rx'][-1:]
wheel_block()
# supplementary: a dead key by keysym (mutter must map dead_acute to a keycode; not a layout change, not counted as a pass)
click_term(0.4, 0.8); send('type clear'); send('key Return'); n = len(latest()['rx'])
send('keysym 0xfe51'); send('type e'); time.sleep(0.3); send('keysym 0xfe57'); send('type u')
st = snap('deadkeys-keysym'); res['supplementary dead_acute+e, dead_diaeresis+u (keysym injection)'] = st['rx'][n:]
json.dump(res, open(os.path.join(ev, f'keys-{tag}-summary.json'), 'w'), indent=2, ensure_ascii=False)
send('quit'); print(json.dumps(res, indent=1, ensure_ascii=False)[:6000])
