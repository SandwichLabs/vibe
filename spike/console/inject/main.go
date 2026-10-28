// SPIKE #257 - throwaway, never merge. Compositor-level input for the PRIVATE headless mutter that
// headless-compositor.sh starts: it talks to that mutter's own org.gnome.Mutter.RemoteDesktop on the private
// session bus (address in $SPIKE_DBUS). It never touches the user's session bus, the xdg portals or the user's
// compositor. XTEST cannot be used here: Xwayland routes it through a portal approval dialog that nobody can click.
//
// Reads commands from stdin, one per line:
//
//	key ctrl+shift+c Return Left      key chords (evdev keycodes, US layout), several per line
//	type some text                    printable ASCII, one key press per character
//	keysym 0xfe51                     one keysym (dead_acute...), mutter maps it to a keycode if it can
//	abs X Y                           move the pointer to monitor coordinates (via the top-left corner)
//	rel DX DY                         relative motion
//	click [left|middle|right] [N]     N presses (default 1)
//	down / up [button]                hold or release a button
//	wheel up|down N                   discrete wheel steps
//	smooth up|down N                  smooth (pixel) scroll steps
//	sleep MS
//	quit
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

var keycodes = map[string]uint32{
	"Escape": 1, "1": 2, "2": 3, "3": 4, "4": 5, "5": 6, "6": 7, "7": 8, "8": 9, "9": 10, "0": 11, "minus": 12, "equal": 13,
	"BackSpace": 14, "Tab": 15, "q": 16, "w": 17, "e": 18, "r": 19, "t": 20, "y": 21, "u": 22, "i": 23, "o": 24, "p": 25,
	"bracketleft": 26, "bracketright": 27, "Return": 28, "ctrl": 29, "a": 30, "s": 31, "d": 32, "f": 33, "g": 34, "h": 35,
	"j": 36, "k": 37, "l": 38, "semicolon": 39, "apostrophe": 40, "grave": 41, "shift": 42, "backslash": 43, "z": 44, "x": 45,
	"c": 46, "v": 47, "b": 48, "n": 49, "m": 50, "comma": 51, "period": 52, "slash": 53, "alt": 56, "space": 57,
	"Home": 102, "Up": 103, "Page_Up": 104, "Left": 105, "Right": 106, "End": 107, "Down": 108, "Page_Down": 109,
	"Insert": 110, "Delete": 111, "super": 125,
}

// shifted printable -> (base key, needs shift)
var shifted = map[rune][2]string{
	'!': {"1", "s"}, '@': {"2", "s"}, '#': {"3", "s"}, '$': {"4", "s"}, '%': {"5", "s"}, '^': {"6", "s"}, '&': {"7", "s"}, '*': {"8", "s"},
	'(': {"9", "s"}, ')': {"0", "s"}, '_': {"minus", "s"}, '+': {"equal", "s"}, ':': {"semicolon", "s"}, '"': {"apostrophe", "s"},
	'<': {"comma", "s"}, '>': {"period", "s"}, '?': {"slash", "s"}, '{': {"bracketleft", "s"}, '}': {"bracketright", "s"}, '|': {"backslash", "s"}, '~': {"grave", "s"},
}
var plain = map[rune]string{' ': "space", '-': "minus", '=': "equal", ',': "comma", '.': "period", '/': "slash", ';': "semicolon", '\'': "apostrophe", '[': "bracketleft", ']': "bracketright", '\\': "backslash", '`': "grave", '\n': "Return", '\t': "Tab"}

type inj struct{ s dbus.BusObject }

func (i *inj) call(method string, args ...any) {
	if c := i.s.Call("org.gnome.Mutter.RemoteDesktop.Session."+method, 0, args...); c.Err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", method, c.Err)
	}
	time.Sleep(12 * time.Millisecond)
}
func (i *inj) key(code uint32, down bool) { i.call("NotifyKeyboardKeycode", code, down) }
func (i *inj) chord(spec string) {
	parts := strings.Split(spec, "+")
	var codes []uint32
	for _, p := range parts {
		name := p
		if len(p) == 1 && p[0] >= 'A' && p[0] <= 'Z' {
			name = strings.ToLower(p)
		}
		c, ok := keycodes[strings.ToLower(name)]
		if !ok {
			c, ok = keycodes[name]
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown key %q\n", p)
			return
		}
		codes = append(codes, c)
	}
	for _, c := range codes {
		i.key(c, true)
	}
	time.Sleep(30 * time.Millisecond)
	for j := len(codes) - 1; j >= 0; j-- {
		i.key(codes[j], false)
	}
	time.Sleep(60 * time.Millisecond)
}
func (i *inj) typ(text string) {
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			i.chord(string(r))
		case r >= 'A' && r <= 'Z':
			i.chord("shift+" + strings.ToLower(string(r)))
		default:
			if n, ok := plain[r]; ok {
				i.chord(n)
			} else if s, ok := shifted[r]; ok {
				i.chord("shift+" + s[0])
			}
		}
	}
}
func button(name string) uint32 {
	switch name {
	case "middle":
		return 0x112
	case "right":
		return 0x111
	}
	return 0x110
}

func main() {
	addr := os.Getenv("SPIKE_DBUS")
	conn, err := dbus.Connect(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect private bus:", err)
		os.Exit(1)
	}
	var path dbus.ObjectPath
	rd := conn.Object("org.gnome.Mutter.RemoteDesktop", "/org/gnome/Mutter/RemoteDesktop")
	if err := rd.Call("org.gnome.Mutter.RemoteDesktop.CreateSession", 0).Store(&path); err != nil {
		fmt.Fprintln(os.Stderr, "CreateSession:", err)
		os.Exit(1)
	}
	i := &inj{s: conn.Object("org.gnome.Mutter.RemoteDesktop", path)}
	if c := i.s.Call("org.gnome.Mutter.RemoteDesktop.Session.Start", 0); c.Err != nil {
		fmt.Fprintln(os.Stderr, "Start:", c.Err)
		os.Exit(1)
	}
	fmt.Println("ready", path)
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		arg := func(n int, def string) string {
			if n < len(f) {
				return f[n]
			}
			return def
		}
		num := func(n int) float64 { v, _ := strconv.ParseFloat(arg(n, "0"), 64); return v }
		switch f[0] {
		case "key":
			for _, c := range f[1:] {
				i.chord(c)
			}
		case "type":
			i.typ(strings.TrimPrefix(sc.Text(), "type "))
		case "keysym":
			v, _ := strconv.ParseUint(strings.TrimPrefix(arg(1, "0"), "0x"), 16, 32)
			i.call("NotifyKeyboardKeysym", uint32(v), true)
			i.call("NotifyKeyboardKeysym", uint32(v), false)
		case "abs":
			i.call("NotifyPointerMotionRelative", -5000.0, -5000.0)
			i.call("NotifyPointerMotionRelative", num(1), num(2))
		case "rel":
			i.call("NotifyPointerMotionRelative", num(1), num(2))
		case "click":
			n, _ := strconv.Atoi(arg(2, "1"))
			for k := 0; k < n; k++ {
				i.call("NotifyPointerButton", int32(button(arg(1, "left"))), true)
				i.call("NotifyPointerButton", int32(button(arg(1, "left"))), false)
				time.Sleep(40 * time.Millisecond)
			}
		case "down":
			i.call("NotifyPointerButton", int32(button(arg(1, "left"))), true)
		case "up":
			i.call("NotifyPointerButton", int32(button(arg(1, "left"))), false)
		case "wheel":
			n, _ := strconv.Atoi(arg(2, "1"))
			dir := int32(-1)
			if arg(1, "up") == "down" {
				dir = 1
			}
			for k := 0; k < n; k++ {
				i.call("NotifyPointerAxisDiscrete", uint32(0), dir)
			}
		case "smooth": // smooth up|down N: pixel-style scroll (NotifyPointerAxis), like a trackpad or a high-res wheel
			n, _ := strconv.Atoi(arg(2, "1"))
			dy := 12.0
			if arg(1, "up") == "up" {
				dy = -12.0
			}
			for k := 0; k < n; k++ {
				i.call("NotifyPointerAxis", 0.0, dy, uint32(0))
			}
			i.call("NotifyPointerAxis", 0.0, 0.0, uint32(1)) // finish
		case "sleep":
			time.Sleep(time.Duration(num(1)) * time.Millisecond)
		case "quit":
			_ = i.s.Call("org.gnome.Mutter.RemoteDesktop.Session.Stop", 0).Err
			return
		}
		fmt.Println("done", sc.Text())
	}
}
