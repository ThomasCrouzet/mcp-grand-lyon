package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReusablePlaceIDAcceptance(t *testing.T) {
	runCase(t, "place-id-reuse-tools", testPlaceIDReuseAcrossTools)
	runCase(t, "place-id-reuse-restart", testPlaceIDReuseAfterRestart)
	runCase(t, "place-id-identity-collision", testPlaceIDIdentityCollision)
	runCase(t, "place-id-storage-failure", testPlaceIDStorageFailure)
}

func testPlaceIDReuseAcrossTools(t *testing.T) {
	folder, env := startReusablePlaceEnvironment(t, "place-id-reuse-tools")
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	id := resolveSinglePlaceID(t, client, "Annex")

	trip := client.callTool("lyon_trip_options", map[string]any{
		"origin":      map[string]any{"place_id": id},
		"destination": map[string]any{"latitude": 45.7600, "longitude": 4.8500},
		"modes":       []string{"walk"},
	})
	options := objectList(t, dataObject(t, trip), "options")
	if len(options) != 1 || options[0]["mode"] != "walk" {
		t.Fatalf("resolved place ID did not drive a trip: %#v", trip)
	}

	parking := client.callTool("lyon_parking_options", map[string]any{
		"destination": map[string]any{"place_id": id}, "radius_m": 5000,
	})
	if len(objectList(t, dataObject(t, parking), "options")) == 0 {
		t.Fatalf("resolved place ID did not reach parking search: %#v", parking)
	}

	nearby := client.callTool("lyon_nearby_facilities", map[string]any{
		"location": map[string]any{"place_id": id}, "categories": []string{"toilet"}, "radius_m": 5000,
	})
	if len(objectList(t, dataObject(t, nearby), "items")) == 0 {
		t.Fatalf("resolved place ID did not reach nearby search: %#v", nearby)
	}
	client.close(true)
}

func testPlaceIDReuseAfterRestart(t *testing.T) {
	folder, env := startReusablePlaceEnvironment(t, "place-id-reuse-restart")
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	id := resolveSinglePlaceID(t, client, "Annex")
	before := walkDistanceForPlace(t, client, id)
	client.close(true)

	restarted := startMCP(t, folder, env)
	initializeMCP(t, restarted)
	after := walkDistanceForPlace(t, restarted, id)
	if before <= 0 || after != before {
		t.Fatalf("place ID walking distance changed after restart: before=%v after=%v", before, after)
	}
	restarted.close(true)
}

func testPlaceIDIdentityCollision(t *testing.T) {
	folder, env := startReusablePlaceEnvironment(t, "place-id-identity-collision")
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	result := client.callTool("lyon_resolve_place", map[string]any{"query": "Twin", "limit": 5})
	candidates := objectList(t, dataObject(t, result), "candidates")
	if len(candidates) != 2 {
		t.Fatalf("Twin candidates = %#v", candidates)
	}
	ids := stringFields(t, candidates, "id")
	wantIDs := map[string]bool{"photon:node:9001": true, "photon:way:9001": true}
	if !wantIDs[ids[0]] || !wantIDs[ids[1]] || ids[0] == ids[1] {
		t.Fatalf("colliding upstream identifiers were not disambiguated: %v", ids)
	}
	distances := map[string]int{}
	for _, id := range ids {
		distances[id] = walkDistanceForPlace(t, client, id)
	}
	if distances[ids[0]] == distances[ids[1]] || distances[ids[0]] <= 0 || distances[ids[1]] <= 0 {
		t.Fatalf("distinct provider objects resolved to the same coordinates: %v", distances)
	}
	client.close(true)

	restarted := startMCP(t, folder, env)
	initializeMCP(t, restarted)
	after := map[string]int{}
	for _, id := range ids {
		after[id] = walkDistanceForPlace(t, restarted, id)
	}
	if !reflect.DeepEqual(after, distances) {
		t.Fatalf("colliding identifiers did not survive restart: before=%v after=%v", distances, after)
	}
	restarted.close(true)
}

func startReusablePlaceEnvironment(t *testing.T, name string) (string, []string) {
	t.Helper()
	folder, env, _, err := testHarness.newEnvironment(name)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, fixtures); err != nil {
		t.Fatal(err)
	}
	feature := func(osmType string, osmID float64, name string, longitude, latitude float64) map[string]any {
		return map[string]any{
			"type":     "Feature",
			"geometry": map[string]any{"type": "Point", "coordinates": []any{longitude, latitude}},
			"properties": map[string]any{
				"name": name, "city": "Lyon", "postcode": "69000",
				"osm_type": osmType, "osm_id": osmID, "confidence": 0.9,
			},
		}
	}
	document := map[string]any{"type": "FeatureCollection", "features": []any{
		feature("poi", 42, "Annex", 4.8355, 45.7675),
		feature("node", 9001, "Twin", 4.8500, 45.7500),
		feature("way", 9001, "Twin", 4.8700, 45.7700),
	}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "photon", "part_dieu.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return folder, mergeEnvironment(env, map[string]string{fixturesEnv: fixtures})
}

func testPlaceIDStorageFailure(t *testing.T) {
	folder, env := startReusablePlaceEnvironment(t, "place-id-storage-failure")
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	// The fixture database path is shared by the process environment.
	database := openAuditDatabase(t, filepath.Join(folder, "app.db"))
	execAuditSQL(t, database, `DROP TABLE entities`)
	result := client.callTool("lyon_resolve_place", map[string]any{"query": "Annex", "limit": 5})
	assertStatus(t, result, "not_found")
	assertWarningSource(t, result, "SOURCE_UNAVAILABLE", "places")
	if candidates := objectList(t, dataObject(t, result), "candidates"); len(candidates) != 0 {
		t.Fatalf("unresolvable place IDs were advertised: %#v", candidates)
	}
	client.close(true)
}

func resolveSinglePlaceID(t *testing.T, client *mcpProcess, query string) string {
	t.Helper()
	result := client.callTool("lyon_resolve_place", map[string]any{"query": query, "limit": 5})
	candidates := objectList(t, dataObject(t, result), "candidates")
	if len(candidates) != 1 {
		t.Fatalf("%s candidates = %#v", query, candidates)
	}
	id := stringValue(candidates[0]["id"])
	if id == "" {
		t.Fatalf("%s candidate has no reusable ID: %#v", query, candidates[0])
	}
	return id
}

func walkDistanceForPlace(t *testing.T, client *mcpProcess, id string) int {
	t.Helper()
	result := client.callTool("lyon_trip_options", map[string]any{
		"origin":      map[string]any{"place_id": id},
		"destination": map[string]any{"latitude": 45.7600, "longitude": 4.8500},
		"modes":       []string{"walk"},
	})
	options := objectList(t, dataObject(t, result), "options")
	if len(options) != 1 {
		t.Fatalf("place ID %q produced no walk option: %#v", id, result)
	}
	distance, ok := options[0]["walking_distance_m"].(float64)
	if !ok {
		t.Fatalf("place ID %q produced no walking distance: %#v", id, options[0])
	}
	return int(distance)
}
