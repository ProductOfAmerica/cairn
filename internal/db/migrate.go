package db

import "database/sql"

// migrate is the migration runner. Task 5.1 ships a no-op stub; Task 5.2
// replaces it with the real embedded-schema runner.
func migrate(_ *sql.DB) error { return nil }
