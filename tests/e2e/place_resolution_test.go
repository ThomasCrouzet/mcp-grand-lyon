package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestGoPlaceResolutionAcceptance(t *testing.T) {
	runCase(t, "place-multiple-types", testPlaceMultipleTypes)
	runCase(t, "place-ranking-and-ambiguity", testPlaceRankingAndAmbiguity)
	runCase(t, "place-type-and-near", testPlaceTypeAndNear)
	runCase(t, "place-accessibility-unknowns", testPlaceAccessibilityUnknowns)
}

func testPlaceMultipleTypes(t *testing.T) {
	client := startPlaceResolutionMCP(t, "place-multiple-types")
	result := client.callTool("lyon_resolve_place", map[string]any{"query": "Bellecour", "limit": 5})
	assertStatus(t, result, "partial")
	candidates := objectList(t, dataObject(t, result), "candidates")
	if len(candidates) != 2 {
		t.Fatalf("Bellecour candidates = %#v", candidates)
	}
	types := []string{stringValue(candidates[0]["type"]), stringValue(candidates[1]["type"])}
	if !reflect.DeepEqual(types, []string{"transport_stop", "point_of_interest"}) {
		t.Fatalf("Bellecour types = %v", types)
	}
	for _, candidate := range candidates {
		if candidate["name"] != "Bellecour" {
			t.Fatalf("nonexact candidate leaked into exact result: %#v", candidate)
		}
	}
	client.close(true)
}

func testPlaceRankingAndAmbiguity(t *testing.T) {
	client := startPlaceResolutionMCP(t, "place-ranking-and-ambiguity")
	result := client.callTool("lyon_resolve_place", map[string]any{
		"query": "Central", "near": map[string]any{"latitude": 45.7500, "longitude": 4.8500}, "limit": 5,
	})
	assertStatus(t, result, "partial")
	candidates := objectList(t, dataObject(t, result), "candidates")
	ids := stringFields(t, candidates, "id")
	want := []string{"photon:central-near", "photon:central-far", "photon:central-park"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ranked IDs = %v, want %v", ids, want)
	}
	nearDistance, nearOK := candidates[0]["distance_m"].(float64)
	farDistance, farOK := candidates[1]["distance_m"].(float64)
	if !nearOK || !farOK || nearDistance >= farDistance {
		t.Fatalf("near distances are not ordered: %#v", candidates)
	}

	ambiguous := client.callTool("lyon_resolve_place", map[string]any{"query": "Gare", "limit": 5})
	assertStatus(t, ambiguous, "ambiguous")
	assertWarningSource(t, ambiguous, "AMBIGUOUS_PLACE", "places")
	names := stringFields(t, objectList(t, dataObject(t, ambiguous), "candidates"), "name")
	if !reflect.DeepEqual(names, []string{"Gare Alpha", "Gare Beta"}) {
		t.Fatalf("ambiguous names = %v", names)
	}
	client.close(true)
}

func testPlaceTypeAndNear(t *testing.T) {
	client := startPlaceResolutionMCP(t, "place-type-and-near")
	missingType := client.callTool("lyon_resolve_place", map[string]any{
		"query": "Bellecour", "types": []string{"district"}, "limit": 5,
	})
	assertStatus(t, missingType, "not_found")
	if candidates := objectList(t, dataObject(t, missingType), "candidates"); len(candidates) != 0 {
		t.Fatalf("requested type leaked candidates: %#v", candidates)
	}

	unknownNear := client.callTool("lyon_resolve_place", map[string]any{
		"query": "Central", "near": map[string]any{"query": "Unfindable near"}, "limit": 5,
	})
	assertStatus(t, unknownNear, "partial")
	assertWarningSource(t, unknownNear, "PLACE_NOT_FOUND", "places")
	if candidates := objectList(t, dataObject(t, unknownNear), "candidates"); len(candidates) != 3 {
		t.Fatalf("unknown near removed candidates: %#v", candidates)
	}
	client.close(true)
}

func testPlaceAccessibilityUnknowns(t *testing.T) {
	client := startPlaceResolutionMCP(t, "place-accessibility-unknowns")
	result := client.callTool("lyon_accessibility_check", map[string]any{
		"origin":      map[string]any{"query": "Missing origin name"},
		"destination": map[string]any{"profile_place": "missing-destination-name"},
		"needs":       []string{"wheelchair"},
	})
	assertStatus(t, result, "partial")
	data := dataObject(t, result)
	if data["overall_status"] != "unknown" {
		t.Fatalf("unknown places became accessible: %#v", data)
	}
	segments := objectList(t, data, "segments")
	if len(segments) != 2 || segments[0]["name"] != "Missing origin name" || segments[1]["name"] != "missing-destination-name" {
		t.Fatalf("unknown place labels were lost: %#v", segments)
	}
	for _, segment := range segments {
		if segment["status"] != "unknown" || len(objectList(t, segment, "evidence")) != 0 {
			t.Fatalf("unknown segment gained accessibility evidence: %#v", segment)
		}
	}
	client.close(true)
}

func startPlaceResolutionMCP(t *testing.T, name string) *mcpProcess {
	t.Helper()
	folder, env, _, err := testHarness.newEnvironment(name)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, fixtures); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(placeResolutionPhotonFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "photon", "part_dieu.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{fixturesEnv: fixtures})
	if _, err := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--from-file", filepath.Join(fixtures, "gtfs", "mini_gtfs.zip")); err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	return client
}

func placeResolutionPhotonFixture() map[string]any {
	feature := func(id, name string, longitude, latitude, confidence float64) map[string]any {
		return map[string]any{
			"type":     "Feature",
			"geometry": map[string]any{"type": "Point", "coordinates": []any{longitude, latitude}},
			"properties": map[string]any{
				"name": name, "city": "Lyon", "postcode": "69000", "osm_id": id, "confidence": confidence,
			},
		}
	}
	return map[string]any{"type": "FeatureCollection", "features": []any{
		feature("bellecour-poi", "Bellecour", 4.8321, 45.7579, 0.95),
		feature("central-far", "Central", 4.8800, 45.7800, 0.80),
		feature("central-near", "Central", 4.8501, 45.7501, 0.80),
		feature("central-park", "Central Park", 4.8502, 45.7502, 1.00),
		feature("gare-alpha", "Gare Alpha", 4.8600, 45.7600, 0.80),
		feature("gare-beta", "Gare Beta", 4.8610, 45.7610, 0.80),
	}}
}
