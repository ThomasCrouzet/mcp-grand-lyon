package e2e_test

import (
	"strings"
	"testing"
)

func TestModeSeparationAcceptance(t *testing.T) {
	runCase(t, "mode-live-missing-credentials", testLiveMissingCredentialsDoesNotUseFixtures)
	runCase(t, "mode-live-keeps-gtfs", testLiveMissingCredentialsKeepsGTFS)
}

func testLiveMissingCredentialsDoesNotUseFixtures(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("mode-live-missing-credentials")
	if err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{offlineEnv: "false"})
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	result := client.callTool("lyon_next_departures", map[string]any{
		"stop": map[string]any{"latitude": 45.7578, "longitude": 4.8320},
		"at":   "2026-07-20T08:00:00+02:00", "limit": 3,
	})
	assertStatus(t, result, "unavailable")
	for _, warning := range objectList(t, result, "warnings") {
		if warning["code"] == "OFFLINE_FIXTURE" {
			t.Fatalf("live mode fell back to demonstration fixtures: %#v", result)
		}
	}
	for _, source := range objectList(t, result, "sources") {
		if source["provider"] == "Fixtures" {
			t.Fatalf("live mode exposed fixture provenance: %#v", result)
		}
	}
	client.close(true)
}

func testLiveMissingCredentialsKeepsGTFS(t *testing.T) {
	folder, env, _, err := preparedEnvironment("mode-live-keeps-gtfs")
	if err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{offlineEnv: "false"})
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	result := client.callTool("lyon_next_departures", map[string]any{
		"stop": map[string]any{"place_id": "gtfs:stop:PD1"}, "line": "A",
		"at": "2026-07-20T08:00:00+02:00", "limit": 3,
	})
	assertStatus(t, result, "partial")
	items := objectList(t, dataObject(t, result), "departures")
	if len(items) == 0 {
		t.Fatalf("imported GTFS was not used without credentials: %#v", result)
	}
	for _, item := range items {
		if item["realtime"] != false {
			t.Fatalf("GTFS fallback claimed realtime: %#v", item)
		}
	}
	hasGTFS := false
	for _, source := range objectList(t, result, "sources") {
		if strings.HasPrefix(stringValue(source["provider"]), "GTFS") {
			hasGTFS = true
		}
		if source["provider"] == "Fixtures" {
			t.Fatalf("live mode exposed fixture provenance: %#v", result)
		}
	}
	if !hasGTFS {
		t.Fatalf("GTFS provenance is absent: %#v", result["sources"])
	}
	client.close(true)
}
