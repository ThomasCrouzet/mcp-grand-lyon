package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadUsesEnvironmentBeforeSecretsAndYAML(t *testing.T) {
	isolateEnvironment(t)
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	dbPath := filepath.Join(root, "database", "custom.db")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "must-not-exist")
	secrets := strings.Join([]string{
		"DATAGRANDLYON_USERNAME=saved-account",
		`DATAGRANDLYON_PASSWORD="$(touch ` + marker + `)"`,
		"GRAND_LYON_MCP_OFFLINE=false",
		"GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS=7",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(configDir, "secrets.env"), []byte(secrets), 0o600); err != nil {
		t.Fatal(err)
	}
	yaml := `version: 1
runtime:
  offline: false
  max_parallel_requests: 4
places:
  fuzzy_score_threshold: 64
network:
  connect_timeout_seconds: 11
  read_timeout_seconds: 27
  verify_tls: false
journey_scoring:
  duration: 0.30
  reliability: 0.30
  disruptions: 0.15
  walking: 0.10
  availability: 0.10
  user_preference: 0.05
`
	if err := os.WriteFile(filepath.Join(configDir, "settings.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GRAND_LYON_MCP_CONFIG_DIR", configDir)
	t.Setenv("GRAND_LYON_MCP_DATA_DIR", dataDir)
	t.Setenv("GRAND_LYON_MCP_DB_PATH", dbPath)
	t.Setenv("GRAND_LYON_MCP_OFFLINE", "true")
	t.Setenv("GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS", "9")
	t.Setenv("DATAGRANDLYON_USERNAME", "process-account")
	before := environmentSnapshot()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ConfigDir != configDir || cfg.DataDir != dataDir || cfg.DBPath != dbPath {
		t.Fatalf("paths = %#v", cfg)
	}
	if !cfg.Offline || cfg.MaxParallel != 9 {
		t.Fatalf("environment priority failed: %#v", cfg)
	}
	if cfg.Username != "process-account" || cfg.Password != "$(touch "+marker+")" {
		t.Fatalf("credential load failed: username=%q password=%q", cfg.Username, cfg.Password)
	}
	if cfg.VerifyTLS || cfg.ConnectTimeout != 11*time.Second || cfg.ReadTimeout != 27*time.Second {
		t.Fatalf("YAML network settings failed: %#v", cfg)
	}
	if cfg.FuzzyScoreThreshold != 64 || cfg.JourneyScoring.Duration != 0.30 {
		t.Fatalf("YAML application settings failed: %#v", cfg)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("secrets parser executed shell syntax: %v", err)
	}
	if after := environmentSnapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("Load mutated process environment\nbefore=%v\nafter=%v", before, after)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := map[string]string{
		"unsupported version": "version: 2\n",
		"negative timeout":    "version: 1\nnetwork:\n  connect_timeout_seconds: -1\n",
		"invalid threshold":   "version: 1\nplaces:\n  fuzzy_score_threshold: 101\n",
		"negative weight": `version: 1
journey_scoring:
  duration: -0.1
  reliability: 0.4
  disruptions: 0.25
  walking: 0.15
  availability: 0.15
  user_preference: 0.15
`,
		"invalid weight sum": `version: 1
journey_scoring:
  duration: 0.5
  reliability: 0.5
  disruptions: 0.5
  walking: 0.5
  availability: 0.5
  user_preference: 0.5
`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			isolateEnvironment(t)
			configDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(configDir, "settings.yaml"), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GRAND_LYON_MCP_CONFIG_DIR", configDir)
			t.Setenv("GRAND_LYON_MCP_DATA_DIR", filepath.Join(configDir, "data"))
			t.Setenv("GRAND_LYON_MCP_DB_PATH", filepath.Join(configDir, "data", "app.db"))

			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	tests := map[string][2]string{
		"boolean":     {"GRAND_LYON_MCP_OFFLINE", "sometimes"},
		"timeout":     {"GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS", "0"},
		"concurrency": {"GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS", "0"},
		"threshold":   {"GRAND_LYON_MCP_FUZZY_SCORE_THRESHOLD", "-1"},
	}
	for name, item := range tests {
		t.Run(name, func(t *testing.T) {
			isolateEnvironment(t)
			configDir := t.TempDir()
			t.Setenv("GRAND_LYON_MCP_CONFIG_DIR", configDir)
			t.Setenv("GRAND_LYON_MCP_DATA_DIR", filepath.Join(configDir, "data"))
			t.Setenv(item[0], item[1])

			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestLoadUsesIsolatedPlatformDefaults(t *testing.T) {
	isolateEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	for _, key := range append([]string(nil), supportedEnvironmentKeys...) {
		if key != "HOME" {
			t.Setenv(key, "")
		}
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !filepath.IsAbs(cfg.ConfigDir) || !filepath.IsAbs(cfg.DataDir) {
		t.Fatalf("default paths must be absolute: %#v", cfg)
	}
	if !strings.HasPrefix(cfg.ConfigDir, home) || !strings.HasPrefix(cfg.DataDir, home) {
		t.Fatalf("defaults escaped isolated home: %#v", cfg)
	}
	if cfg.DBPath != filepath.Join(cfg.DataDir, "grand_lyon_mcp.db") {
		t.Fatalf("DBPath = %q", cfg.DBPath)
	}
	if cfg.LogLevel != "INFO" || cfg.ConnectTimeout != 5*time.Second || cfg.ReadTimeout != 20*time.Second {
		t.Fatalf("defaults = %#v", cfg)
	}
	if cfg.MaxParallel != 6 || cfg.FuzzyScoreThreshold != 72 || !cfg.VerifyTLS {
		t.Fatalf("defaults = %#v", cfg)
	}
}

func TestWriteSecretsUsesMode0600AndRoundTripsValues(t *testing.T) {
	isolateEnvironment(t)
	root := t.TempDir()
	cfg := defaultConfig(filepath.Join(root, "config"), filepath.Join(root, "data"))
	cfg.DBPath = filepath.Join(root, "database", "custom.db")
	cfg.Offline = true
	cfg.Username = "saved account"
	cfg.Password = "spaces $dollar `tick` \"double\" \\ slash"

	if err := WriteSecrets(cfg); err != nil {
		t.Fatalf("WriteSecrets() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(cfg.ConfigDir, "secrets.env"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %04o, want 0600", mode)
	}
	t.Setenv("GRAND_LYON_MCP_CONFIG_DIR", cfg.ConfigDir)
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Username != cfg.Username || loaded.Password != cfg.Password {
		t.Fatalf("credentials did not round-trip: %#v", loaded)
	}
	if loaded.DataDir != cfg.DataDir || loaded.DBPath != cfg.DBPath || !loaded.Offline {
		t.Fatalf("runtime settings did not round-trip: %#v", loaded)
	}

	cfg.Password = "first\nsecond"
	if err := WriteSecrets(cfg); err == nil {
		t.Fatal("WriteSecrets accepted a multiline value")
	}
}

func environmentSnapshot() map[string]string {
	values := make(map[string]string, len(supportedEnvironmentKeys))
	for _, key := range supportedEnvironmentKeys {
		values[key] = os.Getenv(key)
	}
	return values
}

func isolateEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range supportedEnvironmentKeys {
		t.Setenv(key, "")
	}
}
