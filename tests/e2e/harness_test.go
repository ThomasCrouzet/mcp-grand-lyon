package e2e_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	configEnv   = "GRAND_LYON_MCP_CONFIG_DIR"
	dataEnv     = "GRAND_LYON_MCP_DATA_DIR"
	databaseEnv = "GRAND_LYON_MCP_DB_PATH"
	fixturesEnv = "GRAND_LYON_MCP_FIXTURES_DIR"
	offlineEnv  = "GRAND_LYON_MCP_OFFLINE"
)

var acceptanceCaseNames = []string{
	"installed-setup",
	"all-tools-and-eof",
	"nested-preference-defaults",
	"wrong-stop-and-unknown-place",
	"transit-cases",
	"mixed-parking",
	"waste-constraints",
	"gtfs-import-rollback",
	"startup-invalid-config",
	"startup-invalid-database",
	"setup-destination-safety",
	"secrets-file-safety",
	"finite-config-values",
	"signal-shutdown",
	"effective-config-reporting",
	"validation-place-ref",
	"validation-control-payloads",
	"validation-frame-bounds",
}

var testHarness *harness

type harness struct {
	root        string
	artifact    string
	binary      string
	buildBinary bool
	baseEnv     []string
	fixtures    string
	coverageDir string
	report      evidenceReport
	reportLock  sync.Mutex
}

type evidenceReport struct {
	Source         sourceEvidence    `json:"source"`
	FixturesSHA256 map[string]string `json:"fixtures_sha256"`
	Binary         binaryEvidence    `json:"binary"`
	CoverageDir    string            `json:"coverage_dir,omitempty"`
	Commands       []commandEvidence `json:"commands"`
	Checks         []checkEvidence   `json:"checks"`
	Status         string            `json:"status"`
	Error          string            `json:"error,omitempty"`
}

type binaryEvidence struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type sourceEvidence struct {
	Revision          string            `json:"revision"`
	WorkingDiffSHA256 string            `json:"working_diff_sha256"`
	WorkingTreeSHA256 string            `json:"working_tree_sha256"`
	UntrackedSHA256   map[string]string `json:"untracked_sha256"`
	GoVersion         string            `json:"go_version"`
	Platform          string            `json:"platform"`
	TestCommand       []string          `json:"test_command"`
}

type commandEvidence struct {
	Argv        []string          `json:"argv"`
	Cwd         string            `json:"cwd"`
	Environment map[string]string `json:"environment"`
	ReturnCode  int               `json:"returncode"`
	Stdout      string            `json:"stdout,omitempty"`
	Stderr      string            `json:"stderr,omitempty"`
	Transcript  string            `json:"transcript,omitempty"`
	StderrLog   string            `json:"stderr_log,omitempty"`
	DurationMS  int64             `json:"duration_ms"`
}

type checkEvidence struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type commandResult struct {
	Stdout     []byte
	Stderr     []byte
	ReturnCode int
}

func TestMain(m *testing.M) {
	h, err := newHarness()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e harness: %v\n", err)
		os.Exit(1)
	}
	testHarness = h
	code := 1
	defer func() {
		if code == 0 {
			h.report.Status = "passed"
			for _, check := range h.report.Checks {
				if check.Status != "passed" {
					h.report.Status = "partial"
					break
				}
			}
		} else {
			h.report.Status = "failed"
		}
		if err := h.writeReport(); err != nil {
			fmt.Fprintf(os.Stderr, "write e2e report: %v\n", err)
			code = 1
		}
		fmt.Printf("Evidence: %s\n", filepath.Join(h.artifact, "report.json"))
		os.Exit(code)
	}()

	if h.buildBinary {
		arguments := []string{"go", "build"}
		if h.coverageDir != "" {
			arguments = append(arguments, "-cover", "-coverpkg=github.com/ThomasCrouzet/mcp-grand-lyon/...")
		}
		arguments = append(arguments, "-o", h.binary, "./cmd/grand-lyon-mcp")
		if _, err := h.runCommand(h.root, h.baseEnv, 3*time.Minute, arguments...); err != nil {
			h.report.Error = err.Error()
			return
		}
	}
	if err := h.captureBinary(); err != nil {
		h.report.Error = err.Error()
		return
	}
	code = m.Run()
}

func newHarness() (*harness, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return nil, errors.New("cannot locate the e2e test source")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	artifactBase := os.Getenv("GLMCP_ARTIFACT_DIR")
	if artifactBase == "" {
		artifactBase = os.TempDir()
	} else {
		artifactBase, err = filepath.Abs(artifactBase)
		if err != nil {
			return nil, fmt.Errorf("resolve GLMCP_ARTIFACT_DIR: %w", err)
		}
		if isWithin(artifactBase, root) {
			return nil, errors.New("GLMCP_ARTIFACT_DIR must be outside the repository")
		}
		if err := os.MkdirAll(artifactBase, 0o755); err != nil {
			return nil, fmt.Errorf("create artifact base: %w", err)
		}
	}
	artifact, err := os.MkdirTemp(artifactBase, "grand-lyon-mcp-e2e-")
	if err != nil {
		return nil, fmt.Errorf("create artifact directory: %w", err)
	}
	binDir := filepath.Join(artifact, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		return nil, fmt.Errorf("create binary directory: %w", err)
	}
	coverageDir := os.Getenv("GLMCP_COVERAGE_DIR")
	if coverageDir != "" {
		coverageDir, err = filepath.Abs(coverageDir)
		if err != nil {
			return nil, fmt.Errorf("resolve GLMCP_COVERAGE_DIR: %w", err)
		}
		if isWithin(coverageDir, root) {
			return nil, errors.New("GLMCP_COVERAGE_DIR must be outside the repository")
		}
		if err := os.MkdirAll(coverageDir, 0o755); err != nil {
			return nil, fmt.Errorf("create coverage directory: %w", err)
		}
	}
	binary := os.Getenv("GLMCP_BINARY")
	buildBinary := binary == ""
	if buildBinary {
		binary = filepath.Join(binDir, "grand-lyon-mcp")
	} else {
		binary, err = filepath.Abs(binary)
		if err != nil {
			return nil, fmt.Errorf("resolve GLMCP_BINARY: %w", err)
		}
		info, statErr := os.Stat(binary)
		if statErr != nil {
			return nil, fmt.Errorf("inspect GLMCP_BINARY: %w", statErr)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return nil, errors.New("GLMCP_BINARY must be an executable regular file")
		}
	}
	h := &harness{
		root:        root,
		artifact:    artifact,
		binary:      binary,
		buildBinary: buildBinary,
		baseEnv:     cleanEnvironment(),
		fixtures:    filepath.Join(root, "internal", "assets", "fixtures"),
		coverageDir: coverageDir,
	}
	h.report.Status = "running"
	h.report.CoverageDir = coverageDir
	for _, name := range acceptanceCaseNames {
		h.report.Checks = append(h.report.Checks, checkEvidence{Name: name, Status: "not_run"})
	}
	if err := h.captureSource(); err != nil {
		return nil, err
	}
	fixtureHashes, err := treeHashes(h.fixtures)
	if err != nil {
		return nil, fmt.Errorf("hash fixtures: %w", err)
	}
	h.report.FixturesSHA256 = fixtureHashes
	return h, nil
}

func (h *harness) captureBinary() error {
	info, err := os.Stat(h.binary)
	if err != nil {
		return fmt.Errorf("inspect tested binary: %w", err)
	}
	digest, err := fileDigest(h.binary)
	if err != nil {
		return fmt.Errorf("hash tested binary: %w", err)
	}
	h.report.Binary = binaryEvidence{Path: h.binary, SHA256: digest, Size: info.Size()}
	return nil
}

func (h *harness) captureSource() error {
	revision, err := gitOutput(h.root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	diff, err := gitBytes(h.root, "diff", "--binary", "HEAD")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(h.artifact, "working.patch"), diff, 0o644); err != nil {
		return fmt.Errorf("write working patch: %w", err)
	}
	untrackedOutput, err := gitBytes(h.root, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	untracked := make(map[string]string)
	for _, rawName := range bytes.Split(untrackedOutput, []byte{0}) {
		if len(rawName) == 0 {
			continue
		}
		name := string(rawName)
		path := filepath.Join(h.root, filepath.FromSlash(name))
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		digest, digestErr := fileDigest(path)
		if digestErr != nil {
			return digestErr
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
	h.report.Source = sourceEvidence{
		Revision:          strings.TrimSpace(revision),
		WorkingDiffSHA256: bytesDigest(diff),
		WorkingTreeSHA256: hex.EncodeToString(combined.Sum(nil)),
		UntrackedSHA256:   untracked,
		GoVersion:         runtime.Version(),
		Platform:          runtime.GOOS + "/" + runtime.GOARCH,
		TestCommand:       append([]string(nil), os.Args...),
	}
	return nil
}

func (h *harness) newEnvironment(name string) (string, []string, map[string]string, error) {
	folder := filepath.Join(h.artifact, "cases", name)
	config := filepath.Join(folder, "config")
	data := filepath.Join(folder, "data")
	database := filepath.Join(folder, "app.db")
	if err := os.MkdirAll(config, 0o755); err != nil {
		return "", nil, nil, err
	}
	values := map[string]string{
		configEnv:   config,
		dataEnv:     data,
		databaseEnv: database,
		offlineEnv:  "true",
		"NO_COLOR":  "1",
	}
	if h.coverageDir != "" {
		values["GOCOVERDIR"] = h.coverageDir
	}
	env := mergeEnvironment(h.baseEnv, values)
	return folder, env, values, nil
}

func (h *harness) runCommand(cwd string, env []string, timeout time.Duration, argv ...string) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	result := commandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ReturnCode: 0}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ReturnCode = exitErr.ExitCode()
		} else if ctx.Err() != nil {
			result.ReturnCode = -1
		} else {
			result.ReturnCode = -1
		}
	}
	h.addCommand(commandEvidence{
		Argv:        append([]string(nil), argv...),
		Cwd:         cwd,
		Environment: reportEnvironment(env),
		ReturnCode:  result.ReturnCode,
		Stdout:      string(result.Stdout),
		Stderr:      string(result.Stderr),
		DurationMS:  time.Since(start).Milliseconds(),
	})
	if ctx.Err() != nil {
		return result, fmt.Errorf("command timed out: %s", strings.Join(argv, " "))
	}
	if err != nil {
		return result, fmt.Errorf("command failed (%d): %s: %s", result.ReturnCode, strings.Join(argv, " "), strings.TrimSpace(string(result.Stderr)))
	}
	return result, nil
}

func (h *harness) addCommand(command commandEvidence) {
	h.reportLock.Lock()
	defer h.reportLock.Unlock()
	h.report.Commands = append(h.report.Commands, command)
}

func (h *harness) setCheck(name, status string) {
	h.reportLock.Lock()
	defer h.reportLock.Unlock()
	for index := range h.report.Checks {
		if h.report.Checks[index].Name == name {
			h.report.Checks[index].Status = status
			return
		}
	}
	h.report.Checks = append(h.report.Checks, checkEvidence{Name: name, Status: status})
}

func (h *harness) writeReport() error {
	h.reportLock.Lock()
	defer h.reportLock.Unlock()
	data, err := json.MarshalIndent(h.report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(h.artifact, "report.json"), data, 0o644)
}

func runCase(t *testing.T, name string, test func(t *testing.T)) {
	t.Helper()
	ran := false
	skipped := false
	passed := t.Run(name, func(t *testing.T) {
		ran = true
		testHarness.setCheck(name, "running")
		defer func() { skipped = t.Skipped() }()
		test(t)
	})
	if !ran {
		return
	}
	if skipped {
		testHarness.setCheck(name, "skipped")
		return
	}
	status := "failed"
	if passed {
		status = "passed"
	}
	testHarness.setCheck(name, status)
}

func cleanEnvironment() []string {
	result := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "GRAND_LYON_MCP_") ||
			strings.HasPrefix(key, "DATAGRANDLYON_") ||
			strings.HasPrefix(key, "TRANSITOUS_") ||
			strings.HasPrefix(key, "GLMCP_") {
			continue
		}
		result = append(result, item)
	}
	return mergeEnvironment(result, map[string]string{offlineEnv: "true", "NO_COLOR": "1"})
}

func mergeEnvironment(base []string, values map[string]string) []string {
	merged := make(map[string]string, len(base)+len(values))
	for _, item := range base {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			merged[key] = value
		}
	}
	for key, value := range values {
		merged[key] = value
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+merged[key])
	}
	return result
}

func reportEnvironment(env []string) map[string]string {
	result := make(map[string]string)
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "GRAND_LYON_MCP_") &&
			!strings.Contains(key, "PASSWORD") &&
			!strings.Contains(key, "USERNAME") {
			result[key] = value
		}
		if key == "NO_COLOR" {
			result[key] = value
		}
		if key == "GOCOVERDIR" {
			result[key] = value
		}
	}
	return result
}

func treeHashes(root string) (map[string]string, error) {
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		digest, err := fileDigest(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = digest
		return nil
	})
	return result, err
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

func bytesDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func gitOutput(root string, args ...string) (string, error) {
	data, err := gitBytes(root, args...)
	return string(data), err
}

func gitBytes(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return data, nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func makeZip(destination, sourceDir string, transform func(name string, data []byte) []byte) error {
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(file)
	walkErr := filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if transform != nil {
			data = transform(filepath.ToSlash(name), data)
		}
		header := &zip.FileHeader{Name: filepath.ToSlash(name), Method: zip.Store}
		header.SetModTime(time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = writer.Write(data)
		return err
	})
	closeArchiveErr := archive.Close()
	closeFileErr := file.Close()
	return errors.Join(walkErr, closeArchiveErr, closeFileErr)
}

func isWithin(path, parent string) bool {
	relative, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}
