package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"go.yaml.in/yaml/v3"
)

const (
	appName         = "grand-lyon-mcp"
	settingsVersion = 1
)

var supportedEnvironmentKeys = []string{
	"DATAGRANDLYON_USERNAME",
	"DATAGRANDLYON_PASSWORD",
	"GRAND_LYON_MCP_CONFIG_DIR",
	"GRAND_LYON_MCP_DATA_DIR",
	"GRAND_LYON_MCP_DB_PATH",
	"GRAND_LYON_MCP_FIXTURES_DIR",
	"GRAND_LYON_MCP_LOG_LEVEL",
	"GRAND_LYON_MCP_OFFLINE",
	"GRAND_LYON_MCP_VERIFY_TLS",
	"GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS",
	"GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS",
	"GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS",
	"GRAND_LYON_MCP_FUZZY_SCORE_THRESHOLD",
	"TRANSITOUS_ENABLED",
	"TRANSITOUS_BASE_URL",
}

var supportedEnvironmentKeySet = func() map[string]struct{} {
	result := make(map[string]struct{}, len(supportedEnvironmentKeys))
	for _, key := range supportedEnvironmentKeys {
		result[key] = struct{}{}
	}
	return result
}()

// Config contains validated runtime configuration.
type Config struct {
	ConfigDir           string
	DataDir             string
	DBPath              string
	FixturesDir         string
	LogLevel            string
	Username            string
	Password            string
	TransitousBaseURL   string
	Offline             bool
	VerifyTLS           bool
	TransitousEnabled   bool
	MaxParallel         int
	ConnectTimeout      time.Duration
	ReadTimeout         time.Duration
	FuzzyScoreThreshold int
	JourneyScoring      model.JourneyScoringWeights
}

type yamlConfig struct {
	Version        int                `yaml:"version"`
	Runtime        yamlRuntime        `yaml:"runtime"`
	Results        yamlResults        `yaml:"results"`
	Places         yamlPlaces         `yaml:"places"`
	Cache          yamlCache          `yaml:"cache"`
	Network        yamlNetwork        `yaml:"network"`
	JourneyScoring yamlJourneyScoring `yaml:"journey_scoring"`
}

type yamlRuntime struct {
	Offline             *looseBool `yaml:"offline"`
	Timezone            *string    `yaml:"timezone"`
	Locale              *string    `yaml:"locale"`
	MaxParallelRequests *int       `yaml:"max_parallel_requests"`
}

type yamlResults struct {
	MaximumItemsPerTool *int `yaml:"maximum_items_per_tool"`
	MaximumWarningCount *int `yaml:"maximum_warning_count"`
}

type yamlPlaces struct {
	DefaultRadiusM      *int `yaml:"default_radius_m"`
	MaximumRadiusM      *int `yaml:"maximum_radius_m"`
	FuzzyScoreThreshold *int `yaml:"fuzzy_score_threshold"`
}

type yamlCache struct {
	AllowStaleOnError  *looseBool `yaml:"allow_stale_on_error"`
	NegativeTTLSeconds *int       `yaml:"negative_ttl_seconds"`
}

type yamlNetwork struct {
	ConnectTimeoutSeconds *float64   `yaml:"connect_timeout_seconds"`
	ReadTimeoutSeconds    *float64   `yaml:"read_timeout_seconds"`
	MaximumAttempts       *int       `yaml:"maximum_attempts"`
	VerifyTLS             *looseBool `yaml:"verify_tls"`
}

type yamlJourneyScoring struct {
	Duration       *float64 `yaml:"duration"`
	Reliability    *float64 `yaml:"reliability"`
	Disruptions    *float64 `yaml:"disruptions"`
	Walking        *float64 `yaml:"walking"`
	Availability   *float64 `yaml:"availability"`
	UserPreference *float64 `yaml:"user_preference"`
}

type looseBool bool

func (value *looseBool) UnmarshalYAML(node *yaml.Node) error {
	parsed, err := strconv.ParseBool(strings.TrimSpace(node.Value))
	if err != nil {
		return fmt.Errorf("invalid boolean %q", node.Value)
	}
	*value = looseBool(parsed)
	return nil
}

// Load reads environment, user secrets, and YAML without mutating process state.
func Load() (Config, error) {
	return load(true)
}

// LoadForSetup skips existing settings when setup replaces them.
func LoadForSetup(replaceSettings bool) (Config, error) {
	return load(!replaceSettings)
}

func load(readSettings bool) (Config, error) {
	environment := selectEnvironment(os.Environ())
	configDir, dataDir, err := defaultDirectories()
	if err != nil {
		return Config{}, err
	}
	if value := environment["GRAND_LYON_MCP_CONFIG_DIR"]; value != "" {
		configDir = value
	}
	if value := environment["GRAND_LYON_MCP_DATA_DIR"]; value != "" {
		dataDir = value
	}

	secrets, err := readSecrets(filepath.Join(configDir, "secrets.env"))
	if err != nil {
		return Config{}, err
	}
	values := make(map[string]string, len(secrets)+len(environment))
	for key, value := range secrets {
		if value != "" {
			values[key] = value
		}
	}
	for key, value := range environment {
		if value != "" {
			values[key] = value
		}
	}
	if value := values["GRAND_LYON_MCP_CONFIG_DIR"]; value != "" {
		configDir = value
	}
	if value := values["GRAND_LYON_MCP_DATA_DIR"]; value != "" {
		dataDir = value
	}

	cfg := defaultConfig(configDir, dataDir)
	if readSettings {
		if err := applyYAML(&cfg, filepath.Join(configDir, "settings.yaml")); err != nil {
			return Config{}, err
		}
	}
	if err := applyEnvironment(&cfg, values); err != nil {
		return Config{}, err
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// WriteSecrets stores runtime values in the resolved user configuration directory.
func WriteSecrets(cfg Config) error {
	values := environmentValues(cfg)
	var contents bytes.Buffer
	for _, key := range supportedEnvironmentKeys {
		value, err := encodeEnvironmentValue(values[key])
		if err != nil {
			return fmt.Errorf("encode secrets key %s: %w", key, err)
		}
		fmt.Fprintf(&contents, "%s=%s\n", key, value)
	}
	if err := os.MkdirAll(cfg.ConfigDir, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	path := filepath.Join(cfg.ConfigDir, "secrets.env")
	temporary, err := os.CreateTemp(cfg.ConfigDir, ".secrets-*")
	if err != nil {
		return fmt.Errorf("create temporary secrets file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary secrets permissions: %w", err)
	}
	if _, err := temporary.Write(contents.Bytes()); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary secrets file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary secrets file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace secrets file: %w", err)
	}
	return nil
}

// ClientEnvironment returns the nonsecret values required by an MCP client.
func ClientEnvironment(cfg Config) map[string]string {
	return map[string]string{
		"GRAND_LYON_MCP_CONFIG_DIR": cfg.ConfigDir,
		"GRAND_LYON_MCP_DATA_DIR":   cfg.DataDir,
		"GRAND_LYON_MCP_DB_PATH":    cfg.DBPath,
		"GRAND_LYON_MCP_OFFLINE":    strconv.FormatBool(cfg.Offline),
	}
}

func defaultConfig(configDir, dataDir string) Config {
	return Config{
		ConfigDir:           configDir,
		DataDir:             dataDir,
		DBPath:              filepath.Join(dataDir, "grand_lyon_mcp.db"),
		LogLevel:            "INFO",
		TransitousBaseURL:   "https://api.transitous.org/api/",
		VerifyTLS:           true,
		MaxParallel:         6,
		ConnectTimeout:      5 * time.Second,
		ReadTimeout:         20 * time.Second,
		FuzzyScoreThreshold: 72,
		JourneyScoring: model.JourneyScoringWeights{
			Duration:       0.35,
			Reliability:    0.25,
			Disruptions:    0.15,
			Walking:        0.10,
			Availability:   0.10,
			UserPreference: 0.05,
		},
	}
}

func applyYAML(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read settings %s: %w", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("parse settings %s: %w", path, err)
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("settings file must contain a mapping: %s", path)
	}
	var overlay yamlConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&overlay); err != nil {
		return fmt.Errorf("parse settings %s: %w", path, err)
	}
	if overlay.Version == 0 {
		overlay.Version = settingsVersion
	}
	if overlay.Version != settingsVersion {
		return fmt.Errorf("unsupported settings version: %d", overlay.Version)
	}
	if overlay.Runtime.Offline != nil {
		cfg.Offline = bool(*overlay.Runtime.Offline)
	}
	if overlay.Runtime.MaxParallelRequests != nil {
		cfg.MaxParallel = *overlay.Runtime.MaxParallelRequests
	}
	if overlay.Places.FuzzyScoreThreshold != nil {
		cfg.FuzzyScoreThreshold = *overlay.Places.FuzzyScoreThreshold
	}
	if overlay.Network.ConnectTimeoutSeconds != nil {
		if !finite(*overlay.Network.ConnectTimeoutSeconds) {
			return errors.New("network.connect_timeout_seconds must be finite")
		}
		cfg.ConnectTimeout = seconds(*overlay.Network.ConnectTimeoutSeconds)
	}
	if overlay.Network.ReadTimeoutSeconds != nil {
		if !finite(*overlay.Network.ReadTimeoutSeconds) {
			return errors.New("network.read_timeout_seconds must be finite")
		}
		cfg.ReadTimeout = seconds(*overlay.Network.ReadTimeoutSeconds)
	}
	if overlay.Network.VerifyTLS != nil {
		cfg.VerifyTLS = bool(*overlay.Network.VerifyTLS)
	}
	applyJourneyYAML(&cfg.JourneyScoring, overlay.JourneyScoring)
	return nil
}

func applyJourneyYAML(weights *model.JourneyScoringWeights, overlay yamlJourneyScoring) {
	if overlay.Duration != nil {
		weights.Duration = *overlay.Duration
	}
	if overlay.Reliability != nil {
		weights.Reliability = *overlay.Reliability
	}
	if overlay.Disruptions != nil {
		weights.Disruptions = *overlay.Disruptions
	}
	if overlay.Walking != nil {
		weights.Walking = *overlay.Walking
	}
	if overlay.Availability != nil {
		weights.Availability = *overlay.Availability
	}
	if overlay.UserPreference != nil {
		weights.UserPreference = *overlay.UserPreference
	}
}

func applyEnvironment(cfg *Config, values map[string]string) error {
	cfg.ConfigDir = valueOr(values, "GRAND_LYON_MCP_CONFIG_DIR", cfg.ConfigDir)
	cfg.DataDir = valueOr(values, "GRAND_LYON_MCP_DATA_DIR", cfg.DataDir)
	cfg.DBPath = valueOr(values, "GRAND_LYON_MCP_DB_PATH", filepath.Join(cfg.DataDir, "grand_lyon_mcp.db"))
	cfg.FixturesDir = valueOr(values, "GRAND_LYON_MCP_FIXTURES_DIR", cfg.FixturesDir)
	cfg.LogLevel = strings.ToUpper(valueOr(values, "GRAND_LYON_MCP_LOG_LEVEL", cfg.LogLevel))
	cfg.Username = valueOr(values, "DATAGRANDLYON_USERNAME", cfg.Username)
	cfg.Password = valueOr(values, "DATAGRANDLYON_PASSWORD", cfg.Password)
	cfg.TransitousBaseURL = valueOr(values, "TRANSITOUS_BASE_URL", cfg.TransitousBaseURL)

	var err error
	if cfg.Offline, err = boolValue(values, "GRAND_LYON_MCP_OFFLINE", cfg.Offline); err != nil {
		return err
	}
	if cfg.VerifyTLS, err = boolValue(values, "GRAND_LYON_MCP_VERIFY_TLS", cfg.VerifyTLS); err != nil {
		return err
	}
	if cfg.TransitousEnabled, err = boolValue(values, "TRANSITOUS_ENABLED", cfg.TransitousEnabled); err != nil {
		return err
	}
	if cfg.MaxParallel, err = intValue(values, "GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS", cfg.MaxParallel); err != nil {
		return err
	}
	if cfg.FuzzyScoreThreshold, err = intValue(values, "GRAND_LYON_MCP_FUZZY_SCORE_THRESHOLD", cfg.FuzzyScoreThreshold); err != nil {
		return err
	}
	if cfg.ConnectTimeout, err = durationValue(values, "GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS", cfg.ConnectTimeout); err != nil {
		return err
	}
	if cfg.ReadTimeout, err = durationValue(values, "GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS", cfg.ReadTimeout); err != nil {
		return err
	}
	return nil
}

func validate(cfg Config) error {
	if cfg.ConfigDir == "" || cfg.DataDir == "" || cfg.DBPath == "" {
		return errors.New("configuration, data, and database paths must not be empty")
	}
	if cfg.ConnectTimeout <= 0 || cfg.ReadTimeout <= 0 {
		return errors.New("HTTP timeouts must be greater than zero")
	}
	if cfg.MaxParallel < 1 {
		return errors.New("max_parallel_requests must be at least 1")
	}
	if cfg.FuzzyScoreThreshold < 0 || cfg.FuzzyScoreThreshold > 100 {
		return errors.New("fuzzy_score_threshold must be between 0 and 100")
	}
	weights := []float64{
		cfg.JourneyScoring.Duration,
		cfg.JourneyScoring.Reliability,
		cfg.JourneyScoring.Disruptions,
		cfg.JourneyScoring.Walking,
		cfg.JourneyScoring.Availability,
		cfg.JourneyScoring.UserPreference,
	}
	total := 0.0
	for _, weight := range weights {
		if !finite(weight) {
			return errors.New("journey scoring weights must be finite")
		}
		if weight < 0 || weight > 1 {
			return errors.New("journey scoring weights must be between 0 and 1")
		}
		total += weight
	}
	if math.Abs(total-1) > 1e-6 {
		return fmt.Errorf("journey scoring weights must sum to 1, got %g", total)
	}
	return nil
}

func defaultDirectories() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve home directory: %w", err)
	}
	return platformDirectories(home)
}

func readSecrets(path string) (map[string]string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect secrets %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("secrets file must not be a symbolic link: %s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("secrets file must be a regular file: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("secrets file permissions must deny group and other access: %s", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read secrets %s: %w", path, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect open secrets %s: %w", path, err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("secrets file changed while opening: %s", path)
	}
	if runtime.GOOS != "windows" && openedInfo.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("secrets file permissions must deny group and other access: %s", path)
	}

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found {
			return nil, fmt.Errorf("invalid secrets entry on line %d: expected KEY=value", lineNumber)
		}
		if _, approved := supportedEnvironmentKeySet[key]; !approved {
			continue
		}
		value, err := decodeEnvironmentValue(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("parse secrets key %s: %w", key, err)
		}
		result[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read secrets %s: %w", path, err)
	}
	return result, nil
}

func decodeEnvironmentValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	quote := raw[0]
	if quote != '\'' && quote != '"' {
		return raw, nil
	}
	if len(raw) < 2 || raw[len(raw)-1] != quote {
		return "", errors.New("unterminated quoted value")
	}
	body := raw[1 : len(raw)-1]
	if quote == '\'' {
		return body, nil
	}
	var decoded strings.Builder
	for index := 0; index < len(body); index++ {
		character := body[index]
		if character != '\\' {
			decoded.WriteByte(character)
			continue
		}
		if index+1 >= len(body) {
			decoded.WriteByte('\\')
			continue
		}
		next := body[index+1]
		if strings.ContainsRune("$`\"\\", rune(next)) {
			decoded.WriteByte(next)
			index++
			continue
		}
		decoded.WriteByte('\\')
	}
	return decoded.String(), nil
}

func encodeEnvironmentValue(value string) (string, error) {
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("environment values cannot contain line breaks or null bytes")
	}
	if value == "" {
		return "", nil
	}
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"`", "\\`",
		"$", "\\$",
	)
	return "\"" + replacer.Replace(value) + "\"", nil
}

func environmentValues(cfg Config) map[string]string {
	return map[string]string{
		"DATAGRANDLYON_USERNAME":                      cfg.Username,
		"DATAGRANDLYON_PASSWORD":                      cfg.Password,
		"GRAND_LYON_MCP_CONFIG_DIR":                   cfg.ConfigDir,
		"GRAND_LYON_MCP_DATA_DIR":                     cfg.DataDir,
		"GRAND_LYON_MCP_DB_PATH":                      cfg.DBPath,
		"GRAND_LYON_MCP_FIXTURES_DIR":                 cfg.FixturesDir,
		"GRAND_LYON_MCP_LOG_LEVEL":                    cfg.LogLevel,
		"GRAND_LYON_MCP_OFFLINE":                      strconv.FormatBool(cfg.Offline),
		"GRAND_LYON_MCP_VERIFY_TLS":                   strconv.FormatBool(cfg.VerifyTLS),
		"GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS": formatSeconds(cfg.ConnectTimeout),
		"GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS":    formatSeconds(cfg.ReadTimeout),
		"GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS":        strconv.Itoa(cfg.MaxParallel),
		"GRAND_LYON_MCP_FUZZY_SCORE_THRESHOLD":        strconv.Itoa(cfg.FuzzyScoreThreshold),
		"TRANSITOUS_ENABLED":                          strconv.FormatBool(cfg.TransitousEnabled),
		"TRANSITOUS_BASE_URL":                         cfg.TransitousBaseURL,
	}
}

func formatSeconds(value time.Duration) string {
	return strconv.FormatFloat(value.Seconds(), 'f', -1, 64)
}

func selectEnvironment(items []string) map[string]string {
	result := make(map[string]string)
	for _, item := range items {
		key, value, found := strings.Cut(item, "=")
		if found {
			result[key] = value
		}
	}
	return result
}

func valueOr(values map[string]string, key, fallback string) string {
	if value := values[key]; value != "" {
		return value
	}
	return fallback
}

func boolValue(values map[string]string, key string, fallback bool) (bool, error) {
	value := values[key]
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return parsed, nil
}

func intValue(values map[string]string, key string, fallback int) (int, error) {
	value := values[key]
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func durationValue(values map[string]string, key string, fallback time.Duration) (time.Duration, error) {
	value := values[key]
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", key, err)
	}
	if !finite(parsed) {
		return 0, fmt.Errorf("%s must be finite", key)
	}
	return seconds(parsed), nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func seconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}
