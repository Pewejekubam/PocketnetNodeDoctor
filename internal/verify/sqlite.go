package verify

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// VerifySQLite opens the SQLite database at sqlitePath, runs
// "PRAGMA integrity_check", and returns nil if the result is "ok".
// Returns an error containing the diagnostic string on failure.
func VerifySQLite(sqlitePath string) error {
	db, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		return fmt.Errorf("verify sqlite: open %s: %w", sqlitePath, err)
	}
	defer db.Close()

	row := db.QueryRow("PRAGMA integrity_check")
	var result string
	if err := row.Scan(&result); err != nil {
		return fmt.Errorf("verify sqlite: integrity_check query failed for %s: %w", sqlitePath, err)
	}
	if result != "ok" {
		return fmt.Errorf("verify sqlite: integrity_check failed for %s: %s", sqlitePath, result)
	}
	return nil
}
