package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

const (
	privateDirectoryMode = 0o700
	privateFileMode      = 0o600
)

// preparePrivatePath creates the database parent and file with owner-only access.
// Existing destinations are validated but never chmod-ed implicitly: broad modes
// are diagnosed and corrected through the explicit remediation command instead.
func preparePrivatePath(dbPath string) error {
	parent := filepath.Dir(dbPath)
	if parent != "." && parent != "" {
		info, err := os.Lstat(parent)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if err := os.MkdirAll(parent, privateDirectoryMode); err != nil {
				return fmt.Errorf("create private data directory: %w", err)
			}
		case err != nil:
			return fmt.Errorf("inspect data directory %s: %w", parent, err)
		case info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("data directory must not be a symbolic link: %s", parent)
		case !info.IsDir():
			return fmt.Errorf("data directory must be a directory: %s", parent)
		}
	}
	_, err := os.Lstat(dbPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		file, createErr := os.OpenFile(dbPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, privateFileMode)
		if errors.Is(createErr, fs.ErrExist) {
			return validatePrivateDatabase(dbPath)
		}
		if createErr != nil {
			return fmt.Errorf("create database file: %w", createErr)
		}
		return file.Close()
	case err != nil:
		return fmt.Errorf("inspect database file %s: %w", dbPath, err)
	default:
		return validatePrivateDatabase(dbPath)
	}
}

// EnsureDataDirectory creates the configured application data directory with
// owner-only access when absent and validates an existing destination.
func EnsureDataDirectory(path string) error {
	if path == "" {
		return errors.New("data directory path is empty")
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(path, privateDirectoryMode); err != nil {
			return fmt.Errorf("create private data directory: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("inspect data directory %s: %w", path, err)
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("data directory must not be a symbolic link: %s", path)
	case !info.IsDir():
		return fmt.Errorf("data directory must be a directory: %s", path)
	default:
		return nil
	}
}

func validatePrivateDatabase(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect database file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("database file must not be a symbolic link: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("database file must be a regular file: %s", path)
	}
	return nil
}

// InspectStorage reports the effective privacy of the application storage without
// changing it. Windows ACL guarantees are not inferred from POSIX mode bits.
func InspectStorage(dataDir, dbPath string) map[string]any {
	issues := []map[string]any{}
	addIssue := func(kind, path string, expected os.FileMode, info os.FileInfo, reason string) {
		issues = append(issues, map[string]any{
			"kind": kind, "path": path, "expected_mode": fmt.Sprintf("%04o", expected),
			"actual_mode": fmt.Sprintf("%04o", info.Mode().Perm()), "reason": reason,
		})
	}
	if info, err := os.Lstat(dataDir); err == nil {
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			addIssue("data_directory", dataDir, privateDirectoryMode, info, "data directory is a symbolic link")
		case !info.IsDir():
			addIssue("data_directory", dataDir, privateDirectoryMode, info, "data directory is not a directory")
		case posixModesEnforced() && info.Mode().Perm()&0o077 != 0:
			addIssue("data_directory", dataDir, privateDirectoryMode, info, "group or other users can access the data directory")
		case posixModesEnforced() && !ownedByCurrentUser(info):
			addIssue("data_directory", dataDir, privateDirectoryMode, info, "data directory is owned by another user")
		}
	}
	for _, item := range []struct {
		kind string
		path string
	}{
		{"database", dbPath},
		{"database_journal", dbPath + "-journal"},
		{"database_wal", dbPath + "-wal"},
		{"database_shm", dbPath + "-shm"},
	} {
		info, err := os.Lstat(item.path)
		if err != nil {
			continue
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			addIssue(item.kind, item.path, privateFileMode, info, item.kind+" is a symbolic link")
		case !info.Mode().IsRegular():
			addIssue(item.kind, item.path, privateFileMode, info, item.kind+" is not a regular file")
		case posixModesEnforced() && info.Mode().Perm()&0o077 != 0:
			addIssue(item.kind, item.path, privateFileMode, info, "group or other users can access the "+item.kind)
		case posixModesEnforced() && !ownedByCurrentUser(info):
			addIssue(item.kind, item.path, privateFileMode, info, item.kind+" is owned by another user")
		}
	}
	result := map[string]any{
		"platform":    runtime.GOOS,
		"ok":          len(issues) == 0,
		"issues":      issues,
		"remediation": "Run 'grand-lyon-mcp db secure' to restrict the application data directory and database files to the current user.",
	}
	if !posixModesEnforced() {
		result["verified"] = false
		result["note"] = "POSIX mode bits do not prove private Windows storage; verify the destination ACL separately."
	}
	return result
}

// SecureStorage applies owner-only access to the configured application paths.
// It changes only the named files and never walks a parent or shared directory.
func SecureStorage(dataDir, dbPath string) (map[string]any, error) {
	changes := []map[string]any{}
	resecure := func(kind, path string, mode os.FileMode, directory bool) error {
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) && directory {
			if err := os.MkdirAll(path, mode); err != nil {
				return fmt.Errorf("create %s: %w", kind, err)
			}
			changes = append(changes, map[string]any{"kind": kind, "path": path, "mode": fmt.Sprintf("%04o", mode)})
			return nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect %s: %w", kind, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s must not be a symbolic link: %s", kind, path)
		}
		if directory && !info.IsDir() {
			return fmt.Errorf("%s must be a directory: %s", kind, path)
		}
		if !directory && !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular file: %s", kind, path)
		}
		if posixModesEnforced() && !ownedByCurrentUser(info) {
			return fmt.Errorf("%s is owned by another user: %s", kind, path)
		}
		if posixModesEnforced() && info.Mode().Perm()&0o077 != 0 {
			if err := os.Chmod(path, mode); err != nil {
				return fmt.Errorf("secure %s: %w", kind, err)
			}
			changes = append(changes, map[string]any{"kind": kind, "path": path, "mode": fmt.Sprintf("%04o", mode)})
		}
		return nil
	}
	if err := resecure("data_directory", dataDir, privateDirectoryMode, true); err != nil {
		return nil, err
	}
	if err := resecure("database", dbPath, privateFileMode, false); err != nil {
		return nil, err
	}
	for _, sidecar := range []string{dbPath + "-journal", dbPath + "-wal", dbPath + "-shm"} {
		if err := resecure(filepath.Base(sidecar), sidecar, privateFileMode, false); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"status": "OK", "changes": changes, "storage": InspectStorage(dataDir, dbPath),
	}, nil
}
