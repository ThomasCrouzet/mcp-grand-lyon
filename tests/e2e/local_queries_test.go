package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGoLocalQueryAcceptance(t *testing.T) {
	runCase(t, "local-query-unknown-places", testLocalQueryUnknownPlaces)
	runCase(t, "local-query-unsupported-values", testLocalQueryUnsupportedValues)
	runCase(t, "local-query-malformed-fixtures", testLocalQueryMalformedFixtures)
	runCase(t, "local-query-custom-taxonomy", testLocalQueryCustomTaxonomy)
}

func testLocalQueryUnknownPlaces(t *testing.T) {
	client := startLocalQueryMCP(t, "local-query-unknown-places", nil, nil)
	refs := []map[string]any{
		{"query": "Unfindable local query"},
		{"profile_place": "missing-profile"},
		{"place_id": "missing:place"},
	}
	for _, ref := range refs {
		facilities := client.callTool("lyon_nearby_facilities", map[string]any{
			"location": ref, "categories": []string{"toilet"},
		})
		assertStatus(t, facilities, "not_found")
		if items := objectList(t, dataObject(t, facilities), "items"); len(items) != 0 {
			t.Fatalf("unknown place produced facilities: %#v", items)
		}

		environment := client.callTool("lyon_environment_brief", map[string]any{
			"location": ref, "indicators": []string{"pollen"},
		})
		assertStatus(t, environment, "not_found")
		if items := objectList(t, dataObject(t, environment), "indicators"); len(items) != 0 {
			t.Fatalf("unknown place produced indicators: %#v", items)
		}

		waste := client.callTool("lyon_waste_dropoff", map[string]any{
			"item": "pile", "location": ref,
		})
		assertStatus(t, waste, "not_found")
		if items := objectList(t, dataObject(t, waste), "facilities"); len(items) != 0 {
			t.Fatalf("unknown place produced waste facilities: %#v", items)
		}
	}
	client.close(true)
}

func testLocalQueryUnsupportedValues(t *testing.T) {
	client := startLocalQueryMCP(t, "local-query-unsupported-values", nil, nil)
	location := map[string]any{"latitude": 45.76, "longitude": 4.85}
	facilities := client.callTool("lyon_nearby_facilities", map[string]any{
		"location": location, "categories": []string{"sauna"},
	})
	assertStatus(t, facilities, "invalid_request")

	environment := client.callTool("lyon_environment_brief", map[string]any{
		"location": location, "indicators": []string{"cosmic_rays"},
	})
	assertStatus(t, environment, "unavailable")
	assertWarningSource(t, environment, "UNSUPPORTED_INDICATOR", "cosmic_rays")
	if items := objectList(t, dataObject(t, environment), "indicators"); len(items) != 0 {
		t.Fatalf("unsupported indicator produced values: %#v", items)
	}
	client.close(true)
}

func testLocalQueryMalformedFixtures(t *testing.T) {
	overrides := map[string]any{
		"datagrandlyon/facilities.json": map[string]any{"values": []map[string]any{
			{"id": "ghost", "name": "Missing coordinates", "category": "toilet"},
		}},
		"datagrandlyon/environment.json": map[string]any{
			"supported": []string{"pollen"}, "pollen": map[string]any{},
		},
		"datagrandlyon/waste_facilities.json": map[string]any{"values": []map[string]any{
			{"id": "ghost", "name": "Missing coordinates", "categories": []string{"portable_battery"}},
		}},
	}
	client := startLocalQueryMCP(t, "local-query-malformed-fixtures", overrides, nil)
	location := map[string]any{"latitude": 45.76, "longitude": 4.85}

	facilities := client.callTool("lyon_nearby_facilities", map[string]any{
		"location": location, "categories": []string{"toilet"},
	})
	if items := objectList(t, dataObject(t, facilities), "items"); len(items) != 0 {
		t.Fatalf("malformed facility fixture produced results: %#v", items)
	}

	environment := client.callTool("lyon_environment_brief", map[string]any{
		"location": location, "indicators": []string{"pollen"},
	})
	assertStatus(t, environment, "unavailable")
	assertWarningSource(t, environment, "SOURCE_UNAVAILABLE", "pollen")
	if items := objectList(t, dataObject(t, environment), "indicators"); len(items) != 0 {
		t.Fatalf("malformed environmental fixture produced results: %#v", items)
	}

	waste := client.callTool("lyon_waste_dropoff", map[string]any{
		"item": "pile", "location": location,
	})
	if items := objectList(t, dataObject(t, waste), "facilities"); len(items) != 0 {
		t.Fatalf("malformed waste fixture produced results: %#v", items)
	}
	client.close(true)
}

func testLocalQueryCustomTaxonomy(t *testing.T) {
	taxonomy := []byte(`version: 1
rules:
  - id: solvent
    category: paint
    priority: 100
    confidence: 0.95
    hazardous: true
    aliases: [solvent]
    patterns: ['\bsolvent\b']
  - id: mixed_glass
    category: glass
    priority: 90
    confidence: 0.90
    hazardous: false
    aliases: [mixed]
  - id: mixed_paint
    category: paint
    priority: 80
    confidence: 0.85
    hazardous: true
    aliases: [mixed]
`)
	facilities := map[string]any{"values": []map[string]any{
		{
			"id": "verified", "name": "Verified hours", "lat": 45.7601, "lon": 4.8501,
			"categories": []string{"paint"}, "opening_hours": "08:00-18:00", "open_status": "unknown",
		},
		{
			"id": "current-only", "name": "Current status only", "lat": 45.7602, "lon": 4.8502,
			"categories": []string{"paint"}, "open_status": "open",
		},
	}}
	client := startLocalQueryMCP(t, "local-query-custom-taxonomy", map[string]any{
		"datagrandlyon/waste_facilities.json": facilities,
	}, map[string][]byte{"waste-taxonomy.yaml": taxonomy})

	unknown := client.callTool("lyon_waste_dropoff", map[string]any{"item": "mystery"})
	assertStatus(t, unknown, "not_found")
	unknownClassification := objectValue(t, dataObject(t, unknown), "classification")
	if unknownClassification["category"] != "unknown" || len(objectList(t, dataObject(t, unknown), "facilities")) != 0 {
		t.Fatalf("unexpected unknown classification: %#v", unknown)
	}

	ambiguous := client.callTool("lyon_waste_dropoff", map[string]any{"item": "mixed"})
	assertStatus(t, ambiguous, "ambiguous")
	ambiguousClassification := objectValue(t, dataObject(t, ambiguous), "classification")
	if ambiguousClassification["category"] != "glass" || !reflect.DeepEqual(ambiguousClassification["candidates"], []any{"glass", "paint"}) {
		t.Fatalf("unexpected ambiguous classification: %#v", ambiguousClassification)
	}

	location := map[string]any{"latitude": 45.76, "longitude": 4.85}
	hazardous := client.callTool("lyon_waste_dropoff", map[string]any{
		"item": "solvent", "location": location, "open_at": "2026-07-20T12:00:00+02:00",
	})
	assertStatus(t, hazardous, "partial")
	classification := objectValue(t, dataObject(t, hazardous), "classification")
	if classification["category"] != "paint" || classification["hazardous"] != true {
		t.Fatalf("unexpected hazardous classification: %#v", classification)
	}
	assertWarningSource(t, hazardous, "PARTIAL_RESULT", "waste_opening_hours")
	items := objectList(t, dataObject(t, hazardous), "facilities")
	if len(items) != 1 || items[0]["id"] != "verified" {
		t.Fatalf("dated waste hours were not enforced: %#v", items)
	}

	closed := client.callTool("lyon_waste_dropoff", map[string]any{
		"item": "solvent", "location": location, "open_at": "2026-07-20T20:00:00+02:00",
	})
	assertStatus(t, closed, "partial")
	assertWarningSource(t, closed, "PARTIAL_RESULT", "waste_opening_hours")
	if items := objectList(t, dataObject(t, closed), "facilities"); len(items) != 0 {
		t.Fatalf("closed waste facilities were retained: %#v", items)
	}
	client.close(true)
}

func startLocalQueryMCP(t *testing.T, name string, overrides map[string]any, configFiles map[string][]byte) *mcpProcess {
	t.Helper()
	folder, env, values, err := testHarness.newEnvironment(name)
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
	for name, data := range configFiles {
		if writeErr := os.WriteFile(filepath.Join(values[configEnv], filepath.FromSlash(name)), data, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	client := startMCP(t, folder, mergeEnvironment(env, map[string]string{fixturesEnv: fixtures}))
	initializeMCP(t, client)
	return client
}
