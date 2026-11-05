package storage

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureDataDirectoryValidatesDestinations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := EnsureDataDirectory(""); err == nil {
		t.Fatal("empty data directory was accepted")
	}
	absent := filepath.Join(root, "missing", "data")
	if err := EnsureDataDirectory(absent); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(absent)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("data directory mode = %04o, want 0700", info.Mode().Perm())
	}
	if err := EnsureDataDirectory(absent); err != nil {
		t.Fatalf("repeated ensure failed: %v", err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDataDirectory(file); err == nil {
		t.Fatal("regular file was accepted as a data directory")
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "link")
		if err := os.Symlink(absent, link); err != nil {
			t.Fatal(err)
		}
		if err := EnsureDataDirectory(link); err == nil {
			t.Fatal("symbolic link was accepted as a data directory")
		}
	}
}

func TestPreparePrivatePathRejectsUnsafeDestinations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	parentFile := filepath.Join(root, "parent-file")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), filepath.Join(parentFile, "app.db")); err == nil {
		t.Fatal("database below a regular file was accepted")
	}
	dbDir := filepath.Join(root, "db-dir")
	if err := os.Mkdir(dbDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), dbDir); err == nil {
		t.Fatal("directory was accepted as a database file")
	}
	if runtime.GOOS != "windows" {
		target := filepath.Join(root, "target.db")
		if err := os.WriteFile(target, []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link.db")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), link); err == nil {
			t.Fatal("symbolic link was accepted as a database file")
		}
		parentLink := filepath.Join(root, "parent-link")
		if err := os.Symlink(root, parentLink); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), filepath.Join(parentLink, "other.db")); err == nil {
			t.Fatal("database below a symbolic-link parent was accepted")
		}
	}
}

func TestInspectStorageReportsUnsafePaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	db := filepath.Join(root, "app.db")
	if err := os.Mkdir(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db+"-journal", []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	report := InspectStorage(data, db)
	if report["ok"] != false {
		t.Fatalf("unsafe storage reported healthy: %#v", report)
	}
	if len(report["issues"].([]map[string]any)) != 3 {
		t.Fatalf("issues = %#v", report["issues"])
	}
	// A missing directory is not itself a permission issue.
	if report := InspectStorage(filepath.Join(root, "absent"), filepath.Join(root, "absent.db")); report["ok"] != true {
		t.Fatalf("absent storage reported unsafe: %#v", report)
	}
	if runtime.GOOS != "windows" {
		if err := os.Remove(db); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "missing-target"), db); err != nil {
			t.Fatal(err)
		}
		if report := InspectStorage(data, db); report["ok"] != false {
			t.Fatalf("symbolic-link database reported healthy: %#v", report)
		}
		if err := os.Remove(data); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(root, data); err != nil {
			t.Fatal(err)
		}
		if report := InspectStorage(data, db); report["ok"] != false {
			t.Fatalf("symbolic-link data directory reported healthy: %#v", report)
		}
	}
	if err := os.RemoveAll(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(data, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if report := InspectStorage(data, db); report["ok"] != false {
		t.Fatalf("regular-file data directory reported healthy: %#v", report)
	}
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(db, 0o700); err != nil {
		t.Fatal(err)
	}
	if report := InspectStorage(root, db); report["ok"] != false {
		t.Fatalf("directory database reported healthy: %#v", report)
	}
}

func TestSecureStorageCorrectsOnlyApplicationPaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	db := filepath.Join(root, "app.db")
	unrelated := filepath.Join(root, "unrelated")
	if err := os.Mkdir(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(unrelated, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db+"-wal", []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := SecureStorage(data, db)
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "OK" {
		t.Fatalf("status = %v", result["status"])
	}
	assertPermissions := func(path string, want os.FileMode) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %04o, want %04o", path, got, want)
		}
	}
	if runtime.GOOS != "windows" {
		assertPermissions(data, 0o700)
		assertPermissions(db, 0o600)
		assertPermissions(db+"-wal", 0o600)
		assertPermissions(unrelated, 0o755)
	}
	// A missing database is created only by Open, not by remediation.
	missingRoot := t.TempDir()
	missingDB := filepath.Join(missingRoot, "app.db")
	if _, err := SecureStorage(filepath.Join(missingRoot, "data"), missingDB); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(missingDB); !os.IsNotExist(err) {
		t.Fatalf("db secure created an absent database: %v", err)
	}
}

func TestSecureStorageRejectsUnsafeDestinations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SecureStorage(file, filepath.Join(root, "app.db")); err == nil {
		t.Fatal("db secure accepted a regular file as data directory")
	}
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := SecureStorage(data, data); err == nil {
		t.Fatal("db secure accepted a directory as database")
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "link")
		if err := os.Symlink(data, link); err != nil {
			t.Fatal(err)
		}
		if _, err := SecureStorage(link, filepath.Join(root, "app.db")); err == nil {
			t.Fatal("db secure accepted a symbolic-link data directory")
		}
		dbLink := filepath.Join(root, "db-link")
		if err := os.Symlink(filepath.Join(root, "missing"), dbLink); err != nil {
			t.Fatal(err)
		}
		if _, err := SecureStorage(data, dbLink); err == nil {
			t.Fatal("db secure accepted a symbolic-link database")
		}
	}
}

func TestOwnedByCurrentUserHandlesForeignStat(t *testing.T) {
	t.Parallel()
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !ownedByCurrentUser(info) {
		t.Fatal("temporary directory should be owned by the current user")
	}
	if !ownedByCurrentUser(fakeFileInfo{FileInfo: info}) {
		t.Fatal("an unknown stat representation should not be treated as foreign")
	}
}

type fakeFileInfo struct{ os.FileInfo }

func (fakeFileInfo) Sys() any { return nil }

func TestStoragePermissionErrors(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission denial is not portable to Windows")
	}
	root := t.TempDir()

	readOnly := filepath.Join(root, "read-only")
	if err := os.Mkdir(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), filepath.Join(readOnly, "app.db")); err == nil {
		t.Fatal("Open succeeded below a read-only parent")
	}
	if err := EnsureDataDirectory(filepath.Join(readOnly, "data")); err == nil {
		t.Fatal("EnsureDataDirectory succeeded below a read-only parent")
	}
	if _, err := SecureStorage(filepath.Join(readOnly, "data"), filepath.Join(readOnly, "app.db")); err == nil {
		t.Fatal("SecureStorage succeeded below a read-only parent")
	}

	noAccess := filepath.Join(root, "no-access")
	if err := os.Mkdir(noAccess, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(noAccess, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(noAccess, 0o700) })
	if _, err := Open(context.Background(), filepath.Join(noAccess, "app.db")); err == nil {
		t.Fatal("Open succeeded inside an untraversable directory")
	}
	if err := EnsureDataDirectory(filepath.Join(noAccess, "data")); err == nil {
		t.Fatal("EnsureDataDirectory succeeded inside an untraversable directory")
	}
	if err := validatePrivateDatabase(filepath.Join(noAccess, "app.db")); err == nil {
		t.Fatal("validatePrivateDatabase succeeded inside an untraversable directory")
	}
	if _, err := SecureStorage(filepath.Join(noAccess, "data"), filepath.Join(noAccess, "app.db")); err == nil {
		t.Fatal("SecureStorage succeeded inside an untraversable directory")
	}
}

func TestSecureStorageRejectsNonregularSidecar(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	db := filepath.Join(root, "app.db")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(db+"-journal", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := SecureStorage(data, db); err == nil {
		t.Fatal("db secure accepted a directory sidecar")
	}
}
