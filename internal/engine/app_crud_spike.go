package engine

// AppCRUDViabilitySpikeVerdict / AppCRUDVerdict are intentionally stubbed for
// the Phase-4 P4.0 RED step (parity plan §8/§4.1 + §4 Phase 4). Real findings
// are recorded in the GREEN step.
//
// P4.0 is the mandatory DuckDB-as-CRUD risk spike that gates Phase 4
// (App Shell / Switchboard / dual-mode). Where the P1.0 spike exercised raw
// ddb.Exec statements and the P2.1 spike exercised the relationship/FK DDL
// surface, P4.0 must exercise the PRODUCTION write path that App Mode's
// switchboard/forms actually navigate to: the transactional
// crud.InsertRowTx / crud.UpdateRowTx / crud.DeleteRowTx core over a real
// FK relationship applied via the shared schema.ApplyForeignKey emit path.
//
// AppCRUDVerdict records the conclusion App Mode data-entry builds on: whether
// per-row UPDATE/DELETE-by-PK through the production CRUD layer is reliable and
// durable across a DuckDB-blob round-trip, whether the FK constraint keeps
// enforcing referential integrity after that round-trip, and whether the
// per-row UPDATE latency at SMB scale (N=500) is viable. If any of these fail,
// it is a BLOCKING finding and later P4.x units must not build on top.
type AppCRUDVerdict struct {
	// OrphanInsertRejected is true if inserting a child row referencing a
	// non-existent parent PK is rejected through the production CRUD path.
	OrphanInsertRejected bool

	// ParentDeleteBehavior describes what happens when deleting a parent row
	// still referenced by a child: "RESTRICT", "CASCADE", or "UNENFORCED".
	ParentDeleteBehavior string

	// FKSurvivesRoundTrip is true if the FK constraint still enforces
	// referential integrity after Save (checkpoint -> re-serialize blob) +
	// Close + reopen.
	FKSurvivesRoundTrip bool

	// PerRowWriteDurable is true if per-row UPDATE/DELETE-by-PK writes through
	// the production CRUD layer survive the Save + Close + reopen round-trip.
	PerRowWriteDurable bool

	// Recommendation is the spike's blocking go/no-go conclusion for Phase 4:
	// either "app-mode-crud-viable" or "blocking-crud-risk".
	Recommendation string
}

// Recorded P4.0 viability recommendation constants.
const (
	AppCRUDRecommendViable   = "app-mode-crud-viable"
	AppCRUDRecommendBlocking = "blocking-crud-risk"
)

// AppCRUDViabilitySpikeVerdict returns the recorded conclusion of the Phase-4
// P4.0 DuckDB-as-CRUD viability spike. These findings were observed by running
// TestP4AppModeCRUDViabilitySpike against a real encrypted .vibe container with
// a materialized DuckDB, driving the PRODUCTION transactional CRUD core
// (crud.InsertRowTx / crud.UpdateRowTx / crud.DeleteRowTx) over a real FK
// relationship applied through the shared schema.ApplyForeignKey emit path
// (parity plan §8/§4.1 + §4 Phase 4):
//
//   - 500 per-row UPDATEs by PK through crud.UpdateRowTx (each inside its own
//     ddb.Begin()/Commit(), exactly as studio/app.go + internal/server invoke
//     it) complete well within an SMB-scale budget.
//   - 100 per-row DELETEs by PK through crud.DeleteRowTx succeed.
//   - An orphan-child INSERT (child referencing a non-existent parent PK)
//     through crud.InsertRowTx is REJECTED by the FK constraint.
//   - Deleting a parent row still referenced by a child is RESTRICTed (DuckDB
//     errors out; it does not silently orphan or cascade).
//   - All valid per-row writes survive Save (checkpoint -> re-serialize blob) +
//     Close + reopen, and the FK constraint keeps rejecting orphans after the
//     blob round-trip.
//
// Because per-row CRUD through the production transactional core is durable
// across the blob round-trip AND DuckDB keeps enforcing the FK (orphan rejected
// + RESTRICT on parent delete) after reopen, the spike marks App-Mode CRUD as
// VIABLE: the rest of Phase 4 (App Shell / Switchboard / dual-mode forms) may
// build on DuckDB as the transactional store.
func AppCRUDViabilitySpikeVerdict() AppCRUDVerdict {
	return AppCRUDVerdict{
		OrphanInsertRejected: true,
		ParentDeleteBehavior: FKParentDeleteRestrict,
		FKSurvivesRoundTrip:  true,
		PerRowWriteDurable:   true,
		Recommendation:       AppCRUDRecommendViable,
	}
}
