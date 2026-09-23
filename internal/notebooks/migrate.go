package notebooks

import (
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/pages"
)

// MigratePages converts every not-yet-migrated `_vibe_pages` row into a
// notebook (issue #66 part A). It is idempotent: a page whose migrated_to
// column is already set is skipped, and running it again on an already-
// migrated file does nothing. Page rows are NEVER deleted or altered beyond
// stamping migrated_to — this is additive, like every other container
// migration.
//
// System pages (id prefixed with "_", e.g. the built-in "_dashboard" page)
// are skipped, matching ListPages' existing exclusion of them from the
// user-facing Pages list.
//
// Mapping (issue #66 part A):
//   - prose runs -> one markdown cell
//   - named SQL blocks -> one sql cell each (label = block name, view "table",
//     unpinned — "auto")
//   - {table:} -> an additional pinned sql cell, view "table", over the
//     referenced block's SQL
//   - {chart: type: bar|line} / {bar_chart:} / {line_chart:} -> a pinned sql
//     cell, view "bar"/"line"
//   - {metric:} / {value:} -> a pinned sql cell, view "stat"
//   - any other directive or layout block ({form}, {subform}, {button},
//     inputs, {if}, {row}, {stack}, {tabs}, {callout}, other chart kinds) ->
//     a markdown cell noting "Not migrated: <kind>" plus the original
//     directive text in a fenced block, so nothing is silently lost. Layout
//     blocks' children are still walked (only the grouping/conditional is
//     dropped, per the issue's stated gap).
//   - a SQL block containing `${var}` parameters keeps its SQL as-is (params
//     are not substitutable yet) with a note cell explaining why.
//   - an html-type page becomes a single markdown cell with a note, since raw
//     HTML is not rendered in a notebook.
func MigratePages(db *sql.DB, logger *slog.Logger) (int, error) {
	rows, err := db.Query(
		`SELECT id, title, content, page_type FROM _vibe_pages
		 WHERE (migrated_to IS NULL OR migrated_to = '') AND id NOT LIKE '\_%' ESCAPE '\'
		 ORDER BY sort_order, id`,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to list pages to migrate: %w", err)
	}
	type pageRow struct{ id, title, content, pageType string }
	var toMigrate []pageRow
	for rows.Next() {
		var p pageRow
		if err := rows.Scan(&p.id, &p.title, &p.content, &p.pageType); err != nil {
			rows.Close()
			return 0, fmt.Errorf("failed to scan page: %w", err)
		}
		toMigrate = append(toMigrate, p)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()

	migrated := 0
	for _, p := range toMigrate {
		nb, err := migratePage(p.id, p.title, p.content, p.pageType)
		if err != nil {
			if logger != nil {
				logger.Warn("page migration failed, skipping", "page_id", p.id, "error", err)
			}
			continue
		}
		warnings, err := migrateOnePage(db, p.id, nb)
		if err != nil {
			if logger != nil {
				logger.Warn("failed to migrate page, rolled back", "page_id", p.id, "error", err)
			}
			continue
		}
		for _, w := range warnings {
			if logger != nil {
				logger.Warn("notebook migration warning", "page_id", p.id, "warning", w)
			}
		}
		migrated++
	}
	return migrated, nil
}

// migrateOnePage inserts nb and stamps the source page's migrated_to in ONE
// transaction (review fix: without this, a crash or error between the two
// statements could leave an orphaned notebook with no page ever pointing to
// it, or — if the order were reversed — a page marked migrated with no
// notebook behind it). Either both happen or neither does.
func migrateOnePage(db *sql.DB, pageID string, nb Notebook) ([]string, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin migration transaction for page %q: %w", pageID, err)
	}
	defer tx.Rollback() //nolint:errcheck

	saved, warnings, err := Insert(tx, nb)
	if err != nil {
		return nil, fmt.Errorf("failed to save migrated notebook for page %q: %w", pageID, err)
	}
	if _, err := tx.Exec(`UPDATE _vibe_pages SET migrated_to = ? WHERE id = ?`, saved.ID, pageID); err != nil {
		return nil, fmt.Errorf("failed to mark page %q migrated: %w", pageID, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit migration for page %q: %w", pageID, err)
	}
	return warnings, nil
}

var varParamRe = regexp.MustCompile(`\$\{[^}]+\}`)

// fenceFor returns a backtick fence long enough to safely wrap s as a
// fenced code block — one backtick longer than the longest run of
// backticks already inside s (minimum 3), so a page whose prose itself
// contains a ``` (or longer) fence can never prematurely close the note's
// own fence (review fix, step 4a #11a).
func fenceFor(s string) string {
	longest, cur := 0, 0
	for _, r := range s {
		if r == '`' {
			cur++
			if cur > longest {
				longest = cur
			}
		} else {
			cur = 0
		}
	}
	n := longest + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}

// fence wraps s in a fence long enough for it, with an optional language tag.
func fence(lang, s string) string {
	f := fenceFor(s)
	return f + lang + "\n" + s + "\n" + f + "\n"
}

// migratePage converts a single page's content into a Notebook (not yet
// persisted).
func migratePage(pageID, title, content, pageType string) (Notebook, error) {
	if pageType == "html" {
		nb := Empty(displayTitle(title, pageID))
		cell, _ := NewCell(CellMarkdown)
		cell.Source = "**Not migrated: raw HTML page.** Raw HTML is not rendered in a notebook. " +
			"The original page content is kept below for reference.\n\n" + fence("html", content)
		nb.Cells = append(nb.Cells, cell)
		return nb, nil
	}

	parsed, err := pages.Parse(content)
	if err != nil {
		nb := Empty(displayTitle(title, pageID))
		cell, _ := NewCell(CellMarkdown)
		cell.Source = fmt.Sprintf("**Not migrated: this page could not be parsed (%v).** The original content is kept below.\n\n%s", err, fence("markdown", content))
		nb.Cells = append(nb.Cells, cell)
		return nb, nil
	}

	nbTitle := title
	if parsed.Frontmatter.Title != "" {
		nbTitle = parsed.Frontmatter.Title
	}
	nb := Empty(displayTitle(nbTitle, pageID))

	rawLines := strings.Split(content, "\n")
	m := &migrator{
		sqlBlocks: map[string]string{}, consumed: map[string]bool{},
		rawLines: rawLines, cursor: frontmatterEndLine(rawLines) + 1,
	}
	m.walk(parsed.Nodes)
	m.flushGapTo(len(rawLines))
	m.flushMarkdown()
	m.emitUnconsumedBlocks()

	nb.Cells = m.cells
	if len(nb.Cells) == 0 {
		// Never persist a notebook with zero cells — an empty page still
		// deserves a placeholder so the migration is visibly complete.
		cell, _ := NewCell(CellMarkdown)
		cell.Source = "_This page had no content to migrate._"
		nb.Cells = append(nb.Cells, cell)
	}
	return nb, nil
}

// frontmatterEndLine returns the 1-indexed line number of the closing "---"
// of a YAML frontmatter block at the very start of rawLines, or 0 if there
// is none — mirroring pages.Parse's own frontmatter detection, duplicated
// here (rather than exported from internal/pages) because it is only used
// to keep the raw-line cursor below out of the frontmatter, not to parse it.
func frontmatterEndLine(rawLines []string) int {
	if len(rawLines) == 0 || strings.TrimSpace(rawLines[0]) != "---" {
		return 0
	}
	for i := 1; i < len(rawLines); i++ {
		if strings.TrimSpace(rawLines[i]) == "---" {
			return i + 1
		}
	}
	return 0
}

func displayTitle(title, fallbackID string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	return fallbackID
}

// migrator accumulates cells while walking a parsed page's node list.
//
// Markdown text is NEVER taken from a TextNode's own .Content — goldmark's
// inline parser tokenizes `**bold**`/`_em_`/`[text](url)`/etc. into
// structural nodes wrapping a bare Text node, so reconstructing text from
// Text-node segments (as the html renderer's own extractParagraphText does)
// silently drops every markdown marker (review fix, step 4a #5). Instead,
// rawLines holds the page's ORIGINAL content split on "\n", and every
// structural node (sql block, directive, layout block) is resolved to its
// raw [startLine, endLine] span; whatever raw lines fall in the GAPS between
// those spans — never reformatted, never re-derived from any AST text — are
// what becomes markdown cell content, via flushGapTo/appendMarkdown.
type migrator struct {
	cells     []Cell
	mdBuf     strings.Builder
	sqlBlocks map[string]string // name -> SQL, for blocks not yet consumed by a directive
	consumed  map[string]bool
	rawLines  []string // page content split on "\n"; rawLines[i] is 1-indexed line i+1
	cursor    int      // next unconsumed 1-indexed line
}

// rawLineRange returns the raw text of lines [startLine, endLine] (1-indexed,
// inclusive), exactly as written in the original page.
func (m *migrator) rawLineRange(startLine, endLine int) string {
	if startLine > endLine || startLine < 1 {
		return ""
	}
	lo := startLine - 1
	hi := endLine
	if hi > len(m.rawLines) {
		hi = len(m.rawLines)
	}
	if lo >= hi {
		return ""
	}
	return strings.Join(m.rawLines[lo:hi], "\n")
}

// flushGapTo appends the raw lines from m.cursor through endLine
// (1-indexed, inclusive) as markdown text and advances the cursor past it.
func (m *migrator) flushGapTo(endLine int) {
	if endLine < m.cursor {
		return
	}
	m.appendMarkdown(m.rawLineRange(m.cursor, endLine))
	m.cursor = endLine + 1
}

// findFenceStart scans BACKWARD from a fenced SQL block's first BODY line
// (1-indexed — see the doc comment on SQLBlockNode's case in walk() for why
// node.Line is the first line of SQL, not the ```sql opener line) for that
// opener line, returning its 1-indexed line number. Falls back to bodyLine
// itself (degrading to the old, slightly-wrong-but-non-crashing behavior)
// if somehow not found. TrimSpace handles a trailing "\r" from CRLF content
// the same way as every other raw-line comparison in this file.
func (m *migrator) findFenceStart(bodyLine int) int {
	for i := bodyLine - 2; i >= 0; i-- { // i is 0-indexed => line i+1, scanning back from bodyLine-1
		if strings.HasPrefix(strings.TrimSpace(m.rawLines[i]), "```") {
			return i + 1
		}
	}
	return bodyLine
}

// findFenceEnd scans forward from a fenced SQL block's first body line
// (1-indexed) for the matching closing "```" line, returning its 1-indexed
// line number — or the end of the page if the fence is unterminated. An
// unterminated fence is exactly what CommonMark/goldmark itself does with
// one (consumes every remaining line as code — see SQLBlockNode.SQL, which
// the parser already built that way), so treating the rest of the page as
// this block's span here is consistent with that, and — the point of this
// function's review fix — never lets an unterminated fence's opener leak
// into a markdown cell, nor duplicates its "trailing" content as prose that
// goldmark itself already folded into the SQL body.
func (m *migrator) findFenceEnd(bodyLine int) int {
	for i := bodyLine - 1; i < len(m.rawLines); i++ { // i is 0-indexed => line i+1
		if strings.TrimSpace(m.rawLines[i]) == "```" {
			return i + 1
		}
	}
	return len(m.rawLines)
}

// findCloseTag scans forward from a layout block's opening line (1-indexed)
// for its matching "{/kind}" close tag, tracking nesting depth so a nested
// block of the SAME kind doesn't close the outer one early. Returns the
// close tag's 1-indexed line number, or the end of the page if unterminated.
func (m *migrator) findCloseTag(openLine int, openPrefix, closeTag string) int {
	depth := 1
	for i := openLine; i < len(m.rawLines); i++ { // i 0-indexed => line i+1, starts right after the opening line
		t := strings.TrimSpace(m.rawLines[i])
		if t == closeTag {
			depth--
			if depth == 0 {
				return i + 1
			}
			continue
		}
		if strings.HasPrefix(t, openPrefix) {
			depth++
		}
	}
	return len(m.rawLines)
}

func (m *migrator) appendMarkdown(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if m.mdBuf.Len() > 0 {
		m.mdBuf.WriteString("\n\n")
	}
	m.mdBuf.WriteString(text)
}

func (m *migrator) flushMarkdown() {
	text := strings.TrimSpace(m.mdBuf.String())
	m.mdBuf.Reset()
	if text == "" {
		return
	}
	cell, _ := NewCell(CellMarkdown)
	cell.Source = text
	m.cells = append(m.cells, cell)
}

func (m *migrator) addSQLCell(source, view, label string, pinned bool) {
	cell, _ := NewCell(CellSQL)
	cell.Source = source
	cell.View = view
	cell.ViewPinned = pinned
	cell.Label = label
	m.cells = append(m.cells, cell)
	if varParamRe.MatchString(source) {
		note, _ := NewCell(CellMarkdown)
		note.Source = fmt.Sprintf("_Note: the SQL above for **%s** used `${var}` parameters, which notebooks do not yet support substituting. The SQL is kept as written._", labelOr(label, "this query"))
		m.cells = append(m.cells, note)
	}
}

func labelOr(label, fallback string) string {
	if strings.TrimSpace(label) != "" {
		return label
	}
	return fallback
}

func (m *migrator) notMigrated(kind, original string) {
	m.flushMarkdown()
	cell, _ := NewCell(CellMarkdown)
	cell.Source = fmt.Sprintf("**Not migrated: %s.** The original directive is kept below for reference.\n\n%s", kind, fence("", strings.TrimSpace(original)))
	m.cells = append(m.cells, cell)
}

// notMigratedShort is notMigrated's counterpart for a layout WRAPPER
// ({row:}, {stack:}, {tabs:}, {callout:}, {if:}) whose children are still
// walked and turned into real cells right after this note (review fix,
// step 4a round 2 #3) — unlike an unresolvable directive, dumping the
// wrapper's entire raw span here would duplicate content the recursive
// walk is about to emit properly as its own cells.
func (m *migrator) notMigratedShort(note string) {
	m.flushMarkdown()
	cell, _ := NewCell(CellMarkdown)
	cell.Source = "_Not migrated: " + note + "._"
	m.cells = append(m.cells, cell)
}

func (m *migrator) walk(nodes []pages.Node) {
	for _, n := range nodes {
		switch node := n.(type) {
		case *pages.TextNode:
			// No-op: prose is recovered from the raw line gaps around the
			// structural nodes below, not from this node's own .Content.

		case *pages.SQLBlockNode:
			// Review fix, step 4a round 2 #4: node.Line is the fenced
			// block's FIRST SQL BODY line, not the ```sql opener line (the
			// parser's getNodeLine prefers a FencedCodeBlock's own Lines()
			// — its code CONTENT segments — over the Info segment the
			// opener sits on). Flushing the prose gap up to node.Line-1
			// therefore used to include the ```sql opener line itself in
			// the preceding markdown cell. findFenceStart walks back to
			// find the REAL opener line so the gap stops one line earlier.
			fenceStart := m.findFenceStart(node.Line)
			m.flushGapTo(fenceStart - 1)
			end := m.findFenceEnd(node.Line)
			m.sqlBlocks[node.Name] = node.SQL
			m.cursor = end + 1

		case *pages.DirectiveNode:
			m.flushGapTo(node.Line - 1)
			m.handleDirective(node)
			m.cursor = node.Line + 1

		case *pages.ConditionalNode:
			// Review fix, step 4a round 2 #3: layout wrappers must still
			// let their CONTENTS become cells — only the grouping itself
			// (the {if:}/{/if} pair, i.e. the conditional logic) is
			// dropped, per the issue's stated gap. A short note replaces
			// the old "swallow the whole span verbatim" behavior, then the
			// children are walked with the cursor positioned just past the
			// opening line, so any sql block / directive / prose inside
			// still becomes real cells.
			end := m.findCloseTag(node.Line, "{if", "{/if}")
			m.flushGapTo(node.Line - 1)
			m.notMigratedShort(fmt.Sprintf("{if:} conditional block (%s) — the condition itself was dropped, its contents below were kept", node.Condition))
			m.cursor = node.Line + 1
			m.walk(node.Children)
			m.flushGapTo(end - 1)
			m.cursor = end + 1

		case *pages.RowNode:
			end := m.findCloseTag(node.Line, "{row", "{/row}")
			m.flushGapTo(node.Line - 1)
			m.notMigratedShort("{row:} layout block — the grid grouping was dropped, its contents below were kept")
			m.cursor = node.Line + 1
			m.walk(node.Children)
			m.flushGapTo(end - 1)
			m.cursor = end + 1

		case *pages.BlockNode:
			if node.Kind == "tab" {
				// A {tab:} marker has no directive semantics of its own —
				// just flatten its children into the surrounding flow,
				// skipping past the "{tab: ...}" marker line itself.
				m.flushGapTo(node.Line - 1)
				m.cursor = node.Line + 1
				m.walk(node.Children)
				continue
			}
			end := m.findCloseTag(node.Line, "{"+node.Kind, "{/"+node.Kind+"}")
			m.flushGapTo(node.Line - 1)
			m.notMigratedShort(fmt.Sprintf("{%s} layout block — the grouping was dropped, its contents below were kept", node.Kind))
			m.cursor = node.Line + 1
			m.walk(node.Children)
			m.flushGapTo(end - 1)
			m.cursor = end + 1
		}
	}
}

func (m *migrator) handleDirective(node *pages.DirectiveNode) {
	source, _ := node.Config["source"].(string)
	label, _ := node.Config["label"].(string)
	if label == "" {
		if t, ok := node.Config["title"].(string); ok {
			label = t
		}
	}
	if label == "" {
		label = source
	}

	sql, haveSQL := m.sqlBlocks[source]
	raw := m.rawLineRange(node.Line, node.Line)

	switch node.Type {
	case "table":
		m.flushMarkdown()
		if !haveSQL {
			m.notMigrated(fmt.Sprintf("{table:} directive referencing unknown source %q", source), raw)
			return
		}
		m.consumed[source] = true
		m.addSQLCell(sql, "table", label, true)

	case "chart":
		chartType, _ := node.Config["type"].(string)
		view := ""
		switch chartType {
		case "bar":
			view = "bar"
		case "line":
			view = "line"
		}
		if view == "" || !haveSQL {
			m.notMigrated(fmt.Sprintf("{chart: type=%s} directive (unsupported chart type or unknown source)", chartType), raw)
			return
		}
		m.flushMarkdown()
		m.consumed[source] = true
		m.addSQLCell(sql, view, label, true)

	case "metric", "value":
		m.flushMarkdown()
		if !haveSQL {
			m.notMigrated(fmt.Sprintf("{%s:} directive referencing unknown source %q", node.Type, source), raw)
			return
		}
		m.consumed[source] = true
		m.addSQLCell(sql, "stat", label, true)

	default:
		m.notMigrated(fmt.Sprintf("{%s:} directive", node.Type), raw)
	}
}

// emitUnconsumedBlocks appends a trailing sql cell for every named SQL block
// that no directive turned into a pinned cell, so a block that only ever fed
// e.g. a {{range}} Go-template loop still becomes a notebook cell instead of
// vanishing.
func (m *migrator) emitUnconsumedBlocks() {
	names := make([]string, 0, len(m.sqlBlocks))
	for name := range m.sqlBlocks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if m.consumed[name] {
			continue
		}
		m.addSQLCell(m.sqlBlocks[name], "table", name, false)
	}
}
