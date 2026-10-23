package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/assets"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/config"
)

var offlineSetting = regexp.MustCompile(`(?m)^(\s*offline:\s*)(true|false)\s*$`)

type clientDocument struct {
	Servers map[string]clientServer `json:"mcpServers"`
}

type clientServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

func clientConfig(cfg config.Config, executable string) clientDocument {
	return clientDocument{Servers: map[string]clientServer{
		"grand-lyon": {
			Command: executable,
			Args:    []string{"serve", "--transport", "stdio"},
			Env:     config.ClientEnvironment(cfg),
		},
	}}
}

func writeClientConfig(cfg config.Config, executable string) (string, error) {
	document := clientConfig(cfg, executable)
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	path := filepath.Join(cfg.ConfigDir, "mcp-client.snippet.json")
	if err := writeAtomicRegular(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write client configuration: %w", err)
	}
	return path, nil
}

func copyConfiguration(cfg config.Config, force bool) error {
	if err := os.MkdirAll(cfg.ConfigDir, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	files := []struct {
		source      string
		destination string
	}{
		{source: "settings.example.yaml", destination: "settings.yaml"},
		{source: "profiles.example.yaml", destination: "profiles.yaml"},
		{source: "sources.example.yaml", destination: "sources.yaml"},
		{source: "waste-taxonomy.yaml", destination: "waste-taxonomy.yaml"},
	}
	for _, file := range files {
		path := filepath.Join(cfg.ConfigDir, file.destination)
		exists, err := regularDestination(path)
		if err != nil {
			return err
		}
		if exists && !force {
			if file.destination == "settings.yaml" {
				if err := patchOffline(path, cfg.Offline); err != nil {
					return err
				}
			}
			continue
		}
		data, err := assets.Files.ReadFile("config/" + file.source)
		if err != nil {
			return fmt.Errorf("read embedded configuration %s: %w", file.source, err)
		}
		if file.destination == "settings.yaml" {
			data = offlineSetting.ReplaceAll(data, []byte("${1}"+strconv.FormatBool(cfg.Offline)))
		}
		if err := writeAtomicRegular(path, data, 0o644); err != nil {
			return fmt.Errorf("write configuration file %s: %w", path, err)
		}
	}
	return nil
}

func patchOffline(path string, offline bool) error {
	if _, err := regularDestination(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read settings file: %w", err)
	}
	patched := offlineSetting.ReplaceAll(data, []byte("${1}"+strconv.FormatBool(offline)))
	if err := writeAtomicRegular(path, patched, 0o644); err != nil {
		return fmt.Errorf("update settings file: %w", err)
	}
	return nil
}

func regularDestination(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect destination %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("destination must not be a symbolic link: %s", path)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("destination must be a regular file: %s", path)
	}
	return true, nil
}

func writeAtomicRegular(path string, data []byte, mode fs.FileMode) error {
	if _, err := regularDestination(path); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func executablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	return path, nil
}
