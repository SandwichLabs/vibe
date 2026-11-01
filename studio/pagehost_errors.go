package main

import "errors"

// Fixed refusals of a page window: what the person sees, never a path, an
// errno or a policy reason (the cause goes to the debug log).
var (
	errPageWindowUnavailable = errors.New("page windows are not available in this build")
	errPageNoGate            = errors.New("this system cannot isolate a page window (it needs bubblewrap and unprivileged user namespaces), so the page cannot be opened")
	errPageNoDisplay         = errors.New("a page window needs a Wayland session")
	errPageNeedsRestart      = errors.New("Assay was replaced while it was running; restart it to open page windows")
	errPageWindowSetup       = errors.New("the page window could not be set up")
	errPageNotScripted       = errors.New("this page has no script, so it is shown in the page view")
	errPageTooLarge          = errors.New("the page is too large to open")
	errPageTooMany           = errors.New("too many page windows are open")
)
