// montage OUT COLS CELLW x y w h FILE... : crop the same rect (pixels) from each file, tile into a grid, scale to CELLW wide
import AppKit
let a = CommandLine.arguments; let out = a[1]; let cols = Int(a[2])!; let cw = Double(a[3])!
let r = CGRect(x: Double(a[4])!, y: Double(a[5])!, width: Double(a[6])!, height: Double(a[7])!)
let files = Array(a[8...]); let ch = cw * r.height / r.width; let rows = (files.count + cols - 1) / cols
let ctx = CGContext(data: nil, width: Int(cw) * cols, height: Int(ch + 20) * rows, bitsPerComponent: 8, bytesPerRow: 0, space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
ctx.setFillColor(CGColor(gray: 1, alpha: 1)); ctx.fill(CGRect(x: 0, y: 0, width: ctx.width, height: ctx.height))
for (i, f) in files.enumerated() {
  guard let src = CGImageSourceCreateWithURL(URL(fileURLWithPath: f) as CFURL, nil), let img = CGImageSourceCreateImageAtIndex(src, 0, nil), let c = img.cropping(to: r) else { continue }
  let col = i % cols, row = i / cols; let y = Double(ctx.height) - Double(row + 1) * (ch + 20)
  ctx.draw(c, in: CGRect(x: Double(col) * cw, y: y, width: cw, height: ch))
  NSGraphicsContext.current = NSGraphicsContext(cgContext: ctx, flipped: false)
  (URL(fileURLWithPath: f).lastPathComponent as NSString).draw(at: CGPoint(x: Double(col) * cw + 4, y: y + ch + 3), withAttributes: [.font: NSFont.systemFont(ofSize: 12), .foregroundColor: NSColor.black])
}
let rep = NSBitmapImageRep(cgImage: ctx.makeImage()!); try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
