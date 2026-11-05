package e2e_test

import (
	"strings"
	"testing"
)

func TestParkAndRideComposition(t *testing.T) {
	runCase(t, "park-and-ride-complete-chain", testParkAndRideCompleteChain)
	runCase(t, "park-and-ride-unavailable-connection", testParkAndRideUnavailableConnection)
	runCase(t, "park-and-ride-unknown-occupancy", testParkAndRideUnknownOccupancy)
}

func testParkAndRideCompleteChain(t *testing.T) {
	parkings := []map[string]any{
		{"id": "a", "name": "P+R A", "type": "parc relais", "lat": 45.7601, "lon": 4.8500, "capacity": 100, "available": 12},
		{"id": "b", "name": "P+R B", "type": "parc relais", "lat": 45.7900, "lon": 4.8500, "capacity": 100, "available": 2},
	}
	client := startJourneyMCP(t, "park-and-ride-chain", map[string]any{
		"datagrandlyon/parkings.json": map[string]any{"values": parkings},
		"routing/transitous_plan.json": transitousWalkAndBusFixture(
			"2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", nil,
		),
	}, map[string]string{"TRANSITOUS_ENABLED": "true"})
	arguments := journeyArguments([]string{"park_and_ride"})
	arguments["preferences"] = walkingPreferences(800)
	result := client.callTool("lyon_trip_options", arguments)
	options := objectList(t, dataObject(t, result), "options")
	if len(options) != 2 {
		t.Fatalf("P+R candidates = %#v", options)
	}
	recommended := indexByField(options, "summary")["Park-and-ride at P+R A; C3 puis M A"]
	if recommended == nil {
		t.Fatalf("feasible interchange was not selected: %#v", options)
	}
	if recommended["evidence"] != "mixed" || recommended["departure_at"] != "2026-07-20T08:00:00+02:00" ||
		recommended["arrival_at"] != "2026-07-20T08:50:00+02:00" || recommended["walking_distance_m"] != float64(450) {
		t.Fatalf("P+R chain timing = %#v", recommended)
	}
	legs := objectList(t, recommended, "legs")
	if len(legs) < 4 || legs[0]["mode"] != "car" || legs[0]["evidence"] != "estimated" || legs[1]["mode"] != "transfer" {
		t.Fatalf("P+R segments = %#v", legs)
	}
	if legs[len(legs)-1]["evidence"] != "calculated" {
		t.Fatalf("routed segment evidence = %#v", legs[len(legs)-1])
	}
	if dataObject(t, result)["recommended_mode"] != "park_and_ride" {
		t.Fatalf("P+R was not recommended: %#v", result)
	}

	deadline := mapsClone(arguments)
	deadline["arrival_before"] = "2026-07-20T08:40:00+02:00"
	late := client.callTool("lyon_trip_options", deadline)
	if dataObject(t, late)["recommended_mode"] != nil {
		t.Fatalf("late P+R chain met the deadline: %#v", late)
	}
	if reasons := optionIneligibilityText(objectList(t, dataObject(t, late), "options")); !strings.Contains(reasons, "arrival") {
		t.Fatalf("deadline reason = %q", reasons)
	}

	shortWalk := mapsClone(arguments)
	shortWalk["preferences"] = walkingPreferences(400)
	walkLimit := client.callTool("lyon_trip_options", shortWalk)
	if dataObject(t, walkLimit)["recommended_mode"] != nil {
		t.Fatalf("P+R walking violation was recommended: %#v", walkLimit)
	}
	if reasons := optionIneligibilityText(objectList(t, dataObject(t, walkLimit), "options")); !strings.Contains(reasons, "max_walking_m") {
		t.Fatalf("walking reason = %q", reasons)
	}
	client.close(true)
}

func testParkAndRideUnavailableConnection(t *testing.T) {
	parkings := []map[string]any{
		{"id": "a", "name": "P+R A", "type": "parc relais", "lat": 45.7601, "lon": 4.8500, "capacity": 100, "available": 12},
	}
	client := startJourneyMCP(t, "park-and-ride-no-connection", map[string]any{
		"datagrandlyon/parkings.json": map[string]any{"values": parkings},
	}, map[string]string{"TRANSITOUS_ENABLED": "true"})
	result := client.callTool("lyon_trip_options", journeyArguments([]string{"park_and_ride"}))
	assertStatus(t, result, "unavailable")
	assertWarningSource(t, result, "ROUTING_UNAVAILABLE", "park_and_ride")
	options := objectList(t, dataObject(t, result), "options")
	if len(options) != 1 || options[0]["evidence"] != "unavailable" || options[0]["arrival_at"] != nil ||
		!strings.Contains(stringValue(options[0]["summary"]), "transit connection unavailable") {
		t.Fatalf("incomplete chain = %#v", options)
	}
	if options[0]["availability"] != "12 spaces" {
		t.Fatalf("parking information was lost: %#v", options[0])
	}
	client.close(true)
}

func testParkAndRideUnknownOccupancy(t *testing.T) {
	parkings := []map[string]any{
		{"id": "a", "name": "P+R A", "type": "parc relais", "lat": 45.7601, "lon": 4.8500, "capacity": 100},
	}
	client := startJourneyMCP(t, "park-and-ride-unknown-occupancy", map[string]any{
		"datagrandlyon/parkings.json": map[string]any{"values": parkings},
		"routing/transitous_plan.json": transitousWalkAndBusFixture(
			"2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", nil,
		),
	}, map[string]string{"TRANSITOUS_ENABLED": "true"})
	result := client.callTool("lyon_trip_options", journeyArguments([]string{"park_and_ride"}))
	assertStatus(t, result, "unavailable")
	if dataObject(t, result)["recommended_mode"] != nil {
		t.Fatalf("unknown occupancy was recommended: %#v", result)
	}
	if reasons := optionIneligibilityText(objectList(t, dataObject(t, result), "options")); !strings.Contains(reasons, "availability is unknown") {
		t.Fatalf("occupancy reason = %q", reasons)
	}
	client.close(true)
}
