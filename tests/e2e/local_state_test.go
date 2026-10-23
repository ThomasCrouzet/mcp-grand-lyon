package e2e_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestLocalStateResilience(t *testing.T) {
	runCase(t, "local-state-corrupt-json", testLocalStateCorruptJSON)
	runCase(t, "local-state-write-failures", testLocalStateWriteFailures)
	runCase(t, "local-state-profile-validation", testLocalStateProfileValidation)
}

func testLocalStateCorruptJSON(t *testing.T) {
	folder, env, values, err := preparedEnvironment("local-state-corrupt-json")
	if err != nil {
		t.Fatal(err)
	}
	writeLocalStateProfiles(t, values[configEnv])
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	database := openAuditDatabase(t, values[databaseEnv])

	execAuditSQL(t, database, `UPDATE commute_profiles SET config_json='[broken' WHERE profile_key='healthy_route'`)
	commute := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "healthy", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	assertStatus(t, commute, "partial")
	assertWarningSource(t, commute, "PARTIAL_RESULT", "profiles")
	assertFactObject(t, commute, "mobility")
	assertProcessRemainsHealthy(t, client)

	execAuditSQL(t, database, `UPDATE briefing_profiles SET config_json='{"commute":' WHERE profile_key='corrupt_profile'`)
	profile := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "corrupt_profile", "compare_with_previous": false,
	})
	assertStatus(t, profile, "unavailable")
	assertWarningSource(t, profile, "SOURCE_UNAVAILABLE", "profiles")
	assertProcessRemainsHealthy(t, client)

	execAuditSQL(t, database, `UPDATE commute_profiles SET config_json='{"origin":"dock_area","destination":"south_area","minimum_velov_bikes":0}' WHERE profile_key='healthy_route'`)
	execAuditSQL(t, database, `INSERT INTO briefing_snapshots(profile_key,captured_at,payload_json) VALUES ('healthy','2026-07-20T07:00:00Z','{"facts":')`)
	snapshot := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "healthy", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": true,
	})
	assertStatus(t, snapshot, "partial")
	assertWarningSource(t, snapshot, "PARTIAL_RESULT", "briefing_history")
	assertFactObject(t, snapshot, "velov")
	assertFactObject(t, snapshot, "mobility")
	assertProcessRemainsHealthy(t, client)
	client.close(true)
}

func testLocalStateWriteFailures(t *testing.T) {
	folder, env, values, err := preparedEnvironment("local-state-write-failures")
	if err != nil {
		t.Fatal(err)
	}
	writeLocalStateProfiles(t, values[configEnv])
	override := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, override); err != nil {
		t.Fatal(err)
	}
	velovFixture := `{"values":[{"number":"1002","name":"Bellecour / République","lat":45.7578,"lng":4.8325,"available_bikes":3,"available_bike_stands":12,"bike_stands":20,"status":"OPEN","observed_at":"2099-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(override, "datagrandlyon", "velov.json"), []byte(velovFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{fixturesEnv: override})
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	database := openAuditDatabase(t, values[databaseEnv])

	execAuditSQL(t, database, `CREATE TRIGGER reject_briefing_snapshot BEFORE INSERT ON briefing_snapshots BEGIN SELECT RAISE(ABORT,'audit snapshot rejection'); END`)
	execAuditSQL(t, database, `CREATE TRIGGER reject_velov_snapshot BEFORE INSERT ON velov_snapshots BEGIN SELECT RAISE(ABORT,'audit Velov rejection'); END`)
	result := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "healthy", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	assertStatus(t, result, "partial")
	assertWarningSource(t, result, "PARTIAL_RESULT", "briefing_history")
	assertWarningSource(t, result, "PARTIAL_RESULT", "velov_history")
	assertFactObject(t, result, "velov")
	assertFactObject(t, result, "mobility")
	assertProcessRemainsHealthy(t, client)
	client.close(true)
}

func testLocalStateProfileValidation(t *testing.T) {
	folder, env, values, err := preparedEnvironment("local-state-profile-validation")
	if err != nil {
		t.Fatal(err)
	}
	writeLocalStateProfiles(t, values[configEnv])
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	withoutCommute := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "without_commute", "compare_with_previous": false,
	})
	assertStatus(t, withoutCommute, "partial")
	assertLocalStateWarning(t, withoutCommute, "profiles", "commute")
	assertFactObject(t, withoutCommute, "mobility")
	assertProcessRemainsHealthy(t, client)

	unknownOrigin := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "unknown_origin", "compare_with_previous": false,
	})
	assertStatus(t, unknownOrigin, "partial")
	assertLocalStateWarning(t, unknownOrigin, "profiles", "missing_place")
	assertFactObject(t, unknownOrigin, "mobility")
	assertProcessRemainsHealthy(t, client)

	missingOrigin := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "missing_origin", "compare_with_previous": false,
	})
	assertStatus(t, missingOrigin, "partial")
	assertLocalStateWarning(t, missingOrigin, "profiles", "origin")
	assertFactObject(t, missingOrigin, "mobility")
	assertProcessRemainsHealthy(t, client)

	invalidCoordinates := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "invalid_coordinates", "compare_with_previous": false,
	})
	assertStatus(t, invalidCoordinates, "partial")
	assertLocalStateWarning(t, invalidCoordinates, "profiles", "invalid coordinates")
	assertFactObject(t, invalidCoordinates, "mobility")
	assertProcessRemainsHealthy(t, client)
	client.close(true)
}

func writeLocalStateProfiles(t *testing.T, configDir string) {
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
  invalid_place:
    label: Invalid coordinates
    latitude: 999
    longitude: 4.8325
commutes:
  healthy_route:
    origin: dock_area
    destination: south_area
    minimum_velov_bikes: 0
  unknown_route:
    origin: missing_place
    destination: south_area
  missing_origin_route:
    destination: south_area
  invalid_route:
    origin: invalid_place
    destination: south_area
briefings:
  healthy:
    commute: healthy_route
    include: [velov, traffic]
  corrupt_profile:
    commute: healthy_route
    include: [velov, traffic]
  without_commute:
    include: [velov, traffic]
  unknown_origin:
    commute: unknown_route
    include: [velov, traffic]
  missing_origin:
    commute: missing_origin_route
    include: [velov, traffic]
  invalid_coordinates:
    commute: invalid_route
    include: [velov, traffic]
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profiles), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openAuditDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	return database
}

func execAuditSQL(t *testing.T, database *sql.DB, statement string) {
	t.Helper()
	if _, err := database.Exec(statement); err != nil {
		t.Fatalf("execute audit SQL: %v", err)
	}
}

func assertFactObject(t *testing.T, envelope map[string]any, key string) {
	t.Helper()
	facts := objectValue(t, dataObject(t, envelope), "facts")
	if _, ok := facts[key].(map[string]any); !ok {
		t.Fatalf("briefing fact %q is absent: %#v", key, facts)
	}
}

func assertProcessRemainsHealthy(t *testing.T, client *mcpProcess) {
	t.Helper()
	result := client.callTool("lyon_mobility_status", map[string]any{"include": []string{"traffic"}})
	assertStatus(t, result, "partial")
	assertOfflineFixture(t, result)
}

func assertLocalStateWarning(t *testing.T, envelope map[string]any, source, fragment string) {
	t.Helper()
	for _, warning := range objectList(t, envelope, "warnings") {
		if warning["source"] == source && strings.Contains(strings.ToLower(stringValue(warning["message"])), strings.ToLower(fragment)) {
			return
		}
	}
	t.Fatalf("warning from %s does not identify %q: %#v", source, fragment, envelope["warnings"])
}
