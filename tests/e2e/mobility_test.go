package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMobilityProcessAudit(t *testing.T) {
	runCase(t, "mobility-area-semantics", testMobilityAreaSemantics)
	runCase(t, "mobility-source-failures", testMobilitySourceFailures)
	runCase(t, "briefing-profile-and-change", testBriefingProfileAndChange)
	runCase(t, "velov-freshness", testVelovFreshness)
}

func testMobilityAreaSemantics(t *testing.T) {
	folder, env, values, err := preparedEnvironment("mobility-area-semantics")
	if err != nil {
		t.Fatal(err)
	}
	writeMobilityProfiles(t, values[configEnv])
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	result := client.callTool("lyon_mobility_status", map[string]any{
		"areas": []any{
			map[string]any{"latitude": 45.7600, "longitude": 4.8500},
			map[string]any{"profile_place": "south_area"},
			map[string]any{"query": "unresolvable audit area"},
		},
		"include": []string{"roadworks"},
	})
	assertStatus(t, result, "partial")
	assertOfflineFixture(t, result)
	events := objectList(t, dataObject(t, result), "road_events")
	if !containsFieldValue(events, "id", "r1") || !containsFieldValue(events, "id", "r2") {
		t.Fatalf("road events do not cover all resolved areas: %#v", events)
	}
	assertWarningMessage(t, result, "PLACE_NOT_FOUND", "unresolvable audit area")

	invalid := client.callTool("lyon_mobility_status", map[string]any{
		"include": []string{"weather"},
	})
	assertStatus(t, invalid, "invalid_request")
	if !strings.Contains(strings.ToLower(stringValue(invalid["summary"])), "weather") {
		t.Fatalf("invalid include is not identified: %#v", invalid)
	}
	client.close(true)
}

func testMobilitySourceFailures(t *testing.T) {
	folder, env, values, err := preparedEnvironment("mobility-source-failures")
	if err != nil {
		t.Fatal(err)
	}
	writeMobilityProfiles(t, values[configEnv])
	override := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, override); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(override, "datagrandlyon", "traffic.json"), []byte("{broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	velovPath := filepath.Join(override, "datagrandlyon", "velov.json")
	if err := os.Remove(velovPath); err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{fixturesEnv: override})
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	mobility := client.callTool("lyon_mobility_status", map[string]any{
		"include": []string{"traffic"},
	})
	assertStatus(t, mobility, "partial")
	assertWarningSource(t, mobility, "SOURCE_UNAVAILABLE", "traffic")

	briefing := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "audit_commute", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	assertWarningSource(t, briefing, "SOURCE_UNAVAILABLE", "velov_realtime")
	assertWarningSource(t, briefing, "SOURCE_UNAVAILABLE", "traffic")
	client.close(true)
}

func testBriefingProfileAndChange(t *testing.T) {
	folder, env, values, err := preparedEnvironment("briefing-profile-and-change")
	if err != nil {
		t.Fatal(err)
	}
	writeMobilityProfiles(t, values[configEnv])
	override := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, override); err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{fixturesEnv: override})
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	first := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "audit_commute", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	assertOfflineFixture(t, first)
	assertAuditBriefingFacts(t, first)

	unchanged := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "audit_commute", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": true,
	})
	if changes := objectList(t, dataObject(t, unchanged), "changes"); len(changes) != 0 {
		t.Fatalf("unchanged fixture facts produced changes: %#v", changes)
	}

	changedVelov := `{"values":[{"number":"1002","name":"Bellecour / République","lat":45.7578,"lng":4.8325,"available_bikes":1,"available_bike_stands":14,"bike_stands":20,"status":"OPEN"}]}`
	if err := os.WriteFile(filepath.Join(override, "datagrandlyon", "velov.json"), []byte(changedVelov), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(override, "datagrandlyon", "traffic.json"), []byte("[invalid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "audit_commute", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": true,
	})
	assertStatus(t, changed, "partial")
	assertWarningSource(t, changed, "SOURCE_UNAVAILABLE", "traffic")
	assertAuditBriefingFacts(t, changed)
	if !containsFieldValue(objectList(t, dataObject(t, changed), "changes"), "key", "velov") {
		t.Fatalf("changed Velov facts were not reported: %#v", dataObject(t, changed)["changes"])
	}
	client.close(true)
}

func testVelovFreshness(t *testing.T) {
	folder, env, values, err := preparedEnvironment("velov-freshness")
	if err != nil {
		t.Fatal(err)
	}
	writeMobilityProfiles(t, values[configEnv])
	override := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, override); err != nil {
		t.Fatal(err)
	}
	fixture := `{"values":[
		{"number":"old","name":"Old observation","lat":45.7578,"lng":4.8325,"available_bikes":4,"available_bike_stands":6,"bike_stands":10,"status":"OPEN","observed_at":"2000-01-01T00:00:00Z"},
		{"number":"unknown","name":"Unknown observation","lat":45.7580,"lng":4.8327,"available_bikes":5,"available_bike_stands":5,"bike_stands":10,"status":"OPEN","observed_at":null}
	]}`
	if err := os.WriteFile(filepath.Join(override, "datagrandlyon", "velov.json"), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{fixturesEnv: override})
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	result := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "audit_commute", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	assertWarningSource(t, result, "STALE_DATA", "velov_realtime")
	assertWarningSource(t, result, "PARTIAL_RESULT", "velov_realtime")
	client.close(true)
}

func writeMobilityProfiles(t *testing.T, configDir string) {
	t.Helper()
	profiles := `version: 1
places:
  dock_area:
    label: Bellecour dock
    latitude: 45.7578
    longitude: 4.8325
  south_area:
    label: South road area
    latitude: 45.7400
    longitude: 4.8300
commutes:
  audit_route:
    origin: dock_area
    destination: south_area
    minimum_velov_bikes: 0
briefings:
  audit_commute:
    commute: audit_route
    include:
      - velov
      - traffic
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profiles), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertAuditBriefingFacts(t *testing.T, envelope map[string]any) {
	t.Helper()
	facts := objectValue(t, dataObject(t, envelope), "facts")
	commute := objectValue(t, facts, "commute")
	if commute["origin"] != "dock_area" {
		t.Fatalf("custom commute origin is absent: %#v", commute)
	}
	velov := objectValue(t, facts, "velov")
	if !containsFieldValue(objectList(t, velov, "stations"), "id", "1002") {
		t.Fatalf("custom profile place did not select the Bellecour station: %#v", velov)
	}
}

func assertWarningMessage(t *testing.T, envelope map[string]any, code, fragment string) {
	t.Helper()
	for _, warning := range objectList(t, envelope, "warnings") {
		if warning["code"] == code && strings.Contains(strings.ToLower(stringValue(warning["message"])), strings.ToLower(fragment)) {
			return
		}
	}
	t.Fatalf("warning %s does not identify %q: %#v", code, fragment, envelope["warnings"])
}
