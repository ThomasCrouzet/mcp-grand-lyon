package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTransitLineReferences(t *testing.T) {
	runCase(t, "transit-line-references", func(t *testing.T) {
		folder, env, _, err := preparedEnvironment("transit-line-references")
		if err != nil {
			t.Fatal(err)
		}
		fixtures := filepath.Join(folder, "fixtures")
		if err := copyTree(testHarness.fixtures, fixtures); err != nil {
			t.Fatal(err)
		}
		payload := `{"Siri":{"ServiceDelivery":{"EstimatedTimetableDelivery":{"EstimatedJourneyVersionFrame":{"EstimatedVehicleJourney":[
		{"LineRef":"ActIV:Line::C12:SYTRAL","DestinationName":"Test destination","EstimatedCalls":{"EstimatedCall":[{"StopPointRef":"BEL1","ExpectedDepartureTime":"2026-07-20T08:05:00+02:00"}]}},
		{"LineRef":"ActIV:Line::T3:SYTRAL","DestinationName":"Test destination","EstimatedCalls":{"EstimatedCall":[{"StopPointRef":"BEL1","ExpectedDepartureTime":"2026-07-20T08:07:00+02:00"}]}}
		]}}}}}`
		if err := os.WriteFile(filepath.Join(fixtures, "siri", "estimated_timetable.json"), []byte(payload), 0o644); err != nil {
			t.Fatal(err)
		}
		env = mergeEnvironment(env, map[string]string{fixturesEnv: fixtures})
		client := startMCP(t, folder, env)
		initializeMCP(t, client)
		for _, item := range []struct {
			line string
			time string
		}{
			{"C12", "2026-07-20T08:05:00+02:00"},
			{"ActIV:Line::C12:SYTRAL", "2026-07-20T08:05:00+02:00"},
			{"bus   c12", "2026-07-20T08:05:00+02:00"},
			{"tramway t3", "2026-07-20T08:07:00+02:00"},
			{"C1", ""},
		} {
			result := client.callTool("lyon_next_departures", map[string]any{
				"stop": map[string]any{"place_id": "gtfs:stop:BEL1"},
				"line": item.line, "at": "2026-07-20T08:00:00+02:00", "limit": 10,
			})
			departures := objectList(t, dataObject(t, result), "departures")
			if item.time == "" {
				if len(departures) != 0 {
					t.Errorf("line %q matched a different line: %#v", item.line, departures)
				}
			} else if len(departures) != 1 || departures[0]["expected_at"] != item.time {
				t.Errorf("line %q returned %#v; want one departure at %s", item.line, departures, item.time)
			}
		}
		client.close(true)
	})
}
