package e2e_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAlertFallbacksThroughInstalledProcess(t *testing.T) {
	t.Run("missing SIRI uses filtered DataPusher alerts", func(t *testing.T) {
		folder, env := alertFallbackEnvironment(t, "alert-fallback-missing-siri")
		override := fixtureOverride(t, folder)
		removeFixture(t, filepath.Join(override, "siri", "situation_exchange.json"))
		duplicateFirstAlert(t, filepath.Join(override, "datagrandlyon", "tcl_alerts.json"))
		env = mergeEnvironment(env, map[string]string{fixturesEnv: override})

		result := callMobilityAlerts(t, folder, env, "A")
		alerts := objectList(t, dataObject(t, result), "alerts")
		if len(alerts) != 1 {
			t.Fatalf("line A alerts = %#v, want one deduplicated alert", alerts)
		}
		if alerts[0]["id"] != "al1" || alerts[0]["severity"] != "major" {
			t.Fatalf("unexpected DataPusher alert: %#v", alerts[0])
		}
		if lines := stringSliceValue(t, alerts[0], "lines"); len(lines) != 1 || lines[0] != "A" {
			t.Fatalf("unexpected alert lines: %#v", alerts[0]["lines"])
		}
	})

	t.Run("malformed SIRI uses valid DataPusher alerts", func(t *testing.T) {
		folder, env := alertFallbackEnvironment(t, "alert-fallback-malformed-siri")
		override := fixtureOverride(t, folder)
		writeFixture(t, filepath.Join(override, "siri", "situation_exchange.json"), []byte("{"))
		env = mergeEnvironment(env, map[string]string{fixturesEnv: override})

		result := callMobilityAlerts(t, folder, env, "C3")
		alerts := objectList(t, dataObject(t, result), "alerts")
		if len(alerts) != 1 || alerts[0]["id"] != "al2" || alerts[0]["severity"] != "minor" {
			t.Fatalf("unexpected C3 fallback alerts: %#v", alerts)
		}
	})

	t.Run("missing alert sources report unavailable", func(t *testing.T) {
		folder, env := alertFallbackEnvironment(t, "alert-fallback-both-missing")
		override := fixtureOverride(t, folder)
		removeFixture(t, filepath.Join(override, "siri", "situation_exchange.json"))
		removeFixture(t, filepath.Join(override, "datagrandlyon", "tcl_alerts.json"))
		env = mergeEnvironment(env, map[string]string{fixturesEnv: override})

		result := callMobilityAlerts(t, folder, env, "A")
		assertWarningSource(t, result, "SOURCE_UNAVAILABLE", "tcl_alerts")
		if len(objectList(t, dataObject(t, result), "alerts")) != 0 {
			t.Fatalf("missing sources returned alerts: %#v", result)
		}
	})

	t.Run("malformed alert sources report unavailable", func(t *testing.T) {
		folder, env := alertFallbackEnvironment(t, "alert-fallback-both-malformed")
		override := fixtureOverride(t, folder)
		writeFixture(t, filepath.Join(override, "siri", "situation_exchange.json"), []byte("{"))
		writeFixture(t, filepath.Join(override, "datagrandlyon", "tcl_alerts.json"), []byte("{"))
		env = mergeEnvironment(env, map[string]string{fixturesEnv: override})

		result := callMobilityAlerts(t, folder, env, "A")
		assertWarningSource(t, result, "SOURCE_UNAVAILABLE", "tcl_alerts")
		if len(objectList(t, dataObject(t, result), "alerts")) != 0 {
			t.Fatalf("malformed sources returned alerts: %#v", result)
		}
	})
}

func alertFallbackEnvironment(t *testing.T, name string) (string, []string) {
	t.Helper()
	folder, env, _, err := testHarness.newEnvironment(name)
	if err != nil {
		t.Fatal(err)
	}
	return folder, env
}

func fixtureOverride(t *testing.T, folder string) string {
	t.Helper()
	override := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, override); err != nil {
		t.Fatal(err)
	}
	return override
}

func callMobilityAlerts(t *testing.T, folder string, env []string, line string) map[string]any {
	t.Helper()
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	result := client.callTool("lyon_mobility_status", map[string]any{
		"lines": []string{line}, "include": []string{"transit"},
	})
	client.close(true)
	return result
}

func duplicateFirstAlert(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var alerts []map[string]any
	if err := json.Unmarshal(data, &alerts); err != nil {
		t.Fatal(err)
	}
	if len(alerts) == 0 {
		t.Fatal("DataPusher alert fixture is empty")
	}
	alerts = append(alerts, alerts[0])
	data, err = json.Marshal(alerts)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, path, data)
}

func removeFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func stringSliceValue(t *testing.T, object map[string]any, key string) []string {
	t.Helper()
	values, ok := object[key].([]any)
	if !ok {
		t.Fatalf("%s is not a list: %#v", key, object[key])
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("%s contains a non-string value: %#v", key, value)
		}
		result = append(result, text)
	}
	return result
}
