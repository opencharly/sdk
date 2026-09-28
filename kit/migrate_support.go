package kit

// migrate_support.go — the stable schema/ledger path constants shared between
// charly core and the importable kit. The migration chain, its host-prelifted
// runtime context / reply types, and the plural→singular key map folded back into
// charly core at the migration-baseline reset (the former out-of-core migration
// module and its cross-boundary contract were folded away); only these constants — read on
// the current-format load + ledger paths, not migration — remain here.

import "github.com/opencharly/spec/spec"

// Layout path constants shared by core and kit. The canonical values moved to
// spec (spec.DefaultBoxDir / spec.DefaultCandyDir, #55 import-purity cone-render) —
// loader-result DATA the types-only spec module owns, mirroring spec.UnifiedFileName;
// kit re-exports them via const alias so existing kit.DefaultBoxDir / kit.DefaultCandyDir
// call sites (plugins + sdk) are untouched (R3, one source).
const (
	DefaultBoxDir   = spec.DefaultBoxDir   // discovered box/<name>/ directory
	DefaultCandyDir = spec.DefaultCandyDir // discovered candy/<name>/ directory
)

// The former LedgerSchemaVersion constant is DELETED (the schema-versioning removal
// cutover): a ledger-record FORMAT version compared against a build constant was the
// same class of arbitrary check as the project-schema stamp. The ledger is read and
// written by ONE binary version; there is no cross-version ledger to arbitrate.
