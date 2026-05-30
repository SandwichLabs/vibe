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
	Name  string
	Cache time.Duration
	SQL   string
	Line  int // source line number for error reporting
}

func (n *SQLBlockNode) nodeType() string { return "sql_block" }

// DirectiveNode represents an inline directive like {table: ...}.
type DirectiveNode struct {
	Type   string // "table" | "chart" | "metric" | "form" | "detail" | "if"
	Config map[string]any
	Line   int
}

func (n *DirectiveNode) nodeType() string { return "directive" }

// ConditionalNode represents an {if: ...} ... {/if} block.
type ConditionalNode struct {
	Condition string // name of the SQL result set to test
	Mode      string // "rows_present" (default) | "no_rows"
	Children  []Node
	Line      int
}

func (n *ConditionalNode) nodeType() string { return "conditional" }

// RowNode represents a {row: cols=3} ... {/row} grid layout block.
type RowNode struct {
	Config   map[string]any
	Children []Node
	Line     int
}

func (n *RowNode) nodeType() string { return "row" }

// BlockNode is the generalized layout-block AST node (P3.5). It represents
// {stack}...{/stack}, {tabs}{tab:..}...{/tabs}, and {callout: type:..}...{/callout}
// blocks, reusing the {row} sibling-collection-until-{/x} mechanism. Kind names
// the block type ("stack", "tabs", "tab", "callout"); Config carries directive
// options; Children holds the collected nodes.
//
// STUB (TDD RED, P3.5): type declared so the parser/executor/render tests
// compile and fail on assertions; no parser/executor/render wiring exists yet.
type BlockNode struct {
	Kind     string
	Config   map[string]any
	Children []Node
	Line     int
}

func (n *BlockNode) nodeType() string { return "block" }
