package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStoragePermissionAcceptance(t *testing.T) {
	runCase(t, "storage-private-setup", testPrivateStorageFromSetup)
	runCase(t, "storage-private-direct-startup", testPrivateStorageDirectStartup)
	runCase(t, "storage-permission-diagnosis", testStoragePermissionDiagnosis)
}

func testPrivateStorageFromSetup(t *testing.T) {
	requirePrivateModes(t)
	folder, env, values, err := testHarness.newEnvironment("storage-private-setup")
	if err != nil {
		t.Fatal(err)
	}
	// Start from absent paths so setup itself creates them under the ambient umask.
	if err := os.RemoveAll(values[configEnv]); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(values[dataEnv]); err != nil {
		t.Fatal(err)
	}
	if _, err := testHarness.runCommand(folder, env, 90*time.Second,
		testHarness.binary, "setup", "--yes", "--offline"); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, values[configEnv], 0o700)
	assertPrivateMode(t, values[dataEnv], 0o700)
	assertPrivateMode(t, values[databaseEnv], 0o600)
}

func testPrivateStorageDirectStartup(t *testing.T) {
	requirePrivateModes(t)
	folder, env, values, err := testHarness.newEnvironment("storage-private-direct-startup")
	if err != nil {
		t.Fatal(err)
	}
	// No setup: startup must create private storage itself.
	if _, err := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "db", "info"); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, values[dataEnv], 0o700)
	assertPrivateMode(t, values[databaseEnv], 0o600)
}

func testStoragePermissionDiagnosis(t *testing.T) {
	requirePrivateModes(t)
	folder, env, values, err := testHarness.newEnvironment("storage-permission-diagnosis")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHarness.runCommand(folder, env, 90*time.Second,
		testHarness.binary, "setup", "--yes", "--offline"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(values[dataEnv], 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(values[databaseEnv], 0o644); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(values[dataEnv], "unrelated")
	if err := os.Mkdir(unrelated, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unrelated, 0o755); err != nil {
		t.Fatal(err)
	}

	before, err := testHarness.runCommand(folder, env, 30*time.Second,
		testHarness.binary, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(before.Stdout, &report); err != nil {
		t.Fatalf("decode doctor report: %v\n%s", err, before.Stdout)
	}
	storage, ok := report["storage"].(map[string]any)
	if !ok {
		t.Fatalf("doctor has no storage section: %#v", report)
	}
	if storage["ok"] != false {
		t.Fatalf("broad storage permissions were reported healthy: %#v", storage)
	}
	issues, _ := storage["issues"].([]any)
	if len(issues) == 0 {
		t.Fatalf("doctor did not list permission issues: %#v", storage)
	}
	encoded := string(before.Stdout)
	if !strings.Contains(encoded, "db secure") {
		t.Fatalf("doctor does not describe the remediation: %s", before.Stdout)
	}
	if strings.Contains(encoded, "DATAGRANDLYON_PASSWORD") || strings.Contains(encoded, "sensitive-") {
		t.Fatal("doctor exposed credentials")
	}

	secure, err := testHarness.runCommand(folder, env, 30*time.Second,
		testHarness.binary, "db", "secure")
	if err != nil {
		t.Fatal(err)
	}
	var secured map[string]any
	if err := json.Unmarshal(secure.Stdout, &secured); err != nil {
		t.Fatalf("decode db secure report: %v\n%s", err, secure.Stdout)
	}
	if secured["status"] != "OK" {
		t.Fatalf("db secure status = %v", secured["status"])
	}
	assertPrivateMode(t, values[dataEnv], 0o700)
	assertPrivateMode(t, values[databaseEnv], 0o600)
	assertPrivateMode(t, unrelated, 0o755)

	after, err := testHarness.runCommand(folder, env, 30*time.Second,
		testHarness.binary, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	var healthy map[string]any
	if err := json.Unmarshal(after.Stdout, &healthy); err != nil {
		t.Fatal(err)
	}
	healthyStorage := objectValue(t, healthy, "storage")
	if healthyStorage["ok"] != true {
		t.Fatalf("db secure did not correct the diagnosis: %#v", healthyStorage)
	}
}

func requirePrivateModes(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are not an access-control proof on Windows")
	}
}

func assertPrivateMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

func assertPrivateStorageAt(t *testing.T, path string) {
	t.Helper()
	assertPrivateMode(t, path, 0o600)
}
