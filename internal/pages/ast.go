package pages

import "time"

// Node is the interface for all AST node types in a parsed page.
type Node interface {
	nodeType() string
}

// Page is the top-level AST for a parsed .vibe page.
type Page struct {
	Frontmatter Frontmatter
	Nodes       []Node
}

// Frontmatter holds YAML metadata from the page header.
type Frontmatter struct {
	Title   string            `yaml:"title"`
	Type    string            `yaml:"type"`    // notebook | form | dashboard
	Refresh string            `yaml:"refresh"` // e.g., "30s"
	Params  []ParamDef        `yaml:"params"`
	Extra   map[string]string `yaml:",inline"`
}

// ParamDef defines a parameter expected by a page.
type ParamDef struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
}

// TextNode holds plain markdown/text content.
type TextNode struct {
	Content string
}

func (n *TextNode) nodeType() string { return "text" }

// SQLBlockNode represents a fenced SQL code block.
type SQLBlockNode struct {
	Name   string
	Engine string        // "duckdb" | "sqlite", default "duckdb"
	Cache  time.Duration
	SQL    string
	Line   int // source line number for error reporting
}

func (n *SQLBlockNode) nodeType() string { return "sql_block" }

// DirectiveNode represents an inline directive like {table: ...}.
type DirectiveNode struct {
	Type   string         // "table" | "chart" | "metric" | "form" | "detail" | "if"
	Config map[string]any
	Line   int
}

func (n *DirectiveNode) nodeType() string { return "directive" }

// ConditionalNode represents an {if: ...} ... {/if} block.
type ConditionalNode struct {
	Condition string // SQL expression
	Children  []Node
	Line      int
}

func (n *ConditionalNode) nodeType() string { return "conditional" }
