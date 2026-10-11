// Package templates holds the built-in workspace templates that "new
// workspace" and the importer scaffold from.
package templates

// Bundled starter templates (plan §6 / P6.6). A template is an embedded builder
// that seeds a fresh workspace (or legacy container) with a complete little relational application: DuckDB
// user tables (with inline foreign keys so referential integrity is actually
// enforced — DuckDB cannot ALTER a FK onto an existing table, so the constraint
// is declared inline at CREATE time), sample data, and one starter notebook
// (#68: forms/switchboard/macros/nav/reports were dropped from every template
// along with the Access application layer; each template is now just tables +
// seed data + a notebook).

import (
	"context"
	"fmt"
	"sort"

	"github.com/SandwichLabs/vibe/internal/notebooks"
)

// Table describes one DuckDB user-data table to create for a template.
// CreateSQL must be a full CREATE TABLE statement (with inline REFERENCES for any
// foreign keys, since DuckDB only enforces FKs declared at table-create time).
type Table struct {
	Name      string
	CreateSQL string
	Inserts   []string
}

// Template is a complete starter-app definition: tables + seed data, plus a
// starter notebook built directly as a notebooks.Notebook (built via
// internal/notebooks so it passes Coerce the same way any agent- or
// user-authored notebook does).
type Template struct {
	Name   string
	Title  string
	Tables []Table
	Book   notebooks.Notebook
}

// registry holds every registered starter template, keyed by name.
var registry = map[string]Template{
	"contacts":               contactsTemplate(),
	"inventory":              inventoryTemplate(),
	"order-entry":            orderEntryTemplate(),
	"training-registrations": trainingRegistrationsTemplate(),
}

// List returns the names of all registered starter templates that can
// be passed to WithTemplate.
func List() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Description returns a human-readable description for a registered
// starter template (used by the Studio Home quick-start cards). The boolean
// reports whether the name is registered.
func Description(name string) (string, bool) {
	tmpl, ok := registry[name]
	if !ok {
		return "", false
	}
	return tmpl.Title, true
}

// Get returns the named template. The returned value shares no mutable state
// with the registry beyond its slices; treat it as read-only.
func Get(name string) (Template, bool) {
	t, ok := registry[name]
	return t, ok
}

// SeedTables creates the template's tables (with inline FKs) and inserts the
// sample data through exec, which is typically a *sql.DB or *sql.Conn
// ExecContext. Table names and SQL are fixed literals owned by this package.
func SeedTables(ctx context.Context, exec func(ctx context.Context, query string) error, tmpl Template) error {
	for _, tbl := range tmpl.Tables {
		if err := exec(ctx, tbl.CreateSQL); err != nil {
			return fmt.Errorf("failed to create table %q: %w", tbl.Name, err)
		}
		for _, ins := range tbl.Inserts {
			if err := exec(ctx, ins); err != nil {
				return fmt.Errorf("failed to seed rows into %q: %w", tbl.Name, err)
			}
		}
	}
	return nil
}

// sqlCell builds an already-populated SQL notebook cell — a template-building
// convenience wrapping notebooks.NewCell, which cannot itself fail for
// CellSQL. Every template cell pins its view.
func sqlCell(sql, view, label string) notebooks.Cell {
	c, _ := notebooks.NewCell(notebooks.CellSQL)
	c.Source = sql
	c.View = view
	c.ViewPinned = true
	c.Label = label
	return c
}

// markdownCell builds an already-populated markdown notebook cell.
func markdownCell(source string) notebooks.Cell {
	c, _ := notebooks.NewCell(notebooks.CellMarkdown)
	c.Source = source
	return c
}

// --- Template builders ---

func contactsTemplate() Template {
	nb := notebooks.Empty("Contacts")
	nb.Cells = []notebooks.Cell{
		markdownCell("# Contacts\n\nA starter workspace: a contacts list plus a running log of notes per contact."),
		sqlCell(`SELECT * FROM contacts ORDER BY name`, "table", "Contacts"),
		sqlCell(
			`SELECT c.name AS contact, n.note, n.noted_at FROM contact_notes n JOIN contacts c ON c.id = n.contact_id ORDER BY n.noted_at DESC`,
			"table", "Recent notes",
		),
	}

	return Template{
		Name:  "contacts",
		Title: "Contacts",
		Tables: []Table{
			{
				Name: "contacts",
				CreateSQL: `CREATE TABLE contacts (
					id INTEGER PRIMARY KEY,
					name TEXT NOT NULL,
					email TEXT,
					phone TEXT,
					company TEXT
				)`,
				Inserts: []string{
					`INSERT INTO contacts (id, name, email, phone, company) VALUES
						(1, 'Alice Chen', 'alice@example.com', '555-0101', 'Acme Co'),
						(2, 'Bob Martinez', 'bob@example.com', '555-0102', 'Globex'),
						(3, 'Carol Jones', 'carol@example.com', '555-0103', 'Initech')`,
				},
			},
			{
				Name: "contact_notes",
				CreateSQL: `CREATE TABLE contact_notes (
					id INTEGER PRIMARY KEY,
					contact_id INTEGER REFERENCES contacts(id),
					note TEXT NOT NULL,
					noted_at TEXT
				)`,
				Inserts: []string{
					`INSERT INTO contact_notes (id, contact_id, note, noted_at) VALUES
						(1, 1, 'Followed up about renewal', '2025-09-01'),
						(2, 1, 'Sent proposal', '2025-09-05'),
						(3, 2, 'Intro call completed', '2025-09-03')`,
				},
			},
		},
		Book: nb,
	}
}

func inventoryTemplate() Template {
	nb := notebooks.Empty("Inventory")
	nb.Cells = []notebooks.Cell{
		markdownCell("# Inventory\n\nA starter workspace: suppliers and the products they supply, with current stock."),
		sqlCell(`SELECT * FROM suppliers ORDER BY name`, "table", "Suppliers"),
		sqlCell(
			`SELECT p.name, s.name AS supplier, p.price, p.stock FROM products p JOIN suppliers s ON s.id = p.supplier_id ORDER BY p.name`,
			"table", "Products",
		),
		sqlCell(`SELECT name, stock FROM products ORDER BY stock ASC LIMIT 10`, "bar", "Lowest stock"),
	}

	return Template{
		Name:  "inventory",
		Title: "Inventory",
		Tables: []Table{
			{
				Name: "suppliers",
				CreateSQL: `CREATE TABLE suppliers (
					id INTEGER PRIMARY KEY,
					name TEXT NOT NULL,
					contact_email TEXT
				)`,
				Inserts: []string{
					`INSERT INTO suppliers (id, name, contact_email) VALUES
						(1, 'Northwind Supply', 'sales@northwind.example'),
						(2, 'Contoso Parts', 'orders@contoso.example')`,
				},
			},
			{
				Name: "products",
				CreateSQL: `CREATE TABLE products (
					id INTEGER PRIMARY KEY,
					name TEXT NOT NULL,
					supplier_id INTEGER REFERENCES suppliers(id),
					price DOUBLE,
					stock INTEGER
				)`,
				Inserts: []string{
					`INSERT INTO products (id, name, supplier_id, price, stock) VALUES
						(1, 'Widget Pro', 1, 29.99, 150),
						(2, 'Gadget X', 1, 49.99, 75),
						(3, 'Accessory Pack', 2, 14.99, 200)`,
				},
			},
		},
		Book: nb,
	}
}

func orderEntryTemplate() Template {
	nb := notebooks.Empty("Order Entry")
	nb.Cells = []notebooks.Cell{
		markdownCell("# Order Entry\n\nA starter workspace: customers, their orders, and the line items on each order."),
		sqlCell(`SELECT * FROM customers ORDER BY name`, "table", "Customers"),
		sqlCell(
			`SELECT o.id AS order_id, c.name AS customer, o.ordered_at, o.status FROM orders o JOIN customers c ON c.id = o.customer_id ORDER BY o.ordered_at DESC`,
			"table", "Orders",
		),
		sqlCell(
			`SELECT o.id AS order_id, i.product, i.quantity, i.price FROM order_items i JOIN orders o ON o.id = i.order_id ORDER BY o.id`,
			"table", "Order line items",
		),
	}

	return Template{
		Name:  "order-entry",
		Title: "Order Entry",
		Tables: []Table{
			{
				Name: "customers",
				CreateSQL: `CREATE TABLE customers (
					id INTEGER PRIMARY KEY,
					name TEXT NOT NULL,
					email TEXT
				)`,
				Inserts: []string{
					`INSERT INTO customers (id, name, email) VALUES
						(1, 'Alice Chen', 'alice@example.com'),
						(2, 'Bob Martinez', 'bob@example.com')`,
				},
			},
			{
				Name: "orders",
				CreateSQL: `CREATE TABLE orders (
					id INTEGER PRIMARY KEY,
					customer_id INTEGER REFERENCES customers(id),
					ordered_at TEXT,
					status TEXT
				)`,
				Inserts: []string{
					`INSERT INTO orders (id, customer_id, ordered_at, status) VALUES
						(1, 1, '2025-09-01', 'completed'),
						(2, 2, '2025-09-02', 'pending')`,
				},
			},
			{
				Name: "order_items",
				CreateSQL: `CREATE TABLE order_items (
					id INTEGER PRIMARY KEY,
					order_id INTEGER REFERENCES orders(id),
					product TEXT NOT NULL,
					quantity INTEGER,
					price DOUBLE
				)`,
				Inserts: []string{
					`INSERT INTO order_items (id, order_id, product, quantity, price) VALUES
						(1, 1, 'Widget Pro', 2, 29.99),
						(2, 1, 'Gadget X', 1, 49.99),
						(3, 2, 'Accessory Pack', 3, 14.99)`,
				},
			},
		},
		Book: nb,
	}
}

func trainingRegistrationsTemplate() Template {
	nb := notebooks.Empty("Training Registrations")
	nb.Cells = []notebooks.Cell{
		markdownCell("# Training Registrations\n\nA starter workspace: courses and who has registered for each."),
		sqlCell(`SELECT * FROM courses ORDER BY title`, "table", "Courses"),
		sqlCell(
			`SELECT c.title AS course, r.attendee, r.registered_at FROM registrations r JOIN courses c ON c.id = r.course_id ORDER BY r.registered_at`,
			"table", "Registrations",
		),
		sqlCell(
			`SELECT c.title, COUNT(r.id) AS registered FROM courses c LEFT JOIN registrations r ON r.course_id = c.id GROUP BY c.title ORDER BY c.title`,
			"bar", "Registrations per course",
		),
	}

	return Template{
		Name:  "training-registrations",
		Title: "Training Registrations",
		Tables: []Table{
			{
				Name: "courses",
				CreateSQL: `CREATE TABLE courses (
					id INTEGER PRIMARY KEY,
					title TEXT NOT NULL,
					instructor TEXT,
					seats INTEGER
				)`,
				Inserts: []string{
					`INSERT INTO courses (id, title, instructor, seats) VALUES
						(1, 'Intro to Vibe', 'Dr. Ada', 20),
						(2, 'Advanced DuckDB', 'Grace H', 15)`,
				},
			},
			{
				Name: "registrations",
				CreateSQL: `CREATE TABLE registrations (
					id INTEGER PRIMARY KEY,
					course_id INTEGER REFERENCES courses(id),
					attendee TEXT NOT NULL,
					registered_at TEXT
				)`,
				Inserts: []string{
					`INSERT INTO registrations (id, course_id, attendee, registered_at) VALUES
						(1, 1, 'Alice Chen', '2025-09-01'),
						(2, 1, 'Bob Martinez', '2025-09-02'),
						(3, 2, 'Carol Jones', '2025-09-03')`,
				},
			},
		},
		Book: nb,
	}
}
