package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRaceServerAcceptance(t *testing.T) {
	if os.Getenv(raceEnv) != "1" {
		t.Skip("set GLMCP_RACE_BINARY=1 to build and exercise a race-instrumented server")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the race detector acceptance mode targets Linux")
	}
	runCase(t, "race-instrumented-binary", func(t *testing.T) {
		assertRaceInstrumented(t, testHarness.binary)
	})
	runCase(t, "race-concurrent-mcp", testRaceConcurrentMCP)
	runCase(t, "race-cancellation", testRaceCancellation)
	runCase(t, "race-shutdown-with-active-calls", testRaceShutdownWithActiveCalls)
}

func assertRaceInstrumented(t *testing.T, binary string) {
	t.Helper()
	output, err := exec.Command("go", "tool", "nm", binary).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect race binary: %v: %s", err, output)
	}
	if !bytes.Contains(output, []byte("runtime/race")) && !bytes.Contains(output, []byte("__tsan")) {
		t.Fatal("the tested server binary has no race runtime symbols")
	}
}

func testRaceConcurrentMCP(t *testing.T) {
	folder, env, _, err := preparedEnvironment("race-concurrent")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	requests := []struct {
		name      string
		arguments map[string]any
		statuses  []string
	}{
		{"lyon_resolve_place", map[string]any{"query": "Part-Dieu", "limit": 3}, []string{"partial", "ok", "ambiguous"}},
		{"lyon_resolve_place", map[string]any{"query": "Bellecour", "limit": 3}, []string{"partial", "ok", "ambiguous"}},
		{"lyon_resolve_place", map[string]any{"query": "Part-Dieu", "limit": 5}, []string{"partial", "ok", "ambiguous"}},
		{"lyon_resolve_place", map[string]any{"query": "Bellecour", "limit": 5}, []string{"partial", "ok", "ambiguous"}},
		{"lyon_next_departures", map[string]any{"stop": map[string]any{"place_id": "gtfs:stop:PD1"}, "line": "A", "at": "2026-07-20T08:00:00+02:00", "limit": 3}, []string{"partial", "ok"}},
		{"lyon_next_departures", map[string]any{"stop": map[string]any{"place_id": "gtfs:stop:PD1"}, "line": "A", "at": "2026-07-20T08:00:00+02:00", "limit": 5}, []string{"partial", "ok"}},
		{"lyon_next_departures", map[string]any{"stop": map[string]any{"query": "Bellecour"}, "line": "A", "at": "2026-07-20T08:00:00+02:00", "limit": 3}, []string{"partial", "ok", "not_found"}},
		{"lyon_parking_options", map[string]any{"destination": map[string]any{"latitude": 45.7675, "longitude": 4.8355}, "radius_m": 500}, []string{"partial", "ok", "not_found"}},
		{"lyon_parking_options", map[string]any{"destination": map[string]any{"latitude": 45.7675, "longitude": 4.8355}, "radius_m": 1000}, []string{"partial", "ok", "not_found"}},
		{"lyon_trip_options", map[string]any{"origin": map[string]any{"profile_place": "home"}, "destination": map[string]any{"profile_place": "work"}, "modes": []string{"walk", "velov"}, "departure_at": "2026-07-20T08:00:00+02:00"}, []string{"partial", "ok"}},
		{"lyon_trip_options", map[string]any{"origin": map[string]any{"profile_place": "home"}, "destination": map[string]any{"profile_place": "work"}, "modes": []string{"walk", "tcl"}, "departure_at": "2026-07-20T08:00:00+02:00"}, []string{"partial", "ok", "unavailable"}},
		{"lyon_personal_briefing", map[string]any{"profile": "weekday_morning", "at": "2026-07-20T08:00:00+02:00", "compare_with_previous": false}, []string{"partial", "ok"}},
	}
	type pending struct {
		id       int
		statuses []string
	}
	var lock sync.Mutex
	expected := map[int]pending{}
	nextID := client.nextID
	for _, request := range requests {
		nextID++
		expected[nextID] = pending{id: nextID, statuses: request.statuses}
		message := map[string]any{
			"jsonrpc": "2.0", "id": nextID, "method": "tools/call",
			"params": map[string]any{"name": request.name, "arguments": request.arguments},
		}
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		lock.Lock()
		_, err = client.stdin.Write(append(data, '\n'))
		lock.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	responses := readConcurrentResponses(t, client, len(requests))
	for id, response := range responses {
		want, ok := expected[id]
		if !ok {
			t.Fatalf("unexpected response ID %d", id)
		}
		var call toolCallResult
		if err := json.Unmarshal(response, &call); err != nil {
			t.Fatalf("decode response %d: %v", id, err)
		}
		if call.StructuredContent == nil || call.StructuredContent["schema_version"] != "1.0" {
			t.Fatalf("response %d has no valid envelope: %s", id, response)
		}
		assertExecutionStatus(t, "race-concurrent", call.IsError, call.StructuredContent)
		assertStatus(t, call.StructuredContent, want.statuses...)
	}
	client.close(true)
}

func readConcurrentResponses(t *testing.T, client *mcpProcess, count int) map[int]json.RawMessage {
	t.Helper()
	result := map[int]json.RawMessage{}
	deadline := time.After(20 * time.Second)
	for len(result) < count {
		type readResult struct {
			line []byte
			err  error
		}
		read := make(chan readResult, 1)
		go func() {
			line, err := client.stdout.ReadBytes('\n')
			read <- readResult{line: line, err: err}
		}()
		select {
		case value := <-read:
			if value.err != nil {
				t.Fatalf("read concurrent response: %v", value.err)
			}
			client.writeTranscript(map[string]any{"direction": "server", "raw": string(value.line)})
			var response rpcResponse
			if err := json.Unmarshal(value.line, &response); err != nil {
				t.Fatalf("decode concurrent response: %v: %q", err, value.line)
			}
			var id int
			if err := json.Unmarshal(response.ID, &id); err != nil {
				continue
			}
			if len(response.Error) != 0 && string(response.Error) != "null" {
				t.Fatalf("concurrent request %d returned JSON-RPC error: %s", id, response.Error)
			}
			result[id] = response.Result
		case <-deadline:
			t.Fatalf("timed out with %d/%d concurrent responses", len(result), count)
		}
	}
	return result
}

func testRaceCancellation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the controlled FIFO boundary requires Linux")
	}
	folder, env, _, err := testHarness.newEnvironment("race-cancellation")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := filepath.Join(folder, "fixtures")
	if err := copyTree(testHarness.fixtures, fixtures); err != nil {
		t.Fatal(err)
	}
	photonPath := filepath.Join(fixtures, "photon", "part_dieu.json")
	if err := os.Remove(photonPath); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("mkfifo", photonPath).CombinedOutput(); err != nil {
		t.Fatalf("create controlled FIFO: %v: %s", err, output)
	}
	env = mergeEnvironment(env, map[string]string{fixturesEnv: fixtures})
	client := startMCP(t, folder, env)
	initializeMCP(t, client)

	id := client.nextID + 1
	client.nextID = id
	client.writeMessage("client", map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{
			"name": "lyon_resolve_place", "arguments": map[string]any{"query": "Part-Dieu", "limit": 3},
		},
	})
	pending := make(chan []byte, 1)
	go func() {
		line, _ := client.stdout.ReadBytes('\n')
		pending <- line
	}()
	select {
	case line := <-pending:
		t.Fatalf("request was not pending before cancellation: %q", line)
	case <-time.After(300 * time.Millisecond):
	}
	client.notify("notifications/cancelled", map[string]any{"requestId": id, "reason": "race acceptance"})

	fifo, err := os.OpenFile(photonPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := fifo.WriteString(`{"type":"FeatureCollection","features":[]}`)
	closeErr := fifo.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("release blocking boundary: write=%v close=%v", writeErr, closeErr)
	}
	select {
	case line := <-pending:
		client.writeTranscript(map[string]any{"direction": "server", "raw": string(line)})
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled request did not complete")
	}

	if err := os.Remove(photonPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(testHarness.fixtures, "photon", "part_dieu.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(photonPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	after := client.callTool("lyon_resolve_place", map[string]any{"query": "Part-Dieu", "limit": 3})
	assertStatus(t, after, "partial", "ok", "ambiguous")
	client.close(true)
}

func testRaceShutdownWithActiveCalls(t *testing.T) {
	folder, env, _, err := preparedEnvironment("race-shutdown-active")
	if err != nil {
		t.Fatal(err)
	}
	client := startMCP(t, folder, env)
	initializeMCP(t, client)
	lock := sync.Mutex{}
	for index := 0; index < 8; index++ {
		client.nextID++
		message := map[string]any{
			"jsonrpc": "2.0", "id": client.nextID, "method": "tools/call",
			"params": map[string]any{
				"name": "lyon_trip_options",
				"arguments": map[string]any{
					"origin": map[string]any{"profile_place": "home"}, "destination": map[string]any{"profile_place": "work"},
					"modes": []string{"walk", "velov", "tcl"}, "departure_at": "2026-07-20T08:00:00+02:00",
				},
			},
		}
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		lock.Lock()
		_, err = client.stdin.Write(append(data, '\n'))
		lock.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = client.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- client.command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown with active calls: %v; stderr=%s", err, readFileString(client.stderr.Name()))
		}
	case <-time.After(10 * time.Second):
		_ = client.command.Process.Kill()
		<-done
		t.Fatal("shutdown with active calls timed out")
	}
	_ = client.stderr.Close()
	_ = client.transcript.Close()
}

func readFileString(path string) string {
	data, _ := os.ReadFile(path)
	return strings.TrimSpace(string(data))
}
