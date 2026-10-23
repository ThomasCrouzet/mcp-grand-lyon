package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGoConfigValidationAcceptance(t *testing.T) {
	runCase(t, "unknown-settings-key", testUnknownSettingsKey)
	runCase(t, "legacy-settings-compatibility", testLegacySettingsCompatibility)
}

func testUnknownSettingsKey(t *testing.T) {
	folder, env, values, err := testHarness.newEnvironment("unknown-settings-key")
	if err != nil {
		t.Fatal(err)
	}
	settings := `version: 1
runtime:
  offline: true
  max_parallel_request: 3
`
	if err := os.WriteFile(filepath.Join(values[configEnv], "settings.yaml"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	result, runErr := testHarness.runCommand(folder, env, 10*time.Second,
		testHarness.binary, "client-config", "--bin")
	if runErr == nil || result.ReturnCode == 0 {
		t.Fatal("client-config accepted a misspelled active settings key")
	}
	if len(result.Stdout) != 0 {
		t.Fatalf("failed command wrote stdout: %q", result.Stdout)
	}
	if !strings.Contains(string(result.Stderr), "max_parallel_request") {
		t.Fatalf("error did not identify the unknown key: %s", result.Stderr)
	}
}

func testLegacySettingsCompatibility(t *testing.T) {
	folder, env, values, err := testHarness.newEnvironment("legacy-settings-compatibility")
	if err != nil {
		t.Fatal(err)
	}
	settings := `version: 1
runtime:
  offline: true
  timezone: Europe/Paris
  locale: fr-FR
  max_parallel_requests: 4
results:
  maximum_items_per_tool: 20
  maximum_warning_count: 20
places:
  default_radius_m: 1000
  maximum_radius_m: 5000
  fuzzy_score_threshold: 72
cache:
  allow_stale_on_error: true
  negative_ttl_seconds: 30
network:
  connect_timeout_seconds: 5
  read_timeout_seconds: 20
  maximum_attempts: 3
  verify_tls: true
journey_scoring:
  duration: 0.35
  reliability: 0.25
  disruptions: 0.15
  walking: 0.10
  availability: 0.10
  user_preference: 0.05
`
	if err := os.WriteFile(filepath.Join(values[configEnv], "settings.yaml"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	result, runErr := testHarness.runCommand(folder, env, 10*time.Second,
		testHarness.binary, "client-config", "--bin")
	if runErr != nil {
		t.Fatal(runErr)
	}
	if result.ReturnCode != 0 || !strings.Contains(string(result.Stdout), `"mcpServers"`) {
		t.Fatalf("legacy settings were not accepted: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}
