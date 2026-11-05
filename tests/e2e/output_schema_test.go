package e2e_test

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestOutputSchemaContracts(t *testing.T) {
	folder, env, _, err := preparedEnvironment("output-schemas")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	rawList := client.request("tools/list", nil)
	var list struct {
		Tools []struct {
			Name         string          `json:"name"`
			OutputSchema json.RawMessage `json:"outputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rawList, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 10 {
		t.Fatalf("tools/list returned %d tools", len(list.Tools))
	}
	payloads := map[string]map[string]any{
		"lyon_resolve_place":       {"query": "Part-Dieu", "limit": 3},
		"lyon_next_departures":     {"stop": map[string]any{"place_id": "gtfs:stop:PD1"}, "line": "A", "at": "2026-07-20T08:00:00+02:00", "limit": 3},
		"lyon_mobility_status":     {"lines": []string{"A"}, "include": []string{"transit", "accessibility", "traffic", "roadworks"}},
		"lyon_trip_options":        {"origin": map[string]any{"profile_place": "home"}, "destination": map[string]any{"profile_place": "work"}, "modes": []string{"walk", "velov", "tcl"}, "departure_at": "2026-07-20T08:00:00+02:00"},
		"lyon_parking_options":     {"destination": map[string]any{"latitude": 45.7675, "longitude": 4.8355}, "radius_m": 500},
		"lyon_accessibility_check": {"origin": map[string]any{"query": "Bellecour"}, "destination": map[string]any{"query": "Part-Dieu"}, "needs": []string{"wheelchair"}},
		"lyon_nearby_facilities":   {"location": map[string]any{"latitude": 45.777, "longitude": 4.855}, "categories": []string{"toilet", "park"}, "radius_m": 2000},
		"lyon_environment_brief":   {"location": map[string]any{"query": "Lyon"}, "at": "2026-07-20T08:00:00+02:00", "indicators": []string{"pollen", "air_quality", "heat"}},
		"lyon_waste_dropoff":       {"item": "batterie de vélo électrique", "location": map[string]any{"profile_place": "home"}},
		"lyon_personal_briefing":   {"profile": "weekday_morning", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false},
	}
	for _, tool := range list.Tools {
		t.Run(tool.Name, func(t *testing.T) {
			if len(tool.OutputSchema) == 0 {
				t.Fatal("tool advertises no output schema")
			}
			resolved := resolveSchema(t, tool.OutputSchema)
			call := client.callToolResult(tool.Name, payloads[tool.Name])
			if call.StructuredContent == nil {
				t.Fatal("tool returned no structured content")
			}
			if err := resolved.Validate(call.StructuredContent); err != nil {
				t.Fatalf("structured content does not match advertised schema: %v\n%#v", err, call.StructuredContent)
			}
			text, _ := call.Content[0]["text"].(string)
			var fromText map[string]any
			if err := json.Unmarshal([]byte(text), &fromText); err != nil {
				t.Fatalf("decode text content: %v", err)
			}
			if !jsonEqual(fromText, call.StructuredContent) {
				t.Fatalf("text and structured content differ: text=%#v structured=%#v", fromText, call.StructuredContent)
			}
		})
	}

	resolve := findOutputSchema(t, list.Tools, "lyon_resolve_place")
	empty := client.callTool("lyon_resolve_place", map[string]any{"query": "qzxqzx unknown place"})
	assertStatus(t, empty, "not_found")
	if err := resolve.Validate(empty); err != nil {
		t.Fatalf("not_found result does not validate: %v", err)
	}
	ambiguous := client.callTool("lyon_waste_dropoff", map[string]any{
		"item": "batterie de vélo électrique", "location": map[string]any{"profile_place": "home"},
	})
	assertStatus(t, ambiguous, "ambiguous")
	wasteSchema := findOutputSchema(t, list.Tools, "lyon_waste_dropoff")
	if err := wasteSchema.Validate(ambiguous); err != nil {
		t.Fatalf("ambiguous result does not validate: %v", err)
	}

	next := findOutputSchema(t, list.Tools, "lyon_next_departures")
	invalid := callToolArguments(t, client, "lyon_next_departures", map[string]any{
		"stop": map[string]any{"latitude": 91.0, "longitude": 4.85},
	})
	if err := next.Validate(invalid); err != nil {
		t.Fatalf("invalid_request result does not validate: %v", err)
	}
	broken := mapsClone(empty)
	broken["generated_at"] = 42
	if err := resolve.Validate(broken); err == nil {
		t.Fatal("deliberately invalid emitted result passed validation")
	}
	client.close(true)
}

func resolveSchema(t *testing.T, raw json.RawMessage) *jsonschema.Resolved {
	t.Helper()
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode output schema: %v", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve output schema: %v", err)
	}
	return resolved
}

func findOutputSchema(t *testing.T, tools []struct {
	Name         string          `json:"name"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}, name string) *jsonschema.Resolved {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return resolveSchema(t, tool.OutputSchema)
		}
	}
	t.Fatalf("tool %s is absent", name)
	return nil
}

func jsonEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}
