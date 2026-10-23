package e2e_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSourceRegistryPersistsResolutionAcrossProcesses(t *testing.T) {
	name := fmt.Sprintf("source-registry-%d", time.Now().UnixNano())
	folder, env, values, err := testHarness.newEnvironment(name)
	if err != nil {
		t.Fatal(err)
	}
	runDoctorProcess(t, folder, env)

	database, err := sql.Open("sqlite", values[databaseEnv])
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	result, err := database.Exec(`UPDATE source_registry
		SET status='OK', table_name='resolved.runtime_table'
		WHERE source_id='tcl_departures'`)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		t.Fatalf("resolved source rows = %d, %v", count, err)
	}

	status := sourceStatus(t, runDoctorProcess(t, folder, env), "tcl_departures")
	if status["status"] != "OK" || status["table_name"] != "resolved.runtime_table" {
		t.Fatalf("runtime resolution was not preserved: %#v", status)
	}

	configuration := []byte(`version: 1
sources:
  tcl_departures:
    provider: datapusher
    enabled: false
    required: true
    service: rdata
    parser: tcl_departures_v1
    realtime: true
    ttl_seconds: 30
    maximum_stale_seconds: 180
    authentication: basic
    attribution: SYTRAL Mobilités
    license: discover
`)
	if err := os.MkdirAll(values[configEnv], 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(values[configEnv], "sources.yaml"), configuration, 0o600); err != nil {
		t.Fatal(err)
	}
	status = sourceStatus(t, runDoctorProcess(t, folder, env), "tcl_departures")
	if status["enabled"] != false || status["status"] != "DISABLED" || status["table_name"] != nil {
		t.Fatalf("explicit configuration change was not applied: %#v", status)
	}
}

func runDoctorProcess(t *testing.T, folder string, env []string) map[string]any {
	t.Helper()
	result, err := testHarness.runCommand(folder, env, 30*time.Second, testHarness.binary, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Stdout, &output); err != nil {
		t.Fatal(err)
	}
	return output
}

func sourceStatus(t *testing.T, doctor map[string]any, sourceID string) map[string]any {
	t.Helper()
	sources, ok := doctor["sources"].([]any)
	if !ok {
		t.Fatalf("doctor sources = %#v", doctor["sources"])
	}
	for _, value := range sources {
		source, ok := value.(map[string]any)
		if ok && source["source_id"] == sourceID {
			return source
		}
	}
	t.Fatalf("source %q not found in doctor output", sourceID)
	return nil
}
