package store

import (
	"database/sql"
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

func TestOpenSetsALegacyDatabaseAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layer8.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE projects (id INTEGER PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`SELECT default_branch FROM projects`); err != nil {
		t.Fatalf("fresh schema: %v", err)
	}
	legacy, _ := sql.Open("sqlite", "file:"+path+".legacy")
	defer legacy.Close()
	if _, err := legacy.Exec(`SELECT title FROM projects`); err != nil {
		t.Fatalf("legacy database kept: %v", err)
	}
}
