package e2e_test

import (
	"strings"
	"testing"
)

func TestTransitousEvidenceAcceptance(t *testing.T) {
	runCase(t, "transitous-actual-times", testTransitousActualTimes)
	runCase(t, "transitous-invalid-timing", testTransitousInvalidTiming)
	runCase(t, "transitous-walking-and-alerts", testTransitousWalkingAndAlerts)
	runCase(t, "transitous-operator-and-accessibility", testTransitousOperatorAndAccessibility)
}

func testTransitousActualTimes(t *testing.T) {
	client := startTransitousMCP(t, "transitous-actual-times", transitousWalkAndBusFixture("2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", nil))
	result := client.callTool("lyon_trip_options", journeyArguments([]string{"tcl"}))
	assertStatus(t, result, "partial")
	options := objectList(t, dataObject(t, result), "options")
	if len(options) != 1 {
		t.Fatalf("options = %#v", options)
	}
	option := options[0]
	if dataObject(t, result)["recommended_mode"] != "tcl" || option["eligible"] != true || option["evidence"] != "calculated" {
		t.Fatalf("calculated itinerary was not recommended: %#v", option)
	}
	if option["departure_at"] != "2026-07-20T08:20:00+02:00" || option["arrival_at"] != "2026-07-20T08:50:00+02:00" {
		t.Fatalf("provider instants were reconstructed: %#v", option)
	}
	if option["walking_distance_m"] != float64(450) {
		t.Fatalf("walking total = %#v", option["walking_distance_m"])
	}
	if legs := objectList(t, option, "legs"); len(legs) != 4 || legs[1]["line"] != "C3" {
		t.Fatalf("leg evidence = %#v", legs)
	}

	deadline := mapsClone(journeyArguments([]string{"tcl"}))
	deadline["arrival_before"] = "2026-07-20T08:40:00+02:00"
	missed := client.callTool("lyon_trip_options", deadline)
	if dataObject(t, missed)["recommended_mode"] != nil {
		t.Fatalf("late itinerary met the deadline: %#v", missed)
	}
	rejected := objectList(t, dataObject(t, missed), "options")
	if len(rejected) != 1 || rejected[0]["eligible"] != false || !strings.Contains(optionIneligibilityText(rejected), "arrival") {
		t.Fatalf("deadline rejection = %#v", rejected)
	}
	client.close(true)
}

func testTransitousInvalidTiming(t *testing.T) {
	for name, fixture := range map[string]map[string]any{
		"missing":       map[string]any{"itineraries": []any{map[string]any{"duration": 1800, "summary": "Metro A"}}},
		"contradictory": transitousWalkAndBusFixture("2026-07-20T08:50:00+02:00", "2026-07-20T08:20:00+02:00", nil),
	} {
		t.Run(name, func(t *testing.T) {
			client := startTransitousMCP(t, "transitous-invalid-"+name, fixture)
			result := client.callTool("lyon_trip_options", journeyArguments([]string{"tcl"}))
			assertStatus(t, result, "unavailable")
			assertWarningSource(t, result, "ROUTING_UNAVAILABLE", "journey_planner")
			if dataObject(t, result)["recommended_mode"] != nil {
				t.Fatalf("invalid timing was recommended: %#v", result)
			}
			options := objectList(t, dataObject(t, result), "options")
			if len(options) != 1 || options[0]["evidence"] != "unavailable" || options[0]["walking_distance_m"] != nil {
				t.Fatalf("invalid timing produced fabricated evidence: %#v", options)
			}
			client.close(true)
		})
	}
}

func testTransitousWalkingAndAlerts(t *testing.T) {
	fixture := transitousWalkAndBusFixture("2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", nil)
	client := startTransitousMCP(t, "transitous-walking-alerts", fixture)
	over := journeyArguments([]string{"tcl"})
	over["preferences"] = walkingPreferences(449)
	tooLong := client.callTool("lyon_trip_options", over)
	if dataObject(t, tooLong)["recommended_mode"] != nil {
		t.Fatalf("walking-limit violation was recommended: %#v", tooLong)
	}
	if reasons := optionIneligibilityText(objectList(t, dataObject(t, tooLong), "options")); !strings.Contains(reasons, "max_walking_m") {
		t.Fatalf("walking limit reason = %q", reasons)
	}
	exact := journeyArguments([]string{"tcl"})
	exact["preferences"] = walkingPreferences(450)
	atBoundary := client.callTool("lyon_trip_options", exact)
	if dataObject(t, atBoundary)["recommended_mode"] != "tcl" {
		t.Fatalf("exact walking boundary was rejected: %#v", atBoundary)
	}

	alerted := transitousWalkAndBusFixture("2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", map[string]any{
		"alerts": []any{map[string]any{"id": "A1", "severity": "major", "title": "Ligne A interrompue", "description": "Travaux"}},
	})
	client.close(true)
	client = startTransitousMCP(t, "transitous-alerts", alerted)
	avoiding := journeyArguments([]string{"tcl"})
	avoiding["preferences"] = map[string]any{
		"max_walking_m": 800, "minimum_velov_bikes": 3, "minimum_velov_docks": 3,
		"avoid_disruptions": true, "wheelchair": false,
	}
	disrupted := client.callTool("lyon_trip_options", avoiding)
	if dataObject(t, disrupted)["recommended_mode"] != nil {
		t.Fatalf("alerted itinerary was recommended: %#v", disrupted)
	}
	if reasons := optionIneligibilityText(objectList(t, dataObject(t, disrupted), "options")); !strings.Contains(reasons, "alerts") {
		t.Fatalf("alert reason = %q", reasons)
	}
	allowing := journeyArguments([]string{"tcl"})
	allowing["preferences"] = map[string]any{
		"max_walking_m": 800, "minimum_velov_bikes": 3, "minimum_velov_docks": 3,
		"avoid_disruptions": false, "wheelchair": false,
	}
	allowed := client.callTool("lyon_trip_options", allowing)
	options := objectList(t, dataObject(t, allowed), "options")
	if len(options) != 1 || !containsFieldValue(objectList(t, options[0], "alerts"), "id", "A1") {
		t.Fatalf("known alert was not retained: %#v", options)
	}
	client.close(true)
}

func testTransitousOperatorAndAccessibility(t *testing.T) {
	fixture := transitousWalkAndBusFixture("2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", nil)
	client := startTransitousMCP(t, "transitous-operators", fixture)
	result := client.callTool("lyon_trip_options", journeyArguments([]string{"tcl"}))
	option := objectList(t, dataObject(t, result), "options")[0]
	legs := objectList(t, option, "legs")
	if legs[1]["operator"] != "SYTRAL Mobilités" || legs[2]["operator"] != "SNCF" {
		t.Fatalf("operators were not mapped honestly: %#v", legs)
	}
	if option["accessibility"] != nil {
		t.Fatalf("absent accessibility evidence became a claim: %#v", option["accessibility"])
	}

	wheelchair := journeyArguments([]string{"tcl"})
	wheelchair["preferences"] = map[string]any{
		"max_walking_m": 800, "minimum_velov_bikes": 3, "minimum_velov_docks": 3,
		"avoid_disruptions": false, "wheelchair": true,
	}
	unverified := client.callTool("lyon_trip_options", wheelchair)
	assertStatus(t, unverified, "unavailable")
	if dataObject(t, unverified)["recommended_mode"] != nil {
		t.Fatalf("unverified wheelchair itinerary was recommended: %#v", unverified)
	}
	client.close(true)
}

func startTransitousMCP(t *testing.T, name string, fixture map[string]any) *mcpProcess {
	t.Helper()
	return startJourneyMCP(t, name, map[string]any{"routing/transitous_plan.json": fixture}, map[string]string{"TRANSITOUS_ENABLED": "true"})
}

func transitousWalkAndBusFixture(start, end string, extras map[string]any) map[string]any {
	walkStart, busEnd := 300, 150
	itinerary := map[string]any{
		"startTime": start, "endTime": end, "duration": 1800, "transfers": 1,
		"summary": "C3 puis M A",
		"legs": []any{
			map[string]any{
				"mode": "WALK", "startTime": start, "endTime": "2026-07-20T08:24:00+02:00",
				"distance": walkStart, "from": map[string]any{"name": "Origin"}, "to": map[string]any{"name": "Arrêt C3"},
			},
			map[string]any{
				"mode": "BUS", "routeShortName": "C3", "agencyName": "SYTRAL Mobilités",
				"startTime": "2026-07-20T08:24:00+02:00", "endTime": "2026-07-20T08:40:00+02:00",
				"from": map[string]any{"name": "Arrêt C3"}, "to": map[string]any{"name": "Part-Dieu"},
			},
			map[string]any{
				"mode": "SUBWAY", "routeShortName": "A", "agencyName": "SNCF",
				"startTime": "2026-07-20T08:40:00+02:00", "endTime": "2026-07-20T08:45:00+02:00",
				"from": map[string]any{"name": "Part-Dieu"}, "to": map[string]any{"name": "Bellecour"},
			},
			map[string]any{
				"mode": "WALK", "startTime": "2026-07-20T08:45:00+02:00", "endTime": end,
				"distance": busEnd, "from": map[string]any{"name": "Bellecour"}, "to": map[string]any{"name": "Destination"},
			},
		},
	}
	for key, value := range extras {
		itinerary[key] = value
	}
	return map[string]any{"itineraries": []any{itinerary}}
}
