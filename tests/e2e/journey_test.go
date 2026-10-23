package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGoJourneyAcceptance(t *testing.T) {
	runCase(t, "journey-park-and-ride", testJourneyParkAndRide)
	runCase(t, "journey-transitous-deadline", testJourneyTransitousDeadline)
	runCase(t, "journey-mode-exclusion", testJourneyModeExclusion)
	runCase(t, "journey-velov-constraints", testJourneyVelovConstraints)
}

func testJourneyParkAndRide(t *testing.T) {
	available := []map[string]any{
		{"id": "full", "name": "Full P+R", "type": "parc relais", "lat": 45.7601, "lon": 4.8500, "capacity": 100, "available": 0},
		{"id": "unknown", "name": "Unknown P+R", "type": "parc relais", "lat": 45.7602, "lon": 4.8500, "capacity": 100},
		{"id": "available", "name": "Available P+R", "type": "parc relais", "lat": 45.7603, "lon": 4.8500, "capacity": 100, "available": 12},
	}
	client := startJourneyMCP(t, "journey-park-available", map[string]any{"datagrandlyon/parkings.json": map[string]any{"values": available}}, nil)
	arguments := journeyArguments([]string{"park_and_ride"})
	result := client.callTool("lyon_trip_options", arguments)
	assertStatus(t, result, "partial")
	options := objectList(t, dataObject(t, result), "options")
	if len(options) != 1 || options[0]["mode"] != "park_and_ride" || options[0]["summary"] != "Park-and-ride at Available P+R" || options[0]["availability"] != "12 spaces" {
		t.Fatalf("available park-and-ride was not selected: %#v", options)
	}
	client.close(true)

	unknownValues := []map[string]any{
		{"id": "unknown", "name": "Unknown P+R", "type": "parc relais", "lat": 45.7601, "lon": 4.8500, "capacity": 100},
	}
	client = startJourneyMCP(t, "journey-park-unknown", map[string]any{"datagrandlyon/parkings.json": map[string]any{"values": unknownValues}}, nil)
	unknown := client.callTool("lyon_trip_options", arguments)
	assertStatus(t, unknown, "partial")
	assertWarningSource(t, unknown, "PARTIAL_RESULT", "parking_availability")
	unknownOptions := objectList(t, dataObject(t, unknown), "options")
	if len(unknownOptions) != 1 || unknownOptions[0]["availability"] != nil {
		t.Fatalf("unknown parking availability was changed: %#v", unknownOptions)
	}
	client.close(true)

	fullValues := []map[string]any{
		{"id": "full", "name": "Full P+R", "type": "parc relais", "lat": 45.7601, "lon": 4.8500, "capacity": 100, "available": 0},
	}
	client = startJourneyMCP(t, "journey-park-full", map[string]any{"datagrandlyon/parkings.json": map[string]any{"values": fullValues}}, nil)
	full := client.callTool("lyon_trip_options", arguments)
	assertStatus(t, full, "not_found")
	if options := objectList(t, dataObject(t, full), "options"); len(options) != 0 {
		t.Fatalf("full parking was retained: %#v", options)
	}

	missingArguments := mapsClone(arguments)
	missingArguments["destination"] = map[string]any{"query": "Unfindable journey stop"}
	missing := client.callTool("lyon_trip_options", missingArguments)
	assertStatus(t, missing, "not_found")
	if options := objectList(t, dataObject(t, missing), "options"); len(options) != 0 {
		t.Fatalf("unresolved destination produced options: %#v", options)
	}
	client.close(true)
}

func testJourneyTransitousDeadline(t *testing.T) {
	client := startJourneyMCP(t, "journey-transitous", nil, map[string]string{"TRANSITOUS_ENABLED": "true"})
	arguments := journeyArguments([]string{"tcl"})
	result := client.callTool("lyon_trip_options", arguments)
	assertStatus(t, result, "partial")
	assertWarningSource(t, result, "ROUTING_UNAVAILABLE", "journey_planner")
	options := objectList(t, dataObject(t, result), "options")
	if len(options) != 1 || options[0]["mode"] != "tcl" || options[0]["estimated_duration_seconds"] != nil || options[0]["arrival_at"] != nil {
		t.Fatalf("fixture routing fallback claimed timing: %#v", options)
	}

	withDeadline := mapsClone(arguments)
	withDeadline["arrival_before"] = "2026-07-20T09:00:00+02:00"
	deadline := client.callTool("lyon_trip_options", withDeadline)
	assertStatus(t, deadline, "not_found")
	assertWarningSource(t, deadline, "ROUTING_UNAVAILABLE", "journey_planner")
	if options := objectList(t, dataObject(t, deadline), "options"); len(options) != 0 {
		t.Fatalf("untimed routing fallback met a deadline: %#v", options)
	}
	client.close(true)
}

func testJourneyModeExclusion(t *testing.T) {
	overrides := map[string]any{
		"datagrandlyon/parkings.json": []any{"invalid-for-parking-provider"},
		"datagrandlyon/velov.json":    []any{"invalid-for-velov-provider"},
	}
	client := startJourneyMCP(t, "journey-mode-exclusion", overrides, map[string]string{"TRANSITOUS_ENABLED": "true"})
	result := client.callTool("lyon_trip_options", journeyArguments([]string{"walk", "car"}))
	assertStatus(t, result, "partial")
	if modes := optionModes(t, result); !reflect.DeepEqual(modes, []string{"walk", "car"}) && !reflect.DeepEqual(modes, []string{"car", "walk"}) {
		t.Fatalf("trip modes = %v, want walk and car", modes)
	}
	for _, warning := range objectList(t, result, "warnings") {
		if warning["source"] == "parking" || warning["source"] == "velov_realtime" || warning["source"] == "journey_planner" {
			t.Fatalf("excluded mode accessed its provider: %#v", warning)
		}
	}
	client.close(true)
}

func testJourneyVelovConstraints(t *testing.T) {
	stations := []map[string]any{
		{"number": "origin-empty", "name": "Origin empty", "lat": 45.7600, "lng": 4.8600, "available_bikes": 0, "available_bike_stands": 10, "bike_stands": 10, "status": "OPEN"},
		{"number": "origin-good", "name": "Origin good", "lat": 45.7605, "lng": 4.8600, "available_bikes": 8, "available_bike_stands": 2, "bike_stands": 10, "status": "OPEN"},
		{"number": "destination-full", "name": "Destination full", "lat": 45.7580, "lng": 4.8330, "available_bikes": 10, "available_bike_stands": 0, "bike_stands": 10, "status": "OPEN"},
		{"number": "destination-good", "name": "Destination good", "lat": 45.7585, "lng": 4.8330, "available_bikes": 1, "available_bike_stands": 9, "bike_stands": 10, "status": "OPEN"},
	}
	client := startJourneyMCP(t, "journey-velov", map[string]any{"datagrandlyon/velov.json": map[string]any{"values": stations}}, nil)
	base := journeyArguments([]string{"velov"})
	base["destination"] = map[string]any{"latitude": 45.7580, "longitude": 4.8330}
	base["preferences"] = map[string]any{
		"max_walking_m": 200, "minimum_velov_bikes": 3, "minimum_velov_docks": 3,
		"avoid_disruptions": false, "wheelchair": false,
	}
	result := client.callTool("lyon_trip_options", base)
	assertStatus(t, result, "partial")
	options := objectList(t, dataObject(t, result), "options")
	if len(options) != 1 || options[0]["mode"] != "velov" || options[0]["availability"] != "8 bikes / 9 docks" {
		t.Fatalf("Velov availability constraints were not applied: %#v", options)
	}

	noBikes := mapsClone(base)
	noBikes["preferences"] = map[string]any{
		"max_walking_m": 200, "minimum_velov_bikes": 9, "minimum_velov_docks": 3,
		"avoid_disruptions": false, "wheelchair": false,
	}
	assertNoVelovOption(t, client.callTool("lyon_trip_options", noBikes))

	noDocks := mapsClone(base)
	noDocks["preferences"] = map[string]any{
		"max_walking_m": 200, "minimum_velov_bikes": 3, "minimum_velov_docks": 10,
		"avoid_disruptions": false, "wheelchair": false,
	}
	assertNoVelovOption(t, client.callTool("lyon_trip_options", noDocks))

	totalWalking := mapsClone(base)
	totalWalking["preferences"] = map[string]any{
		"max_walking_m": 80, "minimum_velov_bikes": 3, "minimum_velov_docks": 3,
		"avoid_disruptions": false, "wheelchair": false,
	}
	assertNoVelovOption(t, client.callTool("lyon_trip_options", totalWalking))

	zeroWalking := mapsClone(base)
	zeroWalking["preferences"] = map[string]any{
		"max_walking_m": 0, "minimum_velov_bikes": 3, "minimum_velov_docks": 3,
		"avoid_disruptions": false, "wheelchair": false,
	}
	assertNoVelovOption(t, client.callTool("lyon_trip_options", zeroWalking))
	client.close(true)
}

func journeyArguments(modes []string) map[string]any {
	return map[string]any{
		"origin":       map[string]any{"latitude": 45.7600, "longitude": 4.8600},
		"destination":  map[string]any{"latitude": 45.7600, "longitude": 4.8500},
		"departure_at": "2026-07-20T08:00:00+02:00",
		"modes":        modes,
		"preferences": map[string]any{
			"max_walking_m": 800, "minimum_velov_bikes": 3, "minimum_velov_docks": 3,
			"avoid_disruptions": false, "wheelchair": false,
		},
	}
}

func startJourneyMCP(t *testing.T, name string, overrides map[string]any, environment map[string]string) *mcpProcess {
	t.Helper()
	folder, env, _, err := testHarness.newEnvironment(name)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, fixtures); err != nil {
		t.Fatal(err)
	}
	for name, value := range overrides {
		data, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if writeErr := os.WriteFile(filepath.Join(fixtures, filepath.FromSlash(name)), data, 0o644); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	values := map[string]string{fixturesEnv: fixtures}
	for key, value := range environment {
		values[key] = value
	}
	client := startMCP(t, folder, mergeEnvironment(env, values))
	initializeMCP(t, client)
	return client
}

func assertNoVelovOption(t *testing.T, result map[string]any) {
	t.Helper()
	assertStatus(t, result, "unavailable")
	assertWarningSource(t, result, "PARTIAL_RESULT", "velov_realtime")
	if options := objectList(t, dataObject(t, result), "options"); len(options) != 0 {
		t.Fatalf("Velov constraint retained options: %#v", options)
	}
}
