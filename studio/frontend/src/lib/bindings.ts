// The ONE place the frontend touches the Wails-injected `window.go`. Everything
// else imports `App` (or a probe) from here, so binding names and signatures
// are checked in one place (AppApi, declared in env.d.ts). The ast-grep rule
// ts-window-go-outside-binding-layer fails CI on any other access.
//
// `App` resolves `window.go.main.App` at call time, not at import time: the
// bindings are injected after the page loads, and specs install their mocks on
// `window.go` per test. A missing binding (plain-browser run, an older
// backend) reads as undefined, so `App.X()` throws a TypeError just as the
// direct access did.

type GoWindow = { go?: { main?: { App?: Partial<AppApi> } } }

function rawApp(): Partial<AppApi> | undefined {
  if (typeof window === 'undefined') return undefined
  return (window as unknown as GoWindow).go?.main?.App
}

export const App: AppApi = new Proxy({} as AppApi, {
  get: (_target, name) => (typeof name === 'string' ? rawApp()?.[name as keyof AppApi] : undefined),
})

// hasBinding reports whether the Go App exposes `name` right now: a feature
// probe for a plain-browser run or a backend older than the frontend.
export function hasBinding(name: keyof AppApi): boolean {
  return typeof rawApp()?.[name] === 'function'
}
