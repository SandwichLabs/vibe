//go:build linux && webkit2_41

/* The Linux page window (docs/plans/html-artifacts.md, H4): a WebKitGTK view
 * Vibe owns, in the helper process only. The helper runs under the network
 * gate (pagehost_gate_linux.go), so the view has no network of its own; what
 * is here is the rest of the confinement:
 *   - an ephemeral data manager and the web-process sandbox switched on;
 *   - the page served from memory under vibe-page://<name>/, every response
 *     carrying the page CSP;
 *   - a navigation policy that lets through the first page and same-page
 *     links only, and refuses new windows, downloads and every permission;
 *   - one message handler in the main world, beside the injected API;
 *   - a window title that is fixed and never follows document.title. */

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <libsoup/soup.h>
#include <string.h>
#include <stdlib.h>
#include "pagewindow.h"
#include "_cgo_export.h"

#define EV_LOADED 1
#define EV_WEBGONE 2
#define EV_CLOSED 3

static GtkWidget *g_win = NULL;
static WebKitWebView *g_view = NULL;
static char *g_prefix = NULL;
static GHashTable *g_pending = NULL;
static int g_loaded = 0;

/* The person's input, seen by the native widget before the page can see it. A
 * page that posts straight to the message handler cannot make one of these: the
 * gesture the `download` capability needs is checked here, outside the web
 * process (vibe_host_take_input). One physical input is one unit; a held key's
 * repeats are not new inputs, and Escape is not one. */
static gint64 g_input_us = 0;
static guint8 g_keys_down[256 / 8]; /* hardware keycodes now held, to tell a fresh press from an auto-repeat */
#define INPUT_WINDOW_US (5 * G_USEC_PER_SEC)

static void note_input(void) { __atomic_store_n(&g_input_us, g_get_monotonic_time(), __ATOMIC_SEQ_CST); }

int vibe_host_take_input(void) {
  gint64 t = __atomic_exchange_n(&g_input_us, 0, __ATOMIC_SEQ_CST);
  return t != 0 && g_get_monotonic_time() - t <= INPUT_WINDOW_US;
}

static gboolean on_button(GtkWidget *w, GdkEventButton *e, gpointer ud) {
  if (e->type == GDK_BUTTON_PRESS) note_input();
  return FALSE;
}
static gboolean on_key(GtkWidget *w, GdkEventKey *e, gpointer ud) {
  if (e->keyval == GDK_KEY_Escape) return FALSE;
  guint k = e->hardware_keycode & 0xff;
  if (g_keys_down[k / 8] & (1u << (k % 8))) return FALSE; /* auto-repeat of a key still held */
  g_keys_down[k / 8] |= (1u << (k % 8));
  note_input();
  return FALSE;
}
static gboolean on_key_up(GtkWidget *w, GdkEventKey *e, gpointer ud) {
  guint k = e->hardware_keycode & 0xff;
  g_keys_down[k / 8] &= ~(1u << (k % 8));
  return FALSE;
}
static gboolean on_touch(GtkWidget *w, GdkEventTouch *e, gpointer ud) {
  if (e->type == GDK_TOUCH_BEGIN) note_input();
  return FALSE;
}

static void on_message(WebKitUserContentManager *m, WebKitJavascriptResult *r, gpointer ud) {
  JSCValue *v = webkit_javascript_result_get_js_value(r);
  if (!jsc_value_is_string(v)) return;
  char *s = jsc_value_to_string(v);
  vibePageMessage(s);
  g_free(s);
}

static void on_scheme(WebKitURISchemeRequest *req, gpointer ud) {
  static unsigned long next_id = 0;
  unsigned long id = ++next_id;
  g_hash_table_insert(g_pending, GSIZE_TO_POINTER(id), g_object_ref(req));
  vibePageServe((char *)webkit_uri_scheme_request_get_uri(req), id);
}

typedef struct {
  unsigned long id;
  int ok;
  char *ctype, *csp;
  char *body;
  int len;
} RespJob;

static gboolean do_respond(gpointer p) {
  RespJob *j = p;
  gpointer key = GSIZE_TO_POINTER(j->id);
  WebKitURISchemeRequest *req = g_hash_table_lookup(g_pending, key);
  if (req) {
    g_hash_table_steal(g_pending, key);
    if (!j->ok) {
      GError *e = g_error_new_literal(G_IO_ERROR, G_IO_ERROR_NOT_FOUND, "not found");
      webkit_uri_scheme_request_finish_error(req, e);
      g_error_free(e);
    } else {
      GInputStream *st = g_memory_input_stream_new_from_data(j->body, j->len, g_free);
      j->body = NULL; /* the stream owns it now */
      WebKitURISchemeResponse *resp = webkit_uri_scheme_response_new(st, j->len);
      webkit_uri_scheme_response_set_content_type(resp, j->ctype);
      /* The response headers must include Content-Type, or a script is silently
       * skipped under nosniff (P1). */
      SoupMessageHeaders *h = soup_message_headers_new(SOUP_MESSAGE_HEADERS_RESPONSE);
      if (j->csp && j->csp[0]) soup_message_headers_append(h, "Content-Security-Policy", j->csp);
      soup_message_headers_append(h, "Content-Type", j->ctype);
      soup_message_headers_append(h, "X-Content-Type-Options", "nosniff");
      soup_message_headers_append(h, "Cache-Control", "no-store");
      webkit_uri_scheme_response_set_http_headers(resp, h);
      webkit_uri_scheme_request_finish_with_response(req, resp);
      g_object_unref(resp);
      g_object_unref(st);
    }
    g_object_unref(req);
  }
  g_free(j->body);
  g_free(j->ctype);
  g_free(j->csp);
  g_free(j);
  return G_SOURCE_REMOVE;
}

void vibe_host_respond(unsigned long id, int ok, const char *ctype, const char *csp, const char *body, int len) {
  RespJob *j = g_new0(RespJob, 1);
  j->id = id;
  j->ok = ok;
  j->ctype = g_strdup(ctype ? ctype : "application/octet-stream");
  j->csp = g_strdup(csp ? csp : "");
  j->len = ok ? len : 0;
  if (ok) {
    /* g_memory_input_stream_new_from_data takes the buffer: allocate one that
     * is never NULL, even for an empty body. */
    j->body = g_malloc(len > 0 ? len : 1);
    if (len > 0) memcpy(j->body, body, len);
  }
  g_idle_add(do_respond, j);
}

/* Only the first page and links within it ("?query" or "#fragment"). */
static gboolean uri_is_page(const char *uri) {
  size_t n = strlen(g_prefix);
  if (strncmp(uri, g_prefix, n) != 0) return FALSE;
  return uri[n] == 0 || uri[n] == '?' || uri[n] == '#';
}

static gboolean on_policy(WebKitWebView *v, WebKitPolicyDecision *d, WebKitPolicyDecisionType t, gpointer ud) {
  if (t == WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION) {
    WebKitNavigationPolicyDecision *nd = WEBKIT_NAVIGATION_POLICY_DECISION(d);
    WebKitNavigationAction *na = webkit_navigation_policy_decision_get_navigation_action(nd);
    WebKitURIRequest *rq = webkit_navigation_action_get_request(na);
    const char *uri = webkit_uri_request_get_uri(rq);
    const char *method = webkit_uri_request_get_http_method(rq);
    gboolean ok = uri && uri_is_page(uri) && (method == NULL || strcmp(method, "GET") == 0);
    if (ok) webkit_policy_decision_use(d); else webkit_policy_decision_ignore(d);
    return TRUE;
  }
  if (t == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION) {
    webkit_policy_decision_ignore(d);
    return TRUE;
  }
  if (t == WEBKIT_POLICY_DECISION_TYPE_RESPONSE) {
    WebKitResponsePolicyDecision *rd = WEBKIT_RESPONSE_POLICY_DECISION(d);
    WebKitURIResponse *rs = webkit_response_policy_decision_get_response(rd);
    const char *uri = webkit_uri_response_get_uri(rs);
    /* A response that is not ours, or that WebKit would download, is refused. */
    if (uri && strncmp(uri, g_prefix, strlen(g_prefix)) == 0 && webkit_response_policy_decision_is_mime_type_supported(rd)) {
      webkit_policy_decision_use(d);
    } else {
      webkit_policy_decision_ignore(d);
    }
    return TRUE;
  }
  return FALSE;
}

static gboolean on_perm(WebKitWebView *v, WebKitPermissionRequest *r, gpointer ud) {
  webkit_permission_request_deny(r);
  return TRUE;
}
static gboolean on_notify(WebKitWebView *v, WebKitNotification *n, gpointer ud) { return TRUE; }
static GtkWidget *on_create(WebKitWebView *v, WebKitNavigationAction *a, gpointer ud) { return NULL; }
static gboolean on_ctxmenu(WebKitWebView *v, WebKitContextMenu *m, GdkEvent *e, WebKitHitTestResult *h, gpointer ud) { return TRUE; }
static gboolean on_dialog(WebKitWebView *v, WebKitScriptDialog *d, gpointer ud) { return TRUE; }
static gboolean on_fullscreen(WebKitWebView *v, gpointer ud) { return TRUE; }
/* <input type=file> opens no chooser: the page has no files to choose from, and
 * a GTK chooser over the helper's allow-listed view would show the wrong tree. */
static gboolean on_file_chooser(WebKitWebView *v, WebKitFileChooserRequest *r, gpointer ud) {
  webkit_file_chooser_request_cancel(r);
  return TRUE;
}
static void on_download(WebKitWebContext *c, WebKitDownload *d, gpointer ud) { webkit_download_cancel(d); }
static void on_crash(WebKitWebView *v, WebKitWebProcessTerminationReason r, gpointer ud) {
  vibePageEvent(EV_WEBGONE);
  gtk_main_quit();
}
static void on_load(WebKitWebView *v, WebKitLoadEvent e, gpointer ud) {
  if (e == WEBKIT_LOAD_FINISHED && !g_loaded) {
    g_loaded = 1;
    vibePageEvent(EV_LOADED);
  }
}
static gboolean on_delete(GtkWidget *w, GdkEvent *e, gpointer ud) {
  vibePageEvent(EV_CLOSED);
  gtk_main_quit();
  return FALSE;
}

typedef struct { char *js; } EvalJob;
static gboolean do_eval(gpointer p) {
  EvalJob *j = p;
  if (g_view) {
#if WEBKIT_CHECK_VERSION(2, 40, 0)
    webkit_web_view_evaluate_javascript(g_view, j->js, -1, NULL, NULL, NULL, NULL, NULL);
#else
    G_GNUC_BEGIN_IGNORE_DEPRECATIONS
    webkit_web_view_run_javascript(g_view, j->js, NULL, NULL, NULL);
    G_GNUC_END_IGNORE_DEPRECATIONS
#endif
  }
  g_free(j->js);
  g_free(j);
  return G_SOURCE_REMOVE;
}
void vibe_host_eval(const char *js) {
  EvalJob *j = g_new0(EvalJob, 1);
  j->js = g_strdup(js);
  g_idle_add(do_eval, j);
}

static gboolean do_quit(gpointer p) { gtk_main_quit(); return G_SOURCE_REMOVE; }
void vibe_host_quit(void) { g_idle_add(do_quit, NULL); }

static WebKitSettings *make_settings(void) {
  WebKitSettings *st = webkit_settings_new();
  /* These are settings, not a layer (P1): the network gate is the gate. */
#if WEBKIT_CHECK_VERSION(2, 38, 0)
  webkit_settings_set_enable_webrtc(st, FALSE);
#endif
  webkit_settings_set_enable_media_stream(st, FALSE);
  webkit_settings_set_enable_media(st, FALSE);
  webkit_settings_set_enable_webgl(st, FALSE);
  webkit_settings_set_enable_fullscreen(st, FALSE);
  webkit_settings_set_enable_developer_extras(st, FALSE);
  webkit_settings_set_enable_spatial_navigation(st, FALSE);
  webkit_settings_set_enable_html5_local_storage(st, FALSE);
  webkit_settings_set_enable_html5_database(st, FALSE);
  webkit_settings_set_enable_site_specific_quirks(st, FALSE);
  webkit_settings_set_enable_page_cache(st, FALSE);
  webkit_settings_set_enable_write_console_messages_to_stdout(st, FALSE);
  webkit_settings_set_allow_modal_dialogs(st, FALSE);
  webkit_settings_set_javascript_can_open_windows_automatically(st, FALSE);
  webkit_settings_set_allow_file_access_from_file_urls(st, FALSE);
  webkit_settings_set_allow_universal_access_from_file_urls(st, FALSE);
  G_GNUC_BEGIN_IGNORE_DEPRECATIONS
  webkit_settings_set_enable_dns_prefetching(st, FALSE);
  webkit_settings_set_enable_hyperlink_auditing(st, FALSE);
  G_GNUC_END_IGNORE_DEPRECATIONS
  return st;
}

int vibe_host_run(const char *start_uri, const char *prefix, const char *title, int width, int height, const char *api_script) {
  g_prefix = g_strdup(prefix);
  g_pending = g_hash_table_new(g_direct_hash, g_direct_equal);
  if (!gtk_init_check(NULL, NULL)) return 2;

  /* An ephemeral store: no localStorage, IndexedDB or cookie outlives the window.
   * The web-process sandbox is OFF unless this is called (P1): required. */
  WebKitWebsiteDataManager *dm = webkit_website_data_manager_new_ephemeral();
  WebKitWebContext *ctx = webkit_web_context_new_with_website_data_manager(dm);
  webkit_web_context_set_sandbox_enabled(ctx, TRUE);
  if (!webkit_web_context_get_sandbox_enabled(ctx)) return 3;

  WebKitSecurityManager *sm = webkit_web_context_get_security_manager(ctx);
  webkit_security_manager_register_uri_scheme_as_secure(sm, "vibe-page");
  webkit_security_manager_register_uri_scheme_as_cors_enabled(sm, "vibe-page");
  webkit_web_context_register_uri_scheme(ctx, "vibe-page", on_scheme, NULL, NULL);
  g_signal_connect(ctx, "download-started", G_CALLBACK(on_download), NULL);

  WebKitUserContentManager *ucm = webkit_user_content_manager_new();
  webkit_user_content_manager_register_script_message_handler(ucm, "vibe");
  g_signal_connect(ucm, "script-message-received::vibe", G_CALLBACK(on_message), NULL);
  WebKitUserScript *us = webkit_user_script_new(api_script, WEBKIT_USER_CONTENT_INJECT_TOP_FRAME,
                                                WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START, NULL, NULL);
  webkit_user_content_manager_add_script(ucm, us);

  WebKitSettings *st = make_settings();
  g_view = WEBKIT_WEB_VIEW(g_object_new(WEBKIT_TYPE_WEB_VIEW, "web-context", ctx, "user-content-manager", ucm, "settings", st, NULL));
  g_signal_connect(g_view, "decide-policy", G_CALLBACK(on_policy), NULL);
  g_signal_connect(g_view, "permission-request", G_CALLBACK(on_perm), NULL);
  g_signal_connect(g_view, "show-notification", G_CALLBACK(on_notify), NULL);
  g_signal_connect(g_view, "create", G_CALLBACK(on_create), NULL);
  g_signal_connect(g_view, "context-menu", G_CALLBACK(on_ctxmenu), NULL);
  g_signal_connect(g_view, "script-dialog", G_CALLBACK(on_dialog), NULL);
  g_signal_connect(g_view, "run-file-chooser", G_CALLBACK(on_file_chooser), NULL);
  g_signal_connect(g_view, "enter-fullscreen", G_CALLBACK(on_fullscreen), NULL);
  g_signal_connect(g_view, "web-process-terminated", G_CALLBACK(on_crash), NULL);
  g_signal_connect(g_view, "load-changed", G_CALLBACK(on_load), NULL);
  /* Before WebKit's own handlers (these signals run last in the class handler). */
  gtk_widget_add_events(GTK_WIDGET(g_view), GDK_BUTTON_PRESS_MASK | GDK_KEY_PRESS_MASK | GDK_KEY_RELEASE_MASK | GDK_TOUCH_MASK);
  g_signal_connect(g_view, "button-press-event", G_CALLBACK(on_button), NULL);
  g_signal_connect(g_view, "key-press-event", G_CALLBACK(on_key), NULL);
  g_signal_connect(g_view, "key-release-event", G_CALLBACK(on_key_up), NULL);
  g_signal_connect(g_view, "touch-event", G_CALLBACK(on_touch), NULL);

  /* The native title is fixed, set once. Nothing listens to the web view's own
   * title, so the document's title never reaches the window. */
  g_win = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  gtk_window_set_title(GTK_WINDOW(g_win), title);
  gtk_window_set_default_size(GTK_WINDOW(g_win), width, height);
  g_signal_connect(g_win, "delete-event", G_CALLBACK(on_delete), NULL);
  gtk_container_add(GTK_CONTAINER(g_win), GTK_WIDGET(g_view));
  gtk_widget_show_all(g_win);
  webkit_web_view_load_uri(g_view, start_uri);
  gtk_main();
  return 0;
}
