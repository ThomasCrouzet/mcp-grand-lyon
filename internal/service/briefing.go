package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func (a *App) PersonalBriefing(ctx context.Context, input model.PersonalBriefingInput) model.Envelope {
	profile := strings.TrimSpace(input.Profile)
	if profile == "" || a.Store == nil {
		return model.NewEnvelope("not_found", "Briefing profile not found.", map[string]any{})
	}
	configuration, err := a.Store.GetBriefing(ctx, profile)
	if err != nil {
		result := model.NewEnvelope("unavailable", "Briefing profile is unavailable.", map[string]any{})
		result.Warn("SOURCE_UNAVAILABLE", "profiles", "Briefing profile retrieval failed.", true)
		return result
	}
	if configuration == nil {
		return model.NewEnvelope("not_found", fmt.Sprintf("Briefing profile %q not found.", profile), map[string]any{})
	}
	at := model.Now()
	if input.At != nil {
		at = input.At.In(model.Paris)
	}
	include := briefingStringSlice(configuration["include"])
	if len(include) == 0 {
		include = []string{"transit", "velov", "traffic"}
	}
	facts := map[string]any{"profile": profile, "include": include}
	warnings := []model.WarningItem{}
	sources := []model.SourceProvenance{}

	commuteKey := briefingStringValue(configuration["commute"])
	if commuteKey == "" {
		warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "profiles", "The briefing profile does not define a commute.", false))
	} else {
		commute, commuteErr := a.Store.GetCommute(ctx, commuteKey)
		if commuteErr != nil {
			message := fmt.Sprintf("Commute profile %q could not be read.", commuteKey)
			warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "profiles", message, true))
		} else if commute == nil {
			message := fmt.Sprintf("Commute profile %q was not found.", commuteKey)
			warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "profiles", message, false))
		} else {
			facts["commute"] = commute
			origin := briefingStringValue(commute["origin"])
			destination := briefingStringValue(commute["destination"])
			modes := briefingStringSlice(commute["preferred_modes"])
			if len(modes) == 0 {
				modes = []string{"tcl", "velov"}
			}
			preferredLines := briefingStringSlice(commute["preferred_lines"])
			facts["preferred_lines"] = preferredLines
			facts["preferred_modes"] = modes
			if origin == "" {
				message := fmt.Sprintf("Commute profile %q does not define an origin.", commuteKey)
				warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "profiles", message, false))
			}
			originRef, originWarning := a.briefingPlaceRef(ctx, origin)
			if originWarning != nil {
				warnings = append(warnings, *originWarning)
			}
			destinationRef, destinationWarning := a.briefingPlaceRef(ctx, destination)
			if destinationWarning != nil {
				warnings = append(warnings, *destinationWarning)
			}
			if originRef != nil && destinationRef != nil {
				preferences := &model.TripPreferences{
					MaxWalkingM:       briefingIntValue(commute["max_walking_m"]),
					MinimumVelovBikes: briefingIntValue(commute["minimum_velov_bikes"]),
					MinimumVelovDocks: briefingIntValue(commute["minimum_velov_docks"]),
					AvoidDisruptions:  briefingBoolValue(commute["avoid_disruptions"], true),
					Wheelchair:        briefingBoolValue(commute["wheelchair"], false),
				}
				if preferences.MaxWalkingM == 0 {
					preferences.MaxWalkingM = 800
				}
				journey := a.TripOptions(ctx, model.TripOptionsInput{
					Origin: *originRef, Destination: *destinationRef, DepartureAt: &at,
					Modes: modes, Preferences: preferences,
				})
				facts["journey"] = journey.Data
				warnings = append(warnings, journey.Warnings...)
				sources = append(sources, journey.Sources...)
				if journey.Data["recommended_mode"] == nil {
					warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "journey_planner", "No configured journey option satisfies the commute constraints.", false))
				}
				if len(preferredLines) == 0 {
					preferredLines = briefingJourneyLines(journey.Data)
					facts["preferred_lines"] = preferredLines
				}
			}
			if briefingContains(include, "transit") && originRef != nil {
				departures := a.NextDepartures(ctx, model.NextDeparturesInput{Stop: *originRef, At: &at, Limit: 6})
				departures.Data["departures"] = filterBriefingDepartures(departures.Data["departures"], preferredLines)
				facts["departures"] = departures.Data
				warnings = append(warnings, departures.Warnings...)
				sources = append(sources, departures.Sources...)
				if departures.Status == "not_found" {
					message := fmt.Sprintf("Departures were not found near profile place %q.", origin)
					warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "transit", message, false))
				}
			}
			if briefingContains(include, "velov") && originRef != nil {
				minimumBikes := briefingIntValue(commute["minimum_velov_bikes"])
				minimumDocks := briefingIntValue(commute["minimum_velov_docks"])
				stations := a.VelovStations(ctx, *originRef, 1000, minimumBikes, 3)
				facts["velov"] = stations.Data
				facts["velov_destination_minimum_docks"] = minimumDocks
				warnings = append(warnings, stations.Warnings...)
				sources = append(sources, stations.Sources...)
			}
			if briefingContains(include, "environment") {
				indicators := briefingStringSlice(commute["environment"])
				if len(indicators) == 0 {
					indicators = []string{"pollen", "air_quality", "heat"}
				}
				location := originRef
				if location == nil {
					location = destinationRef
				}
				if location != nil {
					environment := a.EnvironmentBrief(ctx, model.EnvironmentBriefInput{Location: *location, At: &at, Indicators: indicators})
					facts["environment"] = environment.Data
					warnings = append(warnings, environment.Warnings...)
					sources = append(sources, environment.Sources...)
				}
			}
		}
	}
	if unsupported := briefingUnsupportedSections(include); len(unsupported) != 0 {
		warnings = append(warnings, briefingWarning("UNSUPPORTED_SECTION", "briefing", "Unsupported briefing section(s): "+strings.Join(unsupported, ", ")+".", false))
	}
	mobilityInclude := selectBriefingStrings(include, "transit", "traffic", "accessibility", "roadworks")
	if len(mobilityInclude) != 0 {
		lines := briefingStringSlice(facts["preferred_lines"])
		areas := []model.PlaceRef{}
		if value, ok := facts["commute"].(map[string]any); ok {
			for _, key := range []string{"origin", "destination"} {
				if place := briefingStringValue(value[key]); place != "" {
					areas = append(areas, model.PlaceRef{ProfilePlace: place})
				}
			}
		}
		mobility := a.MobilityStatus(ctx, model.MobilityStatusInput{Include: mobilityInclude, Lines: lines, Areas: areas})
		facts["mobility"] = mobility.Data
		warnings = append(warnings, mobility.Warnings...)
		sources = append(sources, mobility.Sources...)
	}

	changes := []model.BriefingChange{}
	baseline := "not_requested"
	changesTruncated := false
	if input.CompareWithPrevious {
		baseline = "none"
		previous, previousErr := a.Store.LastSnapshot(ctx, profile)
		if previousErr != nil {
			warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "briefing_history", "Previous briefing was not available.", true))
		} else if previous == nil {
			warnings = append(warnings, briefingWarning("NO_BASELINE", "briefing_history", "No previous briefing exists; changes are not available.", false))
		} else {
			baseline = "previous"
			changes, changesTruncated = compareBriefingFacts(previous, facts)
		}
		if changesTruncated {
			warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "briefing_history", "Change list was truncated at 50 entries.", false))
		}
	}
	sources = uniqueSources(sources)
	facts["sources"] = sources
	snapshot := map[string]any{"facts": facts, "at": at.Format(time.RFC3339)}
	if err := a.Store.SaveSnapshot(ctx, profile, snapshot); err != nil {
		warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "briefing_history", "Briefing snapshot was not saved.", true))
	}
	alertCount := 0
	if mobility, ok := facts["mobility"].(map[string]any); ok {
		alertCount = briefingSliceLength(mobility["alerts"])
	}
	summary := fmt.Sprintf("Briefing %s: %d alert(s).", profile, alertCount)
	status := "ok"
	if len(warnings) != 0 {
		status = "partial"
	}
	result := model.NewEnvelope(status, summary, map[string]any{
		"profile": profile, "at": at, "summary": summary, "facts": facts, "changes": changes,
		"changes_baseline": baseline, "changes_truncated": changesTruncated,
	})
	result.Warnings = warnings
	result.Sources = sources
	result.Degraded = len(warnings) != 0
	return result
}

func (a *App) briefingPlaceRef(ctx context.Context, key string) (*model.PlaceRef, *model.WarningItem) {
	return a.briefingOriginRef(ctx, key)
}

func (a *App) briefingOriginRef(ctx context.Context, origin string) (*model.PlaceRef, *model.WarningItem) {
	place, err := a.Store.GetPlace(ctx, origin)
	if err != nil {
		message := fmt.Sprintf("Profile place %q could not be read.", origin)
		warning := briefingWarning("PARTIAL_RESULT", "profiles", message, true)
		return nil, &warning
	}
	if place == nil {
		message := fmt.Sprintf("Profile place %q was not found.", origin)
		warning := briefingWarning("PARTIAL_RESULT", "profiles", message, false)
		return nil, &warning
	}
	latitude, latitudeOK := numberValue(place, "latitude")
	longitude, longitudeOK := numberValue(place, "longitude")
	if !latitudeOK || !longitudeOK {
		message := fmt.Sprintf("Profile place %q has invalid coordinates.", origin)
		warning := briefingWarning("PARTIAL_RESULT", "profiles", message, false)
		return nil, &warning
	}
	ref := model.PlaceRef{Latitude: &latitude, Longitude: &longitude}
	if err := ref.Validate(); err != nil {
		message := fmt.Sprintf("Profile place %q has invalid coordinates.", origin)
		warning := briefingWarning("PARTIAL_RESULT", "profiles", message, false)
		return nil, &warning
	}
	return &ref, nil
}

func compareBriefingFacts(previous, current map[string]any) ([]model.BriefingChange, bool) {
	if previous == nil {
		return []model.BriefingChange{}, false
	}
	previousFacts, _ := previous["facts"].(map[string]any)
	if previousFacts == nil {
		return []model.BriefingChange{}, false
	}
	changes := []model.BriefingChange{}
	sections := []string{"departures", "velov", "journey", "mobility", "environment"}
	for _, section := range sections {
		oldValue, oldPresent := previousFacts[section]
		newValue, newPresent := current[section]
		if oldPresent && !newPresent {
			changes = append(changes, model.BriefingChange{
				Key: section, Kind: "evidence_lost", Message: "Evidence for " + section + " is no longer available.",
				Previous: model.Ptr(sectionSummary(section, oldValue)), Source: model.Ptr(section),
			})
			continue
		}
		if !oldPresent || !newPresent {
			continue
		}
		oldItems := briefingItems(section, oldValue)
		newItems := briefingItems(section, newValue)
		keys := map[string]bool{}
		for key := range oldItems {
			keys[key] = true
		}
		for key := range newItems {
			keys[key] = true
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			oldItem, hadOld := oldItems[key]
			newItem, hasNew := newItems[key]
			changeKey := section + ":" + key
			switch {
			case hadOld && !hasNew:
				changes = append(changes, briefingChange(changeKey, "removed", section, oldItem, nil))
			case !hadOld && hasNew:
				changes = append(changes, briefingChange(changeKey, "added", section, nil, newItem))
			case hadOld && hasNew && !equalBriefingValue(oldItem, newItem):
				changes = append(changes, briefingChange(changeKey, "updated", section, oldItem, newItem))
			}
		}
	}
	const maximumChanges = 50
	truncated := len(changes) > maximumChanges
	if truncated {
		changes = changes[:maximumChanges]
	}
	return changes, truncated
}

func briefingChange(key, kind, section string, previous, current map[string]any) model.BriefingChange {
	change := model.BriefingChange{Key: key, Kind: kind, Source: model.Ptr(section)}
	if previous != nil {
		text := briefingValueText(previous)
		change.Previous = &text
	}
	if current != nil {
		text := briefingValueText(current)
		change.Current = &text
	}
	if observed := briefingObservedAt(current); observed != nil {
		change.ObservedAt = observed
	} else if observed := briefingObservedAt(previous); observed != nil {
		change.ObservedAt = observed
	}
	switch kind {
	case "added":
		change.Message = "Added " + key + "."
	case "removed":
		change.Message = "Removed " + key + "."
	default:
		change.Message = briefingUpdateMessage(section, previous, current)
	}
	return change
}

func briefingUpdateMessage(section string, previous, current map[string]any) string {
	if section == "journey" || section == "velov" {
		oldAvailability := briefingStringValue(previous["availability"])
		newAvailability := briefingStringValue(current["availability"])
		if oldAvailability != "" || newAvailability != "" {
			return "Availability changed from " + briefingDisplay(oldAvailability) + " to " + briefingDisplay(newAvailability) + "."
		}
		if previous["eligible"] != current["eligible"] {
			return "Eligibility changed from " + fmt.Sprint(previous["eligible"]) + " to " + fmt.Sprint(current["eligible"]) + "."
		}
	}
	if section == "departures" {
		if current["cancelled"] == true && previous["cancelled"] != true {
			return "Departure was cancelled."
		}
		return "Departure timing or status changed."
	}
	if section == "mobility" {
		return "Disruption information changed."
	}
	return "Value changed."
}

func briefingDisplay(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func briefingItems(section string, value any) map[string]map[string]any {
	object, ok := value.(map[string]any)
	if !ok {
		return map[string]map[string]any{}
	}
	var raw any
	switch section {
	case "departures":
		raw = object["departures"]
	case "velov":
		raw = object["stations"]
	case "journey":
		raw = object["options"]
	case "mobility":
		raw = object["alerts"]
	case "environment":
		raw = object["indicators"]
	default:
		return map[string]map[string]any{}
	}
	items, ok := raw.([]any)
	if !ok {
		data, err := json.Marshal(raw)
		if err != nil {
			return map[string]map[string]any{}
		}
		if err := json.Unmarshal(data, &items); err != nil {
			return map[string]map[string]any{}
		}
	}
	result := map[string]map[string]any{}
	for _, item := range items {
		value, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := ""
		switch section {
		case "departures":
			when := briefingStringValue(value["expected_at"])
			if when == "" {
				when = briefingStringValue(value["scheduled_at"])
			}
			key = briefingStringValue(value["line_name"]) + "|" + briefingStringValue(value["destination"]) + "|" + when
		case "velov":
			key = briefingStringValue(value["id"])
		case "journey":
			key = briefingStringValue(value["mode"]) + "|" + briefingStringValue(value["summary"])
		case "mobility":
			key = briefingStringValue(value["id"])
		case "environment":
			key = briefingStringValue(value["indicator"])
		}
		if key != "" {
			result[key] = value
		}
	}
	return result
}

func briefingValueText(value map[string]any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	text := string(data)
	if len(text) > 240 {
		text = text[:240]
	}
	return text
}

func briefingObservedAt(value map[string]any) *time.Time {
	if value == nil {
		return nil
	}
	raw := briefingStringValue(value["observed_at"])
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &parsed
}

func sectionSummary(section string, value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return section
	}
	if len(data) > 240 {
		data = data[:240]
	}
	return string(data)
}

func equalBriefingValue(left, right any) bool {
	leftCanonical, leftErr := canonicalBriefingValue(left)
	rightCanonical, rightErr := canonicalBriefingValue(right)
	return leftErr == nil && rightErr == nil && reflect.DeepEqual(leftCanonical, rightCanonical)
}

func canonicalBriefingValue(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var canonical any
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	removeBriefingVolatileFields(canonical)
	return canonical, nil
}

func removeBriefingVolatileFields(value any) {
	switch item := value.(type) {
	case map[string]any:
		for _, key := range []string{"age_seconds", "distance_m", "generated_at", "observed_at", "request_id", "retrieved_at"} {
			delete(item, key)
		}
		for _, child := range item {
			removeBriefingVolatileFields(child)
		}
	case []any:
		for _, child := range item {
			removeBriefingVolatileFields(child)
		}
	}
}

func briefingBoolValue(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return fallback
	}
}

func briefingJourneyLines(data map[string]any) []string {
	options, _ := data["options"].([]model.TripOption)
	for _, option := range options {
		if !option.Eligible {
			continue
		}
		lines := []string{}
		for _, leg := range option.Legs {
			if leg.Line != "" && !containsFold(lines, leg.Line) {
				lines = append(lines, leg.Line)
			}
		}
		if len(lines) != 0 {
			return lines
		}
	}
	return []string{}
}

func filterBriefingDepartures(value any, lines []string) any {
	departures, ok := value.([]model.Departure)
	if !ok || len(lines) == 0 {
		return value
	}
	filtered := make([]model.Departure, 0, len(departures))
	for _, departure := range departures {
		for _, line := range lines {
			if model.LineMatches(line, departure.LineName, departure.LineID) {
				filtered = append(filtered, departure)
				break
			}
		}
	}
	return filtered
}

func briefingUnsupportedSections(include []string) []string {
	supported := map[string]bool{
		"transit": true, "velov": true, "traffic": true, "accessibility": true,
		"roadworks": true, "environment": true,
	}
	result := []string{}
	for _, section := range include {
		if !supported[section] {
			result = append(result, section)
		}
	}
	return result
}

func briefingWarning(code, source, message string, retryable bool) model.WarningItem {
	return model.WarningItem{Code: code, Source: model.Ptr(source), Message: message, Retryable: retryable}
}

func briefingStringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		return append([]string{}, values...)
	case []any:
		result := []string{}
		for _, item := range values {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return []string{}
	}
}

func briefingStringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func numberValue(values map[string]any, key string) (float64, bool) {
	if values == nil {
		return 0, false
	}
	switch value := values[key].(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

func briefingIntValue(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	default:
		return 0
	}
}

func briefingContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func selectBriefingStrings(values []string, allowed ...string) []string {
	set := mobilityStringSet(allowed)
	result := []string{}
	for _, value := range values {
		if set[value] {
			result = append(result, value)
		}
	}
	return result
}

func briefingSliceLength(value any) int {
	reflectValue := reflect.ValueOf(value)
	if reflectValue.IsValid() && (reflectValue.Kind() == reflect.Slice || reflectValue.Kind() == reflect.Array) {
		return reflectValue.Len()
	}
	return 0
}
