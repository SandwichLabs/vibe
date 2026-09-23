// Package pages is the legacy page markup parser: frontmatter + markdown +
// named SQL blocks + {directive:} tags into an AST (Page/Node).
//
// The page system itself is retired (#69/#66): there is no executor, no
// renderer, and no Studio page bindings anymore. This package survives in
// parser-only form because internal/notebooks.MigratePages still needs to
// parse an old page's content to translate it into notebook cells the first
// time a pre-#66 .vibe file is opened. Do not add execution, rendering, or
// new directive types here — that machinery was removed on purpose.
package pages
