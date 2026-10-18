// pagehost_mac.m: the macOS half of the P1 scripted-host spike (H4). NOT COMPILED OR RUN
// on the Linux machine that wrote it; the owner builds it on a Mac:
//
//   clang -fobjc-arc -Wall -o pagehost_mac pagehost_mac.m -framework Cocoa -framework WebKit
//
// It mirrors the Linux helper (spike/pagehost): it owns a WKWebView with
//   - a non-persistent data store (no cookies / localStorage / IndexedDB survive),
//   - a WKURLSchemeHandler for vibe-page:// that serves the page from memory and sends the
//     H4 CSP as an HTTP response header (to be measured: does WebKit honour it on a custom scheme),
//   - a WKNavigationDelegate that refuses everything except the page itself and same-page links,
//   - a WKUIDelegate that refuses new windows, media capture and dialogs,
//   - one WKScriptMessageHandler ("vibe") that checks frameInfo.isMainFrame,
//   - the host-injected `vibe` API as a WKUserScript (main frame only, document start),
//   - the protocol on inherited fd 3 (JSON lines, same as Linux: "P <json>" to the parent,
//     "R <id> <json>" and "Q" from the parent), and a 250 ms heartbeat posted by the page.
//
// usage: pagehost_mac -csp strict|permissive|none -via header|meta -group main|open|form|... -target 127.0.0.1:PORT
//                     -ttl SECONDS [-policy 0|1]
// Run it from parent_mac.py (same directory), which scrubs the environment, passes only fd 3, and
// wraps it in sandbox-exec -p '(version 1)(allow default)(deny network*)' for the network layer.
//
// The page files (index.html, probe.js, frame.html, libs/) live in probe_assets/ (-assets DIR). The
// placeholders TARGET / HTTPTARGET / DNSSUF / GROUP / META are substituted by THIS program at serve time
// (bytesForPath:), not by parent_mac.py. -dnssuf sets DNSSUF (use spike.test with /etc/resolver/spike.test).
// -proxy 1 sets WKWebsiteDataStore.proxyConfigurations to a dead loopback port (macOS 14+).

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <Network/Network.h>

static NSString *gCSP = @"";
static NSString *gVia = @"header";
static NSString *gGroup = @"main";
static NSString *gTarget = @"127.0.0.1:1";
static BOOL gPolicy = YES;
static NSTimeInterval gTTL = 12;
static NSString *gAssets = @"probe_assets";
static NSString *gDnsSuf = @"spike.test";
static BOOL gProxy = NO;
static NSFileHandle *gPipe;
static NSLock *gPipeLock;

static NSString *strictCSP = @"default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; worker-src blob:; frame-src 'none'; form-action 'none'; base-uri 'none'";
static NSString *permissiveCSP = @"default-src * vibe-page: data: blob: 'unsafe-inline' 'unsafe-eval'";

static void sendLine(NSString *line) {
  [gPipeLock lock];
  [gPipe writeData:[[line stringByAppendingString:@"\n"] dataUsingEncoding:NSUTF8StringEncoding]];
  [gPipeLock unlock];
}

@interface Host : NSObject <WKURLSchemeHandler, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler>
@property(strong) WKWebView *view;
@property(strong) NSWindow *window;
@property(copy) NSString *base; // vibe-page://app/
@end

@implementation Host

- (NSData *)bytesForPath:(NSString *)path contentType:(NSString **)ct {
  NSString *repl = nil;
  if ([path isEqualToString:@"/"] || path.length == 0) {
    NSString *meta = @"";
    NSString *csp = [gCSP isEqualToString:@""] ? @"" : gCSP;
    if (csp.length && [gVia isEqualToString:@"meta"])
      meta = [NSString stringWithFormat:@"<meta http-equiv=\"Content-Security-Policy\" content=\"%@\">", csp];
    NSString *h = [NSString stringWithContentsOfFile:[gAssets stringByAppendingString:@"/index.html"] encoding:NSUTF8StringEncoding error:nil];
    h = [h stringByReplacingOccurrencesOfString:@"META" withString:meta];
    h = [h stringByReplacingOccurrencesOfString:@"GROUP" withString:gGroup];
    repl = h; *ct = @"text/html; charset=utf-8";
  } else if ([path isEqualToString:@"/probe.js"]) {
    repl = [NSString stringWithContentsOfFile:[gAssets stringByAppendingString:@"/probe.js"] encoding:NSUTF8StringEncoding error:nil];
    *ct = @"text/javascript";
  } else if ([path isEqualToString:@"/frame.html"]) {
    repl = [NSString stringWithContentsOfFile:[gAssets stringByAppendingString:@"/frame.html"] encoding:NSUTF8StringEncoding error:nil];
    *ct = @"text/html; charset=utf-8";
  } else if ([path isEqualToString:@"/_vibe/theme.css"]) {
    repl = @":root{--vibe-bg:#fff}"; *ct = @"text/css";
  } else if ([path isEqualToString:@"/assets/x.json"]) {
    repl = @"{\"ok\":true}"; *ct = @"application/json";
  } else if ([path hasPrefix:@"/libs/"]) {
    repl = [NSString stringWithContentsOfFile:[NSString stringWithFormat:@"%@/libs/%@", gAssets, path.lastPathComponent] encoding:NSUTF8StringEncoding error:nil];
    *ct = @"text/javascript";
  }
  if (!repl) return nil;
  repl = [repl stringByReplacingOccurrencesOfString:@"HTTPTARGET" withString:[@"http://" stringByAppendingString:gTarget]];
  repl = [repl stringByReplacingOccurrencesOfString:@"TARGET" withString:gTarget];
  repl = [repl stringByReplacingOccurrencesOfString:@"DNSSUF" withString:gDnsSuf];
  return [repl dataUsingEncoding:NSUTF8StringEncoding];
}

// WKURLSchemeHandler
- (void)webView:(WKWebView *)webView startURLSchemeTask:(id<WKURLSchemeTask>)task {
  NSURL *url = task.request.URL;
  NSString *ct = nil;
  NSData *data = [self bytesForPath:url.path contentType:&ct];
  sendLine([NSString stringWithFormat:@"H scheme-request %@ -> %lu bytes", url.absoluteString, (unsigned long)data.length]);
  if (!data) { [task didFailWithError:[NSError errorWithDomain:NSURLErrorDomain code:NSURLErrorFileDoesNotExist userInfo:nil]]; return; }
  NSMutableDictionary *h = [@{ @"Content-Type": ct, @"Content-Length": [NSString stringWithFormat:@"%lu", (unsigned long)data.length],
                               @"X-Content-Type-Options": @"nosniff", @"Cache-Control": @"no-store" } mutableCopy];
  if (gCSP.length && ![gVia isEqualToString:@"meta"]) h[@"Content-Security-Policy"] = gCSP;
  NSHTTPURLResponse *resp = [[NSHTTPURLResponse alloc] initWithURL:url statusCode:200 HTTPVersion:@"HTTP/1.1" headerFields:h];
  [task didReceiveResponse:resp];
  [task didReceiveData:data];
  [task didFinish];
}
- (void)webView:(WKWebView *)webView stopURLSchemeTask:(id<WKURLSchemeTask>)task {}

// WKNavigationDelegate: the page and same-page links only
- (void)webView:(WKWebView *)webView decidePolicyForNavigationAction:(WKNavigationAction *)action decisionHandler:(void (^)(WKNavigationActionPolicy))handler {
  NSString *u = action.request.URL.absoluteString;
  BOOL allow = NO;
  if ([u hasPrefix:self.base]) {
    NSString *rest = [u substringFromIndex:self.base.length];
    allow = (rest.length == 0 || [rest hasPrefix:@"?"] || [rest hasPrefix:@"#"]);
  }
  sendLine([NSString stringWithFormat:@"H navigation %@ allow=%d policy_on=%d main=%d", u, allow, gPolicy, action.targetFrame.isMainFrame]);
  handler((!gPolicy || allow) ? WKNavigationActionPolicyAllow : WKNavigationActionPolicyCancel);
}
- (void)webView:(WKWebView *)webView decidePolicyForNavigationResponse:(WKNavigationResponse *)r decisionHandler:(void (^)(WKNavigationResponsePolicy))handler {
  handler(WKNavigationResponsePolicyAllow);
}
- (void)webViewWebContentProcessDidTerminate:(WKWebView *)webView { sendLine(@"H web-process-terminated"); [NSApp terminate:nil]; }

// WKUIDelegate: no new windows, no media capture, no dialogs
- (WKWebView *)webView:(WKWebView *)webView createWebViewWithConfiguration:(WKWebViewConfiguration *)c forNavigationAction:(WKNavigationAction *)a windowFeatures:(WKWindowFeatures *)f {
  sendLine([NSString stringWithFormat:@"H create-window %@ policy_on=%d", a.request.URL.absoluteString, gPolicy]);
  if (!gPolicy) { // control: behave like a browser
    NSWindow *w = [[NSWindow alloc] initWithContentRect:NSMakeRect(100, 100, 400, 300) styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable backing:NSBackingStoreBuffered defer:NO];
    WKWebView *nv = [[WKWebView alloc] initWithFrame:w.contentView.bounds configuration:c];
    w.contentView = nv; [w makeKeyAndOrderFront:nil]; return nv;
  }
  return nil;
}
- (void)webView:(WKWebView *)webView requestMediaCapturePermissionForOrigin:(WKSecurityOrigin *)o initiatedByFrame:(WKFrameInfo *)f type:(WKMediaCaptureType)t decisionHandler:(void (^)(WKPermissionDecision))h API_AVAILABLE(macos(12.0)) {
  sendLine(@"H permission media-capture");
  h(gPolicy ? WKPermissionDecisionDeny : WKPermissionDecisionPrompt);
}
- (void)webView:(WKWebView *)w runJavaScriptAlertPanelWithMessage:(NSString *)m initiatedByFrame:(WKFrameInfo *)f completionHandler:(void (^)(void))h { h(); }

// WKScriptMessageHandler: main frame only
- (void)userContentController:(WKUserContentController *)ucc didReceiveScriptMessage:(WKScriptMessage *)msg {
  if (!msg.frameInfo.isMainFrame) { sendLine(@"H message-from-subframe REFUSED"); return; }
  if ([msg.body isKindOfClass:[NSString class]]) sendLine([@"P " stringByAppendingString:msg.body]);
}
@end

// The page-side API: identical to the Linux apiScript.
static NSString *apiScript(void) {
  return @"(() => { const pend = new Map(); let n = 0;"
  "const post = (m) => window.webkit.messageHandlers.vibe.postMessage(JSON.stringify(m));"
  "Object.defineProperty(window, '__vibe_reply', { value: (id, r) => { const p = pend.get(id); if (p) { pend.delete(id); p(r); } } });"
  "Object.defineProperty(window, 'vibe', { value: Object.freeze({ run: (name, args) => new Promise((res) => { const id = ++n; pend.set(id, res); post({ t: 'run', id, name, args }); }), params: () => ({}), use: async () => null }) });"
  "Object.defineProperty(window, '__spike', { value: Object.freeze({ log: (o) => post({ t: 'log', o }) }) });"
  "setInterval(() => post({ t: 'hb', i: window.__inline, p: window.__p, rs: document.readyState }), 250);"
  "window.addEventListener('error', (e) => post({ t: 'log', o: { v: 'js-error', r: e.message + ' @' + e.filename + ':' + e.lineno } }));"
  "window.addEventListener('unhandledrejection', (e) => post({ t: 'log', o: { v: 'js-unhandled', r: String(e.reason) } }));"
  "document.addEventListener('securitypolicyviolation', (e) => post({ t: 'log', o: { v: 'csp-violation', r: e.violatedDirective + ' ' + e.blockedURI } })); })();";
}

int main(int argc, const char *argv[]) {
  @autoreleasepool {
    for (int i = 1; i + 1 < argc; i += 2) {
      NSString *k = @(argv[i]), *v = @(argv[i + 1]);
      if ([k isEqualToString:@"-csp"]) gCSP = [v isEqualToString:@"strict"] ? strictCSP : ([v isEqualToString:@"permissive"] ? permissiveCSP : @"");
      else if ([k isEqualToString:@"-via"]) gVia = v;
      else if ([k isEqualToString:@"-group"]) gGroup = v;
      else if ([k isEqualToString:@"-target"]) gTarget = v;
      else if ([k isEqualToString:@"-ttl"]) gTTL = v.doubleValue;
      else if ([k isEqualToString:@"-policy"]) gPolicy = v.boolValue;
      else if ([k isEqualToString:@"-assets"]) gAssets = v;
      else if ([k isEqualToString:@"-dnssuf"]) gDnsSuf = v;
      else if ([k isEqualToString:@"-proxy"]) gProxy = v.boolValue;
    }
    gPipe = [[NSFileHandle alloc] initWithFileDescriptor:3 closeOnDealloc:NO];
    gPipeLock = [NSLock new];
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory]; // no second Dock icon

    Host *host = [Host new];
    host.base = @"vibe-page://app/";
    WKWebViewConfiguration *cfg = [WKWebViewConfiguration new];
    cfg.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];
    if (gProxy) {
      // Public API, macOS 14+: send everything through an HTTP CONNECT proxy on a dead loopback port, so
      // host names are not resolved locally. UDP (WebRTC/STUN) bypasses proxies: keep WebRTC off below.
      if (@available(macOS 14.0, *)) {
        nw_endpoint_t ep = nw_endpoint_create_host("127.0.0.1", "9");
        nw_proxy_config_t pc = nw_proxy_config_create_http_connect(ep, NULL);
        cfg.websiteDataStore.proxyConfigurations = @[ pc ];
        sendLine(@"H proxyConfigurations set -> 127.0.0.1:9");
      } else sendLine(@"H proxyConfigurations unavailable (< macOS 14)");
    }
    [cfg setURLSchemeHandler:host forURLScheme:@"vibe-page"];
    WKUserContentController *ucc = [WKUserContentController new];
    [ucc addScriptMessageHandler:host name:@"vibe"];
    WKUserScript *us = [[WKUserScript alloc] initWithSource:apiScript() injectionTime:WKUserScriptInjectionTimeAtDocumentStart forMainFrameOnly:YES];
    [ucc addUserScript:us];
    cfg.userContentController = ucc;
    // WebRTC / media: there is no public switch. Private SPI (record whether it exists on the tested OS):
    //   [cfg.preferences setValue:@NO forKey:@"peerConnectionEnabled"]; [cfg.preferences setValue:@NO forKey:@"mediaDevicesEnabled"];
    @try { [cfg.preferences setValue:@NO forKey:@"peerConnectionEnabled"]; sendLine(@"H spi peerConnectionEnabled=NO ok"); } @catch (NSException *e) { sendLine(@"H spi peerConnectionEnabled unavailable"); }
    @try { [cfg.preferences setValue:@NO forKey:@"mediaDevicesEnabled"]; sendLine(@"H spi mediaDevicesEnabled=NO ok"); } @catch (NSException *e) { sendLine(@"H spi mediaDevicesEnabled unavailable"); }
    @try { [cfg.preferences setValue:@NO forKey:@"developerExtrasEnabled"]; } @catch (NSException *e) {}

    host.window = [[NSWindow alloc] initWithContentRect:NSMakeRect(200, 200, 640, 480) styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskResizable backing:NSBackingStoreBuffered defer:NO];
    host.window.title = @"Spike page host"; // fixed native title; document.title must not change it
    host.view = [[WKWebView alloc] initWithFrame:host.window.contentView.bounds configuration:cfg];
    host.view.navigationDelegate = host;
    host.view.UIDelegate = host;
    host.window.contentView = host.view;
    [host.window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
    [host.view loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:[NSString stringWithFormat:@"vibe-page://app/?g=%@", gGroup]]]];

    // log the page title vs window title once a second
    [NSTimer scheduledTimerWithTimeInterval:1 repeats:YES block:^(NSTimer *t) {
      sendLine([NSString stringWithFormat:@"H title doc.title=%@ window.title=%@", host.view.title, host.window.title]);
    }];
    // parent -> helper lines: "R <id> <json>" reply, "Q" quit
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
      NSMutableData *buf = [NSMutableData data];
      while (YES) {
        NSData *d = [gPipe availableData];
        if (d.length == 0) { dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; }); return; } // pipe closed: parent is gone
        [buf appendData:d];
        NSString *all = [[NSString alloc] initWithData:buf encoding:NSUTF8StringEncoding];
        NSArray *lines = [all componentsSeparatedByString:@"\n"];
        NSString *last = lines.lastObject; buf = [[last dataUsingEncoding:NSUTF8StringEncoding] mutableCopy];
        for (NSUInteger i = 0; i + 1 < lines.count; i++) {
          NSString *l = lines[i];
          if ([l isEqualToString:@"Q"]) { dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; }); return; }
          if ([l hasPrefix:@"R "]) {
            NSArray *p = [[l substringFromIndex:2] componentsSeparatedByString:@" "];
            NSString *js = [NSString stringWithFormat:@"window.__vibe_reply(%@, %@)", p[0], [[p subarrayWithRange:NSMakeRange(1, p.count - 1)] componentsJoinedByString:@" "]];
            dispatch_async(dispatch_get_main_queue(), ^{ [host.view evaluateJavaScript:js completionHandler:nil]; });
          }
        }
      }
    });
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(gTTL * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{ sendLine(@"H ttl-expired"); [NSApp terminate:nil]; });
    [NSApp run];
  }
  return 0;
}
