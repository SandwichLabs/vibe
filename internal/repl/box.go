package repl

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// minColWidth is the narrowest a column is squeezed to (unless its own name or
// type is wider).
const minColWidth = 10

// layout is which columns a box shows and how wide each is. A column index of
// -1 is the "…" column standing for the ones left out.
type layout struct {
	cols   []int
	widths []int
	hidden bool
	shown  int // original columns shown
}

func (g *grid) naturalWidths() (natural, floor []int) {
	n := len(g.headers)
	natural = make([]int, n)
	floor = make([]int, n)
	for i := 0; i < n; i++ {
		hw := cellWidth(g.headers[i])
		if g.types != nil {
			hw = max(hw, cellWidth(g.types[i]))
		}
		w := hw
		for _, r := range g.rows {
			w = max(w, cellWidth(r[i].text))
		}
		natural[i] = max(w, 1)
		floor[i] = min(natural[i], max(minColWidth, hw))
		if g.numeric[i] {
			floor[i] = natural[i] // a number is never wrapped: it reads as two
		}
	}
	return natural, floor
}

func boxWidth(widths []int) int {
	total := 1
	for _, w := range widths {
		total += w + 3
	}
	return total
}

// fitLayout shrinks the widest columns toward their floors until the box fits
// limit cells, then, if it still does not, leaves columns out from the middle
// (keeping the first and the last) behind one "…" column.
func fitLayout(g *grid, limit int) layout {
	widths, floor := g.naturalWidths()
	n := len(widths)
	for boxWidth(widths) > limit && shrinkWidest(widths, floor, boxWidth(widths)-limit) {
	}
	all := make([]int, n)
	for i := range all {
		all[i] = i
	}
	if boxWidth(widths) <= limit || n <= 1 {
		return layout{cols: all, widths: widths, shown: n}
	}
	for k := n - 1; k >= 1; k-- {
		a := (k + 1) / 2
		cols := append([]int{}, all[:a]...)
		cols = append(cols, -1)
		cols = append(cols, all[n-(k-a):]...)
		ws := make([]int, len(cols))
		for i, c := range cols {
			ws[i] = 1
			if c >= 0 {
				ws[i] = widths[c]
			}
		}
		if boxWidth(ws) <= limit || k == 1 {
			return layout{cols: cols, widths: ws, hidden: true, shown: k}
		}
	}
	return layout{cols: all, widths: widths, shown: n}
}

// shrinkWidest narrows the widest column above its floor: to the next widest
// width, by at most excess cells, and by at least one. It reports whether it
// could.
func shrinkWidest(widths, floor []int, excess int) bool {
	best := -1
	for i, w := range widths {
		if w > floor[i] && (best < 0 || w > widths[best]) {
			best = i
		}
	}
	if best < 0 {
		return false
	}
	second := 0
	for i, w := range widths {
		if i != best {
			second = max(second, w)
		}
	}
	target := max(second, floor[best], widths[best]-excess)
	if target >= widths[best] {
		target = widths[best] - 1
	}
	widths[best] = max(target, floor[best])
	return true
}

// boxDrawer writes one table.
type boxDrawer struct {
	w   io.Writer
	g   *grid
	lay layout
	pal palette
}

func (d *boxDrawer) rule(l, m, r string) {
	parts := make([]string, len(d.lay.widths))
	for i, w := range d.lay.widths {
		parts[i] = strings.Repeat("─", w+2)
	}
	fmt.Fprintln(d.w, l+strings.Join(parts, m)+r)
}

// headerLine is a centred row (names, types), dim for types.
func (d *boxDrawer) headerLine(texts []string, dim bool) {
	parts := make([]string, len(d.lay.cols))
	for i, c := range d.lay.cols {
		t := "…"
		if c >= 0 {
			t = texts[c]
		}
		w := d.lay.widths[i]
		pad := w - cellWidth(t)
		left := max(pad/2, 0)
		if dim {
			t = d.pal.dim(t)
		}
		parts[i] = " " + strings.Repeat(" ", left) + t + strings.Repeat(" ", max(pad-left, 0)) + " "
	}
	fmt.Fprintln(d.w, "│"+strings.Join(parts, "│")+"│")
}

// bodyCell is one line of a cell, padded to its column and aligned.
func (d *boxDrawer) bodyCell(text string, null bool, w int, right bool) string {
	pad := strings.Repeat(" ", max(w-cellWidth(text), 0))
	if null {
		text = d.pal.dim(text)
	}
	if right {
		return " " + pad + text + " "
	}
	return " " + text + pad + " "
}

// bodyRow writes one row, wrapping any cell wider than its column onto more
// lines (the other cells of the row go blank there).
func (d *boxDrawer) bodyRow(row []cell) {
	lines := make([][]string, len(d.lay.cols))
	height := 1
	for i, c := range d.lay.cols {
		t := "…"
		if c >= 0 {
			t = row[c].text
		}
		lines[i] = wrapCell(t, d.lay.widths[i])
		height = max(height, len(lines[i]))
	}
	for h := 0; h < height; h++ {
		parts := make([]string, len(d.lay.cols))
		for i, c := range d.lay.cols {
			text := ""
			if h < len(lines[i]) {
				text = lines[i][h]
			}
			right := c >= 0 && d.g.numeric[c]
			null := c >= 0 && row[c].null && h == 0
			parts[i] = d.bodyCell(text, null, d.lay.widths[i], right)
		}
		fmt.Fprintln(d.w, "│"+strings.Join(parts, "│")+"│")
	}
}

// elisionBand is the three dots rows between the head and the tail.
func (d *boxDrawer) elisionBand() {
	for k := 0; k < 3; k++ {
		parts := make([]string, len(d.lay.cols))
		for i, c := range d.lay.cols {
			parts[i] = d.bodyCell("·", false, d.lay.widths[i], c >= 0 && d.g.numeric[c])
		}
		fmt.Fprintln(d.w, "│"+strings.Join(parts, "│")+"│")
	}
}

// footerLines draws the lines under the box: the row count centred, or, when
// columns were left out, the row count at the left and the column count at the
// right edge.
func (d *boxDrawer) footerLines() {
	lines := append([]string{}, d.g.footer...)
	colText := ""
	if d.lay.hidden {
		colText = fmt.Sprintf("%d columns (%d shown)", len(d.g.headers), d.lay.shown)
		if len(lines) == 0 {
			lines = []string{fmt.Sprintf("%d rows", d.g.total)}
		}
	}
	width := boxWidth(d.lay.widths)
	for i, l := range lines {
		if colText == "" {
			lead := max((width-cellWidth(l))/2, 0)
			fmt.Fprintln(d.w, strings.Repeat(" ", lead)+d.pal.dim(l))
			continue
		}
		if i == 0 {
			gap := max(width-2-cellWidth(l)-cellWidth(colText), 2)
			fmt.Fprintln(d.w, "  "+d.pal.dim(l)+strings.Repeat(" ", gap)+d.pal.dim(colText))
			continue
		}
		fmt.Fprintln(d.w, "  "+d.pal.dim(l))
	}
}

// widenForFooter gives the last column the width the footer lines need, so the
// box is never narrower than its footer.
func widenForFooter(lay *layout, g *grid) {
	need := 0
	for _, l := range g.footer {
		need = max(need, cellWidth(l))
	}
	if len(lay.widths) == 0 {
		return
	}
	if have := boxWidth(lay.widths) - 4; need > have {
		lay.widths[len(lay.widths)-1] += need - have
	}
}

// renderBox draws g as a box: duckbox when it has a type row and footers,
// the plainer box otherwise.
func renderBox(ctx context.Context, w io.Writer, g *grid, o renderOpts) error {
	limit := o.st.maxwidth
	if limit <= 0 {
		limit = o.width
	}
	lay := fitLayout(g, limit)
	if g.footers {
		widenForFooter(&lay, g)
	}
	d := &boxDrawer{w: w, g: g, lay: lay, pal: o.pal}
	d.rule("┌", "┬", "┐")
	d.headerLine(g.headers, false)
	if g.types != nil {
		d.headerLine(g.types, true)
	}
	if len(g.rows) > 0 {
		d.rule("├", "┼", "┤")
	}
	for i, row := range g.rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if g.elideAt == i {
			d.elisionBand()
		}
		d.bodyRow(row)
	}
	d.rule("└", "┴", "┘")
	if g.footers {
		d.footerLines()
	}
	return nil
}
