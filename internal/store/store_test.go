package store

import (
	"path/filepath"
	"testing"
)

func TestOpenMigratesOnceAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layer8.db")
	for range 2 {
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var version int
		db.QueryRow(`PRAGMA user_version`).Scan(&version)
		if version != len(migrations) {
			t.Fatalf("user_version = %d, want %d", version, len(migrations))
		}
		var mode string
		db.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
		if mode != "wal" {
			t.Fatalf("journal_mode = %q", mode)
		}
		for _, table := range []string{"projects", "actions_log"} {
			if _, err := db.Exec(`SELECT count(*) FROM ` + table); err != nil {
				t.Fatalf("%s: %v", table, err)
			}
		}
		db.Close()
	}
}
