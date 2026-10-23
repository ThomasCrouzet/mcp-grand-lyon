package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/assets"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"go.yaml.in/yaml/v3"
)

type wasteTaxonomyFile struct {
	Version int             `yaml:"version"`
	Rules   []wasteRuleYAML `yaml:"rules"`
}

type wasteRuleYAML struct {
	ID         string   `yaml:"id"`
	Category   string   `yaml:"category"`
	Priority   int      `yaml:"priority"`
	Confidence float64  `yaml:"confidence"`
	Hazardous  bool     `yaml:"hazardous"`
	Aliases    []string `yaml:"aliases"`
	Patterns   []string `yaml:"patterns"`
}

type compiledWasteRule struct {
	wasteRuleYAML
	patterns []*regexp.Regexp
}

func (app *App) WasteDropoff(ctx context.Context, input model.WasteDropoffInput) model.Envelope {
	if strings.TrimSpace(input.Item) == "" {
		return model.NewEnvelope("invalid_request", "A waste item is required.", wasteData(model.WasteClassification{Input: input.Item, Category: "unknown", Candidates: []string{}}, nil, input.Transport, input.OpenAt))
	}
	transport := input.Transport
	if transport == "" {
		transport = "car"
	}
	if transport != "car" {
		return model.NewEnvelope("invalid_request", "Unsupported transport: "+transport, wasteData(model.WasteClassification{Input: input.Item, Category: "unknown", Candidates: []string{}}, nil, transport, input.OpenAt))
	}
	rules, err := app.loadWasteTaxonomy()
	if err != nil {
		envelope := model.NewEnvelope("unavailable", "The waste taxonomy is unavailable.", wasteData(model.WasteClassification{Input: input.Item, Category: "unknown", Candidates: []string{}}, nil, transport, input.OpenAt))
		envelope.Warn("SOURCE_UNAVAILABLE", "waste_taxonomy", "The waste taxonomy could not be loaded.", false)
		envelope.Status = "unavailable"
		return envelope
	}
	classification := classifyWaste(input.Item, rules)
	status := "ok"
	if classification.Category == "unknown" {
		status = "not_found"
	} else if len(classification.Candidates) != 0 {
		status = "ambiguous"
	}
	envelope := model.NewEnvelope(status, wasteSummary(classification), wasteData(classification, nil, transport, input.OpenAt))
	if input.Location == nil || classification.Category == "unknown" {
		return envelope
	}
	if err := input.Location.Validate(); err != nil {
		return model.NewEnvelope("invalid_request", "Invalid location: "+err.Error(), wasteData(classification, nil, transport, input.OpenAt))
	}
	point, err := app.ResolvePoint(ctx, *input.Location)
	if err != nil || point == nil {
		return model.NewEnvelope("not_found", "The drop-off location could not be resolved.", wasteData(classification, nil, transport, input.OpenAt))
	}
	radius := input.RadiusM
	if radius == 0 {
		radius = 15000
	}
	limit := input.Limit
	if limit == 0 {
		limit = 10
	}
	if radius < 100 || radius > 50000 || limit < 1 || limit > 20 {
		return model.NewEnvelope("invalid_request", "Waste radius or limit is outside the supported range.", wasteData(classification, nil, transport, input.OpenAt))
	}
	if app.Provider == nil {
		envelope.Warn("SOURCE_UNAVAILABLE", "waste_facilities", "The waste facility provider is unavailable.", true)
		envelope.Data = wasteData(classification, nil, transport, input.OpenAt)
		return envelope
	}
	facilities, err := app.Provider.WasteFacilities(ctx, *point, classification.Category, float64(radius), limit)
	if err != nil {
		envelope.Warn("SOURCE_UNAVAILABLE", "waste_facilities", "Waste facilities are unavailable.", true)
		envelope.Data = wasteData(classification, nil, transport, input.OpenAt)
		return envelope
	}
	filtered := make([]model.WasteFacility, 0, len(facilities))
	unknownHours := 0
	unknownAcceptance := 0
	for _, facility := range facilities {
		if len(facility.AcceptedCategories) == 0 {
			unknownAcceptance++
		} else if !contains(facility.AcceptedCategories, classification.Category) {
			continue
		}
		distance := model.Distance(*point, model.Point{Latitude: facility.Latitude, Longitude: facility.Longitude})
		if distance > float64(radius) {
			continue
		}
		facility.DistanceM = model.Ptr(distance)
		if input.OpenAt != nil {
			status := openingStatusAt(facility.OpeningHoursRaw, *input.OpenAt)
			if status == "unknown" {
				unknownHours++
				continue
			}
			if status == "closed" {
				continue
			}
			facility.OpenStatus = "open"
		}
		filtered = append(filtered, facility)
		if len(filtered) == limit {
			break
		}
	}
	if unknownAcceptance != 0 {
		envelope.Warn("PARTIAL_RESULT", "waste_acceptance", fmt.Sprintf("%d facilities have unverified category acceptance. Verify acceptance before drop-off.", unknownAcceptance), false)
	}
	if unknownHours != 0 {
		envelope.Warn("PARTIAL_RESULT", "waste_opening_hours", fmt.Sprintf("Omitted %d facilities with unverified opening hours.", unknownHours), false)
	}
	envelope.Data = wasteData(classification, filtered, transport, input.OpenAt)
	return envelope
}

func (app *App) loadWasteTaxonomy() ([]compiledWasteRule, error) {
	data, err := assets.Files.ReadFile("config/waste-taxonomy.yaml")
	if err != nil {
		return nil, err
	}
	if app.Config.ConfigDir != "" {
		path := filepath.Join(app.Config.ConfigDir, "waste-taxonomy.yaml")
		override, readErr := os.ReadFile(path)
		if readErr == nil {
			data = override
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
	}
	var taxonomy wasteTaxonomyFile
	if err := yaml.Unmarshal(data, &taxonomy); err != nil {
		return nil, err
	}
	if taxonomy.Version != 1 || len(taxonomy.Rules) == 0 {
		return nil, errors.New("unsupported or empty waste taxonomy")
	}
	rules := make([]compiledWasteRule, 0, len(taxonomy.Rules))
	for _, source := range taxonomy.Rules {
		if source.ID == "" || source.Category == "" || source.Confidence < 0 || source.Confidence > 1 {
			return nil, errors.New("invalid waste taxonomy rule")
		}
		rule := compiledWasteRule{wasteRuleYAML: source}
		for _, pattern := range source.Patterns {
			compiled, compileErr := regexp.Compile("(?i)" + pattern)
			if compileErr != nil {
				return nil, compileErr
			}
			rule.patterns = append(rule.patterns, compiled)
		}
		rules = append(rules, rule)
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Priority > rules[j].Priority })
	return rules, nil
}

func classifyWaste(item string, rules []compiledWasteRule) model.WasteClassification {
	type match struct {
		confidence float64
		rule       compiledWasteRule
	}
	normalized := strings.ToLower(strings.TrimSpace(item))
	matches := []match{}
	for _, rule := range rules {
		matched := false
		for _, alias := range rule.Aliases {
			if strings.Contains(normalized, strings.ToLower(alias)) {
				matched = true
				break
			}
		}
		if !matched {
			for _, pattern := range rule.patterns {
				if pattern.MatchString(normalized) {
					matched = true
					break
				}
			}
		}
		if matched {
			matches = append(matches, match{confidence: rule.Confidence, rule: rule})
		}
	}
	if len(matches) == 0 {
		return model.WasteClassification{Input: item, Category: "unknown", Candidates: []string{}}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].confidence > matches[j].confidence })
	best := matches[0]
	classification := model.WasteClassification{
		Input: item, Category: best.rule.Category, Hazardous: best.rule.Hazardous,
		Confidence: best.confidence, MatchedRule: model.Ptr(best.rule.ID), Candidates: []string{},
	}
	if len(matches) > 1 && matches[0].confidence-matches[1].confidence < 0.1 {
		for _, candidate := range matches {
			if len(classification.Candidates) == 3 {
				break
			}
			classification.Candidates = append(classification.Candidates, candidate.rule.Category)
		}
	}
	return classification
}

func wasteData(classification model.WasteClassification, facilities []model.WasteFacility, transport string, openAt *time.Time) map[string]any {
	if facilities == nil {
		facilities = []model.WasteFacility{}
	}
	return map[string]any{"classification": classification, "facilities": facilities, "transport": transport, "open_at": openAt}
}

func wasteSummary(classification model.WasteClassification) string {
	if classification.Category == "unknown" {
		return "The waste item could not be classified."
	}
	suffix := ""
	if classification.Hazardous {
		suffix = " It is hazardous."
	}
	return fmt.Sprintf("Waste category: %s.%s", classification.Category, suffix)
}
