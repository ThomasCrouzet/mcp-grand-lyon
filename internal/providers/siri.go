package providers

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

// ParseSIRIDepartures converts decoded SIRI timetable data to domain departures.
func ParseSIRIDepartures(payload any) ([]model.Departure, error) {
	if !providerJSONContainer(payload) {
		return nil, errors.New("SIRI timetable root must be an object or array")
	}
	result := []model.Departure{}
	for _, rawJourney := range providerJSONValues(payload, "EstimatedVehicleJourney") {
		for _, journey := range providerJSONMaps(rawJourney) {
			line := providerJSONText(providerJSONFirst(journey, "LineRef", "lineRef"))
			destination := providerJSONText(providerJSONFirst(
				journey, "DestinationName", "destinationName", "DirectionName", "directionName",
			))
			calls := providerJSONFirst(journey, "EstimatedCalls", "estimatedCalls")
			callValues := providerJSONValues(calls, "EstimatedCall")
			if len(callValues) == 0 && calls != nil {
				callValues = []any{calls}
			}
			for _, rawCall := range callValues {
				for _, call := range providerJSONMaps(rawCall) {
					aimed := providerJSONTime(providerJSONFirst(call, "AimedDepartureTime", "aimedDepartureTime"))
					expected := providerJSONTime(providerJSONFirst(call, "ExpectedDepartureTime", "expectedDepartureTime"))
					if expected == nil {
						expected = aimed
					}
					var delay *int
					if aimed != nil && expected != nil {
						seconds := int(expected.Sub(*aimed).Seconds())
						delay = &seconds
					}
					status := strings.ToLower(providerJSONText(providerJSONFirst(call, "DepartureStatus", "departureStatus")))
					stopRef := providerJSONText(providerJSONFirst(
						call, "StopPointRef", "stopPointRef", "StopPointName", "stopPointName",
					))
					platform := providerJSONText(providerJSONFirst(call, "DeparturePlatformName", "departurePlatformName"))
					resolvedDestination := destination
					if resolvedDestination == "" {
						resolvedDestination = providerJSONText(providerJSONFirst(call, "DestinationDisplay", "destinationDisplay"))
					}
					departure := model.Departure{
						LineID:       "tcl:unknown",
						LineName:     providerSIRILineName(line),
						Destination:  resolvedDestination,
						ScheduledAt:  aimed,
						ExpectedAt:   expected,
						DelaySeconds: delay,
						Realtime:     true,
						Cancelled:    status == "cancelled" || status == "canceled",
					}
					if line != "" {
						departure.LineID = "tcl:" + line
					}
					if stopRef != "" {
						departure.StopRef = model.Ptr(stopRef)
					}
					if platform != "" {
						departure.Platform = model.Ptr(platform)
					}
					result = append(result, departure)
				}
			}
		}
	}
	return result, nil
}

// ParseSIRIAlerts converts decoded SIRI situation data to transit alerts.
func ParseSIRIAlerts(payload any) ([]model.TransitAlert, error) {
	if !providerJSONContainer(payload) {
		return nil, errors.New("SIRI situation root must be an object or array")
	}
	result := []model.TransitAlert{}
	for _, key := range []string{"PtSituationElement", "Situation"} {
		for _, rawSituation := range providerJSONValues(payload, key) {
			for _, situation := range providerJSONMaps(rawSituation) {
				id := providerJSONText(providerJSONFirst(situation, "SituationNumber", "situationNumber", "id"))
				title := providerJSONText(providerJSONFirst(situation, "Summary", "summary", "Title", "title"))
				description := providerJSONText(providerJSONFirst(situation, "Description", "description"))
				if title == "" {
					title = "Alerte TCL"
				}
				if id == "" {
					id = title
					if len(id) > 40 {
						id = id[:40]
					}
				}
				alert := model.TransitAlert{
					ID: id, Title: title, Description: description,
					Severity: providerSIRISeverity(providerJSONText(providerJSONFirst(situation, "Severity", "severity"))),
					Lines:    providerSIRILines(providerJSONFirst(situation, "Affects", "affects")),
				}
				result = append(result, alert)
			}
		}
	}
	return result, nil
}

func providerSIRILines(affects any) []string {
	lines := []string{}
	seen := map[string]bool{}
	for _, raw := range providerJSONValues(affects, "LineRef") {
		for _, value := range providerJSONItems(raw) {
			line := providerSIRILineName(providerJSONText(value))
			if line != "" && !seen[line] {
				seen[line] = true
				lines = append(lines, line)
			}
		}
	}
	return lines
}

func providerSIRISeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "normal", "noimpact", "no impact", "info", "information":
		return "info"
	case "slight", "veryslight", "very slight", "minor":
		return "minor"
	case "severe", "major":
		return "major"
	case "verysevere", "very severe", "critical":
		return "critical"
	default:
		return "unknown"
	}
}

func providerSIRILineName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "?"
	}
	parts := strings.FieldsFunc(strings.ReplaceAll(value, "::", ":"), func(r rune) bool { return r == ':' })
	noise := map[string]bool{
		"sytral": true, "tcl": true, "line": true, "activ": true, "mdl": true,
		"metro": true, "métro": true, "tram": true, "bus": true,
	}
	lower := strings.ToLower(value)
	for _, prefix := range []string{"ligne ", "line ", "métro ", "metro ", "tram ", "tramway ", "bus "} {
		if strings.HasPrefix(lower, prefix) {
			return providerSIRILineName(strings.TrimSpace(value[len(prefix):]))
		}
	}
	for index := len(parts) - 1; index >= 0; index-- {
		candidate := strings.TrimSpace(parts[index])
		if candidate == "" || noise[strings.ToLower(candidate)] {
			continue
		}
		if len(candidate) <= 6 {
			return strings.ToUpper(candidate)
		}
	}
	return value
}

func providerJSONContainer(value any) bool {
	switch value.(type) {
	case map[string]any, []any:
		return true
	default:
		return false
	}
}

func providerJSONValues(value any, key string) []any {
	result := []any{}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for name, child := range typed {
				if strings.EqualFold(name, key) {
					result = append(result, child)
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return result
}

func providerJSONMaps(value any) []map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return []map[string]any{typed}
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				result = append(result, object)
			}
		}
		return result
	default:
		return nil
	}
}

func providerJSONItems(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return []any{value}
}

func providerJSONFirst(object map[string]any, keys ...string) any {
	for _, key := range keys {
		for name, value := range object {
			if strings.EqualFold(name, key) && value != nil {
				return value
			}
		}
	}
	return nil
}

func providerJSONText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"value", "#text"} {
			if nested := providerJSONFirst(typed, key); nested != nil {
				return providerJSONText(nested)
			}
		}
		for _, nested := range typed {
			return providerJSONText(nested)
		}
		return ""
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	default:
		return fmt.Sprint(typed)
	}
}

func providerJSONTime(value any) *time.Time {
	text := strings.TrimSpace(providerJSONText(value))
	if text == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return nil
	}
	return &parsed
}
