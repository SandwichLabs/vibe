package main

// The seam between the helper's platform-independent logic (pagehelper.go) and
// the native web view (pagewindow_linux.go, pagewindow_darwin.go, and a stub
// where there is none). The native side owns a window, a web view, a custom
// scheme and a navigation policy; everything it needs to decide is passed in,
// and everything the page does reaches Go only through these four calls.

// nativeConfig is what a window is opened with. Everything in it came from
// Studio, validated by the protocol.
type nativeConfig struct {
	// name is the host of the page's one origin, vibe-page://<name>/.
	name string
	// title is the fixed native title: "<artifact title> — Vibe page".
	title         string
	width, height int
	// script is the injected page API (pagehost.APIScript).
	script string
	// query is the raw query string of the first document ("" for none).
	query string
}

// nativeEvent is what the window reports.
type nativeEvent int

const (
	// evLoaded: the first document finished loading.
	evLoaded nativeEvent = iota + 1
	// evWebGone: the web process died.
	evWebGone
	// evClosed: the person closed the window.
	evClosed
)

// nativeHooks is what the helper gives the native view to call.
type nativeHooks interface {
	// serve is a request to the custom scheme; the answer comes, from any
	// goroutine, through nativeRespond with the same id.
	serve(id uint64, rawURL string)
	// message is a string the page posted to its one message handler. It is
	// untrusted text.
	message(s string)
	// event reports a window event.
	event(ev nativeEvent)
}

// nativeOps is the helper's calls into the running window, behind an interface
// so the helper's logic can be tested without one.
type nativeOps interface {
	respond(id uint64, ok bool, ct string, body []byte)
	eval(js string)
	quit()
	// takeInput reports and consumes a physical input of the person in the last
	// 5 seconds (the gesture of the download capability, seen outside the page).
	takeInput() bool
}

// realNative calls the platform's window.
type realNative struct{}

func (realNative) respond(id uint64, ok bool, ct string, body []byte) {
	nativeRespond(id, ok, ct, body)
}
func (realNative) eval(js string)  { nativeEval(js) }
func (realNative) quit()           { nativeQuit() }
func (realNative) takeInput() bool { return nativeTakeInput() }
