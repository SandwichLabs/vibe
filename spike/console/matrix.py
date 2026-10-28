#!/usr/bin/env python3
"""SPIKE #257 - throwaway. Markdown matrix of the automatic checks: rows = check, columns = widget configuration,
one table per environment label. Prints status plus the headline number where there is one."""
import json, glob, os, sys
here = os.path.dirname(os.path.abspath(__file__)); ev = os.path.join(here, 'evidence')
cfgs = [('ghostty url, sb=30M', 'ghostty-url-sb30000000'), ('ghostty url, default sb', 'ghostty-url'), ('ghostty inline, default sb', 'ghostty-inline'),
        ('xterm-webgl', 'xterm-webgl'), ('xterm-webgl pdb', 'xterm-webgl-pdb'), ('xterm-dom', 'xterm-dom')]
envs = sys.argv[1:] or ['chromium-http', 'webkitgtk-dev-wl', 'webkitgtk-dev-x11', 'webkitgtk-prod-wl']
names = {1: 'Renders', 2: 'Theme roles', 3: 'Theme switch', 7: 'Copy (getSelection)', 8: 'Bracketed paste', 9: 'Resize/fit', 10: '10k lines', 11: 'WASM/init', 14: 'Errors'}
for e in envs:
    print(f'\n### {e}\n')
    print('| check | ' + ' | '.join(c[0] for c in cfgs) + ' |'); print('|---|' + '---|' * len(cfgs))
    data = {}
    for _, tag in cfgs:
        f = os.path.join(ev, f'{tag}-{e}-results.json')
        data[tag] = {c['id']: c for c in json.load(open(f))['detail']} if os.path.exists(f) else {}
    for i, n in names.items():
        cells = []
        for _, tag in cfgs:
            c = data[tag].get(i)
            if not c: cells.append('n/a'); continue
            extra = ''
            d = c['data']
            if i == 10: extra = f" {d['combined']['p95']}ms p95, {d['drainMs']}ms drain, {d['lengthAfter']} lines"
            if i == 11: extra = f" {d['initMs']}ms"
            if i == 3 and isinstance(d, dict): extra = ' in place' if d.get('inPlaceOk') else (' recreate only' if d.get('recreatedOk') else '')
            cells.append(c['status'].upper() + extra)
        print(f'| {n} | ' + ' | '.join(cells) + ' |')
