package container

import (
	"database/sql"
	"fmt"
	"time"
)

// seedExampleData populates a new container with rich example tables, pages, and queries.
func seedExampleData(db *sql.DB) error {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin example seed transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// --- Example Tables ---

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS customers (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT NOT NULL,
		region TEXT NOT NULL,
		joined TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("failed to create customers table: %w", err)
	}

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS products (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		category TEXT NOT NULL,
		price REAL NOT NULL,
		stock INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("failed to create products table: %w", err)
	}

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS orders (
		id INTEGER PRIMARY KEY,
		customer_id INTEGER NOT NULL REFERENCES customers(id),
		product_id INTEGER NOT NULL REFERENCES products(id),
		quantity INTEGER NOT NULL,
		total REAL NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		ordered_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("failed to create orders table: %w", err)
	}

	// --- Seed Rows ---

	customers := [][]any{
		{1, "Alice Chen", "alice@example.com", "North", "2025-01-15"},
		{2, "Bob Martinez", "bob@example.com", "South", "2025-02-20"},
		{3, "Carol Jones", "carol@example.com", "East", "2025-03-10"},
		{4, "David Kim", "david@example.com", "West", "2025-04-05"},
		{5, "Eva Müller", "eva@example.com", "North", "2025-05-18"},
		{6, "Frank Osei", "frank@example.com", "South", "2025-06-22"},
		{7, "Grace Liu", "grace@example.com", "East", "2025-07-30"},
		{8, "Hiro Tanaka", "hiro@example.com", "West", "2025-08-12"},
	}
	for _, c := range customers {
		if _, err := tx.Exec("INSERT INTO customers (id, name, email, region, joined) VALUES (?, ?, ?, ?, ?)", c...); err != nil {
			return fmt.Errorf("failed to insert customer: %w", err)
		}
	}

	products := [][]any{
		{1, "Widget Pro", "Hardware", 29.99, 150},
		{2, "Gadget X", "Hardware", 49.99, 75},
		{3, "Service Plan Basic", "Services", 9.99, 999},
		{4, "Service Plan Pro", "Services", 24.99, 999},
		{5, "Accessory Pack", "Accessories", 14.99, 200},
		{6, "Premium Bundle", "Hardware", 99.99, 30},
	}
	for _, p := range products {
		if _, err := tx.Exec("INSERT INTO products (id, name, category, price, stock) VALUES (?, ?, ?, ?, ?)", p...); err != nil {
			return fmt.Errorf("failed to insert product: %w", err)
		}
	}

	orders := [][]any{
		{1, 1, 1, 2, 59.98, "completed", "2025-09-01"},
		{2, 2, 3, 1, 9.99, "completed", "2025-09-02"},
		{3, 3, 2, 1, 49.99, "completed", "2025-09-03"},
		{4, 1, 4, 1, 24.99, "completed", "2025-09-05"},
		{5, 4, 6, 1, 99.99, "completed", "2025-09-08"},
		{6, 5, 1, 3, 89.97, "completed", "2025-09-10"},
		{7, 6, 5, 2, 29.98, "completed", "2025-09-12"},
		{8, 7, 2, 1, 49.99, "shipped", "2025-09-15"},
		{9, 3, 3, 1, 9.99, "shipped", "2025-09-18"},
		{10, 8, 6, 2, 199.98, "shipped", "2025-09-20"},
		{11, 2, 1, 1, 29.99, "pending", "2025-09-22"},
		{12, 5, 4, 1, 24.99, "pending", "2025-09-25"},
	}
	for _, o := range orders {
		if _, err := tx.Exec("INSERT INTO orders (id, customer_id, product_id, quantity, total, status, ordered_at) VALUES (?, ?, ?, ?, ?, ?, ?)", o...); err != nil {
			return fmt.Errorf("failed to insert order: %w", err)
		}
	}

	// --- Example Pages ---

	pages := []struct {
		id, title, content, pageType string
		sortOrder                    int
	}{
		{
			id:        "welcome",
			title:     "Welcome to Vibe",
			sortOrder: 1,
			pageType:  "markdown",
			content: `---
title: Welcome to Vibe
---

# Welcome to Vibe

This file contains example data and pages to help you learn how Vibe works.
Browse the pages below to see each feature in action, then edit or delete them
to make this file your own.

## Key Concepts

**Pages** are markdown documents with two extensions:

1. **SQL Blocks** — fenced code blocks that execute queries and produce named result sets
2. **Directives** — single-line tags like ` + "`{table: source}`" + ` that render results as components

## Quick Example

Here is a live query and table — edit this page to see the source:

` + "```sql recent_orders engine=sqlite" + `
SELECT o.id, c.name AS customer, p.name AS product, o.total, o.status
FROM orders o
JOIN customers c ON c.id = o.customer_id
JOIN products p ON p.id = o.product_id
ORDER BY o.ordered_at DESC
LIMIT 5
` + "```" + `

{table: recent_orders, sortable: true}

## What's Next?

- **Dashboard** — charts, metrics, and Go template loops
- **Products Catalog** — table and detail directives
- **HTML Page** — raw HTML page type for full control
`,
		},
		{
			id:        "dashboard",
			title:     "Dashboard",
			sortOrder: 2,
			pageType:  "markdown",
			content: `---
title: Dashboard
---

# Sales Dashboard

` + "```sql revenue_by_region engine=sqlite" + `
SELECT c.region, SUM(o.total) AS revenue, COUNT(o.id) AS orders
FROM orders o
JOIN customers c ON c.id = o.customer_id
GROUP BY c.region
ORDER BY revenue DESC
` + "```" + `

` + "```sql order_status engine=sqlite" + `
SELECT status, COUNT(*) AS count, SUM(total) AS total
FROM orders GROUP BY status
` + "```" + `

` + "```sql top_products engine=sqlite" + `
SELECT p.name, p.category, SUM(o.quantity) AS units_sold, SUM(o.total) AS revenue
FROM orders o JOIN products p ON p.id = o.product_id
GROUP BY p.id ORDER BY revenue DESC
` + "```" + `

## Revenue by Region

{chart: revenue_by_region, type: bar, x: region, y: revenue, title: Revenue by Region}

## Order Status

{metric: order_status, label: Total Orders, sql: "SELECT COUNT(*) AS value FROM orders", engine: sqlite}

| Status | Count | Total |
|--------|-------|-------|
{{range .Results.order_status.Rows}}| {{index . 0}} | {{index . 1}} | {{index . 2 | fmt_currency}} |
{{end}}

## Top Products

{table: top_products, sortable: true}

## Regional Summary

{{range .Results.revenue_by_region.Rows}}
- **{{index . 0}}**: {{index . 1 | fmt_currency}} from {{index . 2}} orders
{{end}}
`,
		},
		{
			id:        "products",
			title:     "Products Catalog",
			sortOrder: 3,
			pageType:  "markdown",
			content: `---
title: Products Catalog
---

# Products Catalog

` + "```sql all_products engine=sqlite" + `
SELECT id, name, category, price, stock FROM products ORDER BY category, name
` + "```" + `

## Full Catalog

{table: all_products, sortable: true, searchable: true}

## Inventory Summary

` + "```sql inventory engine=sqlite" + `
SELECT category, COUNT(*) AS products, SUM(stock) AS total_stock,
       ROUND(AVG(price), 2) AS avg_price
FROM products GROUP BY category
` + "```" + `

{table: inventory}

## Low Stock Alert

{{if .Results.all_products.Rows}}
Products with less than 50 units in stock:

{{range .Results.all_products.Rows}}{{if lt (index . 4) 50}}
- **{{index . 1}}** ({{index . 2}}): only {{index . 4}} left @ {{index . 3 | fmt_currency}}
{{end}}{{end}}
{{end}}
`,
		},
		{
			id:        "html-example",
			title:     "HTML Page Example",
			sortOrder: 4,
			pageType:  "html",
			content: `---
title: HTML Page Example
type: html
---

` + "```sql customer_stats engine=sqlite" + `
SELECT c.region, COUNT(DISTINCT c.id) AS customers, COUNT(o.id) AS orders, COALESCE(SUM(o.total), 0) AS revenue
FROM customers c LEFT JOIN orders o ON o.customer_id = c.id
GROUP BY c.region ORDER BY revenue DESC
` + "```" + `

<div style="font-family: system-ui; max-width: 800px; margin: 0 auto; padding: 2rem;">
  <h1 style="border-bottom: 3px solid #000; padding-bottom: 0.5rem;">HTML Page Example</h1>

  <p>Pages with <code>type: html</code> in their frontmatter render text nodes as raw HTML
  instead of converting from markdown. SQL blocks and directives still work normally.</p>

  <h2>Regional Performance</h2>

  <table style="width: 100%; border-collapse: collapse; margin: 1rem 0;">
    <thead>
      <tr style="background: #000; color: #fff;">
        <th style="padding: 8px; text-align: left;">Region</th>
        <th style="padding: 8px; text-align: right;">Customers</th>
        <th style="padding: 8px; text-align: right;">Orders</th>
        <th style="padding: 8px; text-align: right;">Revenue</th>
      </tr>
    </thead>
    <tbody>
      {{range .Results.customer_stats.Rows}}
      <tr style="border-bottom: 1px solid #ddd;">
        <td style="padding: 8px;"><strong>{{index . 0}}</strong></td>
        <td style="padding: 8px; text-align: right;">{{index . 1}}</td>
        <td style="padding: 8px; text-align: right;">{{index . 2}}</td>
        <td style="padding: 8px; text-align: right;">{{index . 3 | fmt_currency}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  <h2>Using a Chart Directive Inside HTML</h2>
  <p>Directives work on their own line, even inside HTML pages:</p>
</div>

{chart: customer_stats, type: bar, x: region, y: revenue, title: Revenue by Region}

<div style="font-family: system-ui; max-width: 800px; margin: 0 auto; padding: 0 2rem 2rem;">
  <h2>Tips</h2>
  <ul>
    <li>Set <code>type: html</code> in frontmatter to enable raw HTML mode</li>
    <li>SQL blocks and directives work exactly the same as in markdown pages</li>
    <li>Go template syntax (<code>{{"{{"}}range}}</code>, <code>{{"{{"}}if}}</code>) works in HTML text nodes too</li>
    <li>Use this for pixel-perfect layouts, custom styling, or embedding external widgets</li>
  </ul>
</div>
`,
		},
		{
			id:        "template-guide",
			title:     "Go Template Guide",
			sortOrder: 5,
			pageType:  "markdown",
			content: `---
title: Go Template Guide
---

# Go Template Guide

Vibe pages support full Go template syntax in text blocks. Templates execute
**after** SQL blocks run, so you can loop over, filter, and format query results.

## Available Data

- ` + "`{{.Results.name}}`" + ` — access a named SQL block's result set
- ` + "`{{.Results.name.Rows}}`" + ` — the rows (each row is a slice of values)
- ` + "`{{.Results.name.Columns}}`" + ` — column metadata
- ` + "`{{.Results.name.ColNames}}`" + ` — column names as a string slice
- ` + "`{{.Params.key}}`" + ` — page parameters from URL or frontmatter
- ` + "`{{.Meta.title}}`" + ` — page metadata from frontmatter

## Template Functions

| Function | Example | Description |
|----------|---------|-------------|
| ` + "`index`" + ` | ` + "`{{index .row 0}}`" + ` | Get value at position in a row |
| ` + "`cell`" + ` | ` + "`{{cell .row 0}}`" + ` | Same as index, nil-safe |
| ` + "`col`" + ` | ` + "`{{col .Results.data 0}}`" + ` | Column name by index |
| ` + "`count`" + ` | ` + "`{{count .Results.data}}`" + ` | Number of rows |
| ` + "`sum`" + ` | ` + "`{{sum .Results.data 1}}`" + ` | Sum column values |
| ` + "`fmt_currency`" + ` | ` + "`{{42.5 \\| fmt_currency}}`" + ` | Format as $42.50 |
| ` + "`fmt_percent`" + ` | ` + "`{{0.85 \\| fmt_percent}}`" + ` | Format as 85.0% |
| ` + "`fmt_number`" + ` | ` + "`{{1234 \\| fmt_number}}`" + ` | Format as 1,234 |
| ` + "`json`" + ` | ` + "`{{json .Results.data}}`" + ` | Marshal to JSON |
| ` + "`upper`" + ` / ` + "`lower`" + ` | ` + "`{{\"hi\" \\| upper}}`" + ` | Case conversion |
| ` + "`add`" + ` / ` + "`sub`" + ` / ` + "`mul`" + ` | ` + "`{{add 1 2}}`" + ` | Integer arithmetic |
| ` + "`default`" + ` | ` + "`{{default \"N/A\" .val}}`" + ` | Fallback for nil/empty |

## Live Examples

` + "```sql sample engine=sqlite" + `
SELECT name, category, price, stock FROM products ORDER BY price DESC
` + "```" + `

### Range Loop — Build a Markdown List

{{range .Results.sample.Rows}}
- **{{index . 0}}** ({{index . 1}}): {{index . 2 | fmt_currency}}, {{index . 3}} in stock
{{end}}

### Conditional — Low Stock Warning

{{range .Results.sample.Rows}}{{if lt (index . 3) 50}}
> ⚠ **{{index . 0}}** has only {{index . 3}} units remaining!
{{end}}{{end}}

### Dynamic Table with Column Headers

| {{range .Results.sample.ColNames}}{{.}} | {{end}}
| {{range .Results.sample.ColNames}}--- | {{end}}
{{range .Results.sample.Rows}}| {{range .}}{{.}} | {{end}}
{{end}}
`,
		},
	}

	for _, p := range pages {
		if _, err := tx.Exec(
			`INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			p.id, p.title, p.content, p.pageType, p.sortOrder, now, now,
		); err != nil {
			return fmt.Errorf("failed to insert example page %q: %w", p.id, err)
		}
	}

	// --- Example Queries ---

	queries := []struct {
		id, title, sqlText, queryEngine string
	}{
		{
			id:          "revenue_by_customer",
			title:       "Revenue by Customer",
			queryEngine: "sqlite",
			sqlText:     "SELECT c.name, c.region, COUNT(o.id) AS orders, SUM(o.total) AS total_revenue\nFROM customers c\nJOIN orders o ON o.customer_id = c.id\nGROUP BY c.id\nORDER BY total_revenue DESC",
		},
		{
			id:          "product_performance",
			title:       "Product Performance",
			queryEngine: "sqlite",
			sqlText:     "SELECT p.name, p.category, p.price, SUM(o.quantity) AS units_sold,\n       SUM(o.total) AS revenue, p.stock AS remaining_stock\nFROM products p\nLEFT JOIN orders o ON o.product_id = p.id\nGROUP BY p.id\nORDER BY revenue DESC",
		},
		{
			id:          "monthly_orders",
			title:       "Monthly Order Summary",
			queryEngine: "sqlite",
			sqlText:     "SELECT strftime('%Y-%m', ordered_at) AS month,\n       COUNT(*) AS orders, SUM(total) AS revenue,\n       ROUND(AVG(total), 2) AS avg_order\nFROM orders\nGROUP BY month\nORDER BY month",
		},
	}

	for _, q := range queries {
		if _, err := tx.Exec(
			`INSERT INTO _vibe_queries (id, title, sql, engine, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			q.id, q.title, q.sqlText, q.queryEngine, now, now,
		); err != nil {
			return fmt.Errorf("failed to insert example query %q: %w", q.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit example seed transaction: %w", err)
	}

	return nil
}
