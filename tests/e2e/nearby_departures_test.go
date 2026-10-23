package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeparturesFromNearbyPlace(t *testing.T) {
	runCase(t, "departures-nearby-place", func(t *testing.T) {
		folder, env, values, err := preparedEnvironment("departures-nearby-place")
		if err != nil {
			t.Fatal(err)
		}
		archive := filepath.Join(folder, "nearby-stops.zip")
		if err := makeZip(archive, filepath.Join(testHarness.fixtures, "gtfs", "dated"), func(name string, data []byte) []byte {
			if name == "stops.txt" {
				return append(data, []byte("OTHER,Other nearby stop,45.7581,4.8329\nOUTSIDE,Distant stop,45.77,4.88\n")...)
			}
			return data
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := testHarness.runCommand(folder, env, 30*time.Second, testHarness.binary, "sync", "gtfs", "--from-file", archive); err != nil {
			t.Fatal(err)
		}
		profiles := "version: 1\nplaces:\n  nearby:\n    label: Near Bellecour\n    latitude: 45.7578\n    longitude: 4.8320\n"
		if err := os.WriteFile(filepath.Join(values[configEnv], "profiles.yaml"), []byte(profiles), 0o600); err != nil {
			t.Fatal(err)
		}
		client := startMCP(t, folder, env)
		initializeMCP(t, client)
		for _, ref := range []map[string]any{
			{"latitude": 45.7578, "longitude": 4.8320},
			{"profile_place": "nearby"},
		} {
			result := client.callTool("lyon_next_departures", map[string]any{
				"stop": ref, "line": "A", "at": "2026-07-20T08:00:00+02:00", "limit": 3,
			})
			data := dataObject(t, result)
			stop, _ := data["stop"].(map[string]any)
			if stop["id"] != "gtfs:stop:BEL1" {
				t.Errorf("reference %v resolved to %v, want the nearby imported stop", ref, stop)
			}
			items := objectList(t, data, "departures")
			if len(items) != 1 || items[0]["expected_at"] != "2026-07-20T08:05:00+02:00" {
				t.Errorf("reference %v returned %v, want the Bellecour departure", ref, items)
			}
		}
		far := client.callTool("lyon_next_departures", map[string]any{
			"stop": map[string]any{"latitude": 48.8566, "longitude": 2.3522},
			"at":   "2026-07-20T08:00:00+02:00",
		})
		assertStatus(t, far, "not_found")
		if items := objectList(t, dataObject(t, far), "departures"); len(items) != 0 {
			t.Fatalf("distant coordinates returned Lyon departures: %v", items)
		}
		client.close(true)
	})
}
