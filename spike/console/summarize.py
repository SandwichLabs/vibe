#!/usr/bin/env python3
"""SPIKE #257 - throwaway. One line per check per results.json under evidence/ (optionally filtered by substring)."""
import json, glob, sys, os
flt = sys.argv[1] if len(sys.argv) > 1 else ''
here = os.path.dirname(os.path.abspath(__file__))
for f in sorted(glob.glob(os.path.join(here, 'evidence', '*results.json'))):
    if flt not in f: continue
    r = json.load(open(f)); print('==', os.path.basename(f), r['notes'])
    for c in r['detail']:
        d = json.dumps(c['data'])
        print(f"  {c['id']:>2} {c['status']:<5} {c['name'][:50]:<50} {d[:int(os.environ.get('W','160'))]}")
