package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProfileCLIAcceptance(t *testing.T) {
	runCase(t, "profile-cli-workflow", testProfileCLIWorkflow)
	runCase(t, "profile-cli-validation", testProfileCLIValidation)
}

func testProfileCLIWorkflow(t *testing.T) {
	folder, env, values, err := testHarness.newEnvironment("profile-cli-workflow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHarness.runCommand(folder, env, 90*time.Second, testHarness.binary, "setup", "--yes", "--offline"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(values[configEnv], "profiles.yaml")); err != nil {
		t.Fatal(err)
	}
	runProfileCommand(t, folder, env,
		"set", "--type", "place", "--name", "home2", "--label", "Home 2", "--latitude", "45.7606", "--longitude", "4.8618")
	runProfileCommand(t, folder, env,
		"set", "--type", "place", "--name", "work2", "--label", "Work 2", "--latitude", "45.7578", "--longitude", "4.8320")
	runProfileCommand(t, folder, env,
		"set", "--type", "commute", "--name", "daily", "--origin", "home2", "--destination", "work2", "--max-walking-m", "800", "--modes", "tcl,velov")
	runProfileCommand(t, folder, env,
		"set", "--type", "briefing", "--name", "morning", "--commute", "daily", "--include", "transit,velov,traffic")
	validation := runProfileCommand(t, folder, env, "validate")
	if validation["status"] != "OK" {
		t.Fatalf("profile validation = %#v", validation)
	}
	list := runProfileCommand(t, folder, env, "list")
	commutes, _ := list["commutes"].(map[string]any)
	if _, ok := commutes["daily"].(map[string]any); !ok {
		t.Fatalf("created commute is absent: %#v", list)
	}

	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	briefing := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "morning", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	assertStatus(t, briefing, "partial", "ok")
	facts := objectValue(t, dataObject(t, briefing), "facts")
	commute, _ := facts["commute"].(map[string]any)
	if commute["origin"] != "home2" || commute["destination"] != "work2" {
		t.Fatalf("briefing did not use the CLI commute: %#v", facts)
	}
	client.close(true)

	runProfileCommand(t, folder, env, "remove", "--type", "briefing", "--name", "morning")
	restarted := startMCP(t, folder, env)
	initializeMCP(t, restarted)
	removed := restarted.callTool("lyon_personal_briefing", map[string]any{"profile": "morning"})
	assertStatus(t, removed, "not_found")
	restarted.close(true)
}

func testProfileCLIValidation(t *testing.T) {
	folder, env, values, err := testHarness.newEnvironment("profile-cli-validation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHarness.runCommand(folder, env, 90*time.Second, testHarness.binary, "setup", "--yes", "--offline"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(values[configEnv], "profiles.yaml")); err != nil {
		t.Fatal(err)
	}
	runProfileCommand(t, folder, env,
		"set", "--type", "place", "--name", "home2", "--latitude", "45.7606", "--longitude", "4.8618")
	runProfileCommand(t, folder, env,
		"set", "--type", "place", "--name", "work2", "--latitude", "45.7578", "--longitude", "4.8320")
	runProfileCommand(t, folder, env,
		"set", "--type", "commute", "--name", "daily", "--origin", "home2", "--destination", "work2")

	invalidCases := [][]string{
		{"set", "--type", "place", "--name", "bad", "--latitude", "999", "--longitude", "4.85"},
		{"set", "--type", "commute", "--name", "broken", "--origin", "missing", "--destination", "work2"},
		{"set", "--type", "commute", "--name", "daily", "--origin", "missing", "--destination", "work2", "--force"},
		{"set", "--type", "briefing", "--name", "bad", "--commute", "missing"},
		{"set", "--type", "briefing", "--name", "bad", "--commute", "daily", "--include", "unsupported"},
	}
	for _, arguments := range invalidCases {
		result, runErr := testHarness.runCommand(folder, env, 30*time.Second,
			append([]string{testHarness.binary, "profile"}, arguments...)...)
		if runErr == nil || result.ReturnCode == 0 {
			t.Fatalf("invalid profile command succeeded: %v", arguments)
		}
		if len(result.Stdout) != 0 {
			t.Fatalf("invalid profile command wrote stdout: %q", result.Stdout)
		}
	}
	runProfileCommand(t, folder, env,
		"set", "--type", "briefing", "--name", "morning", "--commute", "daily", "--include", "transit")
	runProfileCommand(t, folder, env, "remove", "--type", "briefing", "--name", "morning")
	list := runProfileCommand(t, folder, env, "list")
	briefings, _ := list["briefings"].(map[string]any)
	if _, exists := briefings["morning"]; exists {
		t.Fatalf("removed briefing remains: %#v", list)
	}
}

func runProfileCommand(t *testing.T, folder string, env []string, args ...string) map[string]any {
	t.Helper()
	result, err := testHarness.runCommand(folder, env, 30*time.Second,
		append([]string{testHarness.binary, "profile"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Stdout, &output); err != nil {
		t.Fatalf("decode profile %v: %v\n%s", args, err, result.Stdout)
	}
	return output
}
