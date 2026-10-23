package e2e_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStationDeparturesProcess(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("station-departures")
	if err != nil {
		t.Fatal(err)
	}
	feed := filepath.Join(folder, "feed")
	if err := os.MkdirAll(feed, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"agency.txt": "agency_id,agency_name\nTCL,TCL\n",
		"routes.txt": "route_id,agency_id,route_short_name,route_type\nA,TCL,A,1\n",
		"stops.txt": "stop_id,stop_name,stop_lat,stop_lon,location_type,parent_station\n" +
			"PARENT,Bellecour,45.7578,4.8320,1,CYCLE\n" +
			"PLATFORM_A,Bellecour quai A,45.7578,4.8321,0,PARENT\n" +
			"PLATFORM_B,Bellecour quai B,45.7579,4.8320,0,PARENT\n" +
			"CYCLE,Bellecour cycle,45.7580,4.8320,0,PARENT\n" +
			"OTHER,Autre gare,45.7600,4.8400,1,PARENT\n" +
			"OTHER_PLATFORM,Autre quai,45.7600,4.8401,0,OTHER\n",
		"trips.txt": "route_id,service_id,trip_id,trip_headsign\n" +
			"A,WEEK,T_A,Vaulx-en-Velin\nA,WEEK,T_B,Perrache\n" +
			"A,WEEK,T_C,Vaulx-en-Velin\nA,WEEK,T_OTHER,Vaulx-en-Velin\n",
		"stop_times.txt": "trip_id,arrival_time,departure_time,stop_id,stop_sequence\n" +
			"T_OTHER,08:04:00,08:04:00,OTHER_PLATFORM,1\n" +
			"T_A,08:05:00,08:05:00,PLATFORM_A,1\n" +
			"T_B,08:06:00,08:06:00,PLATFORM_B,1\n" +
			"T_C,08:08:00,08:08:00,CYCLE,1\n",
		"calendar.txt": "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n" +
			"WEEK,1,0,0,0,0,0,0,20260720,20260720\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(feed, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(folder, "station-feed.zip")
	if err := makeZip(archive, feed, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--from-file", archive); err != nil {
		t.Fatal(err)
	}

	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	assertDepartureTimes(t, client, "gtfs:stop:PARENT", "Vaulx", []string{
		"2026-07-20T08:05:00+02:00", "2026-07-20T08:08:00+02:00",
	})
	assertDepartureTimes(t, client, "gtfs:stop:PARENT", "Perrache", []string{
		"2026-07-20T08:06:00+02:00",
	})
	assertDepartureTimes(t, client, "gtfs:stop:PLATFORM_A", "Vaulx", []string{
		"2026-07-20T08:05:00+02:00",
	})
	client.close(true)
}

func assertDepartureTimes(t *testing.T, client *mcpProcess, stopID, direction string, want []string) {
	t.Helper()
	result := client.callTool("lyon_next_departures", map[string]any{
		"stop": map[string]any{"place_id": stopID}, "line": "A",
		"direction": direction, "at": "2026-07-20T08:00:00+02:00", "limit": 10,
	})
	items := objectList(t, dataObject(t, result), "departures")
	got := stringFields(t, items, "expected_at")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("departures for %s %s = %v, want %v", stopID, direction, got, want)
	}
}
