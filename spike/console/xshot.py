#!/usr/bin/env python3
"""SPIKE #257 - throwaway. X11 window screenshot via XGetImage (ImageMagick's `import` fails here).
  xshot.py out.png [title-substring]      (default title 'Vibe Studio'; XWayland, DISPLAY=:0)"""
import ctypes, ctypes.util, os, struct, subprocess, sys, zlib
# DISPLAY and XAUTHORITY come from the environment: `. evidence/headless.env; export DISPLAY=$SPIKE_XDISPLAY XAUTHORITY=$SPIKE_XAUTH`
x = ctypes.CDLL(ctypes.util.find_library('X11'))
x.XOpenDisplay.restype = ctypes.c_void_p
class XImage(ctypes.Structure):
    _fields_ = [('width', ctypes.c_int), ('height', ctypes.c_int), ('xoffset', ctypes.c_int), ('format', ctypes.c_int),
                ('data', ctypes.POINTER(ctypes.c_char)), ('byte_order', ctypes.c_int), ('bitmap_unit', ctypes.c_int),
                ('bitmap_bit_order', ctypes.c_int), ('bitmap_pad', ctypes.c_int), ('depth', ctypes.c_int),
                ('bytes_per_line', ctypes.c_int), ('bits_per_pixel', ctypes.c_int), ('red_mask', ctypes.c_ulong),
                ('green_mask', ctypes.c_ulong), ('blue_mask', ctypes.c_ulong)]
x.XGetImage.restype = ctypes.POINTER(XImage)
x.XGetImage.argtypes = [ctypes.c_void_p, ctypes.c_ulong, ctypes.c_int, ctypes.c_int, ctypes.c_uint, ctypes.c_uint, ctypes.c_ulong, ctypes.c_int]
x.XGetGeometry.argtypes = [ctypes.c_void_p, ctypes.c_ulong] + [ctypes.c_void_p] * 7
def find(title):
    out = subprocess.check_output(['xprop', '-root', '_NET_CLIENT_LIST']).decode().split('# ')[1].replace(',', ' ').split()
    for wid in out:
        if title in subprocess.run(['xprop', '-id', wid, 'WM_NAME'], capture_output=True, text=True).stdout: return int(wid, 16)
    raise SystemExit('window not found: ' + title)
def png(path, w, h, rgb):
    raw = b''.join(b'\x00' + rgb[y * w * 3:(y + 1) * w * 3] for y in range(h))
    def chunk(t, d): c = struct.pack('>I', len(d)) + t + d; return c + struct.pack('>I', zlib.crc32(t + d))
    open(path, 'wb').write(b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(raw, 6)) + chunk(b'IEND', b''))
d = ctypes.c_void_p(x.XOpenDisplay(None))
wid = find(sys.argv[2] if len(sys.argv) > 2 else 'Vibe Studio')
root = ctypes.c_ulong(); a = [ctypes.c_int() for _ in range(2)]; w = ctypes.c_uint(); h = ctypes.c_uint(); b = ctypes.c_uint(); dp = ctypes.c_uint()
x.XGetGeometry(d, wid, ctypes.byref(root), ctypes.byref(a[0]), ctypes.byref(a[1]), ctypes.byref(w), ctypes.byref(h), ctypes.byref(b), ctypes.byref(dp))
img = x.XGetImage(d, wid, 0, 0, w.value, h.value, 0xFFFFFFFF, 2)
if not img: raise SystemExit('XGetImage failed')
im = img.contents; bpl = im.bytes_per_line; buf = ctypes.string_at(im.data, bpl * im.height)
rgb = bytearray(im.width * im.height * 3)
for yy in range(im.height):
    row = buf[yy * bpl: yy * bpl + im.width * 4]
    rgb[yy * im.width * 3:(yy + 1) * im.width * 3:3] = row[2::4]; rgb[yy * im.width * 3 + 1:(yy + 1) * im.width * 3:3] = row[1::4]; rgb[yy * im.width * 3 + 2:(yy + 1) * im.width * 3:3] = row[0::4]
png(sys.argv[1], im.width, im.height, bytes(rgb)); print(f'{sys.argv[1]} {im.width}x{im.height} wid={wid:#x}')
