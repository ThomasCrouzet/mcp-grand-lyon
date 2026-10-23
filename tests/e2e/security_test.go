package e2e_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGoSecurityAcceptance(t *testing.T) {
	runCase(t, "setup-destination-safety", testSetupDestinationSafety)
	runCase(t, "secrets-file-safety", testSecretsFileSafety)
	runCase(t, "finite-config-values", testFiniteConfigValues)
	runCase(t, "signal-shutdown", testSignalShutdown)
	runCase(t, "effective-config-reporting", testEffectiveConfigReporting)
}

func testSetupDestinationSafety(t *testing.T) {
	if runtime.GOOS != "windows" {
		for _, name := range []string{
			"settings.yaml",
			"profiles.yaml",
			"sources.yaml",
			"waste-taxonomy.yaml",
			"mcp-client.snippet.json",
		} {
			t.Run(name+"-symlink", func(t *testing.T) {
				folder, env, values, err := testHarness.newEnvironment("setup-symlink-" + strings.TrimSuffix(name, filepath.Ext(name)))
				if err != nil {
					t.Fatal(err)
				}
				external := filepath.Join(folder, "external-target")
				externalContents := "external content must stay unchanged\n"
				if name == "settings.yaml" {
					externalContents = "version: 1\nruntime:\n  offline: false\n"
				}
				if err := os.WriteFile(external, []byte(externalContents), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, filepath.Join(values[configEnv], name)); err != nil {
					t.Fatal(err)
				}

				assertCommandFailure(t, folder, env, 10*time.Second,
					testHarness.binary, "setup", "--yes", "--offline", "--skip-migrate")
				contents, err := os.ReadFile(external)
				if err != nil {
					t.Fatal(err)
				}
				if string(contents) != externalContents {
					t.Fatalf("external target changed: %q", contents)
				}
			})
		}
	}

	folder, env, values, err := testHarness.newEnvironment("setup-nonregular-destination")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(values[configEnv], "profiles.yaml"), 0o700); err != nil {
		t.Fatal(err)
	}
	assertCommandFailure(t, folder, env, 10*time.Second,
		testHarness.binary, "setup", "--yes", "--offline", "--skip-migrate")
}

func testSecretsFileSafety(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Run("symlink", func(t *testing.T) {
			folder, env, values, err := testHarness.newEnvironment("secrets-symlink")
			if err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(folder, "external-secrets")
			if err := os.WriteFile(external, []byte(offlineEnv+"=true\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, filepath.Join(values[configEnv], "secrets.env")); err != nil {
				t.Fatal(err)
			}
			assertCommandFailure(t, folder, env, 10*time.Second,
				testHarness.binary, "client-config", "--bin")
		})

		t.Run("permissions", func(t *testing.T) {
			folder, env, values, err := testHarness.newEnvironment("secrets-permissions")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(values[configEnv], "secrets.env")
			if err := os.WriteFile(path, []byte(offlineEnv+"=true\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
			assertCommandFailure(t, folder, env, 10*time.Second,
				testHarness.binary, "client-config", "--bin")
		})

		t.Run("fifo", func(t *testing.T) {
			folder, env, values, err := testHarness.newEnvironment("secrets-fifo")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(values[configEnv], "secrets.env")
			command := exec.Command("mkfifo", path)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("create FIFO: %v: %s", err, output)
			}
			_, runErr := testHarness.runCommand(folder, env, time.Second,
				testHarness.binary, "client-config", "--bin")
			if runErr == nil {
				t.Fatal("client-config accepted a FIFO secrets path")
			}
			if strings.Contains(runErr.Error(), "timed out") {
				t.Fatal("client-config blocked while opening a FIFO secrets path")
			}
		})
	}
}

func testFiniteConfigValues(t *testing.T) {
	settings := []struct {
		name string
		body string
	}{
		{
			name: "nan-weight",
			body: `version: 1
journey_scoring:
  duration: .nan
  reliability: 0.25
  disruptions: 0.15
  walking: 0.10
  availability: 0.10
  user_preference: 0.05
`,
		},
		{
			name: "infinite-timeout",
			body: `version: 1
network:
  connect_timeout_seconds: .inf
`,
		},
	}
	for _, item := range settings {
		t.Run(item.name, func(t *testing.T) {
			folder, env, values, err := testHarness.newEnvironment("finite-" + item.name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(values[configEnv], "settings.yaml"), []byte(item.body), 0o600); err != nil {
				t.Fatal(err)
			}
			assertCommandFailure(t, folder, env, 10*time.Second,
				testHarness.binary, "client-config", "--bin")
		})
	}

	folder, env, _, err := testHarness.newEnvironment("finite-env-timeout")
	if err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{
		"GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS": "NaN",
	})
	assertCommandFailure(t, folder, env, 10*time.Second,
		testHarness.binary, "client-config", "--bin")
}

func testSignalShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses console control events instead of POSIX signals")
	}
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			folder, env, values, err := testHarness.newEnvironment("signal-" + strings.ReplaceAll(signal.String(), " ", "-"))
			if err != nil {
				t.Fatal(err)
			}
			argv := []string{testHarness.binary, "serve", "--transport", "stdio", "--quiet"}
			command := exec.Command(argv[0], argv[1:]...)
			command.Dir = folder
			command.Env = env
			stdin, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			started := time.Now()
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			waitForPath(t, values[databaseEnv], 5*time.Second)
			if err := command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			waitErr := command.Wait()
			_ = stdin.Close()
			returnCode := 0
			if waitErr != nil {
				var exitErr *exec.ExitError
				if errors.As(waitErr, &exitErr) {
					returnCode = exitErr.ExitCode()
				} else {
					returnCode = -1
				}
			}
			testHarness.addCommand(commandEvidence{
				Argv: argv, Cwd: folder, Environment: reportEnvironment(env),
				ReturnCode: returnCode, Stdout: stdout.String(), Stderr: stderr.String(),
				DurationMS: time.Since(started).Milliseconds(),
			})
			if waitErr != nil {
				t.Fatalf("signal shutdown failed: %v; stderr=%q", waitErr, stderr.String())
			}
			if stdout.Len() != 0 || strings.Contains(stderr.String(), "Error:") {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func testEffectiveConfigReporting(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("effective-config")
	if err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{offlineEnv: "false"})
	doctor, err := testHarness.runCommand(folder, env, 10*time.Second,
		testHarness.binary, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		Offline bool `json:"offline"`
	}
	if err := json.Unmarshal(doctor.Stdout, &status); err != nil {
		t.Fatal(err)
	}
	if !status.Offline {
		t.Fatal("doctor reported requested live mode after the app selected fixtures")
	}
	smoke, err := testHarness.runCommand(folder, env, 10*time.Second,
		testHarness.binary, "smoke", "--tool", "lyon_resolve_place")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(smoke.Stdout), "mode=offline") {
		t.Fatalf("smoke did not report effective offline mode: %s", smoke.Stdout)
	}
}

func assertCommandFailure(t *testing.T, folder string, env []string, timeout time.Duration, argv ...string) {
	t.Helper()
	result, err := testHarness.runCommand(folder, env, timeout, argv...)
	if err == nil || result.ReturnCode == 0 {
		t.Fatalf("command succeeded unexpectedly: %s", strings.Join(argv, " "))
	}
	if len(bytes.TrimSpace(result.Stdout)) != 0 {
		t.Fatalf("failed command wrote stdout: %q", result.Stdout)
	}
}

func waitForPath(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(fmt.Errorf("path did not appear: %s", path))
}
