package pages

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

// Parse parses a markdown page with frontmatter, SQL blocks, and directives
// into a Page AST. Uses goldmark to parse the markdown body, then walks the
// AST to extract SQL fenced code blocks and directive lines.
func Parse(input string) (*Page, error) {
	page := &Page{}
	lines := strings.Split(input, "\n")

	idx := 0

	// Parse optional frontmatter.
	if idx < len(lines) && strings.TrimSpace(lines[idx]) == "---" {
		fmEnd := -1
		for i := idx + 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				fmEnd = i
				break
			}
		}
		if fmEnd == -1 {
			return nil, fmt.Errorf("line %d: unclosed frontmatter (missing closing ---)", idx+1)
		}
		fmContent := strings.Join(lines[idx+1:fmEnd], "\n")
		if err := yaml.Unmarshal([]byte(fmContent), &page.Frontmatter); err != nil {
			return nil, fmt.Errorf("line %d: invalid frontmatter YAML: %w", idx+1, err)
		}
		idx = fmEnd + 1
	}

	// The body is everything after frontmatter.
	body := strings.Join(lines[idx:], "\n")
	bodyOffset := idx // line offset for error messages

	// Parse the body with goldmark.
	source := []byte(body)
	md := goldmark.New()
	reader := text.NewReader(source)
	doc := md.Parser().Parse(reader)

	// Walk the goldmark AST to build our page nodes.
	nodes, err := walkGoldmark(doc, source, bodyOffset)
	if err != nil {
		return nil, err
	}

	page.Nodes = nodes
	return page, nil
}

// walkGoldmark walks a goldmark AST document and extracts page nodes.
func walkGoldmark(doc ast.Node, source []byte, lineOffset int) ([]Node, error) {
	var nodes []Node

	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.FencedCodeBlock:
			lang := string(n.Language(source))

			if strings.HasPrefix(lang, "sql") {
				sqlNode, err := parseSQLFencedBlock(n, source, lang, lineOffset)
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, sqlNode)
				continue
			}

			// Non-SQL code blocks become text (rendered as code by goldmark downstream).
			var buf bytes.Buffer
			buf.WriteString("```")
			buf.WriteString(lang)
			buf.WriteString("\n")
			for i := 0; i < n.Lines().Len(); i++ {
				line := n.Lines().At(i)
				buf.Write(line.Value(source))
			}
			buf.WriteString("```")
			nodes = append(nodes, &TextNode{Content: buf.String()})

		case *ast.Paragraph:
			// Check if this paragraph is a directive line.
			content := extractParagraphText(n, source)
			trimmed := strings.TrimSpace(content)

			if isDirectiveLine(trimmed) {
				if isBlockEnd(trimmed) {
					// {/if} — shouldn't appear at top level.
					line := lineOffset + getNodeLine(n, source) + 1
					return nil, fmt.Errorf("line %d: unexpected %s without matching opening block", line, trimmed)
				}

				if isConditionalStart(trimmed) {
					// {if: ...} — collect children until {/if}.
					condNode, nextSibling, err := parseConditionalFromAST(n, child, source, lineOffset)
					if err != nil {
						return nil, err
					}
					nodes = append(nodes, condNode)
					// Advance past the conditional block.
					if nextSibling != nil {
						child = nextSibling
					}
					continue
				}

				if isRowStart(trimmed) {
					rowNode, nextSibling, err := parseRowFromAST(n, child, source, lineOffset)
					if err != nil {
						return nil, err
					}
					nodes = append(nodes, rowNode)
					if nextSibling != nil {
						child = nextSibling
					}
					continue
				}

				if kind := blockStartKind(trimmed); kind != "" {
					blockNode, nextSibling, err := parseBlockFromAST(kind, n, child, source, lineOffset)
					if err != nil {
						return nil, err
					}
					nodes = append(nodes, blockNode)
					if nextSibling != nil {
						child = nextSibling
					}
					continue
				}

				dirNode, err := parseDirective(trimmed, lineOffset+getNodeLine(n, source))
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, dirNode)
				continue
			}

			// Regular paragraph — store as text.
			nodes = append(nodes, &TextNode{Content: content})

		default:
			// All other block elements (headings, lists, blockquotes, thematic breaks, HTML blocks, etc.)
			// are rendered as text nodes. We extract the raw source lines.
			content := extractNodeSource(child, source)
			if content != "" {
				nodes = append(nodes, &TextNode{Content: content})
			}
		}
	}

	return nodes, nil
}

// parseSQLFencedBlock extracts a SQLBlockNode from a goldmark FencedCodeBlock.
// The info string format is: sql block_name [cache=5m]
func parseSQLFencedBlock(n *ast.FencedCodeBlock, source []byte, lang string, lineOffset int) (*SQLBlockNode, error) {
	// The info string after ``` is the full "sql block_name cache=5m" etc.
	// goldmark's Language() returns just "sql", but the full info is in the segment.
	info := string(n.Info.Segment.Value(source))
	parts := strings.Fields(info)

	line := lineOffset + getNodeLine(n, source) + 1

	node := &SQLBlockNode{
		Line: line,
	}

	// parts[0] is "sql"
	if len(parts) < 2 {
		return nil, fmt.Errorf("line %d: SQL block requires a name: ```sql block_name [cache=5m]", line)
	}

	// Support both positional name (```sql my_query) and key=value (```sql name=my_query).
	kvStart := 2
	if key, val, isKV := strings.Cut(parts[1], "="); isKV {
		// parts[1] is key=value — process it as an option, name must come from "name=".
		kvStart = 1
		_ = key
		_ = val
	} else {
		node.Name = parts[1]
	}

	// Parse key=value pairs.
	for _, part := range parts[kvStart:] {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: invalid SQL block option %q (expected key=value)", line, part)
		}
		switch key {
		case "name":
			node.Name = val
		case "engine":
			// Accepted for backward compatibility but ignored.
		case "cache":
			d, err := time.ParseDuration(val)
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid cache duration %q: %w", line, val, err)
			}
			node.Cache = d
		default:
			return nil, fmt.Errorf("line %d: unknown SQL block option %q", line, key)
		}
	}

	if node.Name == "" {
		return nil, fmt.Errorf("line %d: SQL block requires a name: ```sql block_name or ```sql name=block_name", line)
	}

	// Collect the SQL body.
	var sqlBuf strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		if sqlBuf.Len() > 0 {
			sqlBuf.WriteString("\n")
		}
		sqlBuf.WriteString(strings.TrimRight(string(line.Value(source)), "\n"))
	}
	node.SQL = strings.TrimSpace(sqlBuf.String())

	return node, nil
}

// parseConditionalFromAST parses an {if: ...} block by collecting sibling nodes
// until a {/if} paragraph is found.
func parseConditionalFromAST(startPara *ast.Paragraph, startChild ast.Node, source []byte, lineOffset int) (*ConditionalNode, ast.Node, error) {
	content := extractParagraphText(startPara, source)
	trimmed := strings.TrimSpace(content)
	inner := trimmed[1 : len(trimmed)-1]

	config, err := parseDirectiveYAML(inner)
	if err != nil {
		line := lineOffset + getNodeLine(startPara, source) + 1
		return nil, nil, fmt.Errorf("line %d: invalid conditional directive: %w", line, err)
	}

	condition, ok := config["if"]
	if !ok {
		line := lineOffset + getNodeLine(startPara, source) + 1
		return nil, nil, fmt.Errorf("line %d: conditional directive missing condition", line)
	}
	condStr, ok := condition.(string)
	if !ok {
		line := lineOffset + getNodeLine(startPara, source) + 1
		return nil, nil, fmt.Errorf("line %d: conditional expression must be a string", line)
	}

	// Optional "condition" mode selects which branch survives.
	// "rows_present" (default) survives when the result set has >0 rows;
	// "no_rows" survives when the result set is empty.
	mode := "rows_present"
	if modeVal, ok := config["condition"]; ok {
		if modeStr, ok := modeVal.(string); ok && modeStr != "" {
			mode = modeStr
		}
	}
	if mode != "rows_present" && mode != "no_rows" {
		line := lineOffset + getNodeLine(startPara, source) + 1
		return nil, nil, fmt.Errorf("line %d: unknown conditional mode %q (expected \"rows_present\" or \"no_rows\")", line, mode)
	}

	var children []Node
	var lastSibling ast.Node

	for sibling := startChild.NextSibling(); sibling != nil; sibling = sibling.NextSibling() {
		lastSibling = sibling

		// Check if this is a {/if} paragraph.
		if para, ok := sibling.(*ast.Paragraph); ok {
			paraText := strings.TrimSpace(extractParagraphText(para, source))
			if paraText == "{/if}" {
				return &ConditionalNode{
					Condition: condStr,
					Mode:      mode,
					Children:  children,
					Line:      lineOffset + getNodeLine(startPara, source) + 1,
				}, lastSibling, nil
			}
		}

		// Otherwise, process this sibling as a child node.
		childNodes, err := processASTNode(sibling, source, lineOffset)
		if err != nil {
			return nil, nil, err
		}
		children = append(children, childNodes...)
	}

	line := lineOffset + getNodeLine(startPara, source) + 1
	return nil, nil, fmt.Errorf("line %d: unclosed {if:} block (missing {/if})", line)
}

// processASTNode processes a single goldmark AST node into page nodes.
func processASTNode(n ast.Node, source []byte, lineOffset int) ([]Node, error) {
	// Process individual node types directly.
	switch node := n.(type) {
	case *ast.FencedCodeBlock:
		lang := string(node.Language(source))
		if strings.HasPrefix(lang, "sql") {
			sqlNode, err := parseSQLFencedBlock(node, source, lang, lineOffset)
			if err != nil {
				return nil, err
			}
			return []Node{sqlNode}, nil
		}
		var buf bytes.Buffer
		buf.WriteString("```")
		buf.WriteString(lang)
		buf.WriteString("\n")
		for i := 0; i < node.Lines().Len(); i++ {
			line := node.Lines().At(i)
			buf.Write(line.Value(source))
		}
		buf.WriteString("```")
		return []Node{&TextNode{Content: buf.String()}}, nil

	case *ast.Paragraph:
		content := extractParagraphText(node, source)
		trimmed := strings.TrimSpace(content)
		if isDirectiveLine(trimmed) && !isBlockEnd(trimmed) && !isConditionalStart(trimmed) && !isRowStart(trimmed) && blockStartKind(trimmed) == "" && !isTabMarker(trimmed) {
			dirNode, err := parseDirective(trimmed, lineOffset+getNodeLine(node, source))
			if err != nil {
				return nil, err
			}
			return []Node{dirNode}, nil
		}
		return []Node{&TextNode{Content: content}}, nil

	default:
		content := extractNodeSource(n, source)
		if content != "" {
			return []Node{&TextNode{Content: content}}, nil
		}
	}
	return nil, nil
}

// extractParagraphText extracts the raw text content of a paragraph node.
func extractParagraphText(n *ast.Paragraph, source []byte) string {
	var buf strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			buf.Write(t.Segment.Value(source))
			if t.SoftLineBreak() || t.HardLineBreak() {
				buf.WriteString("\n")
			}
		} else if raw, ok := c.(*ast.RawHTML); ok {
			for i := 0; i < raw.Segments.Len(); i++ {
				seg := raw.Segments.At(i)
				buf.Write(seg.Value(source))
			}
		} else {
			// For inline elements (emphasis, links, code spans, etc.),
			// extract their raw source.
			extractInlineText(c, source, &buf)
		}
	}
	return buf.String()
}

// extractInlineText recursively extracts text from inline AST nodes.
func extractInlineText(n ast.Node, source []byte, buf *strings.Builder) {
	if t, ok := n.(*ast.Text); ok {
		buf.Write(t.Segment.Value(source))
		if t.SoftLineBreak() || t.HardLineBreak() {
			buf.WriteString("\n")
		}
		return
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		extractInlineText(c, source, buf)
	}
}

// extractNodeSource extracts the raw source lines covered by an AST node.
func extractNodeSource(n ast.Node, source []byte) string {
	var buf bytes.Buffer

	// For block nodes with lines (like paragraphs, headings with content).
	if block, ok := n.(interface{ Lines() *text.Segments }); ok {
		lines := block.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			buf.Write(seg.Value(source))
		}
		if buf.Len() > 0 {
			return strings.TrimRight(buf.String(), "\n")
		}
	}

	// For container blocks (lists, blockquotes), reconstruct from children.
	// Walk children and collect their source text.
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		childText := extractNodeSource(c, source)
		if childText != "" {
			if buf.Len() > 0 {
				buf.WriteString("\n")
			}
			buf.WriteString(childText)
		}
	}

	// Fallback: try to get text content for leaf nodes.
	if buf.Len() == 0 {
		if t, ok := n.(*ast.Text); ok {
			buf.Write(t.Segment.Value(source))
		}
	}

	return strings.TrimRight(buf.String(), "\n")
}

// getNodeLine returns the 0-based line number of a goldmark node by counting
// newlines in the source up to the node's first byte.
func getNodeLine(n ast.Node, source []byte) int {
	if block, ok := n.(interface{ Lines() *text.Segments }); ok {
		if block.Lines().Len() > 0 {
			seg := block.Lines().At(0)
			return bytes.Count(source[:seg.Start], []byte("\n"))
		}
	}
	// For fenced code blocks, use the info segment.
	if fcb, ok := n.(*ast.FencedCodeBlock); ok {
		if fcb.Info != nil {
			return bytes.Count(source[:fcb.Info.Segment.Start], []byte("\n"))
		}
	}
	return 0
}

// isDirectiveLine checks if a line looks like a directive: {type: ...}.
// Excludes Go template syntax (lines starting with {{).
func isDirectiveLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") && !strings.HasPrefix(trimmed, "{{")
}

// isConditionalStart checks if a directive line is an {if: ...} block start.
func isConditionalStart(trimmed string) bool {
	inner := trimmed[1 : len(trimmed)-1]
	inner = strings.TrimSpace(inner)
	return strings.HasPrefix(inner, "if:")
}

// isBlockEnd checks if a line is a block closing tag ({/if}, {/row}, {/stack}, …).
func isBlockEnd(trimmed string) bool {
	switch trimmed {
	case "{/if}", "{/row}", "{/stack}", "{/tabs}", "{/callout}":
		return true
	}
	return false
}

// layoutBlockKinds enumerates the generalized layout blocks (P3.5) parsed via
// the sibling-collection-until-{/x} mechanism. {row} keeps its dedicated path.
var layoutBlockKinds = []string{"stack", "tabs", "callout"}

// blockStartKind returns the layout-block kind for an opening directive line
// (e.g. "{stack}", "{tabs}", "{callout: type: warning}"), or "" if the line is
// not a layout-block start.
func blockStartKind(trimmed string) string {
	inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	for _, kind := range layoutBlockKinds {
		// Match bare ("{stack}") or with config ("{callout: type: warning}").
		if inner == kind || strings.HasPrefix(inner, kind+":") {
			return kind
		}
	}
	return ""
}

// isTabMarker reports whether a directive line is a {tab: ...} section marker.
func isTabMarker(trimmed string) bool {
	inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	return strings.HasPrefix(inner, "tab:")
}

// parseBlockConfig parses the config of a layout-block opening directive given
// the block kind and the directive's inner text (e.g. kind="callout",
// inner=`callout: type: warning`). It strips the leading "kind" token and parses
// the remainder, supporting both nested-YAML (`type: warning`) and bare
// "key=value" (`type=warning`) forms. A label-only form (`tab: "Overview"`)
// yields {kind: "Overview"} so callers can read the primary value back.
func parseBlockConfig(kind, inner string) (map[string]any, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(inner), kind))
	rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))

	result := make(map[string]any)
	if rest == "" {
		return result, nil
	}

	// Bare "key=value" config (e.g. {row}-style "cols=3").
	if key, val, isKV := strings.Cut(rest, "="); isKV && !strings.Contains(rest, ":") {
		result[key] = val
		return result, nil
	}

	// A quoted-string label (e.g. {tab: "Overview"}) → primary value under kind.
	if strings.HasPrefix(rest, "\"") || strings.HasPrefix(rest, "'") {
		var label string
		if err := yaml.Unmarshal([]byte(rest), &label); err == nil {
			result[kind] = label
			return result, nil
		}
	}

	// Nested YAML config (e.g. "type: warning"). Wrap as a flow-friendly block.
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(rest), &parsed); err != nil {
		// Fall back to treating the remainder as the primary value.
		result[kind] = rest
		return result, nil
	}
	for k, v := range parsed {
		if v == nil {
			if key, val, isKV := strings.Cut(k, "="); isKV {
				result[key] = val
				continue
			}
		}
		result[k] = v
	}
	return result, nil
}

// parseBlockFromAST parses a generalized layout block ({stack}, {tabs}, {callout})
// by collecting sibling nodes until the matching {/kind} closing tag, mirroring
// the {row} parser. For {tabs}, {tab:} markers segment the children into nested
// "tab" BlockNodes carrying their label in Config["tab"].
func parseBlockFromAST(kind string, startPara *ast.Paragraph, startChild ast.Node, source []byte, lineOffset int) (*BlockNode, ast.Node, error) {
	startLine := lineOffset + getNodeLine(startPara, source) + 1

	content := strings.TrimSpace(extractParagraphText(startPara, source))
	inner := content[1 : len(content)-1]
	config, err := parseBlockConfig(kind, inner)
	if err != nil {
		return nil, nil, fmt.Errorf("line %d: invalid %s directive: %w", startLine, kind, err)
	}

	closeTag := "{/" + kind + "}"

	var children []Node
	var lastSibling ast.Node

	// For {tabs}, accumulate children into the currently-open {tab:} section.
	var currentTab *BlockNode

	flushTab := func() {
		if currentTab != nil {
			children = append(children, currentTab)
			currentTab = nil
		}
	}

	addChild := func(n Node) {
		if currentTab != nil {
			currentTab.Children = append(currentTab.Children, n)
			return
		}
		children = append(children, n)
	}

	for sibling := startChild.NextSibling(); sibling != nil; sibling = sibling.NextSibling() {
		lastSibling = sibling

		if para, ok := sibling.(*ast.Paragraph); ok {
			paraText := strings.TrimSpace(extractParagraphText(para, source))

			if paraText == closeTag {
				flushTab()
				return &BlockNode{
					Kind:     kind,
					Config:   config,
					Children: children,
					Line:     startLine,
				}, lastSibling, nil
			}

			// Within {tabs}, a {tab: "Label"} marker opens a new tab section.
			if kind == "tabs" && isDirectiveLine(paraText) && isTabMarker(paraText) {
				flushTab()
				tabInner := strings.TrimSpace(paraText[1 : len(paraText)-1])
				tabConfig, err := parseBlockConfig("tab", tabInner)
				if err != nil {
					tabLine := lineOffset + getNodeLine(para, source) + 1
					return nil, nil, fmt.Errorf("line %d: invalid tab directive: %w", tabLine, err)
				}
				// Preserve the tab label under Config["tab"].
				label := ""
				if labelVal, ok := tabConfig["tab"].(string); ok {
					label = labelVal
				}
				currentTab = &BlockNode{
					Kind:   "tab",
					Config: map[string]any{"tab": label},
					Line:   lineOffset + getNodeLine(para, source) + 1,
				}
				continue
			}
		}

		childNodes, err := processASTNode(sibling, source, lineOffset)
		if err != nil {
			return nil, nil, err
		}
		for _, cn := range childNodes {
			addChild(cn)
		}
	}

	return nil, nil, fmt.Errorf("line %d: unclosed {%s:} block (missing %s)", startLine, kind, closeTag)
}

// isRowStart checks if a directive line is a {row: ...} block start.
func isRowStart(trimmed string) bool {
	inner := trimmed[1 : len(trimmed)-1]
	inner = strings.TrimSpace(inner)
	return strings.HasPrefix(inner, "row:")
}

// parseRowFromAST parses a {row: ...} block by collecting sibling nodes
// until a {/row} paragraph is found.
func parseRowFromAST(startPara *ast.Paragraph, startChild ast.Node, source []byte, lineOffset int) (*RowNode, ast.Node, error) {
	content := extractParagraphText(startPara, source)
	trimmed := strings.TrimSpace(content)
	inner := trimmed[1 : len(trimmed)-1]

	config, err := parseDirectiveYAML(inner)
	if err != nil {
		line := lineOffset + getNodeLine(startPara, source) + 1
		return nil, nil, fmt.Errorf("line %d: invalid row directive: %w", line, err)
	}

	// Remove the "row" key itself, keep the rest as config.
	rowConfig := make(map[string]any)
	for k, v := range config {
		if k == "row" {
			// If row has a value like "cols=3", parse it.
			if s, ok := v.(string); ok && s != "" {
				if key, val, isKV := strings.Cut(s, "="); isKV {
					rowConfig[key] = val
				}
			}
			continue
		}
		rowConfig[k] = v
	}

	var children []Node
	var lastSibling ast.Node

	for sibling := startChild.NextSibling(); sibling != nil; sibling = sibling.NextSibling() {
		lastSibling = sibling

		if para, ok := sibling.(*ast.Paragraph); ok {
			paraText := strings.TrimSpace(extractParagraphText(para, source))
			if paraText == "{/row}" {
				return &RowNode{
					Config:   rowConfig,
					Children: children,
					Line:     lineOffset + getNodeLine(startPara, source) + 1,
				}, lastSibling, nil
			}
		}

		childNodes, err := processASTNode(sibling, source, lineOffset)
		if err != nil {
			return nil, nil, err
		}
		children = append(children, childNodes...)
	}

	line := lineOffset + getNodeLine(startPara, source) + 1
	return nil, nil, fmt.Errorf("line %d: unclosed {row:} block (missing {/row})", line)
}

// parseDirective parses a single directive line into a DirectiveNode.
func parseDirective(trimmed string, lineIdx int) (*DirectiveNode, error) {
	inner := trimmed[1 : len(trimmed)-1]

	config, err := parseDirectiveYAML(inner)
	if err != nil {
		return nil, fmt.Errorf("line %d: invalid directive: %w", lineIdx+1, err)
	}

	// The first key in the YAML map is the directive type. Its value is the primary argument.
	dirType, source, remaining := extractDirectiveType(config)
	if dirType == "" {
		return nil, fmt.Errorf("line %d: empty directive", lineIdx+1)
	}

	// Chart type aliases: {pie_chart: ...} → {chart: type=pie ...}
	if chartType, ok := strings.CutSuffix(dirType, "_chart"); ok {
		dirType = "chart"
		if remaining == nil {
			remaining = make(map[string]any)
		}
		remaining["type"] = chartType
	}

	// Build the config map. The primary positional value normally becomes
	// "source" (the named query a directive renders). Input-filter directives
	// (dropdown, button_group, slider, text_input, toggle) instead use the
	// primary value as the variable NAME they register, with the options query
	// supplied via an explicit "source=" token — so it must not collide with the
	// source key.
	result := make(map[string]any)
	primaryKey := "source"
	if isInputDirective(dirType) {
		primaryKey = "name"
	}
	if source != nil {
		// Support both "{table: query_name}" and "{table: source=query_name}" formats.
		// If the primary value is a string containing "=", parse it as key=value config.
		if s, ok := source.(string); ok {
			if key, val, isKV := strings.Cut(s, "="); isKV {
				result[key] = val
			} else {
				result[primaryKey] = source
			}
		} else {
			result[primaryKey] = source
		}
	}
	for k, v := range remaining {
		// Support the "key=value" form for secondary config entries (used by the
		// {subform} master-detail link, e.g. "link=order_id->id"). YAML parses
		// such a token as a bare key with a nil value, so split it back here.
		if v == nil {
			if key, val, isKV := strings.Cut(k, "="); isKV {
				result[key] = val
				continue
			}
		}
		result[k] = v
	}

	return &DirectiveNode{
		Type:   dirType,
		Config: result,
		Line:   lineIdx + 1,
	}, nil
}

// isInputDirective reports whether the directive type is an interactive input
// filter (P3.6). For these, the primary positional value is the variable name
// the control registers rather than a named-query source reference.
func isInputDirective(dirType string) bool {
	switch dirType {
	case "dropdown", "button_group", "slider", "text_input", "toggle":
		return true
	default:
		return false
	}
}

// parseDirectiveYAML parses the inner content of a directive as YAML.
func parseDirectiveYAML(inner string) (map[string]any, error) {
	// Wrap in braces to make it valid YAML flow mapping.
	yamlStr := "{" + inner + "}"
	var result map[string]any
	if err := yaml.Unmarshal([]byte(yamlStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse directive YAML: %w", err)
	}
	return result, nil
}

// extractDirectiveType finds the first key in the map (the directive type),
// extracts its value, and returns the remaining keys.
// Since Go maps are unordered, we re-parse to find the first key.
func extractDirectiveType(config map[string]any) (string, any, map[string]any) {
	if len(config) == 0 {
		return "", nil, nil
	}

	// To get a stable first key, we look at the keys and use a heuristic:
	// known directive types get priority.
	knownTypes := []string{"table", "chart", "pie_chart", "line_chart", "bar_chart", "area_chart", "scatter_chart", "doughnut_chart", "metric", "value", "form", "detail", "subform", "if", "row", "dropdown", "button_group", "slider", "text_input", "toggle"}
	for _, t := range knownTypes {
		if val, ok := config[t]; ok {
			remaining := make(map[string]any)
			for k, v := range config {
				if k != t {
					remaining[k] = v
				}
			}
			return t, val, remaining
		}
	}

	// Fallback: pick any key.
	for k, v := range config {
		remaining := make(map[string]any)
		for k2, v2 := range config {
			if k2 != k {
				remaining[k2] = v2
			}
		}
		return k, v, remaining
	}

	return "", nil, nil
}
