// axq: QA eyes and hands through the Accessibility API (no Screen Recording needed).
//   axq dump <pid> [depth]            windows and their element tree
//   axq find <pid> <text>             elements whose title/value/description contains text
//   axq click <pid> <text> [n]        real CGEvent click at the centre of the n-th match (default 0)
//   axq clickat <x> <y>               real CGEvent click at a screen point
//   axq key <keycode> [cmd|shift|...] real CGEvent key press (down+up)
//   axq hold <keycode> <ms>           key down, autorepeat events for ms, key up
//   axq windows                       on-screen windows (owner, pid, layer, bounds; names need Screen Recording)
//   axq front <pid>                   activate the app
import Cocoa

func attr(_ e: AXUIElement, _ a: String) -> AnyObject? {
  var v: AnyObject?
  return AXUIElementCopyAttributeValue(e, a as CFString, &v) == .success ? v : nil
}
func str(_ e: AXUIElement, _ a: String) -> String {
  guard let v = attr(e, a) else { return "" }
  if let s = v as? String { return s }
  if let n = v as? NSNumber { return n.stringValue }
  return ""
}
func frame(_ e: AXUIElement) -> CGRect? {
  guard let p = attr(e, kAXPositionAttribute), let s = attr(e, kAXSizeAttribute) else { return nil }
  var pt = CGPoint.zero, sz = CGSize.zero
  AXValueGetValue(p as! AXValue, .cgPoint, &pt); AXValueGetValue(s as! AXValue, .cgSize, &sz)
  return CGRect(origin: pt, size: sz)
}
func kids(_ e: AXUIElement) -> [AXUIElement] { (attr(e, kAXChildrenAttribute) as? [AXUIElement]) ?? [] }
func label(_ e: AXUIElement) -> String {
  [str(e, kAXTitleAttribute), str(e, kAXValueAttribute), str(e, kAXDescriptionAttribute), str(e, "AXHelp")]
    .filter { !$0.isEmpty }.joined(separator: " | ")
}
func line(_ e: AXUIElement, _ d: Int) -> String {
  let f = frame(e).map { "(\(Int($0.minX)),\(Int($0.minY)) \(Int($0.width))x\(Int($0.height)))" } ?? ""
  let sr = str(e, kAXSubroleAttribute)
  return String(repeating: "  ", count: d) + str(e, kAXRoleAttribute) + (sr.isEmpty ? "" : "/" + sr) + " " + f + " " + label(e).replacingOccurrences(of: "\n", with: "⏎").prefix(ProcessInfo.processInfo.environment["AXFULL"] != nil ? 100000 : 300)
}
func walk(_ e: AXUIElement, _ d: Int, _ max: Int, _ f: (AXUIElement, Int) -> Void) {
  f(e, d); if d >= max { return }
  for k in kids(e) { walk(k, d + 1, max, f) }
}
func app(_ s: String) -> AXUIElement {
  let a = AXUIElementCreateApplication(pid_t(Int32(s)!))
  AXUIElementSetAttributeValue(a, "AXEnhancedUserInterface" as CFString, kCFBooleanTrue)
  AXUIElementSetAttributeValue(a, "AXManualAccessibility" as CFString, kCFBooleanTrue)
  return a
}
func click(_ p: CGPoint) {
  CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: p, mouseButton: .left)?.post(tap: .cghidEventTap)
  usleep(80000)
  for t in [CGEventType.leftMouseDown, .leftMouseUp] {
    CGEvent(mouseEventSource: nil, mouseType: t, mouseCursorPosition: p, mouseButton: .left)?.post(tap: .cghidEventTap)
    usleep(60000)
  }
  print("clicked \(p) trusted=\(AXIsProcessTrusted())")
}
let a = CommandLine.arguments
switch a.count > 1 ? a[1] : "" {
case "dump":
  let ap = app(a[2]); usleep(300000)
  for w in (attr(ap, kAXWindowsAttribute) as? [AXUIElement]) ?? [] {
    walk(w, 0, a.count > 3 ? Int(a[3])! : 30) { e, d in print(line(e, d)) }
  }
case "find", "click":
  let ap = app(a[2]); usleep(300000); let want = a[3].lowercased()
  var hits: [AXUIElement] = []
  for w in (attr(ap, kAXWindowsAttribute) as? [AXUIElement]) ?? [] {
    walk(w, 0, 60) { e, _ in if label(e).lowercased().contains(want) { hits.append(e) } }
  }
  for (i, h) in hits.enumerated() { print("[\(i)] " + line(h, 0)) }
  if a[1] == "click" {
    let n = a.count > 4 ? Int(a[4])! : 0
    guard n < hits.count, let f = frame(hits[n]) else { print("no match"); exit(1) }
    click(CGPoint(x: f.midX, y: f.midY))
  }
case "clickat": click(CGPoint(x: Double(a[2])!, y: Double(a[3])!))
case "rclick": // rclick <x> <y>: real right click
  let p = CGPoint(x: Double(a[2])!, y: Double(a[3])!)
  CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: p, mouseButton: .right)?.post(tap: .cghidEventTap); usleep(80000)
  for t in [CGEventType.rightMouseDown, .rightMouseUp] { CGEvent(mouseEventSource: nil, mouseType: t, mouseCursorPosition: p, mouseButton: .right)?.post(tap: .cghidEventTap); usleep(60000) }
  print("right-clicked \(p)")
case "menus": // menus <pid>: any open menus (AXMenu) in the app, and the focused window
  let ap = app(a[2]); usleep(200000)
  if let fw = attr(ap, kAXFocusedWindowAttribute) { print("focused window: " + str(fw as! AXUIElement, kAXTitleAttribute)) } else { print("focused window: none") }
  walk(ap, 0, 8) { e, d in if str(e, kAXRoleAttribute) == "AXMenu" || str(e, kAXRoleAttribute) == "AXMenuBar" { print(line(e, d)); for k in kids(e) { print(line(k, d + 1)) } } }
case "scroll": // scroll <x> <y> <lines> (negative = down)
  CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: CGPoint(x: Double(a[2])!, y: Double(a[3])!), mouseButton: .left)?.post(tap: .cghidEventTap)
  usleep(100000)
  CGEvent(scrollWheelEvent2Source: nil, units: .line, wheelCount: 1, wheel1: Int32(a[4])!, wheel2: 0, wheel3: 0)?.post(tap: .cghidEventTap)
  usleep(300000); print("scrolled")
case "drag": // drag <x1> <y1> <x2> <y2>
  let p1 = CGPoint(x: Double(a[2])!, y: Double(a[3])!), p2 = CGPoint(x: Double(a[4])!, y: Double(a[5])!)
  CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: p1, mouseButton: .left)?.post(tap: .cghidEventTap); usleep(100000)
  CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown, mouseCursorPosition: p1, mouseButton: .left)?.post(tap: .cghidEventTap); usleep(100000)
  for i in 1...20 {
    let t = Double(i) / 20
    CGEvent(mouseEventSource: nil, mouseType: .leftMouseDragged, mouseCursorPosition: CGPoint(x: p1.x + (p2.x - p1.x) * t, y: p1.y + (p2.y - p1.y) * t), mouseButton: .left)?.post(tap: .cghidEventTap); usleep(30000)
  }
  CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp, mouseCursorPosition: p2, mouseButton: .left)?.post(tap: .cghidEventTap)
  print("dragged \(p1) -> \(p2)")
case "key", "hold":
  let code = CGKeyCode(Int(a[2])!)
  var flags: CGEventFlags = []
  if a[1] == "key" { for m in a.dropFirst(3) { switch m { case "cmd": flags.insert(.maskCommand); case "shift": flags.insert(.maskShift); case "alt": flags.insert(.maskAlternate); case "ctrl": flags.insert(.maskControl); default: break } } }
  let d = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: true)!; d.flags = flags; d.post(tap: .cghidEventTap)
  if a[1] == "hold" {
    let end = Date().addingTimeInterval(Double(a[3])! / 1000); usleep(500000)
    var n = 0
    while Date() < end { let r = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: true)!; r.setIntegerValueField(.keyboardEventAutorepeat, value: 1); r.post(tap: .cghidEventTap); n += 1; usleep(50000) }
    print("repeats=\(n)")
  } else { usleep(60000) }
  let u = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: false)!; u.flags = flags; u.post(tap: .cghidEventTap)
  print("key \(code) \(a[1]) trusted=\(AXIsProcessTrusted())")
case "type": // type <text>: one real key event pair per character (ANSI key codes; ':' is shift+';')
  let codes: [Character: (CGKeyCode, Bool)] = [
    "a": (0, false), "s": (1, false), "d": (2, false), "f": (3, false), "h": (4, false), "g": (5, false), "z": (6, false), "x": (7, false),
    "c": (8, false), "v": (9, false), "b": (11, false), "q": (12, false), "w": (13, false), "e": (14, false), "r": (15, false), "y": (16, false),
    "t": (17, false), "1": (18, false), "2": (19, false), "3": (20, false), "4": (21, false), "6": (22, false), "5": (23, false), "=": (24, false),
    "9": (25, false), "7": (26, false), "-": (27, false), "8": (28, false), "0": (29, false), "o": (31, false), "u": (32, false), "i": (34, false),
    "p": (35, false), "l": (37, false), "j": (38, false), "k": (40, false), ";": (41, false), ":": (41, true), "n": (45, false), "m": (46, false),
    ".": (47, false), " ": (49, false), "/": (44, false)]
  for ch in a[2] {
    guard let (code, shift) = codes[ch] else { print("no key for \(ch)"); exit(1) }
    for down in [true, false] {
      let e = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: down)!
      e.flags = shift ? .maskShift : []
      e.post(tap: .cghidEventTap); usleep(20000)
    }
  }
  print("typed \(a[2].count) chars")
case "pids": // pids <name-substring>: pids of running GUI apps
  for r in NSWorkspace.shared.runningApplications where (r.executableURL?.path ?? "").contains(a[2]) {
    print("\(r.processIdentifier) policy=\(r.activationPolicy.rawValue) \(r.localizedName ?? "") \(r.executableURL?.path ?? "")")
  }
case "windows":
  let info = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]
  for w in info where (w[kCGWindowLayer as String] as? Int) == 0 {
    print("\(w[kCGWindowOwnerName as String] ?? "?") pid=\(w[kCGWindowOwnerPID as String] ?? 0) name=\(w[kCGWindowName as String] ?? "-") bounds=\(w[kCGWindowBounds as String] ?? "")")
  }
case "front":
  NSRunningApplication(processIdentifier: pid_t(Int32(a[2])!))?.activate(options: [.activateAllWindows]); usleep(500000); print("activated")
default: print("usage: see header")
}
