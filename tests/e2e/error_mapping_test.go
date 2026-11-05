package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionErrorMapping(t *testing.T) {
	runCase(t, "mcp-invalid-arguments", testInvalidArgumentExecutionStatus)
	runCase(t, "mcp-legitimate-results", testLegitimateResultsStaySuccessful)
	runCase(t, "mcp-complete-failure", testCompleteExecutionFailure)
}

func testInvalidArgumentExecutionStatus(t *testing.T) {
	folder, env, _, err := preparedEnvironment("mcp-invalid-arguments")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	outOfRange := callToolArguments(t, client, "lyon_next_departures", map[string]any{
		"stop": map[string]any{"latitude": 91.0, "longitude": 4.85},
	})
	assertInvalidRequest(t, outOfRange)
	assertErrorDetail(t, outOfRange, "stop", "latitude")

	incompatible := callToolArguments(t, client, "lyon_parking_options", map[string]any{
		"destination": map[string]any{"query": "Bellecour", "latitude": 45.75, "longitude": 4.85},
	})
	assertInvalidRequest(t, incompatible)
	assertErrorDetail(t, incompatible, "destination", "exactly one")

	unknownField := callToolArguments(t, client, "lyon_resolve_place", map[string]any{
		"query": "Bellecour", "unexpected_field": true,
	})
	assertInvalidRequest(t, unknownField)
	detail := objectValue(t, dataObject(t, unknownField), "error")
	if detail["code"] == nil || !strings.Contains(stringValue(detail["expected"]), "unsupported") {
		t.Fatalf("unknown field detail is not actionable: %#v", detail)
	}

	invalidTimestamp := callToolArguments(t, client, "lyon_next_departures", map[string]any{
		"stop": map[string]any{"query": "Part-Dieu"}, "at": "not-a-timestamp",
	})
	assertInvalidRequest(t, invalidTimestamp)
	if detail := objectValue(t, dataObject(t, invalidTimestamp), "error"); detail["code"] != "invalid_argument" {
		t.Fatalf("timestamp error code = %v", detail["code"])
	}
	client.close(true)
}

func testLegitimateResultsStaySuccessful(t *testing.T) {
	folder, env, _, err := preparedEnvironment("mcp-legitimate-results")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	empty := client.callTool("lyon_resolve_place", map[string]any{"query": "qzxqzx unknown place"})
	assertStatus(t, empty, "not_found")

	partial := client.callTool("lyon_parking_options", map[string]any{
		"destination": map[string]any{"latitude": 45.7675, "longitude": 4.8355}, "radius_m": 500,
	})
	assertStatus(t, partial, "partial")
	client.close(true)
}

func testCompleteExecutionFailure(t *testing.T) {
	folder, env, _, err := testHarness.newEnvironment("mcp-complete-failure")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, fixtures); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "datagrandlyon", "parkings.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, mergeEnvironment(env, map[string]string{fixturesEnv: fixtures}))
	initializeMCP(t, client)
	failure := client.callTool("lyon_parking_options", map[string]any{
		"destination": map[string]any{"latitude": 45.7675, "longitude": 4.8355}, "radius_m": 500,
	})
	assertStatus(t, failure, "unavailable")
	if detail := objectValue(t, dataObject(t, failure), "error"); detail["code"] != "execution_failed" {
		t.Fatalf("complete failure detail = %#v", detail)
	}
	client.close(true)
}

func assertErrorDetail(t *testing.T, envelope map[string]any, field, expected string) {
	t.Helper()
	detail := objectValue(t, dataObject(t, envelope), "error")
	gotField := stringValue(detail["field"])
	if !strings.HasSuffix(gotField, field) {
		t.Fatalf("error field = %q, want suffix %q; detail=%#v", gotField, field, detail)
	}
	if !strings.Contains(strings.ToLower(stringValue(detail["expected"])), strings.ToLower(expected)) {
		t.Fatalf("error expected = %q, want %q; detail=%#v", detail["expected"], expected, detail)
	}
	if detail["code"] == "" || detail["code"] == nil {
		t.Fatalf("error has no stable code: %#v", detail)
	}
}

func TestErrorEnvelopeJSONStaysInTextContent(t *testing.T) {
	folder, env, _, err := preparedEnvironment("mcp-error-text")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	call := client.callToolResult("lyon_next_departures", map[string]any{
		"stop": map[string]any{"latitude": 91.0, "longitude": 4.85},
	})
	if !call.IsError || len(call.Content) == 0 {
		t.Fatalf("invalid call shape: %#v", call)
	}
	text, _ := call.Content[0]["text"].(string)
	var fromText map[string]any
	if err := json.Unmarshal([]byte(text), &fromText); err != nil {
		t.Fatalf("text content is not the domain envelope: %v", err)
	}
	if fromText["status"] != "invalid_request" || fromText["request_id"] != call.StructuredContent["request_id"] {
		t.Fatalf("text and structured content diverged: text=%#v structured=%#v", fromText, call.StructuredContent)
	}
	client.close(true)
}
