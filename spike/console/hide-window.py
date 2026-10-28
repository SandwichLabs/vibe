#!/usr/bin/env python3
"""SPIKE #257 - throwaway. hide-window.py iconify|show 'Vibe Studio'  (X11 window of the private compositor)."""
import ctypes, ctypes.util, subprocess, sys
x = ctypes.CDLL(ctypes.util.find_library('X11')); x.XOpenDisplay.restype = ctypes.c_void_p
d = ctypes.c_void_p(x.XOpenDisplay(None))
title = sys.argv[2] if len(sys.argv) > 2 else 'Vibe Studio'
for wid in subprocess.check_output(['xprop', '-root', '_NET_CLIENT_LIST']).decode().split('# ')[1].replace(',', ' ').split():
    if title in subprocess.run(['xprop', '-id', wid, 'WM_NAME'], capture_output=True, text=True).stdout:
        w = ctypes.c_ulong(int(wid, 16))
        if sys.argv[1] == 'iconify': x.XIconifyWindow(d, w, 0)
        else: x.XMapRaised(d, w)
        x.XFlush(d); print(sys.argv[1], wid)
