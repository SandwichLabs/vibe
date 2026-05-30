package container

// Bundled starter templates (plan §6 / P6.6). A template is an embedded builder
// that seeds a fresh .vibe with a complete little relational application: DuckDB
// user tables (with inline foreign keys so referential integrity is actually
// enforced — DuckDB cannot ALTER a FK onto an existing table, so the constraint
// is declared inline at CREATE time), sample data, Access-style metadata
// (_vibe_relationships, _vibe_field_meta, _vibe_forms, _vibe_nav), and a
// Switchboard nav entry. These are the modern stand-ins for the Access "database
// wizard" starter databases (Contacts, Inventory, Order Entry, ...).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// templateTable describes one DuckDB user-data table to create for a template.
// CreateSQL must be a full CREATE TABLE statement (with inline REFERENCES for any
// foreign keys, since DuckDB only enforces FKs declared at table-create time).
type templateTable struct {
	Name      string
	CreateSQL string
	Inserts   []string
}

// templateRelationship mirrors a row of _vibe_relationships. The FK itself is
// declared inline in the child table's CreateSQL; this is the Access-style
// metadata record describing the link.
type templateRelationship struct {
	ParentTable, ParentColumn string
	ChildTable, ChildColumn   string
}

// templateFieldMeta is a single _vibe_field_meta row (a human caption / control
// type for a bound column).
type templateFieldMeta struct {
	Table, Column, Caption, ControlType string
	SortOrder                           int
}

// templateForm is a single _vibe_forms definition. Spec is marshalled to JSON.
type templateForm struct {
	ID, Title, Table, Archetype string
	Spec                        formSpec
}

// templateNav is a single _vibe_nav switchboard entry.
type templateNav struct {
	ID, Label, ActionKind, TargetID string
	SortOrder                       int
}

// formSpec is the minimal JSON form spec persisted into _vibe_forms.spec. It is a
// container-local mirror of schema.FormSpec (container cannot import internal/schema,
// which imports container) carrying just what the starter forms need.
type formSpec struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	Table     string       `json:"table"`
	Archetype string       `json:"archetype"`
	Fields    []formField  `json:"fields"`
	Subform   *formSubform `json:"subform,omitempty"`
}

type formField struct {
	Name        string `json:"name"`
	Caption     string `json:"caption"`
	ControlType string `json:"control_type"`
}

type formSubform struct {
	ChildTable string      `json:"child_table"`
	ChildKey   string      `json:"child_key"`
	ParentKey  string      `json:"parent_key"`
	Fields     []formField `json:"fields"`
}

// template is a complete starter-app definition.
type template struct {
	Name          string
	Title         string
	Tables        []templateTable
	Relationships []templateRelationship
	FieldMeta     []templateFieldMeta
	Forms         []templateForm
	Nav           []templateNav
}

// templateRegistry holds every registered starter template, keyed by name.
var templateRegistry = map[string]template{
	"contacts":               contactsTemplate(),
	"inventory":              inventoryTemplate(),
	"order-entry":            orderEntryTemplate(),
	"training-registrations": trainingRegistrationsTemplate(),
}

// ListTemplates returns the names of all registered Access-style starter
// templates that can be passed to WithTemplate.
func ListTemplates() []string {
	names := make([]string, 0, len(templateRegistry))
	for name := range templateRegistry {
		names = append(names, name)
	}
	return names
}

// TemplateDescription returns a human-readable description for a registered
// starter template (used by the Studio Home quick-start cards). The boolean
// reports whether the name is registered.
func TemplateDescription(name string) (string, bool) {
	tmpl, ok := templateRegistry[name]
	if !ok {
		return "", false
	}
	return tmpl.Title, true
}

// applyTemplate seeds the named starter template into a freshly created
// container: DuckDB user tables + sample data, and the Access-style metadata in
// SQLCipher. It returns a wrapped error if the template name is unknown.
func applyTemplate(c *Container, name string) error {
	tmpl, ok := templateRegistry[name]
	if !ok {
		return fmt.Errorf("unknown template %q: not a registered template", name)
	}

	c.logger.Info("seeding starter template", "template", name)

	if err := seedTemplateTables(c.duckDB.db, tmpl); err != nil {
		return fmt.Errorf("failed to seed template %q tables: %w", name, err)
	}
	if err := seedTemplateMetadata(c.sqliteDB, tmpl); err != nil {
		return fmt.Errorf("failed to seed template %q metadata: %w", name, err)
	}
	return nil
}

// seedTemplateTables creates the DuckDB user tables (with inline FKs) and inserts
// the sample data.
func seedTemplateTables(ddb *sql.DB, tmpl template) error {
	for _, tbl := range tmpl.Tables {
		if _, err := ddb.Exec(tbl.CreateSQL); err != nil {
			return fmt.Errorf("failed to create table %q: %w", tbl.Name, err)
		}
		for _, ins := range tbl.Inserts {
			if _, err := ddb.Exec(ins); err != nil {
				return fmt.Errorf("failed to seed rows into %q: %w", tbl.Name, err)
			}
		}
	}
	return nil
}

// seedTemplateMetadata writes the relationships, field metadata, forms, and nav
// entries into SQLCipher.
func seedTemplateMetadata(sdb *sql.DB, tmpl template) error {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := sdb.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin template metadata transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for i, rel := range tmpl.Relationships {
		id := fmt.Sprintf("%s_rel_%d", tmpl.Name, i+1)
		if _, err := tx.Exec(
			`INSERT INTO _vibe_relationships
				(id, parent_table, parent_column, child_table, child_column,
				 on_delete, on_update, enforce_integrity, created_at)
			 VALUES (?, ?, ?, ?, ?, 'RESTRICT', 'NO ACTION', 1, ?);`,
			id, rel.ParentTable, rel.ParentColumn, rel.ChildTable, rel.ChildColumn, now,
		); err != nil {
			return fmt.Errorf("failed to insert relationship %q: %w", id, err)
		}
	}

	for _, fm := range tmpl.FieldMeta {
		if _, err := tx.Exec(
			`INSERT INTO _vibe_field_meta
				(table_name, column_name, caption, control_type, sort_order)
			 VALUES (?, ?, ?, ?, ?);`,
			fm.Table, fm.Column, fm.Caption, fm.ControlType, fm.SortOrder,
		); err != nil {
			return fmt.Errorf("failed to insert field meta %s.%s: %w", fm.Table, fm.Column, err)
		}
	}

	for _, f := range tmpl.Forms {
		blob, err := json.Marshal(f.Spec)
		if err != nil {
			return fmt.Errorf("failed to marshal form spec %q: %w", f.ID, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO _vibe_forms
				(id, title, table_name, archetype, version, spec, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 1, ?, ?, ?);`,
			f.ID, f.Title, f.Table, f.Archetype, string(blob), now, now,
		); err != nil {
			return fmt.Errorf("failed to insert form %q: %w", f.ID, err)
		}
	}

	for _, n := range tmpl.Nav {
		if _, err := tx.Exec(
			`INSERT INTO _vibe_nav (id, parent_id, label, action_kind, target_id, sort_order)
			 VALUES (?, '', ?, ?, ?, ?);`,
			n.ID, n.Label, n.ActionKind, n.TargetID, n.SortOrder,
		); err != nil {
			return fmt.Errorf("failed to insert nav entry %q: %w", n.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit template metadata transaction: %w", err)
	}
	return nil
}

// --- Template builders ---

func contactsTemplate() template {
	return template{
		Name:  "contacts",
		Title: "Contacts",
		Tables: []templateTable{
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
		Relationships: []templateRelationship{
			{ParentTable: "contacts", ParentColumn: "id", ChildTable: "contact_notes", ChildColumn: "contact_id"},
		},
		FieldMeta: []templateFieldMeta{
			{Table: "contacts", Column: "name", Caption: "Full Name", ControlType: "text", SortOrder: 1},
			{Table: "contacts", Column: "email", Caption: "Email", ControlType: "text", SortOrder: 2},
			{Table: "contacts", Column: "phone", Caption: "Phone", ControlType: "text", SortOrder: 3},
			{Table: "contacts", Column: "company", Caption: "Company", ControlType: "text", SortOrder: 4},
		},
		Forms: []templateForm{
			{
				ID: "contacts_form", Title: "Contacts", Table: "contacts", Archetype: "master_detail",
				Spec: formSpec{
					ID: "contacts_form", Title: "Contacts", Table: "contacts", Archetype: "master_detail",
					Fields: []formField{
						{Name: "name", Caption: "Full Name", ControlType: "text"},
						{Name: "email", Caption: "Email", ControlType: "text"},
						{Name: "phone", Caption: "Phone", ControlType: "text"},
						{Name: "company", Caption: "Company", ControlType: "text"},
					},
					Subform: &formSubform{
						ChildTable: "contact_notes",
						ChildKey:   "contact_id",
						ParentKey:  "id",
						Fields: []formField{
							{Name: "note", Caption: "Note", ControlType: "text"},
							{Name: "noted_at", Caption: "Date", ControlType: "date"},
						},
					},
				},
			},
		},
		Nav: []templateNav{
			{ID: "contacts_nav", Label: "Contacts", ActionKind: "open_form", TargetID: "contacts_form", SortOrder: 1},
		},
	}
}

func inventoryTemplate() template {
	return template{
		Name:  "inventory",
		Title: "Inventory",
		Tables: []templateTable{
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
		Relationships: []templateRelationship{
			{ParentTable: "suppliers", ParentColumn: "id", ChildTable: "products", ChildColumn: "supplier_id"},
		},
		FieldMeta: []templateFieldMeta{
			{Table: "products", Column: "name", Caption: "Product Name", ControlType: "text", SortOrder: 1},
			{Table: "products", Column: "supplier_id", Caption: "Supplier", ControlType: "lookup", SortOrder: 2},
			{Table: "products", Column: "price", Caption: "Price", ControlType: "number", SortOrder: 3},
			{Table: "products", Column: "stock", Caption: "In Stock", ControlType: "number", SortOrder: 4},
			{Table: "suppliers", Column: "name", Caption: "Supplier Name", ControlType: "text", SortOrder: 1},
		},
		Forms: []templateForm{
			{
				ID: "products_form", Title: "Products", Table: "products", Archetype: "continuous",
				Spec: formSpec{
					ID: "products_form", Title: "Products", Table: "products", Archetype: "continuous",
					Fields: []formField{
						{Name: "name", Caption: "Product Name", ControlType: "text"},
						{Name: "supplier_id", Caption: "Supplier", ControlType: "lookup"},
						{Name: "price", Caption: "Price", ControlType: "number"},
						{Name: "stock", Caption: "In Stock", ControlType: "number"},
					},
				},
			},
			{
				ID: "suppliers_form", Title: "Suppliers", Table: "suppliers", Archetype: "columnar",
				Spec: formSpec{
					ID: "suppliers_form", Title: "Suppliers", Table: "suppliers", Archetype: "columnar",
					Fields: []formField{
						{Name: "name", Caption: "Supplier Name", ControlType: "text"},
						{Name: "contact_email", Caption: "Contact Email", ControlType: "text"},
					},
				},
			},
		},
		Nav: []templateNav{
			{ID: "inventory_products_nav", Label: "Products", ActionKind: "open_form", TargetID: "products_form", SortOrder: 1},
			{ID: "inventory_suppliers_nav", Label: "Suppliers", ActionKind: "open_form", TargetID: "suppliers_form", SortOrder: 2},
		},
	}
}

func orderEntryTemplate() template {
	return template{
		Name:  "order-entry",
		Title: "Order Entry",
		Tables: []templateTable{
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
		Relationships: []templateRelationship{
			{ParentTable: "customers", ParentColumn: "id", ChildTable: "orders", ChildColumn: "customer_id"},
			{ParentTable: "orders", ParentColumn: "id", ChildTable: "order_items", ChildColumn: "order_id"},
		},
		FieldMeta: []templateFieldMeta{
			{Table: "orders", Column: "customer_id", Caption: "Customer", ControlType: "lookup", SortOrder: 1},
			{Table: "orders", Column: "ordered_at", Caption: "Order Date", ControlType: "date", SortOrder: 2},
			{Table: "orders", Column: "status", Caption: "Status", ControlType: "text", SortOrder: 3},
			{Table: "order_items", Column: "product", Caption: "Product", ControlType: "text", SortOrder: 1},
			{Table: "order_items", Column: "quantity", Caption: "Qty", ControlType: "number", SortOrder: 2},
		},
		Forms: []templateForm{
			{
				ID: "order_entry_form", Title: "Order Entry", Table: "orders", Archetype: "master_detail",
				Spec: formSpec{
					ID: "order_entry_form", Title: "Order Entry", Table: "orders", Archetype: "master_detail",
					Fields: []formField{
						{Name: "customer_id", Caption: "Customer", ControlType: "lookup"},
						{Name: "ordered_at", Caption: "Order Date", ControlType: "date"},
						{Name: "status", Caption: "Status", ControlType: "text"},
					},
					Subform: &formSubform{
						ChildTable: "order_items",
						ChildKey:   "order_id",
						ParentKey:  "id",
						Fields: []formField{
							{Name: "product", Caption: "Product", ControlType: "text"},
							{Name: "quantity", Caption: "Qty", ControlType: "number"},
							{Name: "price", Caption: "Price", ControlType: "number"},
						},
					},
				},
			},
		},
		Nav: []templateNav{
			{ID: "order_entry_nav", Label: "Order Entry", ActionKind: "open_form", TargetID: "order_entry_form", SortOrder: 1},
		},
	}
}

func trainingRegistrationsTemplate() template {
	return template{
		Name:  "training-registrations",
		Title: "Training Registrations",
		Tables: []templateTable{
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
		Relationships: []templateRelationship{
			{ParentTable: "courses", ParentColumn: "id", ChildTable: "registrations", ChildColumn: "course_id"},
		},
		FieldMeta: []templateFieldMeta{
			{Table: "courses", Column: "title", Caption: "Course Title", ControlType: "text", SortOrder: 1},
			{Table: "courses", Column: "instructor", Caption: "Instructor", ControlType: "text", SortOrder: 2},
			{Table: "registrations", Column: "course_id", Caption: "Course", ControlType: "lookup", SortOrder: 1},
			{Table: "registrations", Column: "attendee", Caption: "Attendee", ControlType: "text", SortOrder: 2},
		},
		Forms: []templateForm{
			{
				ID: "course_registrations_form", Title: "Course Registrations", Table: "courses", Archetype: "master_detail",
				Spec: formSpec{
					ID: "course_registrations_form", Title: "Course Registrations", Table: "courses", Archetype: "master_detail",
					Fields: []formField{
						{Name: "title", Caption: "Course Title", ControlType: "text"},
						{Name: "instructor", Caption: "Instructor", ControlType: "text"},
						{Name: "seats", Caption: "Seats", ControlType: "number"},
					},
					Subform: &formSubform{
						ChildTable: "registrations",
						ChildKey:   "course_id",
						ParentKey:  "id",
						Fields: []formField{
							{Name: "attendee", Caption: "Attendee", ControlType: "text"},
							{Name: "registered_at", Caption: "Registered", ControlType: "date"},
						},
					},
				},
			},
		},
		Nav: []templateNav{
			{ID: "training_nav", Label: "Course Registrations", ActionKind: "open_form", TargetID: "course_registrations_form", SortOrder: 1},
		},
	}
}
