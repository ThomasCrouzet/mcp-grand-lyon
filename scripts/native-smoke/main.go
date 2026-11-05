// Command native-smoke verifies one extracted release archive on its target OS.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type mcpResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type toolList struct {
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

type toolCall struct {
	IsError           bool           `json:"isError"`
	StructuredContent map[string]any `json:"structuredContent"`
}

func main() {
	binary := flag.String("binary", "", "absolute path to the extracted release binary")
	expectedVersion := flag.String("version", "", "expected version")
	flag.Parse()
	if *binary == "" || *expectedVersion == "" {
		fmt.Fprintln(os.Stderr, "usage: native-smoke -binary PATH -version X.Y.Z")
		os.Exit(2)
	}
	if err := run(*binary, *expectedVersion); err != nil {
		fmt.Fprintf(os.Stderr, "native smoke failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("native smoke passed: binary=%s version=%s platform=%s/%s\n", *binary, *expectedVersion, runtime.GOOS, runtime.GOARCH)
}

func run(binary, expectedVersion string) error {
	absolute, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("inspect binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("binary is not a regular file")
	}
	root, err := os.MkdirTemp("", "grand lyon mcp native smoke ")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	configDir := filepath.Join(root, "config dir")
	dataDir := filepath.Join(root, "data dir")
	database := filepath.Join(dataDir, "app dir", "grand_lyon_mcp.db")
	env := append(os.Environ(),
		"GRAND_LYON_MCP_CONFIG_DIR="+configDir,
		"GRAND_LYON_MCP_DATA_DIR="+dataDir,
		"GRAND_LYON_MCP_DB_PATH="+database,
		"GRAND_LYON_MCP_OFFLINE=true",
		"NO_COLOR=1",
	)
	version, err := commandOutput(env, absolute, "version")
	if err != nil {
		return err
	}
	if strings.TrimSpace(version) != expectedVersion {
		return fmt.Errorf("version = %q, want %q", strings.TrimSpace(version), expectedVersion)
	}
	if _, err := commandOutput(env, absolute, "setup", "--yes", "--offline"); err != nil {
		return err
	}
	databaseInfo, err := os.Stat(database)
	if err != nil {
		return fmt.Errorf("setup did not initialize SQLite: %w", err)
	}
	if databaseInfo.Size() == 0 {
		return fmt.Errorf("SQLite file is empty")
	}
	doctorRaw, err := commandOutput(env, absolute, "doctor")
	if err != nil {
		return err
	}
	var doctor map[string]any
	if err := json.Unmarshal([]byte(doctorRaw), &doctor); err != nil {
		return fmt.Errorf("decode doctor: %w", err)
	}
	if doctor["status"] != "OK" || doctor["offline"] != true {
		return fmt.Errorf("doctor did not describe the offline installation: %#v", doctor)
	}
	return mcpSmoke(absolute, root, env)
}

func commandOutput(env []string, argv ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Env = env
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", strings.Join(argv, " "), err, output)
	}
	return string(output), nil
}

func mcpSmoke(binary, folder string, env []string) error {
	command := exec.Command(binary, "serve", "--transport", "stdio", "--quiet")
	command.Dir = folder
	command.Env = env
	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	reader := bufio.NewReader(stdout)
	nextID := 0
	request := func(method string, params any) (json.RawMessage, error) {
		nextID++
		message := map[string]any{"jsonrpc": "2.0", "id": nextID, "method": method}
		if params != nil {
			message["params"] = params
		}
		data, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		if _, err := stdin.Write(append(data, '\n')); err != nil {
			return nil, err
		}
		for {
			line, err := readLine(reader)
			if err != nil {
				return nil, err
			}
			var response mcpResponse
			if err := json.Unmarshal(line, &response); err != nil {
				return nil, err
			}
			var id int
			if err := json.Unmarshal(response.ID, &id); err != nil || id != nextID {
				continue
			}
			if len(response.Error) != 0 && string(response.Error) != "null" {
				return nil, fmt.Errorf("%s returned JSON-RPC error: %s", method, response.Error)
			}
			return response.Result, nil
		}
	}
	if _, err := request("initialize", map[string]any{
		"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "native-smoke", "version": "1.0"},
	}); err != nil {
		return err
	}
	initialized, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if err != nil {
		return err
	}
	if _, err := stdin.Write(append(initialized, '\n')); err != nil {
		return err
	}
	rawTools, err := request("tools/list", nil)
	if err != nil {
		return err
	}
	var tools toolList
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		return err
	}
	expected := []string{
		"lyon_accessibility_check", "lyon_environment_brief", "lyon_mobility_status", "lyon_nearby_facilities",
		"lyon_next_departures", "lyon_parking_options", "lyon_personal_briefing", "lyon_resolve_place",
		"lyon_trip_options", "lyon_waste_dropoff",
	}
	if len(tools.Tools) != len(expected) {
		return fmt.Errorf("tools/list returned %d tools, want %d", len(tools.Tools), len(expected))
	}
	found := map[string]bool{}
	for _, tool := range tools.Tools {
		found[tool.Name] = true
	}
	for _, name := range expected {
		if !found[name] {
			return fmt.Errorf("tools/list is missing %s", name)
		}
	}
	rawCall, err := request("tools/call", map[string]any{
		"name": "lyon_resolve_place", "arguments": map[string]any{"query": "Part-Dieu", "limit": 3},
	})
	if err != nil {
		return err
	}
	var call toolCall
	if err := json.Unmarshal(rawCall, &call); err != nil {
		return err
	}
	if call.IsError || call.StructuredContent == nil {
		return fmt.Errorf("representative tool call failed: %#v", call)
	}
	if call.StructuredContent["status"] != "partial" || call.StructuredContent["degraded"] != true {
		return fmt.Errorf("fixture result is not labelled: %#v", call.StructuredContent)
	}
	if !hasWarning(call.StructuredContent, "OFFLINE_FIXTURE") {
		return fmt.Errorf("fixture result has no OFFLINE_FIXTURE warning: %#v", call.StructuredContent)
	}
	data, _ := call.StructuredContent["data"].(map[string]any)
	candidates, _ := data["candidates"].([]any)
	if len(candidates) == 0 {
		return fmt.Errorf("fixture result has no candidates: %#v", call.StructuredContent)
	}
	if err := stdin.Close(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("clean shutdown: %w", err)
		}
	case <-time.After(15 * time.Second):
		_ = command.Process.Kill()
		<-done
		return fmt.Errorf("server did not stop after stdin EOF")
	}
	return nil
}

func readLine(reader *bufio.Reader) ([]byte, error) {
	type result struct {
		line []byte
		err  error
	}
	value := make(chan result, 1)
	go func() {
		line, err := reader.ReadBytes('\n')
		value <- result{line: line, err: err}
	}()
	select {
	case read := <-value:
		return read.line, read.err
	case <-time.After(15 * time.Second):
		return nil, fmt.Errorf("timed out waiting for MCP response")
	}
}

func hasWarning(envelope map[string]any, code string) bool {
	warnings, _ := envelope["warnings"].([]any)
	for _, raw := range warnings {
		warning, _ := raw.(map[string]any)
		if warning["code"] == code {
			return true
		}
	}
	return false
}
