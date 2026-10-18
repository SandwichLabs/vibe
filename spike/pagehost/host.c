#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <libsoup/soup.h>
#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include "host.h"
#include "_cgo_export.h"

static GtkWidget *g_win = NULL;
static WebKitWebView *g_view = NULL;
static int g_policy_on = 0;
static char *g_start = NULL;
static char *g_title = NULL;

static void on_message(WebKitUserContentManager *m, WebKitJavascriptResult *r, gpointer ud) {
  JSCValue *v = webkit_javascript_result_get_js_value(r);
  char *s = jsc_value_to_string(v);
  goMessage(s);
  g_free(s);
}

static void on_scheme(WebKitURISchemeRequest *req, gpointer ud) {
  const char *uri = webkit_uri_scheme_request_get_uri(req);
  char *body = NULL, *ctype = NULL, *csp = NULL;
  int len = 0, status = 200;
  goPage((char *)uri, &body, &len, &ctype, &csp, &status);
  if (status != 200 || body == NULL) {
    GError *e = g_error_new(G_IO_ERROR, G_IO_ERROR_NOT_FOUND, "not found");
    webkit_uri_scheme_request_finish_error(req, e);
    g_error_free(e);
    if (body) free(body);
    if (ctype) free(ctype);
    if (csp) free(csp);
    return;
  }
  gchar *copy = g_memdup2(body, len ? len : 1);
  GInputStream *st = g_memory_input_stream_new_from_data(copy, len, g_free);
  WebKitURISchemeResponse *resp = webkit_uri_scheme_response_new(st, len);
  webkit_uri_scheme_response_set_content_type(resp, ctype);
  SoupMessageHeaders *h = soup_message_headers_new(SOUP_MESSAGE_HEADERS_RESPONSE);
  if (csp && csp[0]) soup_message_headers_append(h, "Content-Security-Policy", csp);
  soup_message_headers_append(h, "Content-Type", ctype);
  soup_message_headers_append(h, "X-Content-Type-Options", "nosniff");
  soup_message_headers_append(h, "Cache-Control", "no-store");
  webkit_uri_scheme_response_set_http_headers(resp, h);
  webkit_uri_scheme_request_finish_with_response(req, resp);
  g_object_unref(resp);
  g_object_unref(st);
  free(body); free(ctype); free(csp);
}

static gboolean on_policy(WebKitWebView *v, WebKitPolicyDecision *d, WebKitPolicyDecisionType t, gpointer ud) {
  if (t == WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION || t == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION) {
    WebKitNavigationPolicyDecision *nd = WEBKIT_NAVIGATION_POLICY_DECISION(d);
    WebKitNavigationAction *na = webkit_navigation_policy_decision_get_navigation_action(nd);
    WebKitURIRequest *rq = webkit_navigation_action_get_request(na);
    const char *uri = webkit_uri_request_get_uri(rq);
    const char *kind = t == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION ? "new-window" : "navigation";
    int allow = 0;
    if (t == WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION) {
      size_t n = strlen(g_start);
      if (strncmp(uri, g_start, n) == 0 && (uri[n] == 0 || uri[n] == '?' || uri[n] == '#')) allow = 1;
    }
    goPolicyLog((char *)kind, (char *)uri, allow, g_policy_on);
    if (!g_policy_on) return FALSE;
    if (allow) webkit_policy_decision_use(d); else webkit_policy_decision_ignore(d);
    return TRUE;
  }
  return FALSE;
}

static gboolean on_perm(WebKitWebView *v, WebKitPermissionRequest *r, gpointer ud) {
  goPolicyLog("permission", (char *)G_OBJECT_TYPE_NAME(r), 0, g_policy_on);
  if (!g_policy_on) return FALSE;
  webkit_permission_request_deny(r);
  return TRUE;
}
static gboolean on_notify(WebKitWebView *v, WebKitNotification *n, gpointer ud) {
  goPolicyLog("notification", "", 0, g_policy_on);
  return g_policy_on ? TRUE : FALSE;
}
static GtkWidget *on_create(WebKitWebView *v, WebKitNavigationAction *a, gpointer ud) {
  WebKitURIRequest *rq = webkit_navigation_action_get_request(a);
  goPolicyLog("create-window", (char *)webkit_uri_request_get_uri(rq), 0, g_policy_on);
  if (g_policy_on) return NULL;
  /* control: behave like a default browser, open the popup */
  GtkWidget *nv = webkit_web_view_new_with_related_view(v);
  GtkWidget *w = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  gtk_container_add(GTK_CONTAINER(w), nv);
  gtk_widget_show_all(w);
  return nv;
}
static gboolean on_ctxmenu(WebKitWebView *v, WebKitContextMenu *m, GdkEvent *e, WebKitHitTestResult *h, gpointer ud) { return TRUE; }
static void on_download(WebKitWebContext *c, WebKitDownload *d, gpointer ud) {
  goPolicyLog("download", "", 0, g_policy_on);
  if (g_policy_on) webkit_download_cancel(d);
}
static void on_crash(WebKitWebView *v, WebKitWebProcessTerminationReason r, gpointer ud) {
  goPolicyLog("web-process-terminated", "", (int)r, 0);
  gtk_main_quit();
}
static void on_title(GObject *o, GParamSpec *p, gpointer ud) {
  const char *t = webkit_web_view_get_title(g_view);
  char buf[512];
  snprintf(buf, sizeof buf, "doc.title=%s window.title=%s", t ? t : "(null)", gtk_window_get_title(GTK_WINDOW(g_win)));
  goPolicyLog("title", buf, 0, 0);
}
static void on_load(WebKitWebView *v, WebKitLoadEvent e, gpointer ud) {
  const char *u = webkit_web_view_get_uri(v);
  goPolicyLog("load", (char *)(u ? u : ""), (int)e, 0);
}
static gboolean on_load_failed(WebKitWebView *v, WebKitLoadEvent e, gchar *uri, GError *err, gpointer ud) {
  goPolicyLog("load-failed", uri, 0, 0);
  return FALSE;
}
static gboolean on_dialog(WebKitWebView *v, WebKitScriptDialog *d, gpointer ud) { return g_policy_on ? TRUE : FALSE; }

typedef struct { char *js; } EvalJob;
static gboolean do_eval(gpointer p) {
  EvalJob *j = p;
  webkit_web_view_evaluate_javascript(g_view, j->js, -1, NULL, NULL, NULL, NULL, NULL);
  g_free(j->js); g_free(j);
  return G_SOURCE_REMOVE;
}
void host_eval(const char *js) {
  EvalJob *j = g_new0(EvalJob, 1);
  j->js = g_strdup(js);
  g_idle_add(do_eval, j);
}
static gboolean do_quit(gpointer p) { gtk_main_quit(); return G_SOURCE_REMOVE; }
void host_quit(void) { g_idle_add(do_quit, NULL); }

int host_run(int settings_on, int policy_on, int sandbox_on, const char *start_uri, const char *title) {
  g_policy_on = policy_on;
  g_start = g_strdup(start_uri);
  { char *q = strchr(g_start, '?'); if (q) *q = 0; }
  g_title = g_strdup(title);
  gtk_init(NULL, NULL);

  WebKitWebsiteDataManager *dm = webkit_website_data_manager_new_ephemeral();
  WebKitWebContext *ctx = webkit_web_context_new_with_website_data_manager(dm);
  if (sandbox_on) webkit_web_context_set_sandbox_enabled(ctx, TRUE);
  goPolicyLog("sandbox-enabled", "", webkit_web_context_get_sandbox_enabled(ctx), 0);
  goPolicyLog("ephemeral", "", webkit_web_context_is_ephemeral(ctx), 0);

  WebKitSecurityManager *sm = webkit_web_context_get_security_manager(ctx);
  webkit_security_manager_register_uri_scheme_as_secure(sm, "vibe-page");
  webkit_security_manager_register_uri_scheme_as_cors_enabled(sm, "vibe-page");
  webkit_web_context_register_uri_scheme(ctx, "vibe-page", on_scheme, NULL, NULL);
  g_signal_connect(ctx, "download-started", G_CALLBACK(on_download), NULL);

  WebKitSettings *st = webkit_settings_new();
  webkit_settings_set_enable_write_console_messages_to_stdout(st, TRUE);
  if (!settings_on) {
    webkit_settings_set_enable_webrtc(st, TRUE);
    webkit_settings_set_enable_media_stream(st, TRUE);
    webkit_settings_set_enable_media(st, TRUE);
    webkit_settings_set_enable_dns_prefetching(st, TRUE);
    webkit_settings_set_enable_hyperlink_auditing(st, TRUE);
  }
  if (settings_on) {
    webkit_settings_set_enable_webrtc(st, FALSE);
    webkit_settings_set_enable_media_stream(st, FALSE);
    webkit_settings_set_enable_media(st, FALSE);
    webkit_settings_set_enable_dns_prefetching(st, FALSE);
    webkit_settings_set_enable_hyperlink_auditing(st, FALSE);
    webkit_settings_set_enable_developer_extras(st, FALSE);
    webkit_settings_set_enable_spatial_navigation(st, FALSE);
    webkit_settings_set_enable_html5_local_storage(st, FALSE);
    webkit_settings_set_enable_html5_database(st, FALSE);
    webkit_settings_set_enable_site_specific_quirks(st, FALSE);
    webkit_settings_set_enable_page_cache(st, FALSE);
    webkit_settings_set_enable_webgl(st, FALSE);
  }
  goPolicyLog("setting", webkit_settings_get_enable_webrtc(st) ? "webrtc=on" : "webrtc=off", 0, 0);

  WebKitUserContentManager *ucm = webkit_user_content_manager_new();
  webkit_user_content_manager_register_script_message_handler(ucm, "vibe");
  g_signal_connect(ucm, "script-message-received::vibe", G_CALLBACK(on_message), NULL);
  char *api = goAPIScript();
  WebKitUserScript *us = webkit_user_script_new(api, WEBKIT_USER_CONTENT_INJECT_TOP_FRAME,
                                                WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START, NULL, NULL);
  webkit_user_content_manager_add_script(ucm, us);
  free(api);

  g_view = WEBKIT_WEB_VIEW(g_object_new(WEBKIT_TYPE_WEB_VIEW, "web-context", ctx, "user-content-manager", ucm,
                                        "settings", st, NULL));
  g_signal_connect(g_view, "decide-policy", G_CALLBACK(on_policy), NULL);
  g_signal_connect(g_view, "permission-request", G_CALLBACK(on_perm), NULL);
  g_signal_connect(g_view, "show-notification", G_CALLBACK(on_notify), NULL);
  g_signal_connect(g_view, "create", G_CALLBACK(on_create), NULL);
  g_signal_connect(g_view, "context-menu", G_CALLBACK(on_ctxmenu), NULL);
  g_signal_connect(g_view, "web-process-terminated", G_CALLBACK(on_crash), NULL);
  g_signal_connect(g_view, "notify::title", G_CALLBACK(on_title), NULL);
  g_signal_connect(g_view, "load-changed", G_CALLBACK(on_load), NULL);
  g_signal_connect(g_view, "load-failed", G_CALLBACK(on_load_failed), NULL);
  g_signal_connect(g_view, "script-dialog", G_CALLBACK(on_dialog), NULL);

  g_win = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  gtk_window_set_title(GTK_WINDOW(g_win), g_title);
  gtk_window_set_default_size(GTK_WINDOW(g_win), 640, 480);
  g_signal_connect(g_win, "destroy", G_CALLBACK(gtk_main_quit), NULL);
  gtk_container_add(GTK_CONTAINER(g_win), GTK_WIDGET(g_view));
  gtk_widget_show_all(g_win);
  webkit_web_view_load_uri(g_view, start_uri);
  gtk_main();
  return 0;
}
