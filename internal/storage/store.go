package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/assets"
	_ "modernc.org/sqlite"
)

const (
	defaultCacheEntries = 1024
	defaultCacheBytes   = 32 * 1024 * 1024
	defaultHealthRows   = 256
)

type Store struct {
	DB *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is empty")
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := ensureParent(path); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{DB: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := store.PruneCache(ctx, defaultCacheEntries, defaultCacheBytes); err != nil {
		_ = db.Close()
		return nil, err
	}
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if _, err := store.PruneSourceHealth(ctx, defaultHealthRows, cutoff); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func ensureParent(path string) error {
	parent := filepath.Dir(path)
	if parent == "." || parent == "" {
		return nil
	}
	return os.MkdirAll(parent, 0o755)
}

func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	entries, err := fs.ReadDir(assets.Files, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version := strings.TrimSuffix(entry.Name(), ".sql")
		var applied int
		if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version=?", version).Scan(&applied); err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		if applied != 0 {
			continue
		}
		body, err := assets.Files.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", version)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}
	return nil
}

func (s *Store) Info(ctx context.Context) (map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []string{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	fts := probeFeature(ctx, s.DB, "CREATE VIRTUAL TABLE temp._fts_probe USING fts5(x)", "DROP TABLE temp._fts_probe")
	rtree := probeFeature(ctx, s.DB, "CREATE VIRTUAL TABLE temp._rtree_probe USING rtree(id,minx,maxx,miny,maxy)", "DROP TABLE temp._rtree_probe")
	return map[string]any{
		"migrations_applied": versions,
		"migration_count":    len(versions),
		"fts5":               fts,
		"rtree":              rtree,
	}, nil
}

func probeFeature(ctx context.Context, db *sql.DB, create, drop string) bool {
	if _, err := db.ExecContext(ctx, create); err != nil {
		return false
	}
	_, err := db.ExecContext(ctx, drop)
	return err == nil
}
