//go:build darwin

// The macOS page window (docs/plans/html-artifacts.md, H4, Spike P1): a WKWebView
// Vibe owns, in the helper process only. NOT COMPILED OR RUN where it was written
// (a Linux machine): it follows the P1 spike's working helper
// (spike/pagehost/mac/followup/pagehost_rules.m), and P4-MAC-QA.md lists what a Mac
// must check.
//
// WebKit's Networking process is started by launchd, not by the helper, so no
// namespace applies and sandbox-exec is not used. The network is held shut by four
// layers, all set up before the first load:
//   1. the strict CSP on every response of the page's scheme;
//   2. a navigation policy that lets through the first page and same-page links;
//   3. a WKContentRuleList that blocks every URL except vibe-page:, compiled
//      first, with the first load waiting for the compile callback;
//   4. a non-persistent data store whose proxyConfigurations point at a dead
//      loopback port (macOS 14+), so host names are not resolved locally.
// One leak is accepted and disclosed (#189): a link to an IP address that page
// script clicks opens an empty TCP connection, because IP addresses bypass the
// proxy and WebKit preconnects before the rules or the policy apply.
// WebRTC and media devices are off through private preferences (settings, not a layer).

#import <Cocoa/Cocoa.h>
#include <stdatomic.h>
#include <time.h>
#import <WebKit/WebKit.h>
#import <Network/Network.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include "pagewindow.h"
#include "_cgo_export.h"

#define EV_LOADED 1
#define EV_WEBGONE 2
#define EV_CLOSED 3

// Exit codes of a helper that cannot make its layers (Studio sees "stopped").
#define EXIT_NO_PROXY_API 4
#define EXIT_RULES_FAILED 5
#define EXIT_NO_PROXY_PORT 6

static NSString *gRulesDir = nil;
static id gActivity = nil; // keeps the helper out of App Nap while the window lives

// The throwaway rule-list store lives in the temp directory; remove it when the process exits.
static void removeRulesDir(void) {
  if (gRulesDir) [[NSFileManager defaultManager] removeItemAtPath:gRulesDir error:nil];
}

// The proxy the data store points at must refuse every connection at once, which is what
// P1 measured (port 9 with nothing listening: an immediate reset). Instead of trusting a
// fixed port (port 9 could be anyone's), bind a loopback socket of our own and never
// listen on it: a bound, non-listening port resets every connect, and nobody else can take
// it while we hold it. Never listen(): a listener that never accepts makes connects hang
// instead, a timeout path P1 never measured, which could fail over to a direct connection.
// The socket is close-on-exec and lives as long as the helper.
static int deadProxyPort(void) {
  int fd = socket(AF_INET, SOCK_STREAM, 0);
  if (fd < 0) return 0;
  fcntl(fd, F_SETFD, FD_CLOEXEC);
  struct sockaddr_in a;
  memset(&a, 0, sizeof a);
  a.sin_family = AF_INET;
  a.sin_len = sizeof a;
  a.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
  socklen_t len = sizeof a;
  if (bind(fd, (struct sockaddr *)&a, sizeof a) != 0 || getsockname(fd, (struct sockaddr *)&a, &len) != 0) {
    close(fd);
    return 0;
  }
  return ntohs(a.sin_port);
}

// What the rule list blocks: every load, then the page's own scheme is let back in.
static NSString *const kBlockRules =
    @"[{\"trigger\":{\"url-filter\":\".*\"},\"action\":{\"type\":\"block\"}},"
     "{\"trigger\":{\"url-filter\":\"^vibe-page:\"},\"action\":{\"type\":\"ignore-previous-rules\"}}]";

// A WKWebView with no context menu: nothing in it is the page's to offer.
@interface VibeWebView : WKWebView
@end
@implementation VibeWebView
- (void)willOpenMenu:(NSMenu *)menu withEvent:(NSEvent *)event {
  [menu removeAllItems];
}
@end

@interface VibeHost : NSObject <WKURLSchemeHandler, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler, NSWindowDelegate>
@property(strong) VibeWebView *view;
@property(strong) NSWindow *window;
@property(copy) NSString *prefix;
@property(copy) NSURL *startURL;
@property(strong) NSMutableDictionary<NSNumber *, id<WKURLSchemeTask>> *pending;
@property(assign) unsigned long nextID;
@property(assign) BOOL loaded;
@end

static VibeHost *gHost = nil;

@implementation VibeHost

- (BOOL)isPageURL:(NSString *)u {
  if (![u hasPrefix:self.prefix]) return NO;
  NSString *rest = [u substringFromIndex:self.prefix.length];
  return rest.length == 0 || [rest hasPrefix:@"?"] || [rest hasPrefix:@"#"];
}

#pragma mark WKURLSchemeHandler

- (void)webView:(WKWebView *)webView startURLSchemeTask:(id<WKURLSchemeTask>)task {
  unsigned long ident = ++self.nextID;
  self.pending[@(ident)] = task;
  vibePageServe((char *)[task.request.URL.absoluteString UTF8String], ident);
}

- (void)webView:(WKWebView *)webView stopURLSchemeTask:(id<WKURLSchemeTask>)task {
  // A stopped task must not be touched again: forget it.
  for (NSNumber *k in [self.pending allKeysForObject:task]) [self.pending removeObjectForKey:k];
}

- (void)respond:(unsigned long)ident ok:(BOOL)ok ctype:(NSString *)ctype csp:(NSString *)csp body:(NSData *)body {
  id<WKURLSchemeTask> task = self.pending[@(ident)];
  if (!task) return;
  [self.pending removeObjectForKey:@(ident)];
  if (!ok) {
    [task didFailWithError:[NSError errorWithDomain:NSURLErrorDomain code:NSURLErrorFileDoesNotExist userInfo:nil]];
    return;
  }
  // Content-Type must be in the headers (a script is skipped under nosniff without it).
  NSMutableDictionary *h = [@{
    @"Content-Type" : ctype,
    @"Content-Length" : [NSString stringWithFormat:@"%lu", (unsigned long)body.length],
    @"X-Content-Type-Options" : @"nosniff",
    @"Cache-Control" : @"no-store"
  } mutableCopy];
  if (csp.length) h[@"Content-Security-Policy"] = csp;
  NSHTTPURLResponse *resp = [[NSHTTPURLResponse alloc] initWithURL:task.request.URL statusCode:200 HTTPVersion:@"HTTP/1.1" headerFields:h];
  [task didReceiveResponse:resp];
  [task didReceiveData:body];
  [task didFinish];
}

#pragma mark WKNavigationDelegate

// Only the first page and links within it ("?query" or "#fragment"), in the main frame, by GET.
- (void)webView:(WKWebView *)webView decidePolicyForNavigationAction:(WKNavigationAction *)action decisionHandler:(void (^)(WKNavigationActionPolicy))handler {
  BOOL allow = action.targetFrame != nil && action.targetFrame.isMainFrame &&
               [self isPageURL:action.request.URL.absoluteString] &&
               (action.request.HTTPMethod == nil || [action.request.HTTPMethod isEqualToString:@"GET"]);
  handler(allow ? WKNavigationActionPolicyAllow : WKNavigationActionPolicyCancel);
}

// A response that is not ours, or that WebKit would download, is refused.
- (void)webView:(WKWebView *)webView decidePolicyForNavigationResponse:(WKNavigationResponse *)r decisionHandler:(void (^)(WKNavigationResponsePolicy))handler {
  BOOL ok = r.canShowMIMEType && [r.response.URL.absoluteString hasPrefix:self.prefix];
  handler(ok ? WKNavigationResponsePolicyAllow : WKNavigationResponsePolicyCancel);
}

- (void)webView:(WKWebView *)webView didStartProvisionalNavigation:(WKNavigation *)navigation {
  if (!self.loaded) NSLog(@"vibe page: first navigation started");
}

- (void)webView:(WKWebView *)webView didFinishNavigation:(WKNavigation *)navigation {
  if (!self.loaded) {
    self.loaded = YES;
    vibePageEvent(EV_LOADED);
  }
}

- (void)webViewWebContentProcessDidTerminate:(WKWebView *)webView {
  vibePageEvent(EV_WEBGONE);
  [NSApp terminate:nil];
}

#pragma mark WKUIDelegate: no windows, no capture, no dialogs

- (WKWebView *)webView:(WKWebView *)webView createWebViewWithConfiguration:(WKWebViewConfiguration *)c forNavigationAction:(WKNavigationAction *)a windowFeatures:(WKWindowFeatures *)f {
  return nil;
}
- (void)webView:(WKWebView *)webView requestMediaCapturePermissionForOrigin:(WKSecurityOrigin *)o initiatedByFrame:(WKFrameInfo *)f type:(WKMediaCaptureType)t decisionHandler:(void (^)(WKPermissionDecision))h API_AVAILABLE(macos(12.0)) {
  h(WKPermissionDecisionDeny);
}
- (void)webView:(WKWebView *)webView requestDeviceOrientationAndMotionPermissionForOrigin:(WKSecurityOrigin *)o initiatedByFrame:(WKFrameInfo *)f decisionHandler:(void (^)(WKPermissionDecision))h API_AVAILABLE(macos(13.0)) {
  h(WKPermissionDecisionDeny);
}
// An <input type=file> opens no panel: the page has no files to choose from.
- (void)webView:(WKWebView *)w runOpenPanelWithParameters:(WKOpenPanelParameters *)p initiatedByFrame:(WKFrameInfo *)f completionHandler:(void (^)(NSArray<NSURL *> *))h { h(nil); }
- (void)webView:(WKWebView *)w runJavaScriptAlertPanelWithMessage:(NSString *)m initiatedByFrame:(WKFrameInfo *)f completionHandler:(void (^)(void))h { h(); }
- (void)webView:(WKWebView *)w runJavaScriptConfirmPanelWithMessage:(NSString *)m initiatedByFrame:(WKFrameInfo *)f completionHandler:(void (^)(BOOL))h { h(NO); }
- (void)webView:(WKWebView *)w runJavaScriptTextInputPanelWithPrompt:(NSString *)p defaultText:(NSString *)d initiatedByFrame:(WKFrameInfo *)f completionHandler:(void (^)(NSString *))h { h(nil); }

#pragma mark WKScriptMessageHandler: the main frame only

- (void)userContentController:(WKUserContentController *)ucc didReceiveScriptMessage:(WKScriptMessage *)msg {
  if (!msg.frameInfo.isMainFrame) return;
  if (![msg.body isKindOfClass:[NSString class]]) return;
  vibePageMessage((char *)[(NSString *)msg.body UTF8String]);
}

#pragma mark NSWindowDelegate

- (BOOL)windowShouldClose:(NSWindow *)sender {
  vibePageEvent(EV_CLOSED);
  [NSApp terminate:nil];
  return YES;
}

@end

// The window's menu: only Close (Cmd-W). The app is an accessory, so no menu bar
// shows, but the key equivalent still reaches the window.
static void installCloseMenu(void) {
  NSMenu *bar = [NSMenu new];
  NSMenuItem *top = [NSMenuItem new];
  [bar addItem:top];
  NSMenu *m = [[NSMenu alloc] initWithTitle:@"Window"];
  [m addItem:[[NSMenuItem alloc] initWithTitle:@"Close" action:@selector(performClose:) keyEquivalent:@"w"]];
  top.submenu = m;
  [NSApp setMainMenu:bar];
}

static WKWebViewConfiguration *makeConfiguration(VibeHost *host, NSString *apiScript) {
  WKWebViewConfiguration *cfg = [WKWebViewConfiguration new];
  // Nothing survives the window: no localStorage, IndexedDB or cookie.
  cfg.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];
  if (@available(macOS 14.0, *)) {
    // Host names in loads and link-click preconnects go to a proxy that is not
    // there, and are not resolved locally. (dns-prefetch goes around the proxy;
    // only the rule list stops it.)
    int port = deadProxyPort();
    if (port == 0) exit(EXIT_NO_PROXY_PORT);
    char portText[8];
    snprintf(portText, sizeof portText, "%d", port);
    nw_endpoint_t ep = nw_endpoint_create_host("127.0.0.1", portText);
    nw_proxy_config_t pc = nw_proxy_config_create_http_connect(ep, NULL);
    // Never fall back to a direct connection when the proxy refuses.
    nw_proxy_config_set_failover_allowed(pc, false);
    cfg.websiteDataStore.proxyConfigurations = @[ pc ];
  } else {
    exit(EXIT_NO_PROXY_API);
  }
  [cfg setURLSchemeHandler:host forURLScheme:@"vibe-page"];
  WKUserContentController *ucc = [WKUserContentController new];
  [ucc addScriptMessageHandler:host name:@"vibe"];
  [ucc addUserScript:[[WKUserScript alloc] initWithSource:apiScript injectionTime:WKUserScriptInjectionTimeAtDocumentStart forMainFrameOnly:YES]];
  cfg.userContentController = ucc;
  cfg.preferences.javaScriptCanOpenWindowsAutomatically = NO;
  if (@available(macOS 12.3, *)) cfg.preferences.elementFullscreenEnabled = NO;
  // Settings, not a layer (P1 measured RTCPeerConnection as undefined with them).
  @try { [cfg.preferences setValue:@NO forKey:@"peerConnectionEnabled"]; } @catch (NSException *e) {}
  @try { [cfg.preferences setValue:@NO forKey:@"mediaDevicesEnabled"]; } @catch (NSException *e) {}
  @try { [cfg.preferences setValue:@NO forKey:@"developerExtrasEnabled"]; } @catch (NSException *e) {}
  return cfg;
}

static NSWindow *makeWindow(VibeHost *host, NSString *title, int width, int height) {
  NSWindowStyleMask style = NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskResizable | NSWindowStyleMaskMiniaturizable;
  NSWindow *w = [[NSWindow alloc] initWithContentRect:NSMakeRect(200, 200, width, height) styleMask:style backing:NSBackingStoreBuffered defer:NO];
  // The native title is fixed. Nothing observes the document's title.
  w.title = title;
  w.delegate = host;
  w.releasedWhenClosed = NO;
  return w;
}

// The person's input, seen by the app before the page can see it (the `download`
// capability's gesture, checked outside the web process). A local monitor sees
// this process's events only. One physical input is one unit; Escape and a
// held key's repeats are not inputs.
static _Atomic uint64_t gInputNS = 0;
#define INPUT_WINDOW_NS (5ull * 1000000000ull)

static void installInputMonitor(void) {
  NSEventMask mask = NSEventMaskLeftMouseDown | NSEventMaskRightMouseDown | NSEventMaskOtherMouseDown | NSEventMaskKeyDown;
  [NSEvent addLocalMonitorForEventsMatchingMask:mask handler:^NSEvent *(NSEvent *ev) {
    if (ev.type == NSEventTypeKeyDown && (ev.isARepeat || ev.keyCode == 53)) return ev;
    atomic_store(&gInputNS, clock_gettime_nsec_np(CLOCK_UPTIME_RAW));
    return ev;
  }];
}

int vibe_host_take_input(void) {
  uint64_t t = atomic_exchange(&gInputNS, 0);
  return t != 0 && clock_gettime_nsec_np(CLOCK_UPTIME_RAW) - t <= INPUT_WINDOW_NS;
}

int vibe_host_run(const char *start_uri, const char *prefix, const char *title, int width, int height, const char *api_script) {
  @autoreleasepool {
    [NSApplication sharedApplication];
    // No second Dock icon (P1: ApplicationType=UIElement).
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    installCloseMenu();
    installInputMonitor();
    // A page that sits behind other windows must keep posting its heartbeat: without
    // this App Nap can slow the helper until the watchdog kills a healthy page.
    gActivity = [[NSProcessInfo processInfo] beginActivityWithOptions:NSActivityUserInitiatedAllowingIdleSystemSleep reason:@"Vibe page window"];

    VibeHost *host = [VibeHost new];
    gHost = host;
    host.prefix = @(prefix);
    host.startURL = [NSURL URLWithString:@(start_uri)];
    host.pending = [NSMutableDictionary new];

    WKWebViewConfiguration *cfg = makeConfiguration(host, @(api_script));
    host.window = makeWindow(host, @(title), width, height);
    host.view = [[VibeWebView alloc] initWithFrame:host.window.contentView.bounds configuration:cfg];
    host.view.navigationDelegate = host;
    host.view.UIDelegate = host;
    host.view.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    if (@available(macOS 13.3, *)) host.view.inspectable = NO;
    host.window.contentView = host.view;
    [host.window makeKeyAndOrderFront:nil];
    if (@available(macOS 14.0, *)) {
      [NSApp activate];
    } else {
      [NSApp activateIgnoringOtherApps:YES];
    }

    // The rule list is compiled into a throwaway store, added before the first
    // load, and the first load waits for it. A list that does not compile is no
    // page window: the helper exits.
    gRulesDir = [NSTemporaryDirectory() stringByAppendingPathComponent:[NSString stringWithFormat:@"vibe-rules-%d", getpid()]];
    atexit(removeRulesDir);
    NSURL *dir = [NSURL fileURLWithPath:gRulesDir];
    WKContentRuleListStore *store = [WKContentRuleListStore storeWithURL:dir];
    [store compileContentRuleListForIdentifier:@"vibe-page-block" encodedContentRuleList:kBlockRules completionHandler:^(WKContentRuleList *list, NSError *err) {
      if (!list) exit(EXIT_RULES_FAILED);
      [host.view.configuration.userContentController addContentRuleList:list];
      // These two lines, in this order, are how a Mac run shows the rule list was in
      // place before the first load (they carry no page data).
      NSLog(@"vibe page: rule list added, first load next");
      [host.view loadRequest:[NSURLRequest requestWithURL:host.startURL]];
    }];
    [NSApp run];
  }
  return 0;
}

void vibe_host_respond(unsigned long ident, int ok, const char *ctype, const char *csp, const char *body, int len) {
  NSString *ct = @(ctype ? ctype : "application/octet-stream");
  NSString *policy = @(csp ? csp : "");
  NSData *data = ok ? [NSData dataWithBytes:body length:(NSUInteger)(len > 0 ? len : 0)] : [NSData data];
  dispatch_async(dispatch_get_main_queue(), ^{
    [gHost respond:ident ok:ok != 0 ctype:ct csp:policy body:data];
  });
}

void vibe_host_eval(const char *js) {
  NSString *script = @(js);
  dispatch_async(dispatch_get_main_queue(), ^{
    [gHost.view evaluateJavaScript:script completionHandler:nil];
  });
}

void vibe_host_quit(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    [NSApp terminate:nil];
  });
}
