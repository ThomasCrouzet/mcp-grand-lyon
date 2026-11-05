package providers

import (
	"testing"
	"time"
)

func itinerary(start, end string, legs []any) map[string]any {
	return map[string]any{
		"type": "FeatureCollection", "itineraries": []any{map[string]any{
			"startTime": start, "endTime": end, "duration": 1800,
			"transfers": 1,
			"legs":      legs,
		}},
	}
}

func leg(mode, start, end string, distance *int) map[string]any {
	value := map[string]any{
		"mode": mode, "startTime": start, "endTime": end,
		"from": map[string]any{"name": "From " + mode}, "to": map[string]any{"name": "To " + mode},
	}
	if distance != nil {
		value["distance"] = *distance
	}
	return value
}

func TestParseTransitousPlanPreservesActualTimes(t *testing.T) {
	walk, final := 300, 150
	payload := itinerary("2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", []any{
		leg("WALK", "2026-07-20T08:20:00+02:00", "2026-07-20T08:24:00+02:00", &walk),
		leg("BUS", "2026-07-20T08:24:00+02:00", "2026-07-20T08:45:00+02:00", nil),
		leg("WALK", "2026-07-20T08:45:00+02:00", "2026-07-20T08:50:00+02:00", &final),
	})
	options, err := ParseTransitousPlan(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 {
		t.Fatalf("options = %#v", options)
	}
	option := options[0]
	if option.DepartureAt.Format(time.RFC3339) != "2026-07-20T08:20:00+02:00" ||
		option.ArrivalAt.Format(time.RFC3339) != "2026-07-20T08:50:00+02:00" {
		t.Fatalf("provider times were reconstructed: %#v", option)
	}
	if option.EstimatedDurationSeconds == nil || *option.EstimatedDurationSeconds != 1800 {
		t.Fatalf("duration = %#v", option.EstimatedDurationSeconds)
	}
	if option.WalkingDistanceM == nil || *option.WalkingDistanceM != 450 {
		t.Fatalf("walking = %#v", option.WalkingDistanceM)
	}
	if option.Transfers != 1 || option.Evidence != "calculated" || len(option.Legs) != 3 {
		t.Fatalf("option = %#v", option)
	}
	if option.Legs[1].Mode != "bus" || option.Legs[1].From != "From BUS" {
		t.Fatalf("legs = %#v", option.Legs)
	}
}

func TestParseTransitousPlanKeepsInstantsAcrossDateAndDST(t *testing.T) {
	cases := []struct{ start, end string }{
		{"2026-10-25T02:20:00+01:00", "2026-10-25T03:00:00+01:00"},
		{"2026-12-31T23:50:00+01:00", "2027-01-01T00:10:00+01:00"},
		{"2026-07-20T08:20:00Z", "2026-07-20T08:50:00Z"},
	}
	for _, testCase := range cases {
		distance := 100
		options, err := ParseTransitousPlan(itinerary(testCase.start, testCase.end, []any{
			leg("WALK", testCase.start, testCase.end, &distance),
			leg("BUS", testCase.start, testCase.end, nil),
		}))
		if err != nil {
			t.Fatal(err)
		}
		if len(options) != 1 {
			t.Fatalf("%s: options = %#v", testCase.start, options)
		}
		if options[0].DepartureAt.Format(time.RFC3339) != mustParse(testCase.start).Format(time.RFC3339) {
			t.Fatalf("%s: departure = %s", testCase.start, options[0].DepartureAt)
		}
	}
}

func mustParse(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestParseTransitousPlanRejectsMissingOrContradictoryTiming(t *testing.T) {
	distance := 100
	for name, payload := range map[string]any{
		"missing start": map[string]any{"itineraries": []any{map[string]any{
			"endTime": "2026-07-20T08:50:00+02:00",
			"legs":    []any{leg("BUS", "2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", &distance)},
		}}},
		"contradictory": map[string]any{"itineraries": []any{map[string]any{
			"startTime": "2026-07-20T08:50:00+02:00", "endTime": "2026-07-20T08:20:00+02:00",
			"legs": []any{leg("BUS", "2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", &distance)},
		}}},
		"no transit leg": itinerary("2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", []any{
			leg("WALK", "2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", &distance),
		}),
		"leg missing end": map[string]any{"itineraries": []any{map[string]any{
			"startTime": "2026-07-20T08:20:00+02:00", "endTime": "2026-07-20T08:50:00+02:00",
			"legs": []any{map[string]any{"mode": "BUS", "startTime": "2026-07-20T08:20:00+02:00"}},
		}}},
	} {
		t.Run(name, func(t *testing.T) {
			options, err := ParseTransitousPlan(payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(options) != 0 {
				t.Fatalf("invalid itinerary became eligible: %#v", options)
			}
		})
	}
}

func TestParseTransitousPlanMapsEvidence(t *testing.T) {
	busDistance := 1200
	payload := map[string]any{"itineraries": []any{map[string]any{
		"startTime": "2026-07-20T08:20:00+02:00", "endTime": "2026-07-20T08:50:00+02:00",
		"wheelchairAccessible": true,
		"alerts": []any{map[string]any{
			"id": "alert-1", "severity": "major", "title": "Ligne interrompue", "description": "Travaux",
		}},
		"legs": []any{map[string]any{
			"mode": "BUS", "routeShortName": "C3", "agencyName": "SYTRAL Mobilités",
			"startTime": "2026-07-20T08:20:00+02:00", "endTime": "2026-07-20T08:50:00+02:00",
			"distance": busDistance, "realTime": true,
			"from": map[string]any{"name": "Part-Dieu"}, "to": map[string]any{"name": "Bellecour"},
			"alerts": []any{map[string]any{"id": "alert-2", "title": "Arrêt déplacé"}},
		}},
	}}}
	options, err := ParseTransitousPlan(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 {
		t.Fatalf("options = %#v", options)
	}
	option := options[0]
	if option.WalkingDistanceM == nil || *option.WalkingDistanceM != 0 {
		t.Fatalf("transit-only walking = %#v", option.WalkingDistanceM)
	}
	if option.Legs[0].Operator != "SYTRAL Mobilités" || option.Legs[0].Line != "C3" || !option.Legs[0].Realtime {
		t.Fatalf("leg evidence = %#v", option.Legs[0])
	}
	if !option.Realtime || len(option.Alerts) != 2 {
		t.Fatalf("alerts/realtime = %#v %#v", option.Alerts, option.Realtime)
	}
	if option.Accessibility == nil || *option.Accessibility != "accessible" {
		t.Fatalf("explicit accessibility = %#v", option.Accessibility)
	}
}

func TestParseTransitousPlanLeavesUnknownWalkingAndAccessibility(t *testing.T) {
	payload := itinerary("2026-07-20T08:20:00+02:00", "2026-07-20T08:50:00+02:00", []any{
		leg("WALK", "2026-07-20T08:20:00+02:00", "2026-07-20T08:24:00+02:00", nil),
		leg("BUS", "2026-07-20T08:24:00+02:00", "2026-07-20T08:50:00+02:00", nil),
	})
	options, err := ParseTransitousPlan(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 {
		t.Fatalf("options = %#v", options)
	}
	if options[0].WalkingDistanceM != nil {
		t.Fatalf("missing walking distance became zero: %#v", options[0].WalkingDistanceM)
	}
	if options[0].Accessibility != nil {
		t.Fatalf("absent accessibility evidence became a claim: %#v", options[0].Accessibility)
	}
}

func TestParseTransitousPlanRejectsInvalidRoot(t *testing.T) {
	if _, err := ParseTransitousPlan(true); err == nil {
		t.Fatal("scalar root was accepted")
	}
}
