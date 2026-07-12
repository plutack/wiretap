// Package store — driver registration.
//
// Blank import of modernc.org/sqlite registers the "sqlite" driver with
// database/sql. Kept in its own file so the dependency is discoverable and
// so replacing the driver later (e.g. with libsql) is a one-file change.
package store

import (
	// Registers the "sqlite" driver as a side effect of import.
	_ "modernc.org/sqlite"
)
