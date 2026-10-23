package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigurationErrorReporting(t *testing.T) {
	runCase(t, "configuration-environment-errors", func(t *testing.T) {
		for _, key := range []string{
			"GRAND_LYON_MCP_OFFLINE", "GRAND_LYON_MCP_VERIFY_TLS", "TRANSITOUS_ENABLED",
			"GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS", "GRAND_LYON_MCP_FUZZY_SCORE_THRESHOLD",
			"GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS", "GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS",
		} {
			t.Run(key, func(t *testing.T) {
				folder, env, _, err := testHarness.newEnvironment("invalid-" + strings.ToLower(key))
				if err != nil {
					t.Fatal(err)
				}
				env = mergeEnvironment(env, map[string]string{key: "incorrect"})
				result, err := testHarness.runCommand(folder, env, 10*time.Second, testHarness.binary, "serve", "--quiet")
				if err == nil || result.ReturnCode == 0 || len(result.Stdout) != 0 || !strings.Contains(string(result.Stderr), key) {
					t.Fatalf("invalid %s was not rejected with a useful startup error: %#v", key, result)
				}
			})
		}
	})
	runCase(t, "configuration-secret-error-redaction", func(t *testing.T) {
		folder, env, values, err := testHarness.newEnvironment("secret-error-redaction")
		if err != nil {
			t.Fatal(err)
		}
		const sentinel = "private-value-must-not-appear"
		path := filepath.Join(values[configEnv], "secrets.env")
		if err := os.WriteFile(path, []byte("# Invalid pasted value\n"+sentinel+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := testHarness.runCommand(folder, env, 10*time.Second, testHarness.binary, "serve", "--quiet")
		if err == nil || result.ReturnCode == 0 || len(result.Stdout) != 0 {
			t.Fatalf("malformed secrets file did not stop startup: %#v", result)
		}
		if strings.Contains(string(result.Stderr), sentinel) {
			t.Error("startup error disclosed the invalid secrets value")
		}
		if !strings.Contains(string(result.Stderr), "line 2") {
			t.Errorf("startup error did not identify the damaged line: %s", result.Stderr)
		}
	})
}
