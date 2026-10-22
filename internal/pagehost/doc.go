// Package pagehost is the wire protocol between Studio and the page-window
// helper (docs/plans/html-artifacts.md, H4). The helper is the Studio binary in
// its --view-host mode: it owns a native web view for one scripted HTML page,
// serves the page from memory, and talks to Studio over a dedicated pair of
// pipes, one JSON object per line.
//
// The package is a leaf (standard library only) shared by both ends. It owns
// three things: the message shapes (Decode and Encode refuse a message whose
// type, fields or sizes are not exactly those of the protocol), the fixed error
// codes and their texts, and the rate limiter. No message has a field that
// names an artifact: Studio pins the artifact when it opens the window, so a
// page cannot ask for another one.
package pagehost
