#ifndef HOST_H
#define HOST_H
int host_run(int settings_on, int policy_on, int sandbox_on, const char *start_uri, const char *title);
void host_eval(const char *js);
void host_quit(void);
#endif
