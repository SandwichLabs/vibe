package engine

// RelationshipSpikeVerdict / RelationshipVerdict are intentionally stubbed for
// the P2.1 RED step. Real findings are recorded in the GREEN step.

// RelationshipVerdict records the conclusion of the Phase-2 DuckDB
// relationship/FK risk spike (parity plan §8/§4.1). It captures the relationship
// patterns the P1.0 CRUD spike did NOT cover and which are known DuckDB weak
// spots, so the P2.2 relationship builder knows which features can lean on
// DuckDB-native constraints vs. must be enforced in the application CRUD layer.
type RelationshipVerdict struct {
	// AlterAddFKSupported is true if DuckDB accepts adding a FK to an
	// already-existing table via ALTER TABLE ... ADD ... REFERENCES.
	AlterAddFKSupported bool

	// OnDeleteCascadeSupported is true if an inline ON DELETE CASCADE FK actually
	// cascades child deletes when the referenced parent row is deleted.
	OnDeleteCascadeSupported bool

	// Recommendation is the spike's conclusion P2.2 builds on:
	// "rely-on-duckdb-fk", "recreate-table-for-fk", or "enforce-in-crud-layer".
	Recommendation string
}

// Recorded relationship-strategy recommendation constants.
const (
	RelRecommendRelyOnDuckDB   = "rely-on-duckdb-fk"
	RelRecommendRecreateForFK  = "recreate-table-for-fk"
	RelRecommendEnforceInCRUD  = "enforce-in-crud-layer"
)

// RelationshipSpikeVerdict returns the recorded conclusion of the Phase-2 DuckDB
// relationship/FK risk spike. These findings were observed by running
// TestDuckDBRelationshipSpike against a real encrypted .vibe container with a
// materialized DuckDB (parity plan §8/§4.1):
//
//   - ALTER TABLE ... ADD FOREIGN KEY on an already-existing table is NOT
//     supported by DuckDB ("Not implemented Error: No support for that ALTER
//     TABLE option yet!"). A FK can only be declared inline at CREATE TABLE time.
//   - ON DELETE CASCADE / SET NULL / SET DEFAULT referential actions are NOT
//     supported: the DDL fails to parse ("Parser Error: FOREIGN KEY constraints
//     cannot use CASCADE, SET NULL or SET DEFAULT"). DuckDB FKs are effectively
//     RESTRICT-only.
//   - Inline REFERENCES FKs DO enforce integrity: orphan child inserts are
//     rejected at every level of the customers -> orders -> order_items chain,
//     RESTRICT blocks deleting a referenced parent, self-referential FKs
//     (employees.manager_id -> employees.id) reject orphan managers, and all of
//     this survives Save (checkpoint -> re-serialize blob) + Close + reopen.
//
// Because a FK cannot be ALTERed onto an existing table, the P2.2 relationship
// builder cannot assume it can attach a FK to a table that already holds data;
// it must recreate the table with the FK declared inline (copy rows into a new
// table that has the inline REFERENCES, then swap). ON DELETE CASCADE must be
// emulated in the application CRUD layer since DuckDB rejects the cascade DDL.
// The spike therefore recommends "recreate-table-for-fk".
func RelationshipSpikeVerdict() RelationshipVerdict {
	return RelationshipVerdict{
		AlterAddFKSupported:      false,
		OnDeleteCascadeSupported: false,
		Recommendation:           RelRecommendRecreateForFK,
	}
}
