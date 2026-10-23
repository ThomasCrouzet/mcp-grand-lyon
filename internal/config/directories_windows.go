//go:build windows

package config

import (
	"os"
	"path/filepath"
)

func platformDirectories(home string) (string, string, error) {
	environment := selectEnvironment(os.Environ())
	configBase := valueOr(environment, "APPDATA", filepath.Join(home, "AppData", "Roaming"))
	dataBase := valueOr(environment, "LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	return filepath.Join(configBase, appName), filepath.Join(dataBase, appName), nil
}
