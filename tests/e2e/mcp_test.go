package e2e_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

var publicTools = []string{
	"lyon_accessibility_check",
	"lyon_environment_brief",
	"lyon_mobility_status",
	"lyon_nearby_facilities",
	"lyon_next_departures",
	"lyon_parking_options",
	"lyon_personal_briefing",
	"lyon_resolve_place",
	"lyon_trip_options",
	"lyon_waste_dropoff",
}

type mcpProcess struct {
	t          *testing.T
	command    *exec.Cmd
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	stderr     *os.File
	transcript *os.File
	folder     string
	env        []string
	nextID     int
	start      time.Time
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type toolListResult struct {
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

type toolCallResult struct {
	IsError           bool             `json:"isError"`
	StructuredContent map[string]any   `json:"structuredContent"`
	Content           []map[string]any `json:"content"`
}

type transitCases struct {
	Transit []transitCase `json:"transit"`
	Parking struct {
		Values []map[string]any `json:"values"`
	} `json:"parking"`
}

type transitCase struct {
	Name              string   `json:"name"`
	Realtime          string   `json:"realtime"`
	At                string   `json:"at"`
	Expected          []string `json:"expected"`
	StaticUnavailable bool     `json:"static_unavailable"`
}

func TestGoProcessAcceptance(t *testing.T) {
	runCase(t, "installed-setup", testInstalledSetup)
	runCase(t, "all-tools-and-eof", testAllToolsAndEOF)
	runCase(t, "nested-preference-defaults", testNestedPreferenceDefaults)
	runCase(t, "wrong-stop-and-unknown-place", testWrongStopAndUnknownPlace)
	runCase(t, "transit-cases", testTransitCases)
	runCase(t, "mixed-parking", testMixedParking)
	runCase(t, "waste-constraints", testWasteConstraints)
	runCase(t, "gtfs-import-rollback", testGTFSImportRollback)
	runCase(t, "startup-invalid-config", testInvalidConfigStartup)
	runCase(t, "startup-invalid-database", testInvalidDatabaseStartup)
}

func testNestedPreferenceDefaults(t *testing.T) {
	folder, env, _, err := preparedEnvironment("nested-preference-defaults")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	payload := func(preferences any) map[string]any {
		request := map[string]any{
			"origin":       map[string]any{"latitude": 45.767, "longitude": 4.836},
			"destination":  map[string]any{"profile_place": "home"},
			"departure_at": "2026-07-20T08:00:00+02:00",
			"modes":        []string{"velov"},
		}
		if preferences != nil {
			request["preferences"] = preferences
		}
		return request
	}

	omitted := client.callTool("lyon_trip_options", payload(nil))
	empty := client.callTool("lyon_trip_options", payload(map[string]any{}))
	if got, want := optionModes(t, empty), optionModes(t, omitted); !reflect.DeepEqual(got, want) {
		t.Fatalf("empty preferences modes = %v, want omitted defaults %v", got, want)
	}

	explicit := client.callTool("lyon_trip_options", payload(map[string]any{
		"minimum_velov_bikes": 0,
		"avoid_disruptions":   false,
	}))
	if modes := optionModes(t, explicit); !contains(modes, "velov") {
		t.Fatalf("explicit zero/false preferences were overwritten: modes=%v", modes)
	}
	client.close(true)
}

func testInstalledSetup(t *testing.T) {
	folder, env, values, err := testHarness.newEnvironment("installed-setup")
	if err != nil {
		t.Fatal(err)
	}
	before, err := treeHashes(filepath.Dir(testHarness.binary))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHarness.runCommand(folder, env, 90*time.Second,
		testHarness.binary, "setup", "--yes", "--offline", "--force-config"); err != nil {
		t.Fatal(err)
	}
	after, err := treeHashes(filepath.Dir(testHarness.binary))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("setup changed the installation tree: before=%v after=%v", before, after)
	}
	if _, err := os.Stat(values[databaseEnv]); err != nil {
		t.Fatalf("setup did not create the database: %v", err)
	}
	for _, name := range []string{"settings.yaml", "profiles.yaml", "sources.yaml", "waste-taxonomy.yaml"} {
		if _, err := os.Stat(filepath.Join(values[configEnv], name)); err != nil {
			t.Fatalf("setup did not create %s: %v", name, err)
		}
	}
	secretsPath := filepath.Join(values[configEnv], "secrets.env")
	secretsInfo, err := os.Stat(secretsPath)
	if err != nil {
		t.Fatalf("setup did not create secrets.env: %v", err)
	}
	if mode := secretsInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("secrets.env mode = %04o, want 0600", mode)
	}
	fileData := strings.Join([]string{
		dataEnv + "=" + filepath.Join(folder, "file-data"),
		offlineEnv + "=false",
		"",
	}, "\n")
	if err := os.WriteFile(secretsPath, []byte(fileData), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := testHarness.runCommand(folder, env, 30*time.Second,
		testHarness.binary, "client-config", "--bin")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(values[configEnv], "mcp-client.snippet.json"))
	if err != nil {
		t.Fatalf("read generated client configuration: %v", err)
	}
	assertClientConfig(t, generated, values)
	assertClientConfig(t, result.Stdout, values)
}

func assertClientConfig(t *testing.T, raw []byte, values map[string]string) {
	t.Helper()
	var document struct {
		Servers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode client configuration: %v\n%s", err, raw)
	}
	server, ok := document.Servers["grand-lyon"]
	if !ok {
		t.Fatal("client configuration has no grand-lyon server")
	}
	commandPath, err := filepath.EvalSymlinks(server.Command)
	if err != nil {
		t.Fatalf("resolve configured executable: %v", err)
	}
	binaryPath, err := filepath.EvalSymlinks(testHarness.binary)
	if err != nil {
		t.Fatal(err)
	}
	if commandPath != binaryPath {
		t.Fatalf("configured command = %q, want %q", commandPath, binaryPath)
	}
	if !reflect.DeepEqual(server.Args, []string{"serve", "--transport", "stdio"}) {
		t.Fatalf("configured args = %v", server.Args)
	}
	wantEnvironment := map[string]string{
		configEnv:   values[configEnv],
		dataEnv:     values[dataEnv],
		databaseEnv: values[databaseEnv],
		offlineEnv:  "true",
	}
	if !reflect.DeepEqual(server.Env, wantEnvironment) {
		t.Fatalf("configured env = %#v, want %#v", server.Env, wantEnvironment)
	}
	encoded := string(raw)
	if strings.Contains(encoded, "DATAGRANDLYON_") || strings.Contains(encoded, "PASSWORD") {
		t.Fatal("client configuration contains credential fields")
	}
}

func testAllToolsAndEOF(t *testing.T) {
	folder, env, _, err := preparedEnvironment("all-tools")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	assertToolList(t, client)

	place := client.callTool("lyon_resolve_place", map[string]any{"query": "Bellecour", "limit": 3})
	assertStatus(t, place, "partial")
	assertOfflineFixture(t, place)
	candidates := objectList(t, dataObject(t, place), "candidates")
	if !containsFieldValue(candidates, "name", "Bellecour") {
		t.Fatalf("Bellecour is absent from candidates: %#v", candidates)
	}

	departures := client.callTool("lyon_next_departures", map[string]any{
		"stop": map[string]any{"query": "Bellecour"}, "line": "A",
		"at": "2026-07-20T08:00:00+02:00", "limit": 3,
	})
	assertStatus(t, departures, "partial")
	assertOfflineFixture(t, departures)
	departureItems := objectList(t, dataObject(t, departures), "departures")
	assertStringFields(t, departureItems, "expected_at", []string{"2026-07-20T08:05:00+02:00"})
	if value, _ := departureItems[0]["realtime"].(bool); value {
		t.Fatal("the simulated fixture departure must not be realtime")
	}

	mobility := client.callTool("lyon_mobility_status", map[string]any{
		"lines":   []string{"A"},
		"include": []string{"transit", "accessibility", "traffic", "roadworks"},
	})
	assertStatus(t, mobility, "partial")
	assertOfflineFixture(t, mobility)
	mobilityData := dataObject(t, mobility)
	if !containsFieldValue(objectList(t, mobilityData, "alerts"), "id", "SX-1") {
		t.Fatal("mobility result does not contain fixture alert SX-1")
	}
	if len(objectList(t, mobilityData, "road_events")) == 0 || len(objectList(t, mobilityData, "traffic_conditions")) == 0 {
		t.Fatal("mobility result must contain road and traffic fixture data")
	}

	trip := client.callTool("lyon_trip_options", map[string]any{
		"origin":       map[string]any{"profile_place": "home"},
		"destination":  map[string]any{"profile_place": "work"},
		"departure_at": "2026-07-20T08:00:00+02:00",
		"modes":        []string{"walk", "velov", "tcl"},
	})
	assertStatus(t, trip, "partial")
	assertOfflineFixture(t, trip)
	tripData := dataObject(t, trip)
	if tripData["recommended_mode"] != "velov" {
		t.Fatalf("recommended mode = %v, want velov", tripData["recommended_mode"])
	}
	options := objectList(t, tripData, "options")
	if !containsFieldValue(options, "mode", "walk") || !containsFieldValue(options, "mode", "velov") {
		t.Fatalf("trip options do not contain walk and velov: %#v", options)
	}
	for _, option := range options {
		if option["realtime"] == true {
			t.Fatalf("simulated trip option is marked realtime: %#v", option)
		}
	}

	parking := client.callTool("lyon_parking_options", map[string]any{
		"destination": map[string]any{"latitude": 45.7675, "longitude": 4.8355}, "radius_m": 500,
	})
	assertStatus(t, parking, "partial")
	assertOfflineFixture(t, parking)
	parkingItems := objectList(t, dataObject(t, parking), "options")
	if len(parkingItems) != 1 || parkingItems[0]["id"] != "p1" || parkingItems[0]["available_spaces"] != float64(42) || parkingItems[0]["realtime"] != false {
		t.Fatalf("unexpected parking result: %#v", parkingItems)
	}

	accessibility := client.callTool("lyon_accessibility_check", map[string]any{
		"origin":      map[string]any{"query": "Bellecour"},
		"destination": map[string]any{"query": "Part-Dieu"},
		"needs":       []string{"wheelchair"},
	})
	assertStatus(t, accessibility, "partial")
	assertOfflineFixture(t, accessibility)
	accessibilityData := dataObject(t, accessibility)
	if accessibilityData["overall_status"] != "inaccessible" || len(objectList(t, accessibilityData, "segments")) != 2 {
		t.Fatalf("unexpected accessibility result: %#v", accessibilityData)
	}
	accessibilitySegments := indexByField(objectList(t, accessibilityData, "segments"), "name")
	bellecour := accessibilitySegments["Bellecour"]
	if bellecour["status"] != "inaccessible" || !strings.Contains(strings.ToLower(stringValue(bellecour["notes"])), "incident") {
		t.Fatalf("Bellecour incident evidence was weakened: %#v", bellecour)
	}
	assertEvidence(t, objectList(t, bellecour, "evidence"), map[string]any{
		"source": "tcl_accessibility_alerts", "fact": "incident", "confidence": "high",
		"detail": "Ascenseur quai direction Perrache", "status": "inaccessible",
	})
	assertEvidence(t, objectList(t, bellecour, "evidence"), map[string]any{
		"source": "gtfs_wheelchair", "fact": "wheelchair_boarding=1", "confidence": "low",
		"status": "partially_accessible",
	})
	partDieu := accessibilitySegments["Part-Dieu"]
	if partDieu["status"] != "partially_accessible" {
		t.Fatalf("unexpected Part-Dieu accessibility: %#v", partDieu)
	}
	assertEvidence(t, objectList(t, partDieu, "evidence"), map[string]any{
		"source": "gtfs_wheelchair", "fact": "wheelchair_boarding=1", "confidence": "low",
		"status": "partially_accessible",
	})

	facilities := client.callTool("lyon_nearby_facilities", map[string]any{
		"location":   map[string]any{"latitude": 45.777, "longitude": 4.855},
		"categories": []string{"toilet", "park"}, "radius_m": 2000,
	})
	assertStatus(t, facilities, "partial")
	assertOfflineFixture(t, facilities)
	facilityItems := objectList(t, dataObject(t, facilities), "items")
	if !containsFieldValue(facilityItems, "id", "f1") || !containsFieldValue(facilityItems, "id", "f3") {
		t.Fatalf("unexpected facility result: %#v", facilityItems)
	}

	environment := client.callTool("lyon_environment_brief", map[string]any{
		"location": map[string]any{"query": "Lyon"},
		"at":       "2026-07-20T08:00:00+02:00", "indicators": []string{"pollen", "air_quality", "heat"},
	})
	assertStatus(t, environment, "partial")
	assertOfflineFixture(t, environment)
	indicators := objectList(t, dataObject(t, environment), "indicators")
	byIndicator := indexByField(indicators, "indicator")
	if len(indicators) != 2 || byIndicator["pollen"]["value"] != "modéré" || byIndicator["air_quality"]["value"] != float64(42) {
		t.Fatalf("unexpected environment result: %#v", indicators)
	}
	assertWarningCode(t, environment, "UNSUPPORTED_INDICATOR")

	waste := client.callTool("lyon_waste_dropoff", map[string]any{
		"item": "batterie de vélo électrique", "location": map[string]any{"profile_place": "home"},
	})
	assertStatus(t, waste, "ambiguous")
	assertOfflineFixture(t, waste)
	wasteData := dataObject(t, waste)
	classification := objectValue(t, wasteData, "classification")
	if classification["category"] != "portable_battery" || classification["hazardous"] != true {
		t.Fatalf("unexpected waste classification: %#v", classification)
	}
	if len(objectList(t, wasteData, "facilities")) == 0 {
		t.Fatal("waste result must contain a matching fixture facility")
	}

	briefing := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "weekday_morning", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false,
	})
	assertStatus(t, briefing, "partial")
	assertOfflineFixture(t, briefing)
	briefingData := dataObject(t, briefing)
	if briefingData["profile"] != "weekday_morning" {
		t.Fatalf("unexpected briefing profile: %v", briefingData["profile"])
	}
	facts := objectValue(t, briefingData, "facts")
	if _, ok := facts["mobility"].(map[string]any); !ok {
		t.Fatalf("briefing has no mobility facts: %#v", facts)
	}

	client.close(true)
}

func testWrongStopAndUnknownPlace(t *testing.T) {
	folder, env, _, err := preparedEnvironment("strict-place")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	unknown := client.callTool("lyon_resolve_place", map[string]any{"query": "qzxqzx unknown place"})
	assertStatus(t, unknown, "not_found")
	assertOfflineFixture(t, unknown)
	if len(objectList(t, dataObject(t, unknown), "candidates")) != 0 {
		t.Fatal("an unknown place returned candidates")
	}
	partDieu := client.callTool("lyon_next_departures", map[string]any{
		"stop": map[string]any{"query": "Part-Dieu"}, "line": "A",
		"at": "2026-07-20T08:00:00+02:00", "limit": 3,
	})
	assertOfflineFixture(t, partDieu)
	departureItems := objectList(t, dataObject(t, partDieu), "departures")
	assertStringFields(t, departureItems, "expected_at", []string{"2026-07-20T08:12:00+02:00"})
	for _, item := range departureItems {
		if item["stop_id"] == "BEL1" || item["realtime"] == true {
			t.Fatalf("wrong-stop data leaked into Part-Dieu results: %#v", item)
		}
	}
	client.close(true)
}

func testTransitCases(t *testing.T) {
	cases := loadTransitCases(t)
	datedZip := filepath.Join(testHarness.artifact, "dated.zip")
	if err := makeZip(datedZip, filepath.Join(testHarness.fixtures, "gtfs", "dated"), nil); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range cases.Transit {
		testCase := testCase
		t.Run(testCase.Name, func(t *testing.T) {
			folder, env, values, err := testHarness.newEnvironment("transit-" + testCase.Name)
			if err != nil {
				t.Fatal(err)
			}
			override := filepath.Join(folder, "fixtures")
			if err := copyTree(testHarness.fixtures, override); err != nil {
				t.Fatal(err)
			}
			siriPath := filepath.Join(override, "siri", "estimated_timetable.json")
			switch testCase.Realtime {
			case "wrong-line":
				data, err := os.ReadFile(siriPath)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.ReplaceAll(data, []byte("\"LineRef\": \"A\""), []byte("\"LineRef\": \"C12\""))
				if err := os.WriteFile(siriPath, data, 0o644); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(siriPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
				dataPusherPath := filepath.Join(override, "datagrandlyon", "tcl_departures.json")
				if err := os.Remove(dataPusherPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
			case "invalid":
				if err := os.WriteFile(siriPath, []byte("{"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			env = mergeEnvironment(env, map[string]string{fixturesEnv: override})
			if !testCase.StaticUnavailable {
				if _, err := testHarness.runCommand(folder, env, 60*time.Second,
					testHarness.binary, "sync", "gtfs", "--from-file", datedZip); err != nil {
					t.Fatal(err)
				}
			} else if _, err := os.Stat(values[databaseEnv]); err == nil {
				t.Fatal("static-unavailable case must start with an empty database")
			}
			client := startMCP(t, folder, env)
			initializeMCP(t, client)
			limit := len(testCase.Expected)
			if limit < 1 {
				limit = 1
			}
			stop := map[string]any{"place_id": "gtfs:stop:BEL1"}
			if testCase.StaticUnavailable {
				stop = map[string]any{"latitude": 45.7578, "longitude": 4.8320}
			}
			result := client.callTool("lyon_next_departures", map[string]any{
				"stop": stop, "line": "A",
				"direction": "Vaulx", "at": testCase.At, "limit": limit,
			})
			assertOfflineFixture(t, result)
			items := objectList(t, dataObject(t, result), "departures")
			assertStringFields(t, items, "expected_at", testCase.Expected)
			for _, item := range items {
				if item["line_name"] != "A" || item["realtime"] != false {
					t.Fatalf("fallback result is not theoretical line A: %#v", item)
				}
			}
			if len(items) > 0 {
				assertStatus(t, result, "partial")
				assertWarningCode(t, result, "STALE_DATA", "PARTIAL_RESULT")
			} else if testCase.StaticUnavailable {
				assertStatus(t, result, "unavailable")
			} else {
				assertStatus(t, result, "not_found")
			}
			client.close(true)
		})
	}
}

func testMixedParking(t *testing.T) {
	cases := loadTransitCases(t)
	folder, env, _, err := testHarness.newEnvironment("mixed-parking")
	if err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, override); err != nil {
		t.Fatal(err)
	}
	parkingDocument, err := json.Marshal(map[string]any{"values": cases.Parking.Values})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(override, "datagrandlyon", "parkings.json"), parkingDocument, 0o644); err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{fixturesEnv: override})
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	arguments := map[string]any{
		"destination": map[string]any{"latitude": 45.7675, "longitude": 4.8355}, "radius_m": 100,
	}
	result := client.callTool("lyon_parking_options", arguments)
	assertStatus(t, result, "partial")
	assertOfflineFixture(t, result)
	items := objectList(t, dataObject(t, result), "options")
	byID := indexByField(items, "id")
	wantIDs := []string{"capacity", "live", "pr", "unknown", "zero"}
	gotIDs := sortedKeys(byID)
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("parking IDs = %v, want %v", gotIDs, wantIDs)
	}
	for _, item := range items {
		if distance, _ := item["distance_m"].(float64); distance > 100 {
			t.Fatalf("parking outside radius: %#v", item)
		}
		if item["realtime"] != false {
			t.Fatalf("simulated parking is marked realtime: %#v", item)
		}
	}
	if byID["zero"]["available_spaces"] != float64(0) || byID["zero"]["status"] != "full" {
		t.Fatalf("zero availability was not preserved: %#v", byID["zero"])
	}
	if byID["capacity"]["capacity"] != float64(800) {
		t.Fatalf("capacity was not preserved: %#v", byID["capacity"])
	}
	for _, id := range []string{"capacity", "unknown", "pr"} {
		if byID[id]["available_spaces"] != nil || byID[id]["status"] != "unknown" || byID[id]["realtime"] != false {
			t.Fatalf("unknown availability changed for %s: %#v", id, byID[id])
		}
	}
	filtered := client.callTool("lyon_parking_options", map[string]any{
		"destination": arguments["destination"], "radius_m": 100, "minimum_spaces": 1,
	})
	filteredIDs := sortedKeys(indexByField(objectList(t, dataObject(t, filtered), "options"), "id"))
	if !reflect.DeepEqual(filteredIDs, []string{"capacity", "live", "pr", "unknown"}) {
		t.Fatalf("minimum_spaces result = %v", filteredIDs)
	}
	typed := client.callTool("lyon_parking_options", map[string]any{
		"destination": arguments["destination"], "radius_m": 100, "types": []string{"park_and_ride"},
	})
	typedItems := objectList(t, dataObject(t, typed), "options")
	if len(typedItems) != 1 || typedItems[0]["id"] != "pr" {
		t.Fatalf("park-and-ride filter result = %#v", typedItems)
	}
	client.close(true)
}

func testWasteConstraints(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("waste-constraints")
	if err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, override); err != nil {
		t.Fatal(err)
	}
	fixture := map[string]any{"values": []map[string]any{
		{
			"id": "unknown-acceptance", "name": "Unknown acceptance",
			"lat": 45.7601, "lon": 4.8501, "categories": []string{}, "open_status": "open",
		},
		{
			"id": "verified-hours", "name": "Verified hours",
			"lat": 45.7602, "lon": 4.8502, "categories": []string{"portable_battery"},
			"open_status": "open", "opening_hours": "08:00-18:00",
		},
		{
			"id": "unverified-hours", "name": "Unverified hours",
			"lat": 45.7603, "lon": 4.8503, "categories": []string{"portable_battery"}, "open_status": "open",
		},
	}}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(override, "datagrandlyon", "waste_facilities.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{fixturesEnv: override})
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	baseArguments := map[string]any{
		"item": "pile", "location": map[string]any{"latitude": 45.76, "longitude": 4.85}, "radius_m": 1000,
	}
	result := client.callTool("lyon_waste_dropoff", baseArguments)
	assertStatus(t, result, "partial")
	assertOfflineFixture(t, result)
	if result["degraded"] != true {
		t.Fatalf("unknown waste acceptance is not degraded: %#v", result)
	}
	assertWarningSource(t, result, "PARTIAL_RESULT", "waste_acceptance")
	items := indexByField(objectList(t, dataObject(t, result), "facilities"), "id")
	unknown := items["unknown-acceptance"]
	accepted, acceptedOK := unknown["accepted_categories"].([]any)
	if unknown == nil || !acceptedOK || len(accepted) != 0 {
		t.Fatalf("empty accepted categories were changed: %#v", unknown)
	}
	classification := objectValue(t, dataObject(t, result), "classification")
	if classification["hazardous"] != true || classification["category"] != "portable_battery" {
		t.Fatalf("unexpected waste classification: %#v", classification)
	}

	openArguments := mapsClone(baseArguments)
	openArguments["open_at"] = "2026-07-20T12:00:00+02:00"
	open := client.callTool("lyon_waste_dropoff", openArguments)
	assertStatus(t, open, "partial")
	assertOfflineFixture(t, open)
	assertWarningSource(t, open, "PARTIAL_RESULT", "waste_opening_hours")
	openItems := objectList(t, dataObject(t, open), "facilities")
	if len(openItems) != 1 || openItems[0]["id"] != "verified-hours" {
		t.Fatalf("dated opening hours were not enforced: %#v", openItems)
	}

	closedArguments := mapsClone(baseArguments)
	closedArguments["open_at"] = "2026-07-20T20:00:00+02:00"
	closed := client.callTool("lyon_waste_dropoff", closedArguments)
	assertStatus(t, closed, "partial")
	assertOfflineFixture(t, closed)
	assertWarningSource(t, closed, "PARTIAL_RESULT", "waste_opening_hours")
	if items := objectList(t, dataObject(t, closed), "facilities"); len(items) != 0 {
		t.Fatalf("closed waste facility was retained: %#v", items)
	}
	client.close(true)
}

func testGTFSImportRollback(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("gtfs-rollback")
	if err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, override); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(override, "siri", "estimated_timetable.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	env = mergeEnvironment(env, map[string]string{fixturesEnv: override})
	validZip := filepath.Join(testHarness.fixtures, "gtfs", "mini_gtfs.zip")
	if _, err := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--from-file", validZip); err != nil {
		t.Fatal(err)
	}
	before := departuresForRollback(t, folder, env)
	invalidZip := filepath.Join(folder, "invalid-gtfs.zip")
	transform := func(name string, data []byte) []byte {
		if name != "stop_times.txt" {
			return data
		}
		return []byte("trip_id,arrival_time,departure_time,stop_id,stop_sequence\nA1,08:04:00,08:04:00,BEL1,1\nA1,not-a-time,not-a-time,PD1,2\n")
	}
	if err := makeZip(invalidZip, filepath.Join(testHarness.fixtures, "gtfs", "dated"), transform); err != nil {
		t.Fatal(err)
	}
	result, runErr := testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--from-file", invalidZip)
	if runErr == nil || result.ReturnCode == 0 {
		t.Fatal("invalid GTFS import succeeded")
	}
	after := departuresForRollback(t, folder, env)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("failed GTFS import changed departures: before=%v after=%v", before, after)
	}
}

func testInvalidConfigStartup(t *testing.T) {
	folder, env, values, err := testHarness.newEnvironment("invalid-config")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(values[configEnv], "profiles.yaml"), []byte("profiles: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	assertStartupFailure(t, folder, env)
}

func testInvalidDatabaseStartup(t *testing.T) {
	folder, env, values, err := testHarness.newEnvironment("invalid-database")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(values[databaseEnv], []byte("not a SQLite database\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertStartupFailure(t, folder, env)
}

func preparedEnvironment(name string) (string, []string, map[string]string, error) {
	folder, env, values, err := testHarness.newEnvironment(name)
	if err != nil {
		return "", nil, nil, err
	}
	_, err = testHarness.runCommand(folder, env, 60*time.Second,
		testHarness.binary, "sync", "gtfs", "--from-file", filepath.Join(testHarness.fixtures, "gtfs", "mini_gtfs.zip"))
	return folder, env, values, err
}

func departuresForRollback(t *testing.T, folder string, env []string) []string {
	t.Helper()
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	result := client.callTool("lyon_next_departures", map[string]any{
		"stop": map[string]any{"place_id": "gtfs:stop:PD1"}, "line": "A",
		"at": "2026-07-20T08:00:00+02:00", "limit": 3,
	})
	assertOfflineFixture(t, result)
	items := objectList(t, dataObject(t, result), "departures")
	values := stringFields(t, items, "expected_at")
	client.close(true)
	return values
}

func assertStartupFailure(t *testing.T, folder string, env []string) {
	t.Helper()
	result, err := testHarness.runCommand(folder, env, 15*time.Second,
		testHarness.binary, "serve", "--transport", "stdio", "--quiet")
	if err == nil || result.ReturnCode == 0 {
		t.Fatal("invalid startup input returned success")
	}
	if len(result.Stdout) != 0 {
		t.Fatalf("startup failure wrote to stdout: %q", result.Stdout)
	}
	if len(bytes.TrimSpace(result.Stderr)) == 0 {
		t.Fatal("startup failure did not explain the error on stderr")
	}
}

func startMCP(t *testing.T, folder string, env []string) *mcpProcess {
	t.Helper()
	transcript, err := os.Create(filepath.Join(folder, "protocol.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.Create(filepath.Join(folder, "stderr.log"))
	if err != nil {
		transcript.Close()
		t.Fatal(err)
	}
	cmd := exec.Command(testHarness.binary, "serve", "--transport", "stdio", "--quiet")
	cmd.Dir = folder
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	client := &mcpProcess{
		t: t, command: cmd, stdin: stdin, stdout: bufio.NewReader(stdoutPipe),
		stderr: stderr, transcript: transcript, folder: folder, env: env, start: time.Now(),
	}
	t.Cleanup(func() {
		if client.command.ProcessState == nil {
			client.close(false)
		}
	})
	return client
}

func initializeMCP(t *testing.T, client *mcpProcess) {
	t.Helper()
	result := client.request("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "go-e2e", "version": "1.0"},
	})
	var initialized struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(result, &initialized); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}
	if initialized.ServerInfo.Name != "grand-lyon-mcp" || initialized.ServerInfo.Version == "" {
		t.Fatalf("unexpected server info: %#v", initialized.ServerInfo)
	}
	client.notify("notifications/initialized", nil)
}

func assertToolList(t *testing.T, client *mcpProcess) {
	t.Helper()
	var list toolListResult
	if err := json.Unmarshal(client.request("tools/list", nil), &list); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	names := make([]string, 0, len(list.Tools))
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, publicTools) {
		t.Fatalf("tool names = %v, want %v", names, publicTools)
	}
}

func (client *mcpProcess) callTool(name string, arguments map[string]any) map[string]any {
	client.t.Helper()
	raw := client.request("tools/call", map[string]any{"name": name, "arguments": arguments})
	var call toolCallResult
	if err := json.Unmarshal(raw, &call); err != nil {
		client.t.Fatalf("decode %s result: %v", name, err)
	}
	if call.IsError {
		client.t.Fatalf("%s returned protocol error content: %s", name, raw)
	}
	envelope := call.StructuredContent
	if envelope == nil && len(call.Content) > 0 {
		text, _ := call.Content[0]["text"].(string)
		if err := json.Unmarshal([]byte(text), &envelope); err != nil {
			client.t.Fatalf("decode %s text content: %v", name, err)
		}
	}
	if envelope == nil || envelope["schema_version"] != "1.0" {
		client.t.Fatalf("%s returned an invalid envelope: %#v", name, envelope)
	}
	return envelope
}

func (client *mcpProcess) request(method string, params any) json.RawMessage {
	client.t.Helper()
	client.nextID++
	message := map[string]any{"jsonrpc": "2.0", "id": client.nextID, "method": method}
	if params != nil {
		message["params"] = params
	}
	client.writeMessage("client", message)
	for {
		type readResult struct {
			line []byte
			err  error
		}
		result := make(chan readResult, 1)
		go func() {
			line, err := client.stdout.ReadBytes('\n')
			result <- readResult{line: line, err: err}
		}()
		var line []byte
		select {
		case read := <-result:
			if read.err != nil {
				client.t.Fatalf("read %s response: %v", method, read.err)
			}
			line = read.line
		case <-time.After(15 * time.Second):
			client.t.Fatalf("read %s response: timed out", method)
		}
		client.writeTranscript(map[string]any{"direction": "server", "raw": string(line)})
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err != nil {
			client.t.Fatalf("decode %s response: %v: %q", method, err, line)
		}
		var id int
		if err := json.Unmarshal(response.ID, &id); err != nil || id != client.nextID {
			continue
		}
		if len(response.Error) != 0 && string(response.Error) != "null" {
			client.t.Fatalf("%s returned JSON-RPC error: %s", method, response.Error)
		}
		return response.Result
	}
}

func (client *mcpProcess) notify(method string, params any) {
	client.t.Helper()
	message := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		message["params"] = params
	}
	client.writeMessage("client", message)
}

func (client *mcpProcess) writeMessage(direction string, message map[string]any) {
	client.t.Helper()
	client.writeTranscript(map[string]any{"direction": direction, "message": message})
	data, err := json.Marshal(message)
	if err != nil {
		client.t.Fatal(err)
	}
	data = append(data, '\n')
	if _, err := client.stdin.Write(data); err != nil {
		client.t.Fatalf("write MCP request: %v", err)
	}
}

func (client *mcpProcess) writeTranscript(record map[string]any) {
	client.t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		client.t.Fatal(err)
	}
	if _, err := client.transcript.Write(append(data, '\n')); err != nil {
		client.t.Fatal(err)
	}
}

func (client *mcpProcess) close(expectSuccess bool) {
	client.t.Helper()
	if client.command.ProcessState != nil {
		return
	}
	_ = client.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- client.command.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(10 * time.Second):
		_ = client.command.Process.Kill()
		waitErr = errors.New("server did not stop after EOF")
	}
	remainder, readErr := io.ReadAll(client.stdout)
	if readErr != nil && !errors.Is(readErr, os.ErrClosed) && expectSuccess {
		client.t.Fatalf("read server output after EOF: %v", readErr)
	}
	if len(remainder) != 0 && expectSuccess {
		client.t.Fatalf("unexpected server output after EOF: %q", remainder)
	}
	_ = client.stderr.Close()
	_ = client.transcript.Close()
	returnCode := 0
	if client.command.ProcessState != nil {
		returnCode = client.command.ProcessState.ExitCode()
	}
	testHarness.addCommand(commandEvidence{
		Argv: []string{testHarness.binary, "serve", "--transport", "stdio", "--quiet"},
		Cwd:  client.folder, Environment: reportEnvironment(client.env), ReturnCode: returnCode,
		Transcript: filepath.Join(client.folder, "protocol.jsonl"),
		StderrLog:  filepath.Join(client.folder, "stderr.log"),
		DurationMS: time.Since(client.start).Milliseconds(),
	})
	if waitErr != nil && expectSuccess {
		client.t.Fatalf("server EOF shutdown failed: %v", waitErr)
	}
	if expectSuccess && returnCode != 0 {
		client.t.Fatalf("server EOF exit code = %d", returnCode)
	}
}

func loadTransitCases(t *testing.T) transitCases {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testHarness.fixtures, "mobility_cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases transitCases
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Transit) == 0 || len(cases.Parking.Values) == 0 {
		t.Fatal("mobility_cases.json has no transit or parking cases")
	}
	return cases
}

func assertStatus(t *testing.T, envelope map[string]any, allowed ...string) {
	t.Helper()
	status, _ := envelope["status"].(string)
	for _, candidate := range allowed {
		if status == candidate {
			return
		}
	}
	t.Fatalf("status = %q, want one of %v; envelope=%#v", status, allowed, envelope)
}

func assertWarningCode(t *testing.T, envelope map[string]any, allowed ...string) {
	t.Helper()
	warnings := objectList(t, envelope, "warnings")
	for _, warning := range warnings {
		code, _ := warning["code"].(string)
		for _, expected := range allowed {
			if code == expected {
				return
			}
		}
	}
	t.Fatalf("warning codes do not contain one of %v: %#v", allowed, warnings)
}

func assertWarningSource(t *testing.T, envelope map[string]any, code, source string) {
	t.Helper()
	for _, warning := range objectList(t, envelope, "warnings") {
		if warning["code"] == code && warning["source"] == source {
			return
		}
	}
	t.Fatalf("warning %s from %s is absent: %#v", code, source, envelope["warnings"])
}

func assertOfflineFixture(t *testing.T, envelope map[string]any) {
	t.Helper()
	if envelope["degraded"] != true {
		t.Fatalf("offline fixture result is not degraded: %#v", envelope)
	}
	assertWarningCode(t, envelope, "OFFLINE_FIXTURE")
	for _, source := range objectList(t, envelope, "sources") {
		provider, _ := source["provider"].(string)
		if provider == "Fixtures" && source["dataset"] == "offline_demo" {
			return
		}
	}
	t.Fatalf("offline fixture provenance is absent: %#v", envelope["sources"])
}

func assertEvidence(t *testing.T, evidence []map[string]any, expected map[string]any) {
	t.Helper()
	for _, item := range evidence {
		matched := true
		for key, value := range expected {
			if !reflect.DeepEqual(item[key], value) {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Fatalf("evidence does not contain %#v: %#v", expected, evidence)
}

func dataObject(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	return objectValue(t, envelope, "data")
}

func optionModes(t *testing.T, envelope map[string]any) []string {
	t.Helper()
	options := objectList(t, dataObject(t, envelope), "options")
	return stringFields(t, options, "mode")
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func objectValue(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object: %#v", key, object[key])
	}
	return value
}

func objectList(t *testing.T, object map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := object[key].([]any)
	if !ok {
		t.Fatalf("%s is not a list: %#v", key, object[key])
	}
	result := make([]map[string]any, 0, len(raw))
	for _, value := range raw {
		item, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s contains a non-object: %#v", key, value)
		}
		result = append(result, item)
	}
	return result
}

func containsFieldValue(items []map[string]any, field string, value any) bool {
	for _, item := range items {
		if reflect.DeepEqual(item[field], value) {
			return true
		}
	}
	return false
}

func stringFields(t *testing.T, items []map[string]any, field string) []string {
	t.Helper()
	values := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item[field].(string)
		if !ok {
			t.Fatalf("%s is not a string: %#v", field, item[field])
		}
		values = append(values, value)
	}
	return values
}

func assertStringFields(t *testing.T, items []map[string]any, field string, want []string) {
	t.Helper()
	got := stringFields(t, items, field)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s values = %v, want %v", field, got, want)
	}
}

func indexByField(items []map[string]any, field string) map[string]map[string]any {
	result := make(map[string]map[string]any)
	for _, item := range items {
		value, _ := item[field].(string)
		result[value] = item
	}
	return result
}

func sortedKeys(values map[string]map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func mapsClone(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
