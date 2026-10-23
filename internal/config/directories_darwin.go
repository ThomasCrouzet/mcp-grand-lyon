//go:build darwin

package config

import "path/filepath"

func platformDirectories(home string) (string, string, error) {
	base := filepath.Join(home, "Library", "Application Support", appName)
	return base, base, nil
}
