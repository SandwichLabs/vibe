#ifndef ASSAY_PAGEWINDOW_H
#define ASSAY_PAGEWINDOW_H

/* The calls the Go side makes into the native page window (GTK on Linux, Cocoa
 * on macOS). assay_host_run opens the page window and runs its loop until it ends.
 * prefix is "assay-page://<name>/" (the one origin the policy allows). */
int assay_host_run(const char *start_uri, const char *prefix, const char *title, int width, int height, const char *api_script);
/* assay_host_eval runs script in the page's main frame; any thread. */
void assay_host_eval(const char *js);
/* assay_host_respond answers a scheme request (any thread). ok=0 fails the request.
 * The body is copied before it returns. */
void assay_host_respond(unsigned long id, int ok, const char *ctype, const char *csp, const char *body, int len);
/* assay_host_take_input reports whether the person gave the window a physical
 * input (a click, a key press that is not Escape or a repeat, a touch) in the
 * last 5 seconds, and consumes it: one input is one yes. It is the gesture the
 * `download` capability needs, seen by the native widget, not the page; any thread. */
int assay_host_take_input(void);
/* assay_host_quit ends the loop; any thread. */
void assay_host_quit(void);

#endif
