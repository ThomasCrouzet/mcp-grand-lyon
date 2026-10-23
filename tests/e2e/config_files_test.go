package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigurationFileFailures(t *testing.T) {
	runCase(t, "config-path-directories", testConfigurationPathDirectories)
	runCase(t, "config-invalid-yaml", testInvalidConfigurationYAML)
	runCase(t, "config-secrets-parser", testSecretsParserFailures)
	runCase(t, "storage-source-health-startup", testMissingSourceHealthStartup)
}

func testConfigurationPathDirectories(t *testing.T) {
	for _, name := range []string{"settings.yaml", "sources.yaml", "profiles.yaml"} {
		t.Run(name, func(t *testing.T) {
			folder, env, values, err := testHarness.newEnvironment("config-directory-" + strings.TrimSuffix(name, ".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(values[configEnv], name), 0o700); err != nil {
				t.Fatal(err)
			}
			assertBrokenConfigStartup(t, folder, env,
				[]string{name, "directory"}, nil)
		})
	}
}

func testInvalidConfigurationYAML(t *testing.T) {
	tests := map[string]string{
		"sources.yaml":  "sources: [\n",
		"profiles.yaml": "profiles: [\n",
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			folder, env, values, err := testHarness.newEnvironment("config-invalid-" + strings.TrimSuffix(name, ".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(values[configEnv], name), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			assertBrokenConfigStartup(t, folder, env,
				[]string{strings.TrimSuffix(name, ".yaml"), "line 1"}, nil)
		})
	}
}

func testSecretsParserFailures(t *testing.T) {
	tests := []struct {
		name      string
		contents  string
		fragments []string
		sentinel  string
	}{
		{
			name:      "unterminated-quote",
			contents:  "DATAGRANDLYON_PASSWORD=\"sentinel-secret-value\n",
			fragments: []string{"secrets", "datagrandlyon_password", "unterminated"},
			sentinel:  "sentinel-secret-value",
		},
		{
			name:      "line-too-long",
			contents:  "DATAGRANDLYON_PASSWORD=sentinel-long-value-" + strings.Repeat("x", 70*1024) + "\n",
			fragments: []string{"secrets", "too long"},
			sentinel:  "sentinel-long-value",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			folder, env, values, err := testHarness.newEnvironment("secrets-" + test.name)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(values[configEnv], "secrets.env")
			if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			assertBrokenConfigStartup(t, folder, env, test.fragments, []string{test.sentinel})
		})
	}
}

func testMissingSourceHealthStartup(t *testing.T) {
	folder, env, values, err := preparedEnvironment("storage-source-health-startup")
	if err != nil {
		t.Fatal(err)
	}
	database := openAuditDatabase(t, values[databaseEnv])
	execAuditSQL(t, database, `DROP TABLE source_health`)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	assertBrokenConfigStartup(t, folder, env,
		[]string{"source_health", "no such table"}, nil)
}

func assertBrokenConfigStartup(
	t *testing.T,
	folder string,
	env []string,
	fragments []string,
	sentinels []string,
) {
	t.Helper()
	started := time.Now()
	result, runErr := testHarness.runCommand(folder, env, 5*time.Second,
		testHarness.binary, "serve", "--transport", "stdio", "--quiet")
	if runErr == nil || result.ReturnCode == 0 {
		t.Fatal("broken configuration started the server")
	}
	if time.Since(started) >= 5*time.Second || strings.Contains(runErr.Error(), "timed out") {
		t.Fatalf("broken configuration did not fail promptly: %v", runErr)
	}
	if len(bytes.TrimSpace(result.Stdout)) != 0 {
		t.Fatalf("startup failure wrote to stdout: %q", result.Stdout)
	}
	detail := strings.ToLower(string(result.Stderr))
	if detail == "" {
		t.Fatal("startup failure did not explain the error on stderr")
	}
	for _, fragment := range fragments {
		if !strings.Contains(detail, strings.ToLower(fragment)) {
			t.Fatalf("startup error does not identify %q: %q", fragment, result.Stderr)
		}
	}
	combined := string(result.Stdout) + string(result.Stderr)
	for _, sentinel := range sentinels {
		if sentinel != "" && strings.Contains(combined, sentinel) {
			t.Fatalf("startup error disclosed sentinel %q", sentinel)
		}
	}
}
