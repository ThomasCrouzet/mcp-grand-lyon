package providers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func (live *Live) Plan(ctx context.Context, origin, destination model.Point, departure time.Time, modes []string, preferences model.TripPreferences) ([]model.TripOption, error) {
	if !live.cfg.TransitousEnabled {
		return []model.TripOption{}, nil
	}
	if len(modes) != 0 {
		hasTransit := false
		for _, mode := range modes {
			hasTransit = hasTransit || mode == "tcl"
		}
		if !hasTransit {
			return []model.TripOption{}, nil
		}
	}
	base := strings.TrimRight(live.cfg.TransitousBaseURL, "/")
	if base == "" {
		base = "https://api.transitous.org/api"
	}
	values := url.Values{
		"fromPlace": {strconv.FormatFloat(origin.Latitude, 'f', -1, 64) + "," + strconv.FormatFloat(origin.Longitude, 'f', -1, 64)},
		"toPlace":   {strconv.FormatFloat(destination.Latitude, 'f', -1, 64) + "," + strconv.FormatFloat(destination.Longitude, 'f', -1, 64)},
		"time":      {departure.Format(time.RFC3339)},
	}
	if preferences.Wheelchair {
		values.Set("wheelchair", "true")
	}
	decoded, err := live.getJSON(ctx, base+"/v5/plan?"+values.Encode(), false)
	if err != nil {
		return nil, err
	}
	return ParseTransitousPlan(decoded)
}

// ParseTransitousPlan normalizes MOTIS itinerary evidence while preserving the
// provider's actual instants. It never reconstructs missing or contradictory
// timing from the request time.
func ParseTransitousPlan(payload any) ([]model.TripOption, error) {
	root, ok := payload.(map[string]any)
	if !ok {
		return nil, errors.New("Transitous response is not an object")
	}
	raw := root["itineraries"]
	if raw == nil {
		raw = root["plans"]
	}
	items, _ := raw.([]any)
	result := make([]model.TripOption, 0, min(5, len(items)))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		option, ok := parseTransitousItinerary(entry)
		if !ok {
			continue
		}
		result = append(result, option)
		if len(result) == 5 {
			break
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ArrivalAt.Before(*result[j].ArrivalAt) })
	return result, nil
}

func parseTransitousItinerary(entry map[string]any) (model.TripOption, bool) {
	start := parseTransitousTime(first(entry, "startTime", "start_time", "departure"))
	end := parseTransitousTime(first(entry, "endTime", "end_time", "arrival"))
	if start == nil || end == nil || !end.After(*start) {
		return model.TripOption{}, false
	}
	duration := int(end.Sub(*start).Seconds())
	if duration <= 0 {
		return model.TripOption{}, false
	}
	legs, walking, walkingKnown, transitLegs, transfers, alerts, realtime := parseTransitousLegs(entry)
	if transitLegs == 0 {
		return model.TripOption{}, false
	}
	if value := optionalInteger(entry, "transfers"); value != nil && *value >= 0 {
		transfers = *value
	}
	var walkingDistance *int
	if walkingKnown {
		walkingDistance = model.Ptr(walking)
	}
	option := model.TripOption{
		Mode: "tcl", Score: 0.7, Evidence: "calculated", EstimatedDurationSeconds: &duration,
		DepartureAt: start, ArrivalAt: end, WalkingDistanceM: walkingDistance, Transfers: transfers,
		Legs: legs, Alerts: alerts, Realtime: realtime,
		Summary: firstText(entry, transitSummary(legs), "summary", "headline"),
		Sources: []string{"transitous"},
	}
	if len(option.Alerts) == 0 {
		option.Alerts = []model.TransitAlert{}
	}
	if accessible, ok := first(entry, "wheelchairAccessible", "wheelchair_accessible").(bool); ok && accessible {
		option.Accessibility = model.Ptr("accessible")
	}
	return option, true
}

func parseTransitousLegs(entry map[string]any) ([]model.TripLeg, int, bool, int, int, []model.TransitAlert, bool) {
	raw, _ := first(entry, "legs").([]any)
	legs := make([]model.TripLeg, 0, len(raw))
	alerts := []model.TransitAlert{}
	walking := 0
	walkingKnown := true
	transitLegs := 0
	realtime := false
	for index, value := range raw {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		mode := strings.ToLower(providerJSONText(first(item, "mode", "travelMode")))
		if mode == "" {
			mode = "unknown"
		}
		start := parseTransitousTime(first(item, "startTime", "start_time"))
		end := parseTransitousTime(first(item, "endTime", "end_time"))
		distance := optionalInteger(item, "distance", "distanceMeters", "distance_m")
		hasDistance := distance != nil
		legRealtime := providerJSONBool(first(item, "realTime", "realtime", "real_time"))
		if start == nil || end == nil || !end.After(*start) {
			return nil, 0, false, 0, 0, nil, false
		}
		leg := model.TripLeg{
			Mode: mode, Evidence: "calculated", Operator: providerJSONText(first(item, "agencyName", "agency", "operator")),
			Line: providerJSONText(first(item, "routeShortName", "route", "displayName", "routeLongName")),
			From: transitEndpointName(first(item, "from")), To: transitEndpointName(first(item, "to")),
			StartAt: start, EndAt: end, Realtime: legRealtime,
		}
		if hasDistance && *distance >= 0 {
			leg.DistanceM = distance
		}
		legs = append(legs, leg)
		realtime = realtime || legRealtime
		if strings.Contains(mode, "walk") {
			if !hasDistance || *distance < 0 {
				walkingKnown = false
			} else {
				walking += *distance
			}
		} else {
			transitLegs++
		}
		alerts = append(alerts, parseTransitousAlerts(first(item, "alerts"), index)...)
	}
	alerts = append(alerts, parseTransitousAlerts(first(entry, "alerts"), len(legs))...)
	transfers := max(0, transitLegs-1)
	return legs, walking, walkingKnown, transitLegs, transfers, dedupeTransitAlerts(alerts), realtime
}

func parseTransitousTime(value any) *time.Time {
	switch typed := value.(type) {
	case string:
		return parseTime(typed)
	case time.Time:
		copied := typed
		return &copied
	default:
		return nil
	}
}

func transitEndpointName(value any) string {
	item, ok := value.(map[string]any)
	if !ok {
		return providerJSONText(value)
	}
	return providerJSONText(first(item, "name", "stopName", "displayName"))
}

func transitSummary(legs []model.TripLeg) string {
	parts := []string{}
	for _, leg := range legs {
		if leg.Line != "" && !strings.Contains(strings.ToLower(leg.Mode), "walk") {
			parts = append(parts, leg.Line)
		}
	}
	if len(parts) == 0 {
		return "Itinéraire Transitous"
	}
	return strings.Join(parts, " puis ")
}

func parseTransitousAlerts(value any, offset int) []model.TransitAlert {
	raw, _ := value.([]any)
	result := []model.TransitAlert{}
	for index, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, model.TransitAlert{ID: fmt.Sprintf("transitous:%d:%d", offset, index), Severity: "unknown", Title: text, Lines: []string{}})
			continue
		}
		alert, ok := item.(map[string]any)
		if !ok {
			continue
		}
		nested, _ := first(alert, "alert", "alertHeaderText").(map[string]any)
		if nested != nil {
			alert = nested
		}
		title := providerJSONText(first(alert, "title", "headline", "headerText", "alertHeaderText"))
		if title == "" {
			continue
		}
		result = append(result, model.TransitAlert{
			ID:          firstText(alert, fmt.Sprintf("transitous:%d:%d", offset, index), "id", "alertId"),
			Severity:    firstText(alert, "unknown", "severity", "priority"),
			Title:       title,
			Description: providerJSONText(first(alert, "description", "text", "details")),
			Lines:       []string{},
			StartAt:     parseTransitousTime(first(alert, "startTime", "start_time")),
			EndAt:       parseTransitousTime(first(alert, "endTime", "end_time")),
		})
	}
	return result
}

func dedupeTransitAlerts(values []model.TransitAlert) []model.TransitAlert {
	seen := map[string]bool{}
	result := []model.TransitAlert{}
	for _, value := range values {
		key := value.ID
		if key == "" {
			key = value.Title
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func providerJSONBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}
