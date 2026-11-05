package e2e_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestGTFSCoverageDiagnostics(t *testing.T) {
	runCase(t, "gtfs-coverage-missing", testGTFSCoverageMissing)
	runCase(t, "gtfs-coverage-current", testGTFSCoverageCurrent)
	runCase(t, "gtfs-coverage-expired", testGTFSCoverageExpired)
	runCase(t, "gtfs-coverage-future", testGTFSCoverageFuture)
	runCase(t, "gtfs-coverage-no-service", testGTFSCoverageNoService)
}

func testGTFSCoverageMissing(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("gtfs-coverage-missing")
	if err != nil {
		t.Fatal(err)
	}
	coverage := doctorGTFS(t, folder, env)
	if coverage["status"] != "missing" || coverage["imported"] != false {
		t.Fatalf("missing coverage = %#v", coverage)
	}
}

func testGTFSCoverageCurrent(t *testing.T) {
	folder, env, _, err := preparedEnvironment("gtfs-coverage-current")
	if err != nil {
		t.Fatal(err)
	}
	coverage := doctorGTFS(t, folder, env)
	if coverage["status"] != "current" || coverage["imported"] != true || coverage["active_service_count"] == float64(0) {
		t.Fatalf("current coverage = %#v", coverage)
	}
	if coverage["realtime"] != false {
		t.Fatalf("GTFS coverage claimed realtime: %#v", coverage)
	}
}

func testGTFSCoverageExpired(t *testing.T) {
	coverage := coverageForCalendar(t, "gtfs-coverage-expired", "20200101", "20210101", "")
	if coverage["status"] != "expired" {
		t.Fatalf("expired coverage = %#v", coverage)
	}
}

func testGTFSCoverageFuture(t *testing.T) {
	coverage := coverageForCalendar(t, "gtfs-coverage-future", "20300101", "20310101", "")
	if coverage["status"] != "future_only" {
		t.Fatalf("future coverage = %#v", coverage)
	}
}

func testGTFSCoverageNoService(t *testing.T) {
	today := time.Now().In(time.Local).Format("20060102")
	coverage := coverageForCalendar(t, "gtfs-coverage-no-service", "20200101", "20401231", today)
	if coverage["status"] != "covered_no_service" {
		t.Fatalf("no-service coverage = %#v", coverage)
	}
}

func coverageForCalendar(t *testing.T, name, start, end, removedDate string) map[string]any {
	t.Helper()
	folder, env, _, err := testHarness.newEnvironment(name)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(folder, "coverage.zip")
	transform := func(filename string, data []byte) []byte {
		switch filename {
		case "calendar.txt":
			return []byte("service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\nWD,1,1,1,1,1,1,1," + start + "," + end + "\n")
		case "calendar_dates.txt":
			content := "service_id,date,exception_type\n"
			if removedDate != "" {
				content += "WD," + removedDate + ",2\n"
			}
			return []byte(content)
		default:
			return data
		}
	}
	if err := makeZip(archive, filepath.Join(testHarness.fixtures, "gtfs", "dated"), transform); err != nil {
		t.Fatal(err)
	}
	if _, err := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--from-file", archive); err != nil {
		t.Fatal(err)
	}
	return doctorGTFS(t, folder, env)
}

func doctorGTFS(t *testing.T, folder string, env []string) map[string]any {
	t.Helper()
	result, err := testHarness.runCommand(folder, env, 30*time.Second, testHarness.binary, "doctor")
	if err != nil {
		// A missing credentials live run is not expected here; keep the report.
		t.Fatalf("doctor: %v\n%s", err, result.Stderr)
	}
	var report map[string]any
	if err := json.Unmarshal(result.Stdout, &report); err != nil {
		t.Fatalf("decode doctor: %v\n%s", err, result.Stdout)
	}
	coverage, ok := report["gtfs"].(map[string]any)
	if !ok {
		t.Fatalf("doctor has no GTFS coverage: %#v", report)
	}
	return coverage
}
