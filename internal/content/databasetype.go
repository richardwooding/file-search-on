package content

import "maps"

// databaseAttrs packs the cross-format surface (always present:
// database_format) plus per-format extras into a content.Attributes
// map. Mirrors scienceAttrs / bytecodeAttrs / diskImageAttrs /
// installPackageAttrs.
//
// Issue #170 introduces the `database/*` family. Today only
// `database/sqlite` is registered; future additions (DuckDB,
// PostgreSQL dumps, BoltDB, etc.) join under the same umbrella.
func databaseAttrs(format string, extras Attributes) Attributes {
	out := Attributes{
		"database_format": format,
	}
	maps.Copy(out, extras)
	return out
}
