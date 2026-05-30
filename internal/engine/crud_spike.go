package engine

// FKVerdict records the conclusion of the DuckDB-as-CRUD risk spike (parity
// plan §8/§4.1) regarding DuckDB foreign-key constraint maturity. Later CRUD
// units (P1.6 / Phase 2 relationships) depend on a concrete verdict here to
// decide whether referential integrity can lean on DuckDB or must be enforced
// in the application CRUD layer.
type FKVerdict struct {
	// OrphanInsertRejected is true if inserting a child row referencing a
	// non-existent parent PK is rejected by DuckDB's FK constraint.
	OrphanInsertRejected bool

	// ParentDeleteBehavior describes what happens when deleting a parent row
	// that is still referenced by a child: "RESTRICT", "CASCADE", or
	// "UNENFORCED".
	ParentDeleteBehavior string

	// Recommendation is the spike's conclusion that P1.6/P2 build on:
	// either "rely-on-duckdb-fk" or "enforce-in-crud-layer".
	Recommendation string
}

// Recorded FK-behavior constants the spike asserts against.
const (
	FKParentDeleteRestrict   = "RESTRICT"
	FKParentDeleteCascade    = "CASCADE"
	FKParentDeleteUnenforced = "UNENFORCED"

	FKRecommendRelyOnDuckDB  = "rely-on-duckdb-fk"
	FKRecommendEnforceInCRUD = "enforce-in-crud-layer"
)

// CRUDSpikeVerdict returns the recorded conclusion of the DuckDB CRUD/FK risk
// spike. These findings were observed by running TestDuckDBCRUDSpike against a
// real encrypted .vibe container with a materialized DuckDB (parity plan §8/§4.1):
//
//   - Orphan-child insert (child referencing a non-existent parent PK) is
//     REJECTED by DuckDB's FK constraint.
//   - Deleting a parent row still referenced by a child is RESTRICTed (DuckDB
//     errors out; it does not silently orphan or cascade).
//   - Per-row INSERT/UPDATE/DELETE by PK round-trip correctly and persist across
//     Save (checkpoint -> re-serialize blob) + Close + reopen.
//   - 1000 single-row UPDATEs complete well within an SMB-scale budget.
//
// Because DuckDB enforces FK constraints (orphan rejected + RESTRICT on parent
// delete), the spike recommends relying on DuckDB-native FK enforcement for the
// P1.6/P2 CRUD and relationship layers rather than re-implementing referential
// integrity in the application CRUD layer.
func CRUDSpikeVerdict() FKVerdict {
	return FKVerdict{
		OrphanInsertRejected: true,
		ParentDeleteBehavior: FKParentDeleteRestrict,
		Recommendation:       FKRecommendRelyOnDuckDB,
	}
}
