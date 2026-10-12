package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

// DefaultKeyringService is the keychain service name; $VIBE_KEYRING_SERVICE
// overrides it (tests use a throwaway name).
const DefaultKeyringService = "vibe"

// DefaultKeyringTimeout bounds every keychain call. A locked Secret Service
// collection with no prompter blocks forever inside the library (spike #107).
const DefaultKeyringTimeout = 5 * time.Second

// KeyringBackend is the part of go-keyring the store uses.
type KeyringBackend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type osKeyring struct{}

func (osKeyring) Get(s, u string) (string, error) { return keyring.Get(s, u) }
func (osKeyring) Set(s, u, p string) error        { return keyring.Set(s, u, p) }
func (osKeyring) Delete(s, u string) error        { return keyring.Delete(s, u) }

// KeyringStore keeps values in the OS keychain.
type KeyringStore struct {
	service string
	timeout time.Duration
	backend KeyringBackend

	// cooldown is how long every call fails fast after one timed out: the
	// goroutine of a hung call cannot be cancelled, so without this each
	// retry would leave another one behind.
	cooldown time.Duration
	mu       sync.Mutex
	downTill time.Time
	inflight map[string]*keyringCall // single-flight for Get, by account
}

type keyringCall struct {
	done chan struct{}
	val  string
	err  error
}

// DefaultKeyringCooldown is the fail-fast window after a timeout.
const DefaultKeyringCooldown = 30 * time.Second

// WithKeyringCooldown overrides the fail-fast window after a timeout.
func WithKeyringCooldown(d time.Duration) KeyringOption {
	return func(k *KeyringStore) { k.cooldown = d }
}

// KeyringOption configures NewKeyringStore.
type KeyringOption func(*KeyringStore)

// WithKeyringService overrides the service name.
func WithKeyringService(s string) KeyringOption { return func(k *KeyringStore) { k.service = s } }

// WithKeyringTimeout overrides the per-call timeout.
func WithKeyringTimeout(d time.Duration) KeyringOption {
	return func(k *KeyringStore) { k.timeout = d }
}

// WithKeyringBackend swaps the backend (tests).
func WithKeyringBackend(b KeyringBackend) KeyringOption {
	return func(k *KeyringStore) { k.backend = b }
}

// NewKeyringStore returns a keychain-backed store.
func NewKeyringStore(opts ...KeyringOption) *KeyringStore {
	k := &KeyringStore{service: DefaultKeyringService, timeout: DefaultKeyringTimeout, backend: osKeyring{}, cooldown: DefaultKeyringCooldown, inflight: map[string]*keyringCall{}}
	if s := os.Getenv("VIBE_KEYRING_SERVICE"); s != "" {
		k.service = s
	}
	for _, o := range opts {
		o(k)
	}
	return k
}

// Service reports the service name in use.
func (k *KeyringStore) Service() string { return k.service }

// run calls fn in a goroutine so a blocked keychain cannot block the caller.
// After a timeout the store reports unavailable for the cooldown without
// starting new calls, and identical Gets share one call (key != "").
func (k *KeyringStore) run(ctx context.Context, key string, fn func() (string, error)) (string, error) {
	k.mu.Lock()
	if wait := time.Until(k.downTill); wait > 0 {
		k.mu.Unlock()
		return "", fmt.Errorf("%w: keychain locked or not responding (try again in %d s)", ErrUnavailable, int(wait.Seconds())+1)
	}
	var call *keyringCall
	if key != "" {
		if c, ok := k.inflight[key]; ok {
			call = c
		}
	}
	if call == nil {
		call = &keyringCall{done: make(chan struct{})}
		if key != "" {
			k.inflight[key] = call
		}
		go func() {
			call.val, call.err = fn()
			k.mu.Lock()
			if call.err == nil || errors.Is(call.err, keyring.ErrNotFound) {
				// A call that answers, even late (a slow keychain unlock),
				// proves the keychain is back: end the fail-fast window.
				k.downTill = time.Time{}
			}
			if key != "" && k.inflight[key] == call {
				delete(k.inflight, key)
			}
			k.mu.Unlock()
			close(call.done)
		}()
	}
	k.mu.Unlock()

	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, k.timeout)
	defer cancel()
	select {
	case <-call.done:
		return call.val, k.mapErr(call.err)
	case <-ctx.Done():
		if perr := parent.Err(); perr != nil {
			return "", perr // the caller gave up; that says nothing about the keychain
		}
		k.mu.Lock()
		k.downTill = time.Now().Add(k.cooldown)
		k.mu.Unlock()
		return "", fmt.Errorf("%w: keychain locked or not responding (try again in %d s)", ErrUnavailable, int(k.cooldown.Seconds()))
	}
}

func (k *KeyringStore) mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, keyring.ErrSetDataTooBig):
		return errors.New("credential is too large for the keychain")
	default:
		// The backend's message never contains the value, but it can be long
		// and platform-specific; keep it as the detail.
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
}

// forget drops a shared in-flight read of ref, so a Get after a write never
// joins a read that began before it.
func (k *KeyringStore) forget(ref Ref) {
	k.mu.Lock()
	delete(k.inflight, "get|"+ref.Account())
	k.mu.Unlock()
}

// Get implements Store.
func (k *KeyringStore) Get(ctx context.Context, ref Ref) ([]byte, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	v, err := k.run(ctx, "get|"+ref.Account(), func() (string, error) { return k.backend.Get(k.service, ref.Account()) })
	if err != nil {
		return nil, err
	}
	return []byte(v), nil
}

// Set implements Store.
func (k *KeyringStore) Set(ctx context.Context, ref Ref, value []byte) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	k.forget(ref)
	defer k.forget(ref)
	_, err := k.run(ctx, "", func() (string, error) { return "", k.backend.Set(k.service, ref.Account(), string(value)) })
	return err
}

// Delete implements Store; a missing item is not an error.
func (k *KeyringStore) Delete(ctx context.Context, ref Ref) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	k.forget(ref)
	defer k.forget(ref)
	_, err := k.run(ctx, "", func() (string, error) { return "", k.backend.Delete(k.service, ref.Account()) })
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// Probe reports whether the keychain answers, by looking up a name that is
// never stored. ErrNotFound is the healthy answer.
func (k *KeyringStore) Probe(ctx context.Context) error {
	_, err := k.Get(ctx, AppRef("probe"))
	if err == nil || errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
