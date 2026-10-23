package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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
			if origin == "" {
				message := fmt.Sprintf("Commute profile %q does not define an origin.", commuteKey)
				warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "profiles", message, false))
			} else if briefingContains(include, "transit") || briefingContains(include, "velov") {
				originRef, originWarning := a.briefingOriginRef(ctx, origin)
				if originWarning != nil {
					warnings = append(warnings, *originWarning)
				} else {
					if briefingContains(include, "transit") {
						departures := a.NextDepartures(ctx, model.NextDeparturesInput{Stop: *originRef, At: &at, Limit: 3})
						facts["departures"] = departures.Data
						warnings = append(warnings, departures.Warnings...)
						if departures.Status == "not_found" {
							message := fmt.Sprintf("Departures were not found near profile place %q.", origin)
							warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "transit", message, false))
						}
					}
					if briefingContains(include, "velov") {
						minimumBikes := briefingIntValue(commute["minimum_velov_bikes"])
						stations := a.VelovStations(ctx, *originRef, 1000, minimumBikes, 3)
						facts["velov"] = stations.Data
						warnings = append(warnings, stations.Warnings...)
					}
				}
			}
		}
	}
	mobilityInclude := selectBriefingStrings(include, "transit", "traffic", "accessibility", "roadworks")
	if len(mobilityInclude) != 0 {
		mobility := a.MobilityStatus(ctx, model.MobilityStatusInput{Include: mobilityInclude})
		facts["mobility"] = mobility.Data
		warnings = append(warnings, mobility.Warnings...)
	}

	changes := []model.BriefingChange{}
	if input.CompareWithPrevious {
		previous, previousErr := a.Store.LastSnapshot(ctx, profile)
		if previousErr != nil {
			warnings = append(warnings, briefingWarning("PARTIAL_RESULT", "briefing_history", "Previous briefing was not available.", true))
		} else {
			changes = compareBriefingFacts(previous, facts)
		}
	}
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
	})
	result.Warnings = warnings
	result.Degraded = len(warnings) != 0
	return result
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

func compareBriefingFacts(previous map[string]any, current map[string]any) []model.BriefingChange {
	if previous == nil {
		return []model.BriefingChange{}
	}
	previousFacts, _ := previous["facts"].(map[string]any)
	if previousFacts == nil {
		return []model.BriefingChange{}
	}
	result := []model.BriefingChange{}
	previousLabel, currentLabel := "(previous)", "(updated)"
	for _, key := range []string{"departures", "velov", "mobility"} {
		oldValue, hadOld := previousFacts[key]
		newValue, hasNew := current[key]
		if hadOld && hasNew && !equalBriefingValue(oldValue, newValue) {
			result = append(result, model.BriefingChange{Key: key, Previous: &previousLabel, Current: &currentLabel})
		}
	}
	return result
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
