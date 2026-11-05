package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestHistoryLifecycleAcceptance(t *testing.T) {
	runCase(t, "history-deduplication", testHistoryDeduplication)
	runCase(t, "history-retention-and-delete", testHistoryRetentionAndDelete)
}

func testHistoryDeduplication(t *testing.T) {
	folder, env, values, err := preparedEnvironment("history-dedup")
	if err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, override); err != nil {
		t.Fatal(err)
	}
	station := `{"values":[{"number":"history-station","name":"History station","lat":45.76,"lng":4.85,"available_bikes":5,"available_bike_stands":5,"bike_stands":10,"status":"OPEN","observed_at":"2026-11-05T00:00:00+01:00"}]}`
	if err := os.WriteFile(filepath.Join(override, "datagrandlyon", "velov.json"), []byte(station), 0o644); err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{fixturesEnv: override})
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	for index := 0; index < 2; index++ {
		result := client.callTool("lyon_personal_briefing", map[string]any{
			"profile": "weekday_morning", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
		})
		assertStatus(t, result, "partial")
	}
	client.close(true)
	for index := 0; index < 2; index++ {
		if _, err := testHarness.runCommand(folder, env, 30*time.Second, testHarness.binary, "snapshot", "velov"); err != nil {
			t.Fatal(err)
		}
	}
	database := openAuditDatabase(t, values[databaseEnv])
	defer database.Close()
	var briefing, velov int
	if err := database.QueryRow(`SELECT COUNT(*) FROM briefing_snapshots WHERE profile_key='weekday_morning'`).Scan(&briefing); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM velov_snapshots WHERE station_id='history-station'`).Scan(&velov); err != nil {
		t.Fatal(err)
	}
	if briefing != 1 || velov != 1 {
		t.Fatalf("repeated observations were duplicated: briefing=%d velov=%d", briefing, velov)
	}
}

func testHistoryRetentionAndDelete(t *testing.T) {
	folder, env, values, err := preparedEnvironment("history-retention")
	if err != nil {
		t.Fatal(err)
	}
	database := openAuditDatabase(t, values[databaseEnv])
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -120).Format(time.RFC3339Nano)
	fresh := now.AddDate(0, 0, -1).Format(time.RFC3339Nano)
	writeRetentionSettings(t, values[configEnv], 0, 0, 0, 0)
	for _, profile := range []string{"keep", "other"} {
		for _, captured := range []string{old, fresh} {
			execAuditSQL(t, database, `INSERT INTO briefing_snapshots(profile_key,captured_at,payload_json) VALUES ('`+profile+`','`+captured+`','{"facts":{}}')`)
		}
	}
	for _, station := range []string{"station-a", "station-b"} {
		for _, observed := range []string{old, fresh} {
			execAuditSQL(t, database, `INSERT OR IGNORE INTO velov_snapshots(station_id,observed_at,bikes_available,docks_available,hour_slot) VALUES ('`+station+`','`+observed+`',1,1,'08')`)
		}
	}

	before := runHistoryJSON(t, folder, env, "info")
	if before["briefing_snapshots"] != float64(4) || before["velov_snapshots"] != float64(4) {
		t.Fatalf("initial history = %#v", before)
	}
	if before["db_size_bytes"] == nil || before["retention"] == nil {
		t.Fatalf("history info lacks storage diagnostics: %#v", before)
	}
	writeRetentionSettings(t, values[configEnv], 30, 1, 30, 2)
	if _, err := testHarness.runCommand(folder, env, 30*time.Second, testHarness.binary, "history", "prune"); err != nil {
		t.Fatal(err)
	}
	afterPrune := runHistoryJSON(t, folder, env, "info")
	if afterPrune["briefing_snapshots"] != float64(2) || afterPrune["velov_snapshots"] != float64(2) {
		t.Fatalf("prune did not remove expired rows: %#v", afterPrune)
	}

	deleted := runHistoryJSON(t, folder, env, "delete", "--profile", "keep", "--station", "station-a")
	if deleted["briefing_deleted"] != float64(1) || deleted["velov_deleted"] != float64(1) {
		t.Fatalf("scoped deletion = %#v", deleted)
	}
	afterDelete := runHistoryJSON(t, folder, env, "info")
	if afterDelete["briefing_snapshots"] != float64(1) || afterDelete["velov_snapshots"] != float64(1) {
		t.Fatalf("scoped deletion removed unrelated rows: %#v", afterDelete)
	}

	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	departures := client.callTool("lyon_next_departures", map[string]any{
		"stop": map[string]any{"place_id": "gtfs:stop:PD1"}, "line": "A",
		"at": "2026-07-20T08:00:00+02:00", "limit": 3,
	})
	if len(objectList(t, dataObject(t, departures), "departures")) == 0 {
		t.Fatalf("history deletion damaged the GTFS timetable: %#v", departures)
	}
	client.close(true)

	doctor, err := testHarness.runCommand(folder, env, 30*time.Second, testHarness.binary, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(doctor.Stdout, &report); err != nil {
		t.Fatal(err)
	}
	history := objectValue(t, report, "history")
	if history["briefing_snapshots"] != float64(1) || history["velov_snapshots"] != float64(1) || history["retention"] == nil {
		t.Fatalf("doctor history = %#v", history)
	}
}

func writeRetentionSettings(t *testing.T, configDir string, briefingAge, briefingCount, velovAge, velovCount int) {
	t.Helper()
	contents := "version: 1\nretention:\n" +
		"  briefing_max_age_days: " + strconv.Itoa(briefingAge) + "\n" +
		"  briefing_max_count: " + strconv.Itoa(briefingCount) + "\n" +
		"  velov_max_age_days: " + strconv.Itoa(velovAge) + "\n" +
		"  velov_max_count: " + strconv.Itoa(velovCount) + "\n"
	if err := os.WriteFile(filepath.Join(configDir, "settings.yaml"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runHistoryJSON(t *testing.T, folder string, env []string, args ...string) map[string]any {
	t.Helper()
	argv := append([]string{"history"}, args...)
	result, err := testHarness.runCommand(folder, env, 30*time.Second, append([]string{testHarness.binary}, argv...)...)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(result.Stdout, &value); err != nil {
		t.Fatalf("decode history %v: %v\n%s", args, err, result.Stdout)
	}
	return value
}
