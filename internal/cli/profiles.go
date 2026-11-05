package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/config"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/service"
	"go.yaml.in/yaml/v3"
)

type profileDocument struct {
	Places    map[string]map[string]any `yaml:"places" json:"places"`
	Commutes  map[string]map[string]any `yaml:"commutes" json:"commutes"`
	Briefings map[string]map[string]any `yaml:"briefings" json:"briefings"`
}

var (
	supportedProfileModes    = map[string]bool{"tcl": true, "velov": true, "park_and_ride": true, "car": true, "walk": true}
	supportedBriefingSection = map[string]bool{"transit": true, "velov": true, "accessibility": true, "traffic": true, "roadworks": true, "environment": true}
)

func runProfile(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError("profile requires list, set, remove, or validate")
	}
	switch args[0] {
	case "list":
		if err := requireNoArguments(args[1:]); err != nil {
			return err
		}
		document, err := readProfileDocument()
		if err != nil {
			return err
		}
		return writeJSON(stdout, document)
	case "validate":
		if err := requireNoArguments(args[1:]); err != nil {
			return err
		}
		document, err := readProfileDocument()
		if err != nil {
			return err
		}
		if err := validateProfileDocument(document); err != nil {
			return usageError("invalid profile: %v", err)
		}
		return writeJSON(stdout, map[string]any{"status": "OK", "places": len(document.Places), "commutes": len(document.Commutes), "briefings": len(document.Briefings)})
	case "set", "remove":
		return runProfileMutation(ctx, args[0], args[1:], stdout, stderr)
	default:
		return usageError("unknown profile command %q", args[0])
	}
}

func runProfileMutation(ctx context.Context, command string, args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("profile "+command, stderr)
	kind := flags.String("type", "", "place, commute, or briefing")
	name := flags.String("name", "", "profile key")
	label := flags.String("label", "", "place label")
	address := flags.String("address", "", "place address")
	latitude := flags.Float64("latitude", 0, "place latitude")
	longitude := flags.Float64("longitude", 0, "place longitude")
	origin := flags.String("origin", "", "commute origin place")
	destination := flags.String("destination", "", "commute destination place")
	maxWalking := flags.Int("max-walking-m", 800, "commute walking limit")
	modes := flags.String("modes", "", "comma-separated commute modes")
	commute := flags.String("commute", "", "briefing commute key")
	include := flags.String("include", "", "comma-separated briefing sections")
	force := flags.Bool("force", false, "replace an existing profile")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *kind != "place" && *kind != "commute" && *kind != "briefing" {
		return usageError("--type must be place, commute, or briefing")
	}
	if strings.TrimSpace(*name) == "" {
		return usageError("--name is required")
	}
	document, err := readProfileDocument()
	if err != nil {
		return err
	}
	collection := profileCollection(&document, *kind)
	_, exists := collection[*name]
	if command == "set" && exists && !*force {
		return usageError("%s %q already exists; pass --force to replace it", *kind, *name)
	}
	if command == "remove" && !exists {
		return usageError("%s %q does not exist", *kind, *name)
	}
	if command == "remove" {
		delete(collection, *name)
	} else {
		value, err := profileValue(*kind, *name, label, address, latitude, longitude, origin, destination, maxWalking, modes, commute, include, flags)
		if err != nil {
			return err
		}
		collection[*name] = value
	}
	if err := validateProfileDocument(document); err != nil {
		return usageError("invalid profile: %v", err)
	}
	data, err := yaml.Marshal(document)
	if err != nil {
		return err
	}
	return withApp(ctx, func(cfg config.Config, app *service.App) error {
		if err := app.Store.LoadProfiles(ctx, data); err != nil {
			return fmt.Errorf("update profile store: %w", err)
		}
		path := filepath.Join(cfg.ConfigDir, "profiles.yaml")
		if err := writeAtomicRegular(path, data, 0o600); err != nil {
			return fmt.Errorf("write profiles: %w", err)
		}
		return writeJSON(stdout, map[string]any{"status": "OK", "action": command, "type": *kind, "name": *name, "path": path})
	})
}

func profileCollection(document *profileDocument, kind string) map[string]map[string]any {
	switch kind {
	case "place":
		if document.Places == nil {
			document.Places = map[string]map[string]any{}
		}
		return document.Places
	case "commute":
		if document.Commutes == nil {
			document.Commutes = map[string]map[string]any{}
		}
		return document.Commutes
	default:
		if document.Briefings == nil {
			document.Briefings = map[string]map[string]any{}
		}
		return document.Briefings
	}
}

func profileValue(kind, name string, label, address *string, latitude, longitude *float64, origin, destination *string, maxWalking *int, modes, commute, include *string, flags *flag.FlagSet) (map[string]any, error) {
	value := map[string]any{}
	switch kind {
	case "place":
		if *label == "" {
			*label = name
		}
		value["label"] = *label
		if *address != "" {
			value["address"] = *address
		}
		set := map[string]bool{}
		flags.Visit(func(item *flag.Flag) { set[item.Name] = true })
		if set["latitude"] || set["longitude"] {
			if !set["latitude"] || !set["longitude"] {
				return nil, errors.New("provide both --latitude and --longitude")
			}
			if *latitude < -90 || *latitude > 90 || *longitude < -180 || *longitude > 180 {
				return nil, errors.New("latitude or longitude is outside its valid range")
			}
			value["latitude"] = *latitude
			value["longitude"] = *longitude
		} else if *address == "" {
			return nil, errors.New("a place needs coordinates or an address")
		}
	case "commute":
		if *origin == "" || *destination == "" {
			return nil, errors.New("a commute needs --origin and --destination")
		}
		if *maxWalking < 0 || *maxWalking > 5000 {
			return nil, errors.New("--max-walking-m must be between 0 and 5000")
		}
		value["origin"] = *origin
		value["destination"] = *destination
		value["max_walking_m"] = *maxWalking
		if *modes != "" {
			value["preferred_modes"] = splitList(*modes)
		}
	case "briefing":
		if *commute == "" {
			return nil, errors.New("a briefing needs --commute")
		}
		value["commute"] = *commute
		if *include != "" {
			value["include"] = splitList(*include)
		}
	}
	return value, nil
}

func readProfileDocument() (profileDocument, error) {
	cfg, err := config.Load()
	if err != nil {
		return profileDocument{}, err
	}
	path := filepath.Join(cfg.ConfigDir, "profiles.yaml")
	document := profileDocument{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return document, nil
	}
	if err != nil {
		return document, err
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return document, fmt.Errorf("parse profiles: %w", err)
	}
	return document, nil
}

func validateProfileDocument(document profileDocument) error {
	for name, place := range document.Places {
		label, _ := place["label"].(string)
		if strings.TrimSpace(label) == "" {
			return fmt.Errorf("place %q has no label", name)
		}
		latitude, latOK := profileFloat(place["latitude"])
		longitude, lonOK := profileFloat(place["longitude"])
		if latOK != lonOK {
			return fmt.Errorf("place %q must provide both latitude and longitude", name)
		}
		if latOK && (latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180) {
			return fmt.Errorf("place %q has coordinates outside their valid range", name)
		}
		if !latOK {
			address, _ := place["address"].(string)
			if strings.TrimSpace(address) == "" {
				return fmt.Errorf("place %q has neither coordinates nor an address", name)
			}
		}
	}
	for name, commute := range document.Commutes {
		origin, _ := commute["origin"].(string)
		destination, _ := commute["destination"].(string)
		if strings.TrimSpace(origin) == "" || strings.TrimSpace(destination) == "" {
			return fmt.Errorf("commute %q needs origin and destination", name)
		}
		if _, ok := document.Places[origin]; !ok {
			return fmt.Errorf("commute %q references unknown origin %q", name, origin)
		}
		if _, ok := document.Places[destination]; !ok {
			return fmt.Errorf("commute %q references unknown destination %q", name, destination)
		}
		if value, ok := commute["max_walking_m"]; ok {
			walking, valid := profileInt(value)
			if !valid || walking < 0 || walking > 5000 {
				return fmt.Errorf("commute %q has an invalid max_walking_m", name)
			}
		}
		for _, mode := range profileStrings(commute["preferred_modes"]) {
			if !supportedProfileModes[mode] {
				return fmt.Errorf("commute %q uses unsupported mode %q", name, mode)
			}
		}
	}
	for name, briefing := range document.Briefings {
		commute, _ := briefing["commute"].(string)
		if strings.TrimSpace(commute) == "" {
			return fmt.Errorf("briefing %q needs a commute", name)
		}
		if _, ok := document.Commutes[commute]; !ok {
			return fmt.Errorf("briefing %q references unknown commute %q", name, commute)
		}
		for _, section := range profileStrings(briefing["include"]) {
			if !supportedBriefingSection[section] {
				return fmt.Errorf("briefing %q uses unsupported section %q", name, section)
			}
		}
	}
	return nil
}

func profileFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func profileInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func profileStrings(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		result := []string{}
		for _, item := range values {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	case string:
		return splitList(values)
	default:
		return nil
	}
}

func splitList(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}
