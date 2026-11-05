package e2e_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestGTFSMaintenanceAcceptance(t *testing.T) {
	runCase(t, "gtfs-maintenance-idempotent", testGTFSMaintenanceIdempotent)
	runCase(t, "gtfs-maintenance-rollback", testGTFSMaintenanceRollback)
	runCase(t, "gtfs-maintenance-coverage-exit", testGTFSMaintenanceCoverageExit)
	runCase(t, "gtfs-maintenance-offline", testGTFSMaintenanceOffline)
}

func testGTFSMaintenanceIdempotent(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("gtfs-maintenance-idempotent")
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(testHarness.fixtures, "gtfs", "mini_gtfs.zip")
	first := runMaintenance(t, folder, env, true, archive)
	if first["status"] != "OK" || first["unchanged"] != false {
		t.Fatalf("first maintenance = %#v", first)
	}
	second := runMaintenance(t, folder, env, true, archive)
	if second["status"] != "UNCHANGED" || second["unchanged"] != true {
		t.Fatalf("second maintenance = %#v", second)
	}
	coverage, _ := second["coverage"].(map[string]any)
	if coverage["status"] != "current" || coverage["realtime"] != false {
		t.Fatalf("maintenance coverage = %#v", coverage)
	}
}

func testGTFSMaintenanceRollback(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("gtfs-maintenance-rollback")
	if err != nil {
		t.Fatal(err)
	}
	valid := filepath.Join(testHarness.fixtures, "gtfs", "mini_gtfs.zip")
	runMaintenance(t, folder, env, true, valid)
	invalid := filepath.Join(folder, "invalid-maintenance.zip")
	transform := func(name string, data []byte) []byte {
		if name != "stop_times.txt" {
			return data
		}
		return []byte("trip_id,arrival_time,departure_time,stop_id,stop_sequence\nA1,not-a-time,not-a-time,BEL1,1\n")
	}
	if err := makeZip(invalid, filepath.Join(testHarness.fixtures, "gtfs", "dated"), transform); err != nil {
		t.Fatal(err)
	}
	result, runErr := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--maintain", "--from-file", invalid)
	if runErr == nil || result.ReturnCode == 0 {
		t.Fatal("invalid maintenance archive succeeded")
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	departures := client.callTool("lyon_next_departures", map[string]any{
		"stop": map[string]any{"place_id": "gtfs:stop:PD1"}, "line": "A",
		"at": "2026-07-20T08:00:00+02:00", "limit": 3,
	})
	if len(objectList(t, dataObject(t, departures), "departures")) == 0 {
		t.Fatalf("failed maintenance damaged the prior timetable: %#v", departures)
	}
	client.close(true)
}

func testGTFSMaintenanceCoverageExit(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("gtfs-maintenance-coverage")
	if err != nil {
		t.Fatal(err)
	}
	expired := filepath.Join(folder, "expired-maintenance.zip")
	transform := func(name string, data []byte) []byte {
		if name != "calendar.txt" {
			return data
		}
		return []byte("service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\nWD,1,1,1,1,1,1,1,20200101,20210101\n")
	}
	if err := makeZip(expired, filepath.Join(testHarness.fixtures, "gtfs", "dated"), transform); err != nil {
		t.Fatal(err)
	}
	result, runErr := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--maintain", "--from-file", expired)
	if runErr == nil || result.ReturnCode != 3 {
		t.Fatalf("expired coverage exit = %d, %v", result.ReturnCode, runErr)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Stdout, &output); err != nil {
		t.Fatalf("decode maintenance output: %v\n%s", err, result.Stdout)
	}
	if output["status"] != "OK" {
		t.Fatalf("expired archive import = %#v", output)
	}
	coverage, _ := output["coverage"].(map[string]any)
	if coverage["status"] != "expired" {
		t.Fatalf("expired coverage = %#v", coverage)
	}
}

func testGTFSMaintenanceOffline(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("gtfs-maintenance-offline")
	if err != nil {
		t.Fatal(err)
	}
	result, err := testHarness.runCommand(folder, env, 30*time.Second,
		testHarness.binary, "sync", "gtfs", "--maintain")
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Stdout, &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "DISABLED" {
		t.Fatalf("offline maintenance = %#v", output)
	}
}

func runMaintenance(t *testing.T, folder string, env []string, expectSuccess bool, archive string) map[string]any {
	t.Helper()
	result, err := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--maintain", "--from-file", archive)
	if err != nil && expectSuccess {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Stdout, &output); err != nil {
		t.Fatalf("decode maintenance: %v\n%s", err, result.Stdout)
	}
	return output
}
