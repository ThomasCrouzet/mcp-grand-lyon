package e2e_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGoProcessValidation(t *testing.T) {
	runCase(t, "validation-place-ref", testPlaceRefValidation)
	runCase(t, "validation-control-payloads", testControlPayloadValidation)
	runCase(t, "validation-frame-bounds", testFrameBounds)
}

func testPlaceRefValidation(t *testing.T) {
	folder, env, _, err := preparedEnvironment("validation-place-ref")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	tests := map[string]any{
		"multiple modes": map[string]any{
			"query": "Bellecour", "latitude": 45.75, "longitude": 4.85,
		},
		"latitude only": map[string]any{"latitude": 45.75},
		"empty":         map[string]any{},
		"latitude range": map[string]any{
			"latitude": 91.0, "longitude": 4.85,
		},
		"unknown nested field": map[string]any{
			"query": "Bellecour", "unexpected": true,
		},
	}
	for name, stop := range tests {
		t.Run(name, func(t *testing.T) {
			result := callToolArguments(t, client, "lyon_next_departures", map[string]any{"stop": stop})
			assertInvalidRequest(t, result)
		})
	}
	client.close(true)
}

func testControlPayloadValidation(t *testing.T) {
	folder, env, _, err := preparedEnvironment("validation-control-payloads")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	unknown := callToolArguments(t, client, "lyon_resolve_place", map[string]any{
		"query": "Bellecour", "unexpected": true,
	})
	assertInvalidRequest(t, unknown)

	for name, arguments := range map[string]any{
		"null": nil, "array": []any{}, "string": "Bellecour", "boolean": true, "number": 1,
	} {
		t.Run(name, func(t *testing.T) {
			result := callToolArguments(t, client, "lyon_resolve_place", arguments)
			assertInvalidRequest(t, result)
		})
	}

	belowFrameLimit := callToolArguments(t, client, "lyon_resolve_place", map[string]any{
		"query": "Bellecour", "padding": strings.Repeat("x", 128*1024),
	})
	assertInvalidRequest(t, belowFrameLimit)
	if belowFrameLimit["summary"] != "Request arguments exceed the size limit." {
		t.Fatalf("large valid frame summary = %q", belowFrameLimit["summary"])
	}
	client.close(true)
}

func testFrameBounds(t *testing.T) {
	folder, env, _, err := preparedEnvironment("validation-frame-bounds")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	message := map[string]any{
		"jsonrpc": "2.0", "id": 999, "method": "tools/call",
		"params": map[string]any{
			"name":      "lyon_resolve_place",
			"arguments": map[string]any{"query": strings.Repeat("x", 300*1024)},
		},
	}
	frame, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	frame = append(frame, '\n')
	client.writeTranscript(map[string]any{
		"direction": "client", "oversized_frame_bytes": len(frame),
	})
	if _, err := client.stdin.Write(frame); err != nil {
		t.Fatalf("write oversized frame: %v", err)
	}
	assertOversizedFrameStopsServer(t, client)
}

func callToolArguments(t *testing.T, client *mcpProcess, name string, arguments any) map[string]any {
	t.Helper()
	raw := client.request("tools/call", map[string]any{"name": name, "arguments": arguments})
	var call toolCallResult
	if err := json.Unmarshal(raw, &call); err != nil {
		t.Fatalf("decode %s result: %v", name, err)
	}
	if call.IsError {
		t.Fatalf("%s returned protocol error content: %s", name, raw)
	}
	if call.StructuredContent == nil {
		t.Fatalf("%s returned no structured content: %s", name, raw)
	}
	return call.StructuredContent
}

func assertInvalidRequest(t *testing.T, envelope map[string]any) {
	t.Helper()
	if status, _ := envelope["status"].(string); status != "invalid_request" {
		t.Fatalf("status = %q, want invalid_request; envelope=%#v", status, envelope)
	}
}

func assertOversizedFrameStopsServer(t *testing.T, client *mcpProcess) {
	t.Helper()
	type readResult struct {
		data []byte
		err  error
	}
	stdoutDone := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(client.stdout)
		stdoutDone <- readResult{data: data, err: err}
	}()
	done := make(chan error, 1)
	go func() { done <- client.command.Wait() }()
	timedOut := false
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		timedOut = true
		_ = client.command.Process.Kill()
		<-done
	}
	_ = client.stdin.Close()
	stdout := <-stdoutDone
	stderrPath := client.stderr.Name()
	transcriptPath := client.transcript.Name()
	_ = client.stderr.Close()
	_ = client.transcript.Close()
	stderr, stderrErr := os.ReadFile(stderrPath)
	returnCode := client.command.ProcessState.ExitCode()
	testHarness.addCommand(commandEvidence{
		Argv: []string{testHarness.binary, "serve", "--transport", "stdio", "--quiet"},
		Cwd:  client.folder, Environment: reportEnvironment(client.env), ReturnCode: returnCode,
		Transcript: transcriptPath, StderrLog: stderrPath,
		DurationMS: time.Since(client.start).Milliseconds(),
	})

	if timedOut {
		t.Fatal("server did not stop after an oversized stdio frame")
	}
	if stdout.err != nil && !errors.Is(stdout.err, os.ErrClosed) {
		t.Fatalf("read stdout after oversized frame: %v", stdout.err)
	}
	if len(stdout.data) != 0 {
		t.Fatalf("oversized frame produced JSON-RPC output: %q", stdout.data)
	}
	if stderrErr != nil {
		t.Fatalf("read stderr after oversized frame: %v", stderrErr)
	}
	if len(strings.TrimSpace(string(stderr))) == 0 {
		t.Fatal("oversized frame failure did not explain the error on stderr")
	}
	if returnCode == 0 {
		t.Fatal("oversized frame returned a successful process exit")
	}
}
