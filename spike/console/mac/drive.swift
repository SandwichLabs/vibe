// SPIKE #257 - throwaway driver for the #257 macOS human checks: real HID-level input plus AX window/element control.
import AppKit
import ApplicationServices
import Carbon

let a = Array(CommandLine.arguments.dropFirst())
func die(_ s: String) -> Never { FileHandle.standardError.write((s + "\n").data(using: .utf8)!); exit(1) }
func pid(_ s: String) -> pid_t { guard let p = pid_t(s) else { die("bad pid") }; return p }
let src = CGEventSource(stateID: .hidSystemState)
func us(_ ms: Int) { usleep(useconds_t(ms * 1000)) }

func attr(_ e: AXUIElement, _ k: String) -> AnyObject? { var v: AnyObject?; return AXUIElementCopyAttributeValue(e, k as CFString, &v) == .success ? v : nil }
func frame(_ e: AXUIElement) -> CGRect? {
  guard let p = attr(e, kAXPositionAttribute), let s = attr(e, kAXSizeAttribute) else { return nil }
  var pt = CGPoint.zero, sz = CGSize.zero
  AXValueGetValue(p as! AXValue, .cgPoint, &pt); AXValueGetValue(s as! AXValue, .cgSize, &sz)
  return CGRect(origin: pt, size: sz)
}
func window(_ p: pid_t) -> AXUIElement {
  let app = AXUIElementCreateApplication(p)
  guard let ws = attr(app, kAXWindowsAttribute) as? [AXUIElement], let w = ws.first else { die("no window") }
  return w
}
func walk(_ e: AXUIElement, _ depth: Int = 0, _ f: (AXUIElement, Int) -> Bool) -> Bool {
  if f(e, depth) { return true }
  if depth > 40 { return false }
  for c in (attr(e, kAXChildrenAttribute) as? [AXUIElement]) ?? [] { if walk(c, depth + 1, f) { return true } }
  return false
}
func label(_ e: AXUIElement) -> String {
  [kAXTitleAttribute, kAXDescriptionAttribute, kAXValueAttribute].compactMap { attr(e, $0) as? String }.filter { !$0.isEmpty }.joined(separator: " | ")
}

let flagMap: [String: CGEventFlags] = ["cmd": .maskCommand, "opt": .maskAlternate, "ctrl": .maskControl, "shift": .maskShift, "fn": .maskSecondaryFn]
// Safety: keys only go to the app named by DRIVE_PID, and only while it is frontmost.
func guardFront() {
  guard let want = ProcessInfo.processInfo.environment["DRIVE_PID"].flatMap({ pid_t($0) }) else { die("DRIVE_PID not set") }
  let front = NSWorkspace.shared.frontmostApplication?.processIdentifier ?? -1
  if front != want { die("refusing: frontmost pid \(front) is not \(want)") }
}
func key(_ code: CGKeyCode, _ flags: CGEventFlags = []) {
  guardFront()
  let d = CGEvent(keyboardEventSource: src, virtualKey: code, keyDown: true)!; d.flags = flags; d.post(tap: .cghidEventTap); us(25)
  let u = CGEvent(keyboardEventSource: src, virtualKey: code, keyDown: false)!; u.flags = flags; u.post(tap: .cghidEventTap); us(40)
}
// US ANSI virtual key codes
let codes: [Character: (CGKeyCode, Bool)] = {
  var m: [Character: (CGKeyCode, Bool)] = [:]
  let base: [(String, CGKeyCode)] = [("a",0),("s",1),("d",2),("f",3),("h",4),("g",5),("z",6),("x",7),("c",8),("v",9),("b",11),("q",12),("w",13),("e",14),("r",15),("y",16),("t",17),("1",18),("2",19),("3",20),("4",21),("6",22),("5",23),("=",24),("9",25),("7",26),("-",27),("8",28),("0",29),("]",30),("o",31),("u",32),("[",33),("i",34),("p",35),("l",37),("j",38),("'",39),("k",40),(";",41),("\\",42),(",",43),("/",44),("n",45),("m",46),(".",47),(" ",49),("`",50)]
  for (s, c) in base { m[Character(s)] = (c, false); if let u = s.uppercased().first, u != Character(s) { m[u] = (c, true) } }
  for (s, c) in [("!",18),("@",19),("#",20),("$",21),("^",22),("%",23),("+",24),("(",25),("&",26),("_",27),("*",28),(")",29),("}",30),("{",33),("\"",39),(":",41),("|",42),("<",43),("?",44),(">",47),("~",50)] as [(String, CGKeyCode)] { m[Character(s)] = (c, true) }
  return m
}()
func mouse(_ t: CGEventType, _ p: CGPoint, _ b: CGMouseButton = .left, clicks: Int64 = 1) {
  let e = CGEvent(mouseEventSource: src, mouseType: t, mouseCursorPosition: p, mouseButton: b)!
  e.flags = []; e.setIntegerValueField(.mouseEventClickState, value: clicks); e.post(tap: .cghidEventTap)
}
func pt(_ x: String, _ y: String) -> CGPoint { CGPoint(x: Double(x)!, y: Double(y)!) }
func tis(_ id: String) -> TISInputSource {
  let f = [kTISPropertyInputSourceID as String: id] as CFDictionary
  guard let l = TISCreateInputSourceList(f, true)?.takeRetainedValue() as? [TISInputSource], let s = l.first else { die("no input source \(id)") }
  return s
}

switch a.first ?? "" {
case "win": // win PID -> x y w h
  let f = frame(window(pid(a[1])))!; print(Int(f.minX), Int(f.minY), Int(f.width), Int(f.height))
case "activate":
  let app = AXUIElementCreateApplication(pid(a[1]))
  AXUIElementSetAttributeValue(app, kAXFrontmostAttribute as CFString, kCFBooleanTrue)
  AXUIElementPerformAction(window(pid(a[1])), kAXRaiseAction as CFString); us(400)
  print("frontmost:", NSWorkspace.shared.frontmostApplication?.processIdentifier ?? -1)
case "front": print(NSWorkspace.shared.frontmostApplication?.processIdentifier ?? -1)
case "setframe": // setframe PID x y w h
  let w = window(pid(a[1])); var p = pt(a[2], a[3]); var s = CGSize(width: Double(a[4])!, height: Double(a[5])!)
  AXUIElementSetAttributeValue(w, kAXPositionAttribute as CFString, AXValueCreate(.cgPoint, &p)!)
  AXUIElementSetAttributeValue(w, kAXSizeAttribute as CFString, AXValueCreate(.cgSize, &s)!)
case "zoom":
  let b = attr(window(pid(a[1])), kAXZoomButtonAttribute) as! AXUIElement; print(AXUIElementPerformAction(b, kAXPressAction as CFString).rawValue)
case "fullscreen": // fullscreen PID 0|1
  print(AXUIElementSetAttributeValue(window(pid(a[1])), "AXFullScreen" as CFString, (a[2] == "1") as CFBoolean).rawValue)
case "axdump": // axdump PID [maxdepth]
  let md = a.count > 2 ? Int(a[2])! : 30
  _ = walk(window(pid(a[1]))) { e, d in
    if d <= md, let r = attr(e, kAXRoleAttribute) as? String { let f = frame(e) ?? .zero; print(String(repeating: " ", count: d) + r, "[\(label(e).prefix(60))]", Int(f.minX), Int(f.minY), Int(f.width), Int(f.height)) }
    return false
  }
case "axfind": // axfind PID ROLE SUBSTRING -> x y w h of first match
  var out: CGRect?
  _ = walk(window(pid(a[1]))) { e, _ in
    if (attr(e, kAXRoleAttribute) as? String) == a[2], label(e).contains(a[3]), let f = frame(e) { out = f; return true }; return false
  }
  guard let f = out else { die("not found") }; print(Int(f.minX), Int(f.minY), Int(f.width), Int(f.height))
case "menupress": // menupress PID SUBSTRING : press a menu item in any open menu of the app (context menus)
  let app = AXUIElementCreateApplication(pid(a[1])); var hit = false; var seen: [String] = []
  for root in [app] + ((attr(app, kAXWindowsAttribute) as? [AXUIElement]) ?? []) {
    if walk(root, 0, { e, _ in
      if (attr(e, kAXRoleAttribute) as? String) == "AXMenuItem" { let t = label(e); seen.append(t); if t.contains(a[2]) { AXUIElementPerformAction(e, kAXPressAction as CFString); return true } }
      return false
    }) { hit = true; break }
  }
  print(hit ? "pressed" : "not found; items: \(seen)")
case "closewin": // closewin PID TITLE_SUBSTRING
  let app = AXUIElementCreateApplication(pid(a[1])); var done = false
  for w in (attr(app, kAXWindowsAttribute) as? [AXUIElement]) ?? [] where ((attr(w, kAXTitleAttribute) as? String) ?? "").contains(a[2]) {
    if let b = attr(w, kAXCloseButtonAttribute) { AXUIElementPerformAction(b as! AXUIElement, kAXPressAction as CFString); done = true }
  }
  print(done ? "closed" : "not found")
case "move": mouse(.mouseMoved, pt(a[1], a[2]))
case "click": // click x y [right|double]
  let p = pt(a[1], a[2]); let mode = a.count > 3 ? a[3] : ""
  mouse(.mouseMoved, p); us(60)
  if mode == "right" { mouse(.rightMouseDown, p, .right); us(50); mouse(.rightMouseUp, p, .right) }
  else if mode == "double" { mouse(.leftMouseDown, p); mouse(.leftMouseUp, p); us(80); mouse(.leftMouseDown, p, clicks: 2); mouse(.leftMouseUp, p, clicks: 2) }
  else { mouse(.leftMouseDown, p); us(50); mouse(.leftMouseUp, p) }
case "drag": // drag x1 y1 x2 y2
  let p1 = pt(a[1], a[2]), p2 = pt(a[3], a[4])
  mouse(.mouseMoved, p1); us(80); mouse(.leftMouseDown, p1); us(80)
  for i in 1...20 { let t = Double(i) / 20; mouse(.leftMouseDragged, CGPoint(x: p1.x + (p2.x - p1.x) * t, y: p1.y + (p2.y - p1.y) * t)); us(20) }
  us(80); mouse(.leftMouseUp, p2)
case "key": // key CODE [cmd opt ctrl shift]
  key(CGKeyCode(a[1])!, a.dropFirst(2).reduce(into: CGEventFlags()) { $0.insert(flagMap[$1]!) })
case "type": // type TEXT (ASCII, US layout keycodes)
  for ch in a[1] { guard let (c, sh) = codes[ch] else { die("no keycode for \(ch)") }; key(c, sh ? .maskShift : []) }
case "scroll": // scroll trackpad DY STEPS | scroll wheel LINES STEPS  (negative = content up / view down)
  let dy = Int32(a[2])!, n = Int(a[3])!
  if a[1] == "trackpad" {
    for i in 0..<n {
      let e = CGEvent(scrollWheelEvent2Source: src, units: .pixel, wheelCount: 1, wheel1: dy, wheel2: 0, wheel3: 0)!
      e.setIntegerValueField(.scrollWheelEventIsContinuous, value: 1)
      e.setIntegerValueField(.scrollWheelEventScrollPhase, value: i == 0 ? 1 : 2) // began, changed
      e.post(tap: .cghidEventTap); us(16)
    }
    let end = CGEvent(scrollWheelEvent2Source: src, units: .pixel, wheelCount: 1, wheel1: 0, wheel2: 0, wheel3: 0)!
    end.setIntegerValueField(.scrollWheelEventIsContinuous, value: 1); end.setIntegerValueField(.scrollWheelEventScrollPhase, value: 4); end.post(tap: .cghidEventTap)
  } else {
    for _ in 0..<n { CGEvent(scrollWheelEvent2Source: src, units: .line, wheelCount: 1, wheel1: dy, wheel2: 0, wheel3: 0)!.post(tap: .cghidEventTap); us(50) }
  }
case "tis": // tis enable|disable|select|current ID
  switch a[1] {
  case "enable": print(TISEnableInputSource(tis(a[2])))
  case "disable": print(TISDisableInputSource(tis(a[2])))
  case "select": print(TISSelectInputSource(tis(a[2])))
  default: let c = TISCopyCurrentKeyboardInputSource().takeRetainedValue(); print(Unmanaged<CFString>.fromOpaque(TISGetInputSourceProperty(c, kTISPropertyInputSourceID)).takeUnretainedValue())
  }
default: die("usage: win|activate|setframe|zoom|fullscreen|axdump|axfind|menupress|move|click|drag|key|type|scroll|tis")
}
