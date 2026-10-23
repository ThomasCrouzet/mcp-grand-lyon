// Command live-smoke verifies the installed server against live public data.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/config"
)

const (
	verifiedState    = "verified"
	unavailableState = "source_unavailable"
	notVerifiedState = "not_verified"
	failedState      = "failed"
)

type report struct {
	StartedAt   time.Time         `json:"started_at"`
	FinishedAt  time.Time         `json:"finished_at"`
	Status      string            `json:"status"`
	Timeout     string            `json:"timeout"`
	Command     []string          `json:"command"`
	Source      sourceEvidence    `json:"source"`
	Binary      binaryEvidence    `json:"binary"`
	GTFSArchive *binaryEvidence   `json:"gtfs_archive,omitempty"`
	Runtime     runtimeEvidence   `json:"runtime"`
	Doctor      map[string]any    `json:"doctor,omitempty"`
	Commands    []commandEvidence `json:"commands"`
	Checks      []checkEvidence   `json:"checks"`
	Protocol    []exchange        `json:"protocol"`
	ProcessExit int               `json:"process_exit"`
	ProcessErr  string            `json:"process_stderr,omitempty"`
	Error       string            `json:"error,omitempty"`
}

type sourceEvidence struct {
	Revision          string            `json:"revision"`
	TrackedDiffSHA256 string            `json:"tracked_diff_sha256"`
	WorkingTreeSHA256 string            `json:"working_tree_sha256"`
	Dirty             bool              `json:"dirty"`
	UntrackedSHA256   map[string]string `json:"untracked_sha256,omitempty"`
}

type binaryEvidence struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type runtimeEvidence struct {
	ConfigDir string `json:"config_dir"`
	DataDir   string `json:"data_dir"`
	DBPath    string `json:"db_path"`
}

type checkEvidence struct {
	Name           string `json:"name"`
	State          string `json:"state"`
	EnvelopeStatus string `json:"envelope_status,omitempty"`
	Count          int    `json:"count,omitempty"`
	Detail         string `json:"detail,omitempty"`
}

type commandEvidence struct {
	Argv       []string `json:"argv"`
	CWD        string   `json:"cwd"`
	ExitCode   int      `json:"exit_code"`
	DurationMS int64    `json:"duration_ms"`
	Stdout     any      `json:"stdout,omitempty"`
	Stderr     string   `json:"stderr,omitempty"`
}

type exchange struct {
	Method     string `json:"method"`
	Request    any    `json:"request"`
	Response   any    `json:"response,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type options struct {
	Binary       string
	ArtifactDir  string
	Timeout      time.Duration
	SyncGTFS     bool
	GTFSFromFile string
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type mcpClient struct {
	ctx       context.Context
	command   *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	stderr    bytes.Buffer
	nextID    int
	exchanges *[]exchange
	waited    bool
}

func main() {
	os.Exit(run())
}

func run() (exitCode int) {
	opts, err := parseOptions()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	repository, err := repositoryRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	artifactDir, err := createArtifactDirectory(opts.ArtifactDir, repository)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	reportPath := filepath.Join(artifactDir, "live-report.json")
	result := report{
		StartedAt: time.Now().UTC(), Status: "running", Timeout: opts.Timeout.String(),
		Command: append([]string(nil), os.Args...), Commands: []commandEvidence{}, Checks: []checkEvidence{},
		Protocol: []exchange{}, ProcessExit: -1,
	}
	secrets := []string{}
	defer func() {
		result.FinishedAt = time.Now().UTC()
		if err := writeReport(reportPath, result, secrets); err != nil {
			fmt.Fprintf(os.Stderr, "write live report: %v\n", err)
			exitCode = 1
			return
		}
		fmt.Fprintf(os.Stderr, "Live evidence: %s\n", reportPath)
	}()

	result.Source, err = captureSource(repository)
	if err != nil {
		return fail(&result, nil, fmt.Errorf("capture source identity: %w", err))
	}
	result.Binary, err = inspectBinary(opts.Binary)
	if err != nil {
		return fail(&result, nil, err)
	}

	credentials, err := config.Load()
	if err != nil {
		return fail(&result, nil, fmt.Errorf("load user configuration: %w", err))
	}
	secrets = []string{credentials.Username, credentials.Password}
	if credentials.Username == "" || credentials.Password == "" {
		return fail(&result, secrets, errors.New("DataGrandLyon credentials are missing"))
	}
	if opts.GTFSFromFile != "" {
		archive, archiveErr := inspectRegularFile(opts.GTFSFromFile)
		if archiveErr != nil {
			return fail(&result, secrets, fmt.Errorf("inspect GTFS archive: %w", archiveErr))
		}
		result.GTFSArchive = &archive
		opts.GTFSFromFile = archive.Path
	}

	runtimeDir := filepath.Join(artifactDir, "runtime")
	configDir := filepath.Join(runtimeDir, "config")
	dataDir := filepath.Join(runtimeDir, "data")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fail(&result, secrets, fmt.Errorf("create isolated config directory: %w", err))
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fail(&result, secrets, fmt.Errorf("create isolated data directory: %w", err))
	}
	dbPath := filepath.Join(runtimeDir, "live.db")
	result.Runtime = runtimeEvidence{ConfigDir: configDir, DataDir: dataDir, DBPath: dbPath}
	childEnv := liveEnvironment(credentials.Username, credentials.Password, configDir, dataDir, dbPath)

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	doctor, doctorCommand, err := runJSONCommand(ctx, result.Binary.Path, runtimeDir, childEnv, "doctor")
	result.Commands = append(result.Commands, doctorCommand)
	if err != nil {
		result.Checks = append(result.Checks, checkEvidence{Name: "doctor", State: failedState, Detail: cleanText(err.Error(), secrets)})
		return fail(&result, secrets, err)
	}
	result.Doctor = doctor
	if doctor["offline"] != false || doctor["credentials"] != "[set]" {
		detail := fmt.Sprintf("unexpected effective mode: offline=%v credentials=%v", doctor["offline"], doctor["credentials"])
		result.Checks = append(result.Checks, checkEvidence{Name: "doctor", State: failedState, Detail: detail})
		return fail(&result, secrets, errors.New(detail))
	}
	result.Checks = append(result.Checks, checkEvidence{Name: "doctor", State: verifiedState, Detail: "live mode with configured credentials"})
	if opts.SyncGTFS || opts.GTFSFromFile != "" {
		syncBudget := opts.Timeout - 2*time.Minute
		if syncBudget < 10*time.Second {
			syncBudget = opts.Timeout / 2
		}
		syncCtx, cancelSync := context.WithTimeout(ctx, syncBudget)
		gtfsArgs := []string{"sync", "gtfs"}
		if opts.GTFSFromFile != "" {
			gtfsArgs = append(gtfsArgs, "--from-file", opts.GTFSFromFile)
		}
		gtfs, gtfsCommand, syncErr := runJSONCommand(syncCtx, result.Binary.Path, runtimeDir, childEnv, gtfsArgs...)
		cancelSync()
		result.Commands = append(result.Commands, gtfsCommand)
		gtfsCheck := assessGTFS(gtfs, syncErr)
		result.Checks = append(result.Checks, gtfsCheck)
	}

	client, err := startMCP(ctx, result.Binary.Path, runtimeDir, childEnv, &result.Protocol)
	if err != nil {
		return fail(&result, secrets, fmt.Errorf("start MCP process: %w", err))
	}
	checks, verificationErr := verifyLiveMCP(client)
	result.Checks = append(result.Checks, checks...)
	closeErr := client.close()
	result.ProcessExit = client.exitCode()
	result.ProcessErr = cleanText(client.stderr.String(), secrets)
	if verificationErr != nil {
		return fail(&result, secrets, verificationErr)
	}
	if closeErr != nil {
		return fail(&result, secrets, fmt.Errorf("close MCP process: %w", closeErr))
	}

	hasUnavailable := false
	for _, check := range result.Checks {
		if check.State == failedState {
			return fail(&result, secrets, fmt.Errorf("check %s failed", check.Name))
		}
		if check.State == unavailableState || check.State == notVerifiedState {
			hasUnavailable = true
		}
	}
	if hasUnavailable {
		result.Status = "partial"
		return 2
	}
	result.Status = "verified"
	return 0
}

func parseOptions() (options, error) {
	flags := flag.NewFlagSet("live-smoke", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	binary := flags.String("binary", "", "path to the grand-lyon-mcp binary")
	artifactDir := flags.String("artifact-dir", "", "new external evidence directory")
	timeout := flags.Duration("timeout", 2*time.Minute, "maximum run duration")
	syncGTFS := flags.Bool("sync-gtfs", false, "download and import live GTFS before MCP checks")
	gtfsFromFile := flags.String("gtfs-from-file", "", "import an existing live GTFS archive before MCP checks")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if *binary == "" || *artifactDir == "" {
		return options{}, errors.New("--binary and --artifact-dir are required")
	}
	if *timeout < 10*time.Second || *timeout > 10*time.Minute {
		return options{}, errors.New("--timeout must be between 10s and 10m")
	}
	if *syncGTFS && *gtfsFromFile != "" {
		return options{}, errors.New("--sync-gtfs and --gtfs-from-file are mutually exclusive")
	}
	return options{
		Binary: *binary, ArtifactDir: *artifactDir, Timeout: *timeout,
		SyncGTFS: *syncGTFS, GTFSFromFile: *gtfsFromFile,
	}, nil
}

func repositoryRoot() (string, error) {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err != nil {
		return "", errors.New("run live-smoke from the repository worktree")
	}
	return filepath.Abs(strings.TrimSpace(string(output)))
}

func createArtifactDirectory(requested, repository string) (string, error) {
	path, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("resolve artifact directory: %w", err)
	}
	if _, err := os.Lstat(path); err == nil {
		return "", errors.New("--artifact-dir must name a new path")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect artifact directory: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("resolve artifact parent: %w", err)
	}
	path = filepath.Join(parent, filepath.Base(path))
	if within(path, repository) {
		return "", errors.New("--artifact-dir must be outside the repository")
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return "", fmt.Errorf("create artifact directory: %w", err)
	}
	return path, nil
}

func inspectBinary(path string) (binaryEvidence, error) {
	evidence, err := inspectRegularFile(path)
	if err != nil {
		return binaryEvidence{}, err
	}
	info, err := os.Stat(evidence.Path)
	if err != nil {
		return binaryEvidence{}, fmt.Errorf("inspect binary: %w", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return binaryEvidence{}, errors.New("--binary must be executable")
	}
	return evidence, nil
}

func inspectRegularFile(path string) (binaryEvidence, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return binaryEvidence{}, fmt.Errorf("resolve binary: %w", err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return binaryEvidence{}, fmt.Errorf("resolve binary links: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return binaryEvidence{}, fmt.Errorf("inspect binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return binaryEvidence{}, errors.New("path must name a regular file")
	}
	digest, err := fileDigest(absolute)
	if err != nil {
		return binaryEvidence{}, err
	}
	return binaryEvidence{Path: absolute, SHA256: digest, Size: info.Size()}, nil
}

func captureSource(repository string) (sourceEvidence, error) {
	revision, err := gitOutput(repository, "rev-parse", "HEAD")
	if err != nil {
		return sourceEvidence{}, err
	}
	diff, err := gitBytes(repository, "diff", "--binary", "HEAD")
	if err != nil {
		return sourceEvidence{}, err
	}
	untrackedRaw, err := gitBytes(repository, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return sourceEvidence{}, err
	}
	untracked := map[string]string{}
	for _, rawName := range bytes.Split(untrackedRaw, []byte{0}) {
		if len(rawName) == 0 {
			continue
		}
		name := string(rawName)
		path := filepath.Join(repository, filepath.FromSlash(name))
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		digest, digestErr := fileDigest(path)
		if digestErr != nil {
			return sourceEvidence{}, digestErr
		}
		untracked[name] = digest
	}
	combined := sha256.New()
	_, _ = combined.Write(diff)
	names := make([]string, 0, len(untracked))
	for name := range untracked {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		_, _ = io.WriteString(combined, name+"\x00"+untracked[name]+"\x00")
	}
	return sourceEvidence{
		Revision:          strings.TrimSpace(revision),
		TrackedDiffSHA256: dataDigest(diff),
		WorkingTreeSHA256: hex.EncodeToString(combined.Sum(nil)),
		Dirty:             len(diff) != 0 || len(untracked) != 0,
		UntrackedSHA256:   untracked,
	}, nil
}

func liveEnvironment(username, password, configDir, dataDir, dbPath string) []string {
	values := map[string]string{}
	for _, item := range os.Environ() {
		key, value, found := strings.Cut(item, "=")
		if !found || strings.HasPrefix(key, "GRAND_LYON_MCP_") || strings.HasPrefix(key, "DATAGRANDLYON_") || strings.HasPrefix(key, "TRANSITOUS_") {
			continue
		}
		values[key] = value
	}
	values["DATAGRANDLYON_USERNAME"] = username
	values["DATAGRANDLYON_PASSWORD"] = password
	values["GRAND_LYON_MCP_CONFIG_DIR"] = configDir
	values["GRAND_LYON_MCP_DATA_DIR"] = dataDir
	values["GRAND_LYON_MCP_DB_PATH"] = dbPath
	values["GRAND_LYON_MCP_OFFLINE"] = "false"
	values["GRAND_LYON_MCP_LOG_LEVEL"] = "WARNING"
	values["TRANSITOUS_ENABLED"] = "false"
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}

func runJSONCommand(ctx context.Context, binary, workingDir string, environment []string, args ...string) (map[string]any, commandEvidence, error) {
	argv := append([]string{binary}, args...)
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = workingDir
	command.Env = environment
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	started := time.Now()
	runErr := command.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	var result map[string]any
	var publicOutput any
	if stdout.Len() != 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err == nil {
			publicOutput = result
		} else {
			publicOutput = strings.TrimSpace(stdout.String())
		}
	}
	evidence := commandEvidence{
		Argv: argv, CWD: workingDir, ExitCode: exitCode, DurationMS: time.Since(started).Milliseconds(),
		Stdout: publicOutput, Stderr: strings.TrimSpace(stderr.String()),
	}
	if runErr != nil {
		return result, evidence, fmt.Errorf("%s failed: %w: %s", strings.Join(args, " "), runErr, evidence.Stderr)
	}
	if result == nil {
		return nil, evidence, fmt.Errorf("decode %s JSON output", strings.Join(args, " "))
	}
	return result, evidence, nil
}

func startMCP(ctx context.Context, binary, workingDir string, environment []string, exchanges *[]exchange) (*mcpClient, error) {
	client := &mcpClient{ctx: ctx, exchanges: exchanges}
	client.command = exec.CommandContext(ctx, binary, "serve", "--transport", "stdio", "--quiet")
	client.command.Dir = workingDir
	client.command.Env = environment
	stdin, err := client.command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := client.command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	client.stdin = stdin
	client.stdout = bufio.NewReader(stdout)
	client.command.Stderr = &client.stderr
	if err := client.command.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	return client, nil
}

func verifyLiveMCP(client *mcpClient) ([]checkEvidence, error) {
	checks := []checkEvidence{}
	initialized, err := client.request("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "grand-lyon-live-smoke", "version": "1.0"},
	})
	if err != nil {
		return checks, fmt.Errorf("initialize MCP: %w", err)
	}
	serverInfo, _ := initialized["serverInfo"].(map[string]any)
	if serverInfo["name"] != "grand-lyon-mcp" {
		return checks, fmt.Errorf("unexpected MCP server info: %v", serverInfo)
	}
	checks = append(checks, checkEvidence{Name: "mcp_initialize", State: verifiedState})
	if err := client.notify("notifications/initialized", nil); err != nil {
		return checks, err
	}

	resolveArgs := map[string]any{"query": "Bellecour", "limit": 5}
	resolve, err := client.callTool("lyon_resolve_place", resolveArgs)
	if err != nil {
		checks = append(checks, checkEvidence{Name: "resolve_place", State: failedState, Detail: err.Error()})
		return checks, err
	}
	candidates := objectList(resolve, "candidates")
	checks = append(checks, assessRows("resolve_place", resolve, len(candidates)))

	parking, err := client.callTool("lyon_parking_options", map[string]any{
		"destination": map[string]any{"latitude": 45.7675, "longitude": 4.8355},
		"radius_m":    5000, "limit": 10,
	})
	if err != nil {
		checks = append(checks, checkEvidence{Name: "parking_options", State: failedState, Detail: err.Error()})
		return checks, err
	}
	checks = append(checks, assessRows("parking_options", parking, len(objectList(parking, "options"))))
	checks = append(checks, assessParkingAvailability(parking))

	facilities, err := client.callTool("lyon_nearby_facilities", map[string]any{
		"location":   map[string]any{"latitude": 45.7606, "longitude": 4.8618},
		"categories": []string{"velov_station"}, "radius_m": 5000, "limit_per_category": 10,
	})
	if err != nil {
		checks = append(checks, checkEvidence{Name: "nearby_velov", State: failedState, Detail: err.Error()})
		return checks, err
	}
	checks = append(checks, assessRows("nearby_velov", facilities, len(objectList(facilities, "items"))))
	briefing, err := client.callTool("lyon_personal_briefing", map[string]any{
		"profile": "weekday_morning", "compare_with_previous": false,
	})
	if err != nil {
		checks = append(checks, checkEvidence{Name: "personal_briefing_velov", State: failedState, Detail: err.Error()})
		return checks, err
	}
	checks = append(checks, assessVelovBriefing(briefing))

	stopID, stopOK := verifiedStop(candidates)
	if !stopOK {
		checks = append(checks, checkEvidence{Name: "next_departures", State: unavailableState, Detail: "no verified transit stop candidate"})
		return checks, nil
	}
	departures, err := client.callTool("lyon_next_departures", map[string]any{
		"stop": map[string]any{"place_id": stopID}, "limit": 5,
	})
	if err != nil {
		checks = append(checks, checkEvidence{Name: "next_departures", State: failedState, Detail: err.Error()})
		return checks, err
	}
	checks = append(checks, assessRows("next_departures", departures, len(objectList(departures, "departures"))))
	return checks, nil
}

func (client *mcpClient) callTool(name string, arguments map[string]any) (map[string]any, error) {
	result, err := client.request("tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return nil, err
	}
	if isError, _ := result["isError"].(bool); isError {
		return nil, fmt.Errorf("%s returned MCP error content", name)
	}
	envelope, _ := result["structuredContent"].(map[string]any)
	if envelope == nil {
		contents, _ := result["content"].([]any)
		if len(contents) != 0 {
			content, _ := contents[0].(map[string]any)
			text, _ := content["text"].(string)
			_ = json.Unmarshal([]byte(text), &envelope)
		}
	}
	if envelope == nil || envelope["schema_version"] != "1.0" {
		return nil, fmt.Errorf("%s returned an invalid envelope", name)
	}
	return envelope, nil
}

func (client *mcpClient) request(method string, params any) (map[string]any, error) {
	client.nextID++
	request := map[string]any{"jsonrpc": "2.0", "id": client.nextID, "method": method}
	if params != nil {
		request["params"] = params
	}
	started := time.Now()
	exchangeIndex := len(*client.exchanges)
	*client.exchanges = append(*client.exchanges, exchange{Method: method, Request: request})
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := client.stdin.Write(append(data, '\n')); err != nil {
		return nil, err
	}
	type readResult struct {
		line []byte
		err  error
	}
	read := make(chan readResult, 1)
	go func() {
		line, err := client.stdout.ReadBytes('\n')
		read <- readResult{line: line, err: err}
	}()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	var raw []byte
	select {
	case response := <-read:
		if response.err != nil {
			return nil, response.err
		}
		raw = response.line
	case <-timer.C:
		return nil, fmt.Errorf("%s response timed out", method)
	case <-client.ctx.Done():
		return nil, client.ctx.Err()
	}
	var response rpcResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", method, err)
	}
	var responseDocument any
	if err := json.Unmarshal(raw, &responseDocument); err != nil {
		return nil, err
	}
	(*client.exchanges)[exchangeIndex].Response = responseDocument
	(*client.exchanges)[exchangeIndex].DurationMS = time.Since(started).Milliseconds()
	if len(response.Error) != 0 && string(response.Error) != "null" {
		return nil, fmt.Errorf("%s returned JSON-RPC error: %s", method, response.Error)
	}
	var result map[string]any
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("decode %s result: %w", method, err)
	}
	return result, nil
}

func (client *mcpClient) notify(method string, params any) error {
	request := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		request["params"] = params
	}
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if _, err := client.stdin.Write(append(data, '\n')); err != nil {
		return err
	}
	*client.exchanges = append(*client.exchanges, exchange{Method: method, Request: request})
	return nil
}

func (client *mcpClient) close() error {
	if client.waited {
		return nil
	}
	_ = client.stdin.Close()
	waited := make(chan error, 1)
	go func() { waited <- client.command.Wait() }()
	select {
	case err := <-waited:
		client.waited = true
		return err
	case <-time.After(10 * time.Second):
		_ = client.command.Process.Kill()
		err := <-waited
		client.waited = true
		return errors.Join(errors.New("MCP process did not close after EOF"), err)
	case <-client.ctx.Done():
		_ = client.command.Process.Kill()
		err := <-waited
		client.waited = true
		return errors.Join(client.ctx.Err(), err)
	}
}

func (client *mcpClient) exitCode() int {
	if client.command.ProcessState == nil {
		return -1
	}
	return client.command.ProcessState.ExitCode()
}

func assessRows(name string, envelope map[string]any, count int) checkEvidence {
	status, _ := envelope["status"].(string)
	check := checkEvidence{Name: name, EnvelopeStatus: status, Count: count}
	switch status {
	case "error", "invalid_request":
		check.State = failedState
		check.Detail = "tool returned a failure status"
	case "unavailable":
		check.State = unavailableState
		check.Detail = "upstream source is unavailable"
	case "ok", "partial", "ambiguous":
		if count == 0 {
			check.State = unavailableState
			check.Detail = "response contains no verifiable rows"
		} else {
			check.State = verifiedState
		}
	case "not_found":
		check.State = notVerifiedState
		check.Detail = "no verifiable result was found"
	default:
		check.State = failedState
		check.Detail = "tool returned an unknown status"
	}
	return check
}

func assessGTFS(output map[string]any, runErr error) checkEvidence {
	check := checkEvidence{Name: "sync_gtfs"}
	if runErr != nil {
		check.State = failedState
		check.Detail = runErr.Error()
		return check
	}
	status, _ := output["status"].(string)
	if status != "OK" {
		check.State = failedState
		check.Detail = fmt.Sprintf("unexpected sync status %q", status)
		return check
	}
	counts, _ := output["counts"].(map[string]any)
	stops, _ := number(counts["stops"])
	indexed, indexedOK := number(output["entities_indexed"])
	if !indexedOK {
		indexed, _ = number(counts["entities_indexed"])
	}
	if stops < 1 || indexed < 1 {
		check.State = failedState
		check.Detail = "GTFS import produced no indexed stops"
		return check
	}
	check.State = verifiedState
	check.Count = int(stops)
	check.Detail = fmt.Sprintf("%d stop entities indexed", int(indexed))
	return check
}

func assessVelovBriefing(envelope map[string]any) checkEvidence {
	status, _ := envelope["status"].(string)
	check := checkEvidence{Name: "personal_briefing_velov", EnvelopeStatus: status}
	if status == "error" || status == "invalid_request" {
		check.State = failedState
		check.Detail = "briefing returned a failure status"
		return check
	}
	if status == "unavailable" || status == "not_found" {
		check.State = unavailableState
		check.Detail = "briefing profile or source is unavailable"
		return check
	}
	data, _ := envelope["data"].(map[string]any)
	facts, _ := data["facts"].(map[string]any)
	if !stringListContains(facts["include"], "velov") {
		check.State = failedState
		check.Detail = "weekday_morning does not include velov"
		return check
	}
	velov, _ := facts["velov"].(map[string]any)
	stations, _ := velov["stations"].([]any)
	check.Count = len(stations)
	if len(stations) == 0 {
		check.State = unavailableState
		check.Detail = "briefing contains no verifiable Velov stations"
		return check
	}
	now := time.Now().UTC()
	missingFreshness := false
	staleFreshness := false
	for _, raw := range stations {
		station, _ := raw.(map[string]any)
		bikes, bikesOK := number(station["bikes_available"])
		docks, docksOK := number(station["docks_available"])
		if !bikesOK || !docksOK || bikes < 0 || docks < 0 {
			check.State = failedState
			check.Detail = "station bike or dock counts are invalid"
			return check
		}
		observedRaw, observedOK := station["observed_at"].(string)
		if !observedOK || observedRaw == "" {
			missingFreshness = true
			continue
		}
		observed, observedErr := time.Parse(time.RFC3339, observedRaw)
		if observedErr != nil {
			check.State = failedState
			check.Detail = "station observation timestamp is invalid"
			return check
		}
		age := now.Sub(observed)
		if age < -time.Minute {
			check.State = failedState
			check.Detail = "station observation timestamp is in the future"
			return check
		}
		if age > 15*time.Minute {
			staleFreshness = true
		}
	}
	if missingFreshness || staleFreshness {
		check.State = unavailableState
		check.Detail = "station counts are verified, but fresh observations are unavailable"
		return check
	}
	check.State = verifiedState
	check.Detail = "stations include bike and dock counts with fresh timestamps"
	return check
}

func assessParkingAvailability(envelope map[string]any) checkEvidence {
	status, _ := envelope["status"].(string)
	check := checkEvidence{Name: "parking_availability", EnvelopeStatus: status}
	if status == "error" || status == "invalid_request" {
		check.State = failedState
		check.Detail = "parking availability returned a failure status"
		return check
	}
	if status == "unavailable" {
		check.State = unavailableState
		check.Detail = "parking availability source is unavailable"
		return check
	}
	options := objectList(envelope, "options")
	now := time.Now().UTC()
	realtimeCount := 0
	freshCount := 0
	missingTimestamp := 0
	staleTimestamp := 0
	for _, raw := range options {
		option, _ := raw.(map[string]any)
		available, availableOK := number(option["available_spaces"])
		realtime, _ := option["realtime"].(bool)
		if !availableOK || available < 0 || !realtime {
			continue
		}
		realtimeCount++
		observedRaw, observedOK := option["observed_at"].(string)
		if !observedOK || observedRaw == "" {
			missingTimestamp++
			continue
		}
		observed, observedErr := time.Parse(time.RFC3339, observedRaw)
		if observedErr != nil {
			check.State = failedState
			check.Detail = "parking observation timestamp is invalid"
			return check
		}
		age := now.Sub(observed)
		if age < -time.Minute {
			check.State = failedState
			check.Detail = "parking observation timestamp is in the future"
			return check
		}
		if age <= 5*time.Minute {
			freshCount++
			continue
		}
		staleTimestamp++
	}
	if freshCount != 0 {
		check.State = verifiedState
		check.Count = freshCount
		check.Detail = "realtime availability includes fresh observations"
		return check
	}
	check.State = notVerifiedState
	check.Count = realtimeCount
	switch {
	case realtimeCount == 0:
		check.Detail = "no parking row has numeric realtime availability"
	case missingTimestamp != 0:
		check.Detail = "realtime availability has no observable freshness timestamp"
	case staleTimestamp != 0:
		check.Detail = "realtime availability observations are stale"
	default:
		check.Detail = "realtime parking availability is not verified"
	}
	return check
}

func stringListContains(value any, wanted string) bool {
	items, _ := value.([]any)
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func objectList(envelope map[string]any, key string) []any {
	data, _ := envelope["data"].(map[string]any)
	items, _ := data[key].([]any)
	return items
}

func verifiedStop(candidates []any) (string, bool) {
	for _, raw := range candidates {
		candidate, _ := raw.(map[string]any)
		identifier, _ := candidate["id"].(string)
		kind, _ := candidate["type"].(string)
		if identifier != "" && (kind == "transport_stop" || strings.HasPrefix(identifier, "gtfs:stop:")) {
			return identifier, true
		}
	}
	return "", false
}

func fail(result *report, secrets []string, err error) int {
	result.Status = "failed"
	result.Error = cleanText(err.Error(), secrets)
	return 1
}

func writeReport(path string, result report, secrets []string) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = scrub(data, secrets)
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func scrub(data []byte, secrets []string) []byte {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		data = bytes.ReplaceAll(data, []byte(secret), []byte("[REDACTED]"))
		encoded, _ := json.Marshal(secret)
		if len(encoded) >= 2 {
			data = bytes.ReplaceAll(data, encoded[1:len(encoded)-1], []byte("[REDACTED]"))
		}
	}
	return data
}

func cleanText(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func within(path, parent string) bool {
	relative, err := filepath.Rel(parent, path)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))))
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func dataDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func gitOutput(repository string, args ...string) (string, error) {
	data, err := gitBytes(repository, args...)
	return string(data), err
}

func gitBytes(repository string, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = repository
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return output, nil
}
