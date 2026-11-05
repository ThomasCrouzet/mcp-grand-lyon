package e2e_test

import (
	"strings"
	"testing"
)

func TestTripEligibilityAcceptance(t *testing.T) {
	runCase(t, "journey-eligibility-no-tcl-route", testEligibilityWithoutTCLRoute)
	runCase(t, "journey-walking-boundary", testEligibilityWalkingBoundary)
	runCase(t, "journey-informational-unavailable", testEligibilityKeepsInformation)
	runCase(t, "journey-deadline-and-wheelchair", testEligibilityDeadlineAndWheelchair)
}

func testEligibilityWithoutTCLRoute(t *testing.T) {
	client := startJourneyMCP(t, "journey-eligibility-tcl", nil, nil)
	result := client.callTool("lyon_trip_options", journeyArguments([]string{"tcl"}))
	assertStatus(t, result, "unavailable")
	if dataObject(t, result)["recommended_mode"] != nil {
		t.Fatalf("unavailable TCL route was recommended: %#v", result)
	}
	options := objectList(t, dataObject(t, result), "options")
	if len(options) != 1 {
		t.Fatalf("options = %#v", options)
	}
	option := options[0]
	if option["eligible"] != false || option["evidence"] != "unavailable" {
		t.Fatalf("missing itinerary acquired eligibility: %#v", option)
	}
	for _, field := range []string{"estimated_duration_seconds", "walking_distance_m", "arrival_at"} {
		if option[field] != nil {
			t.Fatalf("missing itinerary invented %s: %#v", field, option)
		}
	}
	client.close(true)
}

func testEligibilityWalkingBoundary(t *testing.T) {
	client := startJourneyMCP(t, "journey-walking-boundary", nil, nil)
	base := journeyArguments([]string{"walk"})
	base["preferences"] = map[string]any{
		"max_walking_m": 5000, "minimum_velov_bikes": 3, "minimum_velov_docks": 3,
		"avoid_disruptions": false, "wheelchair": false,
	}
	unlimited := client.callTool("lyon_trip_options", base)
	options := objectList(t, dataObject(t, unlimited), "options")
	if len(options) != 1 {
		t.Fatalf("walk options = %#v", options)
	}
	distance, ok := options[0]["walking_distance_m"].(float64)
	if !ok || distance <= 0 {
		t.Fatalf("walk distance = %#v", options[0]["walking_distance_m"])
	}

	exact := mapsClone(base)
	exact["preferences"] = walkingPreferences(int(distance))
	atBoundary := client.callTool("lyon_trip_options", exact)
	if dataObject(t, atBoundary)["recommended_mode"] != "walk" {
		t.Fatalf("exact walking boundary was rejected: %#v", atBoundary)
	}
	atBoundaryOptions := objectList(t, dataObject(t, atBoundary), "options")
	if atBoundaryOptions[0]["eligible"] != true {
		t.Fatalf("exact walking boundary option = %#v", atBoundaryOptions[0])
	}

	over := mapsClone(base)
	over["preferences"] = walkingPreferences(int(distance) - 1)
	tooLong := client.callTool("lyon_trip_options", over)
	if dataObject(t, tooLong)["recommended_mode"] != nil {
		t.Fatalf("over-budget walk was recommended: %#v", tooLong)
	}
	tooLongOptions := objectList(t, dataObject(t, tooLong), "options")
	if len(tooLongOptions) != 1 || tooLongOptions[0]["eligible"] != false {
		t.Fatalf("over-budget walk was eligible: %#v", tooLongOptions)
	}
	reasons := optionIneligibilityText(tooLongOptions)
	if !strings.Contains(reasons, "max_walking_m") {
		t.Fatalf("walking rejection does not name the limit: %v", reasons)
	}
	client.close(true)
}

func testEligibilityKeepsInformation(t *testing.T) {
	client := startJourneyMCP(t, "journey-info-unavailable", nil, nil)
	result := client.callTool("lyon_trip_options", journeyArguments([]string{"walk", "tcl"}))
	assertStatus(t, result, "partial")
	if dataObject(t, result)["recommended_mode"] != "walk" {
		t.Fatalf("eligible walk was not selected: %#v", result)
	}
	byMode := indexByField(objectList(t, dataObject(t, result), "options"), "mode")
	if byMode["tcl"] == nil || byMode["tcl"]["eligible"] != false || byMode["tcl"]["evidence"] != "unavailable" {
		t.Fatalf("unavailable TCL option is not clearly informational: %#v", byMode["tcl"])
	}
	if byMode["walk"] == nil || byMode["walk"]["eligible"] != true || byMode["walk"]["evidence"] != "estimated" {
		t.Fatalf("estimated walk evidence is unclear: %#v", byMode["walk"])
	}
	client.close(true)
}

func testEligibilityDeadlineAndWheelchair(t *testing.T) {
	client := startJourneyMCP(t, "journey-deadline-wheelchair", nil, nil)
	deadline := mapsClone(journeyArguments([]string{"walk"}))
	deadline["arrival_before"] = "2026-07-20T08:01:00+02:00"
	result := client.callTool("lyon_trip_options", deadline)
	if dataObject(t, result)["recommended_mode"] != nil {
		t.Fatalf("walk met an impossible deadline: %#v", result)
	}
	options := objectList(t, dataObject(t, result), "options")
	reasons := optionIneligibilityText(options)
	if !strings.Contains(reasons, "arrival") {
		t.Fatalf("deadline rejection is not explained: %v", reasons)
	}

	wheelchair := mapsClone(journeyArguments([]string{"walk"}))
	wheelchair["preferences"] = map[string]any{
		"max_walking_m": 5000, "minimum_velov_bikes": 3, "minimum_velov_docks": 3,
		"avoid_disruptions": false, "wheelchair": true,
	}
	unverified := client.callTool("lyon_trip_options", wheelchair)
	assertStatus(t, unverified, "unavailable")
	if dataObject(t, unverified)["recommended_mode"] != nil {
		t.Fatalf("unverified wheelchair route was recommended: %#v", unverified)
	}
	client.close(true)
}

func walkingPreferences(maxWalking int) map[string]any {
	return map[string]any{
		"max_walking_m": maxWalking, "minimum_velov_bikes": 3, "minimum_velov_docks": 3,
		"avoid_disruptions": false, "wheelchair": false,
	}
}

func optionIneligibilityText(options []map[string]any) string {
	reasons := []string{}
	for _, option := range options {
		values, _ := option["ineligibility_reasons"].([]any)
		for _, value := range values {
			if text, ok := value.(string); ok {
				reasons = append(reasons, text)
			}
		}
	}
	return strings.Join(reasons, " ")
}
