package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenAppliesEmbeddedMigrations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	info, err := store.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info["migration_count"] != 6 {
		t.Fatalf("migration_count = %v, want 6", info["migration_count"])
	}
	if info["fts5"] != true || info["rtree"] != true {
		t.Fatalf("SQLite features unavailable: %#v", info)
	}
}

func TestOpenClosesDatabaseAfterMigrationFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "broken.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE schema_migrations (wrong_column TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if store, err := Open(ctx, path); err == nil {
		_ = store.Close()
		t.Fatal("Open succeeded with an incompatible migration table")
	}

	probe, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if _, err := probe.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("database remained in use after failed Open: %v", err)
	}
	_, _ = probe.ExecContext(ctx, "ROLLBACK")
}
