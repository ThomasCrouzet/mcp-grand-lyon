package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
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

func TestWriteSecretsStoresOnlyCredentialsAndConfigBootstrap(t *testing.T) {
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
	secretsPath := filepath.Join(cfg.ConfigDir, "secrets.env")
	info, err := os.Stat(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %04o, want 0600", mode)
	}
	contents, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"HTTP_READ_TIMEOUT", "GRAND_LYON_MCP_OFFLINE", "GRAND_LYON_MCP_DB_PATH", "TRANSITOUS_ENABLED"} {
		if strings.Contains(string(contents), forbidden) {
			t.Fatalf("secrets file still mirrors runtime setting %s: %s", forbidden, contents)
		}
	}
	t.Setenv("GRAND_LYON_MCP_CONFIG_DIR", cfg.ConfigDir)
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Username != cfg.Username || loaded.Password != cfg.Password {
		t.Fatalf("credentials did not round-trip: %#v", loaded)
	}
	if loaded.ConfigDir != cfg.ConfigDir {
		t.Fatalf("config bootstrap did not round-trip: %#v", loaded)
	}
	if loaded.Offline {
		t.Fatalf("offline leaked from secrets into runtime settings: %#v", loaded)
	}

	cfg.Password = "first\nsecond"
	if err := WriteSecrets(cfg); err == nil {
		t.Fatal("WriteSecrets accepted a multiline value")
	}
}

func TestYAMLSettingIsNotOverriddenByGeneratedSecrets(t *testing.T) {
	isolateEnvironment(t)
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	cfg := defaultConfig(configDir, filepath.Join(root, "data"))
	cfg.Username = "account"
	cfg.Password = "password"
	if err := WriteSecrets(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAND_LYON_MCP_CONFIG_DIR", configDir)
	settings := "version: 1\nnetwork:\n  read_timeout_seconds: 1\nruntime:\n  offline: true\n"
	if err := os.WriteFile(filepath.Join(configDir, "settings.yaml"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ReadTimeout != time.Second || !loaded.Offline {
		t.Fatalf("YAML settings did not take effect: %#v", loaded)
	}
	if cfg.ReadTimeout == time.Second {
		t.Fatal("test fixture did not start with the default timeout")
	}
}

func TestMigrateMovesLegacyOverridesAndIsIdempotent(t *testing.T) {
	isolateEnvironment(t)
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secrets := strings.Join([]string{
		"DATAGRANDLYON_USERNAME=\"account\"",
		"DATAGRANDLYON_PASSWORD=\"secret\"",
		"GRAND_LYON_MCP_CONFIG_DIR=\"" + configDir + "\"",
		"GRAND_LYON_MCP_DATA_DIR=\"" + filepath.Join(root, "legacy-data") + "\"",
		"GRAND_LYON_MCP_DB_PATH=\"" + filepath.Join(root, "legacy-data", "legacy.db") + "\"",
		"GRAND_LYON_MCP_OFFLINE=\"true\"",
		"GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS=\"1\"",
		"TRANSITOUS_ENABLED=\"true\"",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(configDir, "secrets.env"), []byte(secrets), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "settings.yaml"), []byte("version: 1\nnetwork:\n  read_timeout_seconds: 20\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAND_LYON_MCP_CONFIG_DIR", configDir)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReadTimeout != time.Second || !cfg.Offline || !cfg.TransitousEnabled || cfg.DBPath != filepath.Join(root, "legacy-data", "legacy.db") {
		t.Fatalf("legacy effective configuration changed before migration: %#v", cfg)
	}
	first, err := Migrate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed || len(first.Migrated) == 0 || len(first.Conflicts) == 0 {
		t.Fatalf("first migration = %#v", first)
	}
	settingsData, err := os.ReadFile(filepath.Join(configDir, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"read_timeout_seconds: 1", "offline: true", "transitous_enabled: true", "db_path:"} {
		if !strings.Contains(string(settingsData), expected) {
			t.Fatalf("settings missing %q:\n%s", expected, settingsData)
		}
	}
	secretData, err := os.ReadFile(filepath.Join(configDir, "secrets.env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(secretData), "READ_TIMEOUT") || strings.Contains(string(secretData), "OFFLINE") {
		t.Fatalf("legacy nonsecret values remain in secrets:\n%s", secretData)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ReadTimeout != time.Second || !reloaded.Offline || !reloaded.TransitousEnabled || reloaded.DBPath != cfg.DBPath {
		t.Fatalf("migration did not preserve effective configuration: %#v", reloaded)
	}
	second, err := Migrate(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed || len(second.Migrated) != 0 {
		t.Fatalf("second migration was not idempotent: %#v", second)
	}
}

func TestExplainReportsOriginsWithoutCredentials(t *testing.T) {
	isolateEnvironment(t)
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	cfg := defaultConfig(configDir, filepath.Join(root, "data"))
	cfg.Username = "account"
	cfg.Password = "secret-value"
	if err := WriteSecrets(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "settings.yaml"), []byte("version: 1\nnetwork:\n  read_timeout_seconds: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAND_LYON_MCP_CONFIG_DIR", configDir)
	t.Setenv("GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS", "3")
	report, err := Explain()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := yaml.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-value") {
		t.Fatal("explain exposed a credential value")
	}
	settings, _ := report["settings"].([]SettingExplanation)
	origins := map[string]string{}
	values := map[string]string{}
	for _, setting := range settings {
		origins[setting.Setting] = setting.Origin
		values[setting.Setting] = setting.Value
	}
	if origins["GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS"] != "yaml" || values["GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS"] != "2" {
		t.Fatalf("timeout explanation = %#v", origins)
	}
	if origins["GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS"] != "process" || values["GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS"] != "3" {
		t.Fatalf("override explanation = %#v", origins)
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
