package llm

import (
	"strings"
)

// redactedError hides the model API key from an error's text while keeping
// the wrapped error for errors.Is/As. Provider SDKs sometimes echo (part of)
// the key in an authentication failure, and these errors reach the UI.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// Every error text that leaves this package goes through scrubKey: returned
// errors, "error" stream/task events (forwarded to the UI as Wails events) and
// log lines. Note fantasy's ProviderError keeps the full request dump,
// Authorization header included, in RequestBody; its Error() omits it, but
// never marshal or %+v-print a provider error.
//
// scrubKey returns err unchanged when key is empty or absent from its text,
// otherwise an error with every occurrence of key replaced.
func scrubKey(err error, key string) error {
	key = strings.TrimSpace(key)
	if err == nil || key == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, key) {
		return err
	}
	return &redactedError{msg: strings.ReplaceAll(msg, key, "[redacted]"), err: err}
}
