package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenProtectsDatabaseAndJournalDuringWrite(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are not an access-control proof on Windows")
	}
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "private", "app.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	assertMode := func(name string, want os.FileMode) {
		t.Helper()
		info, err := os.Stat(name)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %04o, want %04o", name, got, want)
		}
	}
	assertMode(filepath.Dir(path), 0o700)
	assertMode(path, 0o600)

	tx, err := store.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO briefing_snapshots(profile_key,captured_at,payload_json) VALUES ('sidecar','2026-01-01T00:00:00Z','{}')`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	assertMode(path+"-journal", 0o600)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
}

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
