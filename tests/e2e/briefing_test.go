package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBriefingCommuteAcceptance(t *testing.T) {
	runCase(t, "briefing-configured-commutes", testBriefingConfiguredCommutes)
	runCase(t, "briefing-missing-provider-sections", testBriefingMissingSections)
	runCase(t, "briefing-change-baseline", testBriefingChangeBaseline)
}

func testBriefingConfiguredCommutes(t *testing.T) {
	folder, env, values, err := preparedEnvironment("briefing-configured")
	if err != nil {
		t.Fatal(err)
	}
	profiles := `version: 1
places:
  north_home:
    label: North home
    latitude: 45.7700
    longitude: 4.8500
  south_work:
    label: South work
    latitude: 45.7500
    longitude: 4.8300
  east_work:
    label: East work
    latitude: 45.7700
    longitude: 4.8800
commutes:
  line_a:
    origin: north_home
    destination: south_work
    preferred_modes: [tcl, velov]
    preferred_lines: [A]
    max_walking_m: 3000
    minimum_velov_bikes: 1
    minimum_velov_docks: 1
  line_c3:
    origin: north_home
    destination: east_work
    preferred_modes: [tcl, velov]
    preferred_lines: [C3]
    max_walking_m: 3000
    minimum_velov_bikes: 1
    minimum_velov_docks: 1
briefings:
  line_a_morning:
    commute: line_a
    include: [transit, velov, traffic, environment]
  line_c3_morning:
    commute: line_c3
    include: [transit, velov, traffic, environment]
`
	if err := os.WriteFile(filepath.Join(values[configEnv], "profiles.yaml"), []byte(profiles), 0o600); err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	lineA := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "line_a_morning", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	lineC3 := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "line_c3_morning", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	factsA := objectValue(t, dataObject(t, lineA), "facts")
	factsC3 := objectValue(t, dataObject(t, lineC3), "facts")
	if len(objectList(t, objectValue(t, factsA, "journey"), "options")) == 0 {
		t.Fatalf("configured journey is absent: %#v", factsA)
	}
	if objectValue(t, factsA, "journey")["recommended_mode"] == nil {
		t.Fatalf("configured journey has no recommendation: %#v", factsA["journey"])
	}
	if objectValue(t, factsA, "environment")["indicators"] == nil {
		t.Fatalf("requested environment data is absent: %#v", factsA)
	}
	if objectValue(t, factsC3, "environment")["indicators"] == nil {
		t.Fatalf("requested environment data is absent for C3: %#v", factsC3)
	}
	departuresA := objectList(t, objectValue(t, factsA, "departures"), "departures")
	departuresC3 := objectList(t, objectValue(t, factsC3, "departures"), "departures")
	for _, departure := range departuresA {
		if departure["line_name"] != "A" {
			t.Fatalf("preferred line A leaked %v", departure["line_name"])
		}
	}
	for _, departure := range departuresC3 {
		if departure["line_name"] != "C3" {
			t.Fatalf("preferred line C3 leaked %v", departure["line_name"])
		}
	}
	mobilityA := objectValue(t, factsA, "mobility")
	mobilityC3 := objectValue(t, factsC3, "mobility")
	if !containsFieldValue(objectList(t, mobilityA, "alerts"), "id", "SX-1") {
		t.Fatalf("line A disruption is absent: %#v", mobilityA)
	}
	if containsFieldValue(objectList(t, mobilityC3, "alerts"), "id", "SX-1") {
		t.Fatalf("unrelated line A disruption appeared for C3: %#v", mobilityC3)
	}
	lineAWalking := journeyWalking(factsA)
	lineC3Walking := journeyWalking(factsC3)
	if lineAWalking == lineC3Walking {
		t.Fatalf("changing the destination did not change journey facts: %d", lineAWalking)
	}
	if len(objectList(t, objectValue(t, factsA, "journey"), "options")) == 0 {
		t.Fatalf("configured journey options are absent: %#v", factsA)
	}
	client.close(true)
}

func testBriefingMissingSections(t *testing.T) {
	folder, env, values, err := preparedEnvironment("briefing-missing-sections")
	if err != nil {
		t.Fatal(err)
	}
	profiles := `version: 1
places:
  home:
    label: Home
    latitude: 45.7700
    longitude: 4.8500
commutes:
  missing_destination:
    origin: home
    preferred_modes: [walk]
briefings:
  unknown_section:
    commute: missing_destination
    include: [transit, unsupported_section]
`
	if err := os.WriteFile(filepath.Join(values[configEnv], "profiles.yaml"), []byte(profiles), 0o600); err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	result := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "unknown_section", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	assertStatus(t, result, "partial")
	assertWarningSource(t, result, "PARTIAL_RESULT", "profiles")
	assertWarningSource(t, result, "UNSUPPORTED_SECTION", "briefing")
	facts := objectValue(t, dataObject(t, result), "facts")
	if _, ok := facts["mobility"].(map[string]any); !ok {
		t.Fatalf("missing destination removed unrelated sections: %#v", facts)
	}
	client.close(true)
}

func testBriefingChangeBaseline(t *testing.T) {
	folder, env, values, err := preparedEnvironment("briefing-change-baseline")
	if err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, override); err != nil {
		t.Fatal(err)
	}
	parking := func(available int) map[string]any {
		return map[string]any{"values": []map[string]any{
			{"id": "pr1", "name": "P+R One", "type": "parc relais", "lat": 45.7650, "lon": 4.9100, "capacity": 100, "available": available},
			{"id": "pr2", "name": "P+R Two", "type": "parc relais", "lat": 45.7651, "lon": 4.9101, "capacity": 100, "available": 5},
		}}
	}
	writeParking := func(available int) {
		data, err := json.Marshal(parking(available))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(override, "datagrandlyon", "parkings.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeParking(120)
	profiles := `version: 1
places:
  home:
    label: Home
    latitude: 45.7700
    longitude: 4.8500
  park_work:
    label: Park work
    latitude: 45.7650
    longitude: 4.9100
commutes:
  park_commute:
    origin: home
    destination: park_work
    preferred_modes: [park_and_ride]
    max_walking_m: 3000
briefings:
  park_morning:
    commute: park_commute
    include: [traffic]
  fresh_profile:
    commute: park_commute
    include: [traffic]
`
	if err := os.WriteFile(filepath.Join(values[configEnv], "profiles.yaml"), []byte(profiles), 0o600); err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, mergeEnvironment(env, map[string]string{fixturesEnv: override}))
	initializeMCP(t, client)
	first := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "park_morning", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	if dataObject(t, first)["changes_baseline"] != "not_requested" {
		t.Fatalf("first briefing baseline = %#v", first)
	}
	noBaseline := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "fresh_profile", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": true,
	})
	assertStatus(t, noBaseline, "partial")
	assertWarningSource(t, noBaseline, "NO_BASELINE", "briefing_history")
	if dataObject(t, noBaseline)["changes_baseline"] != "none" {
		t.Fatalf("no-baseline result = %#v", noBaseline)
	}

	second := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "park_morning", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": true,
	})
	assertStatus(t, second, "partial")
	if dataObject(t, second)["changes_baseline"] != "previous" {
		t.Fatalf("second briefing baseline = %#v", second)
	}
	changes := objectList(t, dataObject(t, second), "changes")
	if len(changes) != 0 {
		t.Fatalf("identical facts produced changes: %#v", changes)
	}

	writeParking(0)
	third := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "park_morning", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": true,
	})
	changes = objectList(t, dataObject(t, third), "changes")
	if len(changes) == 0 {
		t.Fatalf("parking transition produced no changes: %#v", third)
	}
	parkingChange := indexByField(changes, "key")["journey:park_and_ride"]
	if parkingChange == nil || parkingChange["kind"] != "updated" ||
		!strings.Contains(stringValue(parkingChange["message"]), "120 spaces") ||
		!strings.Contains(stringValue(parkingChange["message"]), "5 spaces") {
		t.Fatalf("parking change = %#v", parkingChange)
	}
	client.close(true)
}

func journeyWalking(facts map[string]any) int {
	journey, _ := facts["journey"].(map[string]any)
	options, _ := journey["options"].([]any)
	for _, raw := range options {
		option, _ := raw.(map[string]any)
		if walking, ok := option["walking_distance_m"].(float64); ok {
			return int(walking)
		}
	}
	return 0
}
