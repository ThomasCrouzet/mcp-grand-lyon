package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"version"}, &stdout, &stderr)

	if code != 0 || stdout.String() != "0.2.0\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestClientConfigUsesAbsoluteBinaryAndNonsecretEnvironment(t *testing.T) {
	configDir, dataDir, database := isolatedCLIEnvironment(t)
	t.Setenv("DATAGRANDLYON_USERNAME", "sensitive-account")
	t.Setenv("DATAGRANDLYON_PASSWORD", "sensitive-password")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"client-config", "--bin"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var document struct {
		Servers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	server := document.Servers["grand-lyon"]
	if !filepath.IsAbs(server.Command) {
		t.Fatalf("command = %q", server.Command)
	}
	if _, err := os.Stat(server.Command); err != nil {
		t.Fatalf("command does not exist: %v", err)
	}
	if strings.Join(server.Args, " ") != "serve --transport stdio" {
		t.Fatalf("args = %v", server.Args)
	}
	want := map[string]string{
		"GRAND_LYON_MCP_CONFIG_DIR": configDir,
		"GRAND_LYON_MCP_DATA_DIR":   dataDir,
		"GRAND_LYON_MCP_DB_PATH":    database,
		"GRAND_LYON_MCP_OFFLINE":    "true",
	}
	if !mapsEqual(server.Env, want) {
		t.Fatalf("env = %#v, want %#v", server.Env, want)
	}
	if strings.Contains(stdout.String(), "sensitive-") || strings.Contains(stdout.String(), "DATAGRANDLYON") {
		t.Fatal("client configuration exposed credentials")
	}
}

func TestEnvironmentCommandRedactsCredentials(t *testing.T) {
	isolatedCLIEnvironment(t)
	t.Setenv("DATAGRANDLYON_USERNAME", "sensitive-account")
	t.Setenv("DATAGRANDLYON_PASSWORD", "sensitive-password")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"env"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sensitive-") {
		t.Fatal("environment output exposed credentials")
	}
	if strings.Count(stdout.String(), "[set]") < 2 {
		t.Fatalf("environment output did not show redacted values: %s", stdout.String())
	}
}

func TestUnknownCommandFailsWithoutStdout(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"unknown-command"}, &stdout, &stderr)

	if code == 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCommandHelpSucceedsWithoutErrorMessage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"setup", "--help"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "Error:") || !strings.Contains(stderr.String(), "Usage of setup:") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestSetupForceConfigReplacesInvalidSettings(t *testing.T) {
	configDir, _, _ := isolatedCLIEnvironment(t)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(configDir, "settings.yaml")
	if err := os.WriteFile(settingsPath, []byte("runtime: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{
		"setup", "--yes", "--offline", "--force-config", "--skip-migrate",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	contents, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "runtime: [") {
		t.Fatalf("invalid settings remain: %s", contents)
	}
}

func isolatedCLIEnvironment(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	database := filepath.Join(root, "app.db")
	t.Setenv("GRAND_LYON_MCP_CONFIG_DIR", configDir)
	t.Setenv("GRAND_LYON_MCP_DATA_DIR", dataDir)
	t.Setenv("GRAND_LYON_MCP_DB_PATH", database)
	t.Setenv("GRAND_LYON_MCP_OFFLINE", "true")
	return configDir, dataDir, database
}

func mapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
