// usage: click <app-name-substring>: activates the app, then clicks the centre of its largest window
import Cocoa
let want = CommandLine.arguments.count > 1 ? CommandLine.arguments[1].lowercased() : "vibe"
guard let app = NSWorkspace.shared.runningApplications.first(where: { ($0.localizedName ?? "").lowercased().contains(want) || ($0.executableURL?.lastPathComponent ?? "").lowercased().contains(want) }) else {
  print("no app for \(want)"); exit(1)
}
app.activate(options: [.activateAllWindows])
usleep(800000)
let info = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]
let mine = info.filter { ($0[kCGWindowOwnerPID as String] as? Int32) == app.processIdentifier && ($0[kCGWindowLayer as String] as? Int) == 0 }
guard let w = mine.max(by: { (($0[kCGWindowBounds as String] as! [String: CGFloat])["Width"]!) < (($1[kCGWindowBounds as String] as! [String: CGFloat])["Width"]!) }),
      let b = w[kCGWindowBounds as String] as? [String: CGFloat] else {
  print("app \(app.localizedName ?? "?") pid \(app.processIdentifier) has no on-screen window; owners: " + info.map { "\($0[kCGWindowOwnerName as String] ?? "?")" }.joined(separator: ",")); exit(1)
}
let p = CGPoint(x: b["X"]! + b["Width"]! / 2, y: b["Y"]! + b["Height"]! / 2)
for t in [CGEventType.leftMouseDown, .leftMouseUp] {
  CGEvent(mouseEventSource: nil, mouseType: t, mouseCursorPosition: p, mouseButton: .left)?.post(tap: .cghidEventTap)
  usleep(50000)
}
print("clicked \(app.localizedName ?? "?") at \(p) trusted=\(AXIsProcessTrusted())")
