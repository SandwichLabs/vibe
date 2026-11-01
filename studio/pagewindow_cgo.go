//go:build (linux && webkit2_41) || darwin

package main

/*
#cgo linux pkg-config: webkit2gtk-4.1 gtk+-3.0 libsoup-3.0
#cgo darwin CFLAGS: -fobjc-arc -Wall
#cgo darwin LDFLAGS: -framework Cocoa -framework WebKit -framework Network
#include <stdlib.h>
#include "pagewindow.h"
*/
import "C"

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"

	"github.com/SandwichLabs/assay/internal/pagehost"
)

// nativeAvailable says this binary has a native page window: WebKitGTK 4.1 on
// Linux (pagewindow_linux.c), WKWebView on macOS (pagewindow_darwin.m). Both
// implement the four calls in pagewindow.h the same way.
const nativeAvailable = true

func init() {
	// GTK wants every call on the thread that initialised it: the main one.
	runtime.LockOSThread()
}

var (
	nativeMu    sync.Mutex
	activeHooks nativeHooks
)

func currentHooks() nativeHooks {
	nativeMu.Lock()
	defer nativeMu.Unlock()
	return activeHooks
}

// nativeRun opens the window and runs the GTK loop until it ends.
func nativeRun(cfg nativeConfig, hk nativeHooks) error {
	nativeMu.Lock()
	activeHooks = hk
	nativeMu.Unlock()
	prefix := "assay-page://" + cfg.name + "/"
	start := prefix
	if cfg.query != "" {
		start += "?" + cfg.query
	}
	cPrefix := C.CString(prefix)
	cStart := C.CString(start)
	cTitle := C.CString(cfg.title)
	cScript := C.CString(cfg.script)
	defer C.free(unsafe.Pointer(cPrefix))
	defer C.free(unsafe.Pointer(cStart))
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cScript))
	switch C.assay_host_run(cStart, cPrefix, cTitle, C.int(cfg.width), C.int(cfg.height), cScript) {
	case 0:
		return nil
	case 2:
		return errors.New("no display is available for a page window")
	default:
		return errors.New("the page window could not be set up")
	}
}

// nativeRespond answers a scheme request (any goroutine). ok=false fails the
// request as not found.
func nativeRespond(id uint64, ok bool, ct string, body []byte) {
	cCT := C.CString(ct)
	cCSP := C.CString(pagehost.CSP)
	defer C.free(unsafe.Pointer(cCT))
	defer C.free(unsafe.Pointer(cCSP))
	var p *C.char
	if len(body) > 0 {
		p = (*C.char)(unsafe.Pointer(&body[0]))
	}
	good := C.int(0)
	if ok {
		good = 1
	}
	C.assay_host_respond(C.ulong(id), good, cCT, cCSP, p, C.int(len(body)))
}

// nativeEval runs script in the page (any goroutine).
func nativeEval(js string) {
	c := C.CString(js)
	defer C.free(unsafe.Pointer(c))
	C.assay_host_eval(c)
}

// nativeTakeInput reports and consumes the person's last physical input in the
// window, within 5 seconds (any goroutine).
func nativeTakeInput() bool { return C.assay_host_take_input() != 0 }

// nativeQuit ends the loop (any goroutine).
func nativeQuit() { C.assay_host_quit() }

//export assayPageServe
func assayPageServe(uri *C.char, id C.ulong) {
	if h := currentHooks(); h != nil {
		h.serve(uint64(id), C.GoString(uri))
	}
}

//export assayPageMessage
func assayPageMessage(s *C.char) {
	if h := currentHooks(); h != nil {
		h.message(C.GoString(s))
	}
}

//export assayPageEvent
func assayPageEvent(kind C.int) {
	if h := currentHooks(); h != nil {
		h.event(nativeEvent(kind))
	}
}
