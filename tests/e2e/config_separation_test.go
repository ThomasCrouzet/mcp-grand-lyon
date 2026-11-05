package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigurationSeparationAcceptance(t *testing.T) {
	runCase(t, "config-yaml-precedence", testYAMLPrecedenceAfterSetup)
	runCase(t, "config-legacy-migration", testLegacySecretsMigration)
}

func testYAMLPrecedenceAfterSetup(t *testing.T) {
	folder, env, values, err := testHarness.newEnvironment("config-yaml-precedence")
	if err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{
		"DATAGRANDLYON_USERNAME": "sensitive-account",
		"DATAGRANDLYON_PASSWORD": "sensitive-password",
	})
	if _, err := testHarness.runCommand(folder, env, 90*time.Second,
		testHarness.binary, "setup", "--yes", "--offline"); err != nil {
		t.Fatal(err)
	}
	secretsPath := filepath.Join(values[configEnv], "secrets.env")
	secretsBefore, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"HTTP_READ_TIMEOUT", "GRAND_LYON_MCP_OFFLINE", "DATAGRANDLYON_PASSWORD=\"sensitive-password\""} {
		if forbidden == "DATAGRANDLYON_PASSWORD=\"sensitive-password\"" {
			if !strings.Contains(string(secretsBefore), forbidden) {
				t.Fatalf("setup did not store the credential: %s", secretsBefore)
			}
			continue
		}
		if strings.Contains(string(secretsBefore), forbidden) {
			t.Fatalf("secrets.env mirrors runtime setting %s:\n%s", forbidden, secretsBefore)
		}
	}

	settingsPath := filepath.Join(values[configEnv], "settings.yaml")
	settings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(settings), "read_timeout_seconds: 20", "read_timeout_seconds: 1", 1)
	if patched == string(settings) {
		t.Fatalf("settings did not contain the default timeout:\n%s", settings)
	}
	if err := os.WriteFile(settingsPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	report := explainConfiguration(t, folder, env)
	if value, origin := settingReport(t, report, "GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS"); value != "1" || origin != "yaml" {
		t.Fatalf("YAML timeout was not effective: value=%q origin=%q", value, origin)
	}
	overridden := explainConfiguration(t, folder, mergeEnvironment(env, map[string]string{
		"GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS": "2",
	}))
	if value, origin := settingReport(t, overridden, "GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS"); value != "2" || origin != "process" {
		t.Fatalf("process override did not win: value=%q origin=%q", value, origin)
	}
	secretsAfter, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(secretsAfter) != string(secretsBefore) {
		t.Fatalf("reading configuration rewrote secrets:\nbefore=%s\nafter=%s", secretsBefore, secretsAfter)
	}
	if strings.Contains(string(reportJSON(t, report)), "sensitive-password") {
		t.Fatal("configuration explanation exposed a credential value")
	}
}

func testLegacySecretsMigration(t *testing.T) {
	folder, env, values, err := testHarness.newEnvironment("config-legacy-migration")
	if err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{
		"DATAGRANDLYON_USERNAME": "sensitive-account",
		"DATAGRANDLYON_PASSWORD": "sensitive-password",
	})
	if _, err := testHarness.runCommand(folder, env, 90*time.Second,
		testHarness.binary, "setup", "--yes", "--offline"); err != nil {
		t.Fatal(err)
	}
	snippetPath := filepath.Join(values[configEnv], "mcp-client.snippet.json")
	snippetBefore, err := os.ReadFile(snippetPath)
	if err != nil {
		t.Fatal(err)
	}
	secretsPath := filepath.Join(values[configEnv], "secrets.env")
	legacy := "GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS=\"1\"\nGRAND_LYON_MCP_MAX_PARALLEL_REQUESTS=\"3\"\n"
	secrets, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretsPath, append(secrets, []byte(legacy)...), 0o600); err != nil {
		t.Fatal(err)
	}

	first := runConfigJSON(t, folder, env, "migrate")
	if first["changed"] != true {
		t.Fatalf("first migration = %#v", first)
	}
	if migrated, _ := first["migrated"].([]any); len(migrated) < 2 {
		t.Fatalf("first migration did not report moved settings: %#v", first)
	}
	if strings.Contains(string(runConfigRaw(t, folder, env, "migrate")), "sensitive-password") {
		t.Fatal("migration output exposed a credential value")
	}
	second := runConfigJSON(t, folder, env, "migrate")
	if second["changed"] != false {
		t.Fatalf("second migration was not idempotent: %#v", second)
	}

	report := explainConfiguration(t, folder, env)
	if value, origin := settingReport(t, report, "GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS"); value != "1" || origin != "yaml" {
		t.Fatalf("migrated timeout = value %q origin %q", value, origin)
	}
	if value, origin := settingReport(t, report, "GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS"); value != "3" || origin != "yaml" {
		t.Fatalf("migrated concurrency = value %q origin %q", value, origin)
	}
	migratedSecrets, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(migratedSecrets), "READ_TIMEOUT") || strings.Contains(string(migratedSecrets), "MAX_PARALLEL") {
		t.Fatalf("legacy nonsecret settings remain in secrets:\n%s", migratedSecrets)
	}
	snippetAfter, err := os.ReadFile(snippetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(snippetAfter) != string(snippetBefore) {
		t.Fatal("migration changed the generated MCP client configuration")
	}
}

func runConfigJSON(t *testing.T, folder string, env []string, command string) map[string]any {
	t.Helper()
	raw := runConfigRaw(t, folder, env, command)
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode config %s: %v\n%s", command, err, raw)
	}
	return result
}

func runConfigRaw(t *testing.T, folder string, env []string, command string) []byte {
	t.Helper()
	result, err := testHarness.runCommand(folder, env, 30*time.Second, testHarness.binary, "config", command)
	if err != nil {
		t.Fatal(err)
	}
	return result.Stdout
}

func explainConfiguration(t *testing.T, folder string, env []string) map[string]any {
	t.Helper()
	return runConfigJSON(t, folder, env, "explain")
}

func settingReport(t *testing.T, report map[string]any, key string) (string, string) {
	t.Helper()
	settings, ok := report["settings"].([]any)
	if !ok {
		t.Fatalf("explain has no settings: %#v", report)
	}
	for _, raw := range settings {
		setting, _ := raw.(map[string]any)
		if setting["setting"] == key {
			return stringValue(setting["value"]), stringValue(setting["origin"])
		}
	}
	t.Fatalf("setting %s is absent: %#v", key, report)
	return "", ""
}

func reportJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
