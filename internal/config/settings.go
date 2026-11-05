package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type settingMapping struct {
	Environment string
	Section     string
	Field       string
	Kind        string
}

var settingMappings = []settingMapping{
	{"GRAND_LYON_MCP_DATA_DIR", "runtime", "data_dir", "string"},
	{"GRAND_LYON_MCP_DB_PATH", "runtime", "db_path", "string"},
	{"GRAND_LYON_MCP_FIXTURES_DIR", "runtime", "fixtures_dir", "string"},
	{"GRAND_LYON_MCP_LOG_LEVEL", "runtime", "log_level", "string"},
	{"GRAND_LYON_MCP_OFFLINE", "runtime", "offline", "bool"},
	{"GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS", "runtime", "max_parallel_requests", "int"},
	{"TRANSITOUS_ENABLED", "runtime", "transitous_enabled", "bool"},
	{"TRANSITOUS_BASE_URL", "runtime", "transitous_base_url", "string"},
	{"GRAND_LYON_MCP_VERIFY_TLS", "network", "verify_tls", "bool"},
	{"GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS", "network", "connect_timeout_seconds", "float"},
	{"GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS", "network", "read_timeout_seconds", "float"},
	{"GRAND_LYON_MCP_FUZZY_SCORE_THRESHOLD", "places", "fuzzy_score_threshold", "int"},
}

var inactiveSettings = [][2]string{
	{"runtime", "timezone"},
	{"runtime", "locale"},
	{"results", "maximum_items_per_tool"},
	{"results", "maximum_warning_count"},
	{"places", "default_radius_m"},
	{"places", "maximum_radius_m"},
	{"cache", "allow_stale_on_error"},
	{"cache", "negative_ttl_seconds"},
	{"network", "maximum_attempts"},
}

// MigrationResult summarizes an idempotent settings/secrets migration.
type MigrationResult struct {
	Changed   bool                `json:"changed"`
	Migrated  []string            `json:"migrated"`
	Conflicts []MigrationConflict `json:"conflicts"`
	Removed   []string            `json:"removed_legacy_keys"`
}

type MigrationConflict struct {
	Setting   string `json:"setting"`
	YAMLValue string `json:"yaml_value"`
	KeptValue string `json:"kept_value"`
}

// Migrate moves legacy nonsecret overrides from secrets.env into settings.yaml
// while preserving their effective values. A second run makes no changes.
func Migrate(cfg Config) (MigrationResult, error) {
	result := MigrationResult{Migrated: []string{}, Conflicts: []MigrationConflict{}, Removed: []string{}}
	secrets, err := readSecrets(filepath.Join(cfg.ConfigDir, "secrets.env"))
	if err != nil {
		return result, err
	}
	path := filepath.Join(cfg.ConfigDir, "settings.yaml")
	document, err := loadSettingsDocument(path)
	if err != nil {
		return result, err
	}
	settingsChanged := false
	legacySeen := map[string]bool{}
	for _, mapping := range settingMappings {
		value := secrets[mapping.Environment]
		if value == "" {
			continue
		}
		legacySeen[mapping.Environment] = true
		old, changed := setSettingsValue(document, mapping.Section, mapping.Field, value, mapping.Kind)
		if old != "" && normalizeSettingValue(old, mapping.Kind) != normalizeSettingValue(value, mapping.Kind) {
			result.Conflicts = append(result.Conflicts, MigrationConflict{Setting: mapping.Section + "." + mapping.Field, YAMLValue: old, KeptValue: value})
		}
		if changed {
			settingsChanged = true
		}
		result.Migrated = append(result.Migrated, mapping.Environment)
		delete(secrets, mapping.Environment)
	}
	if settingsChanged {
		if err := writeSettingsDocument(path, document); err != nil {
			return result, err
		}
		result.Changed = true
	}
	for key := range legacySeen {
		result.Removed = append(result.Removed, key)
	}
	secretValues := map[string]string{
		"DATAGRANDLYON_USERNAME":    cfg.Username,
		"DATAGRANDLYON_PASSWORD":    cfg.Password,
		"GRAND_LYON_MCP_CONFIG_DIR": cfg.ConfigDir,
	}
	if secrets["DATAGRANDLYON_USERNAME"] != "" {
		secretValues["DATAGRANDLYON_USERNAME"] = secrets["DATAGRANDLYON_USERNAME"]
	}
	if secrets["DATAGRANDLYON_PASSWORD"] != "" {
		secretValues["DATAGRANDLYON_PASSWORD"] = secrets["DATAGRANDLYON_PASSWORD"]
	}
	if secrets["GRAND_LYON_MCP_CONFIG_DIR"] != "" {
		secretValues["GRAND_LYON_MCP_CONFIG_DIR"] = secrets["GRAND_LYON_MCP_CONFIG_DIR"]
	}
	if err := writeSecretsFile(cfg.ConfigDir, secretValues); err != nil {
		return result, err
	}
	if len(legacySeen) != 0 {
		result.Changed = true
	}
	return result, nil
}

type SettingExplanation struct {
	Setting string `json:"setting"`
	Value   string `json:"value"`
	Origin  string `json:"origin"`
	Source  string `json:"source"`
	Active  bool   `json:"active"`
}

// Explain reports each effective nonsecret setting and where it came from.
func Explain() (map[string]any, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	environment := selectEnvironment(os.Environ())
	secrets, err := readSecrets(filepath.Join(cfg.ConfigDir, "secrets.env"))
	if err != nil {
		return nil, err
	}
	document, err := loadSettingsDocument(filepath.Join(cfg.ConfigDir, "settings.yaml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	settings := []SettingExplanation{}
	for _, mapping := range settingMappings {
		origin := "default"
		source := "built-in default"
		switch {
		case environment[mapping.Environment] != "":
			origin, source = "process", "process environment"
		case secrets[mapping.Environment] != "":
			origin, source = "legacy", "secrets.env legacy override"
		case settingsNode(document, mapping.Section, mapping.Field) != nil:
			origin, source = "yaml", "settings.yaml"
		}
		settings = append(settings, SettingExplanation{
			Setting: mapping.Environment, Value: effectiveSettingValue(cfg, mapping),
			Origin: origin, Source: source, Active: true,
		})
	}
	configOrigin := "default"
	configSource := "platform default"
	if environment["GRAND_LYON_MCP_CONFIG_DIR"] != "" {
		configOrigin, configSource = "process", "process environment"
	} else if secrets["GRAND_LYON_MCP_CONFIG_DIR"] != "" {
		configOrigin, configSource = "legacy", "secrets.env bootstrap"
	}
	settings = append([]SettingExplanation{{
		Setting: "GRAND_LYON_MCP_CONFIG_DIR", Value: cfg.ConfigDir, Origin: configOrigin, Source: configSource, Active: true,
	}}, settings...)
	credentials := []SettingExplanation{}
	for _, key := range []string{"DATAGRANDLYON_USERNAME", "DATAGRANDLYON_PASSWORD"} {
		origin, source := "missing", "not configured"
		if environment[key] != "" {
			origin, source = "process", "process environment"
		} else if secrets[key] != "" {
			origin, source = "legacy", "secrets.env"
		}
		value := "(empty)"
		if (key == "DATAGRANDLYON_USERNAME" && cfg.Username != "") || (key == "DATAGRANDLYON_PASSWORD" && cfg.Password != "") {
			value = "[set]"
		}
		credentials = append(credentials, SettingExplanation{Setting: key, Value: value, Origin: origin, Source: source, Active: true})
	}
	inactive := []SettingExplanation{}
	for _, item := range inactiveSettings {
		if node := settingsNode(document, item[0], item[1]); node != nil {
			inactive = append(inactive, SettingExplanation{
				Setting: item[0] + "." + item[1], Value: node.Value, Origin: "yaml", Source: "settings.yaml", Active: false,
			})
		}
	}
	return map[string]any{
		"precedence":  "process environment > settings.yaml > legacy secrets.env > built-in default",
		"settings":    settings,
		"credentials": credentials,
		"inactive":    inactive,
	}, nil
}

func effectiveSettingValue(cfg Config, mapping settingMapping) string {
	switch mapping.Environment {
	case "GRAND_LYON_MCP_DATA_DIR":
		return cfg.DataDir
	case "GRAND_LYON_MCP_DB_PATH":
		return cfg.DBPath
	case "GRAND_LYON_MCP_FIXTURES_DIR":
		return cfg.FixturesDir
	case "GRAND_LYON_MCP_LOG_LEVEL":
		return cfg.LogLevel
	case "GRAND_LYON_MCP_OFFLINE":
		return strconv.FormatBool(cfg.Offline)
	case "GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS":
		return strconv.Itoa(cfg.MaxParallel)
	case "TRANSITOUS_ENABLED":
		return strconv.FormatBool(cfg.TransitousEnabled)
	case "TRANSITOUS_BASE_URL":
		return cfg.TransitousBaseURL
	case "GRAND_LYON_MCP_VERIFY_TLS":
		return strconv.FormatBool(cfg.VerifyTLS)
	case "GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS":
		return formatSeconds(cfg.ConnectTimeout)
	case "GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS":
		return formatSeconds(cfg.ReadTimeout)
	case "GRAND_LYON_MCP_FUZZY_SCORE_THRESHOLD":
		return strconv.Itoa(cfg.FuzzyScoreThreshold)
	default:
		return ""
	}
}

func loadSettingsDocument(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read settings %s: %w", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse settings %s: %w", path, err)
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("settings file must contain a mapping: %s", path)
	}
	if settingsNode(&document, "", "version") == nil {
		setSettingsValue(&document, "", "version", "1", "int")
	}
	return &document, nil
}

func writeSettingsDocument(path string, document *yaml.Node) error {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("close settings encoder: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return fmt.Errorf("create temporary settings file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(buffer.Bytes()); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func settingsNode(document *yaml.Node, section, field string) *yaml.Node {
	if document == nil || len(document.Content) == 0 {
		return nil
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	if section == "" {
		return mappingValue(root, field)
	}
	node := mappingValue(root, section)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	return mappingValue(node, field)
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func setSettingsValue(document *yaml.Node, section, field, value, kind string) (string, bool) {
	root := document.Content[0]
	mapping := root
	if section != "" {
		node := mappingValue(root, section)
		if node == nil {
			key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: section}
			node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			root.Content = append(root.Content, key, node)
		}
		if node.Kind != yaml.MappingNode {
			return "", false
		}
		mapping = node
	}
	existing := mappingValue(mapping, field)
	encoded := yamlScalar(value, kind)
	if existing == nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: field}, encoded)
		return "", true
	}
	if normalizeSettingValue(existing.Value, kind) == normalizeSettingValue(encoded.Value, kind) {
		return existing.Value, false
	}
	old := existing.Value
	*existing = *encoded
	return old, true
}

func yamlScalar(value, kind string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.ScalarNode, Value: value}
	switch kind {
	case "bool":
		node.Value = strconv.FormatBool(normalizeSettingValue(value, kind) == "true")
	case "int":
		parsed, _ := strconv.Atoi(strings.TrimSpace(value))
		node.Value = strconv.Itoa(parsed)
	case "float":
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
		node.Value = strconv.FormatFloat(parsed, 'f', -1, 64)
	default:
		node.Tag = "!!str"
	}
	return node
}

func normalizeSettingValue(value, kind string) string {
	switch kind {
	case "bool":
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return strings.TrimSpace(value)
		}
		return strconv.FormatBool(parsed)
	case "int":
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return strings.TrimSpace(value)
		}
		return strconv.Itoa(parsed)
	case "float":
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return strings.TrimSpace(value)
		}
		return strconv.FormatFloat(parsed, 'f', -1, 64)
	default:
		return strings.TrimSpace(value)
	}
}
