package providers

import (
	"testing"
	"time"
)

func TestParseSIRIDeparturesSupportsScalarObjects(t *testing.T) {
	payload := map[string]any{
		"EstimatedVehicleJourney": map[string]any{
			"LineRef":         map[string]any{"value": "ActIV:Line::C12:SYTRAL"},
			"DestinationName": "Bellecour",
			"EstimatedCalls": map[string]any{
				"EstimatedCall": map[string]any{
					"StopPointRef":          map[string]any{"#text": "PD1"},
					"AimedDepartureTime":    "2026-07-20T08:04:00+02:00",
					"ExpectedDepartureTime": "2026-07-20T08:05:00+02:00",
					"DepartureStatus":       "cancelled",
				},
			},
		},
	}

	departures, err := ParseSIRIDepartures(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(departures) != 1 {
		t.Fatalf("departures = %d, want 1", len(departures))
	}
	departure := departures[0]
	if departure.LineName != "C12" || departure.LineID != "tcl:ActIV:Line::C12:SYTRAL" {
		t.Fatalf("unexpected line: %#v", departure)
	}
	if departure.StopRef == nil || *departure.StopRef != "PD1" || !departure.Cancelled {
		t.Fatalf("unexpected call fields: %#v", departure)
	}
	if departure.DelaySeconds == nil || *departure.DelaySeconds != 60 {
		t.Fatalf("delay = %v, want 60", departure.DelaySeconds)
	}
	want := time.Date(2026, 7, 20, 8, 5, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	if departure.ExpectedAt == nil || !departure.ExpectedAt.Equal(want) {
		t.Fatalf("expected time = %v, want %v", departure.ExpectedAt, want)
	}
}

func TestParseSIRIAlertsPreservesScalarLineAndSeverity(t *testing.T) {
	payload := map[string]any{
		"Situations": map[string]any{
			"PtSituationElement": []any{
				map[string]any{
					"SituationNumber": "critical",
					"Summary":         "Critical alert",
					"Severity":        "verySevere",
					"Affects":         map[string]any{"AffectedLine": map[string]any{"LineRef": "ActIV:Line::A:SYTRAL"}},
				},
				map[string]any{
					"SituationNumber": "minor",
					"Summary":         "Minor alert",
					"Severity":        "verySlight",
				},
				map[string]any{
					"SituationNumber": "info",
					"Summary":         "Information",
					"Severity":        "noImpact",
				},
			},
		},
	}

	alerts, err := ParseSIRIAlerts(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 3 {
		t.Fatalf("alerts = %d, want 3", len(alerts))
	}
	if alerts[0].Severity != "critical" || len(alerts[0].Lines) != 1 || alerts[0].Lines[0] != "A" {
		t.Fatalf("unexpected critical alert: %#v", alerts[0])
	}
	if alerts[1].Severity != "minor" || alerts[2].Severity != "info" {
		t.Fatalf("unexpected severity mapping: %#v", alerts)
	}
}

func TestParseSIRIDeparturesSupportsDirectCallLists(t *testing.T) {
	payload := map[string]any{
		"EstimatedVehicleJourney": map[string]any{
			"LineRef": "A",
			"EstimatedCalls": []any{
				map[string]any{"StopPointRef": "BEL1", "DestinationDisplay": "Vaulx"},
				map[string]any{"StopPointRef": "BEL1", "DestinationDisplay": "Perrache"},
			},
		},
	}

	departures, err := ParseSIRIDepartures(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(departures) != 2 || departures[0].Destination != "Vaulx" || departures[1].Destination != "Perrache" {
		t.Fatalf("direct calls were not preserved: %#v", departures)
	}
}

func TestSIRIParsersRejectInvalidRoots(t *testing.T) {
	if _, err := ParseSIRIDepartures("invalid"); err == nil {
		t.Fatal("departures accepted a scalar root")
	}
	if _, err := ParseSIRIAlerts(42); err == nil {
		t.Fatal("alerts accepted a scalar root")
	}
}
