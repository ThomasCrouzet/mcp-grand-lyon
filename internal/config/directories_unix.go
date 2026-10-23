//go:build !darwin && !windows

package config

import (
	"os"
	"path/filepath"
)

func platformDirectories(home string) (string, string, error) {
	configBase := os.Getenv("XDG_CONFIG_HOME")
	if configBase == "" {
		configBase = filepath.Join(home, ".config")
	}
	dataBase := os.Getenv("XDG_DATA_HOME")
	if dataBase == "" {
		dataBase = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(configBase, appName), filepath.Join(dataBase, appName), nil
}
