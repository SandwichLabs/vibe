package awssso

// Bypass ignores the in-memory caches. Step 7's rotation (#131) is its first
// production caller; until then only tests use it, so it lives here.
func Bypass() CredOption { return func(c *credCall) { c.bypass = true } }
