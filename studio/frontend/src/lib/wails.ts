// hasWailsRuntime reports whether the Wails runtime is injected (the desktop
// app or the Wails-bound dev server). Plain-browser and unit-test runs have no
// runtime, so every EventsOn / binding call is gated on this.
export const hasWailsRuntime = (): boolean =>
  typeof window !== 'undefined' && !!(window as unknown as { runtime?: unknown }).runtime
