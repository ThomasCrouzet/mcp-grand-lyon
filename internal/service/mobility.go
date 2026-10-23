package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func (a *App) MobilityStatus(ctx context.Context, input model.MobilityStatusInput) model.Envelope {
	if len(input.Include) == 0 {
		input.Include = []string{"transit", "accessibility", "traffic", "roadworks"}
	}
	data := map[string]any{
		"alerts": []model.TransitAlert{}, "line_statuses": []map[string]any{},
		"accessibility_incidents": []model.AccessibilityIncident{},
		"road_events":             []model.RoadEvent{}, "traffic_conditions": []model.TrafficCondition{},
	}
	supportedIncludes := mobilityStringSet([]string{"transit", "accessibility", "traffic", "roadworks"})
	for _, include := range input.Include {
		if !supportedIncludes[include] {
			return model.NewEnvelope("invalid_request", "Unsupported mobility include: "+include, data)
		}
	}
	result := model.NewEnvelope("ok", "Mobility status: 0 alert(s).", data)
	if a.Provider == nil {
		result.Status = "unavailable"
		result.Warn("SOURCE_UNAVAILABLE", "mobility", "Mobility providers are not configured.", true)
		return result
	}
	includes := mobilityStringSet(input.Include)
	if includes["transit"] {
		alerts, err := a.Provider.Alerts(ctx, input.Lines)
		if err != nil {
			result.Warn("SOURCE_UNAVAILABLE", "tcl_alerts", "TCL alerts are unavailable.", true)
		} else {
			alerts = uniqueAlerts(alerts)
			data["alerts"] = alerts
			statuses := make([]map[string]any, 0, len(input.Lines))
			for _, line := range input.Lines {
				count := 0
				for _, alert := range alerts {
					if len(alert.Lines) == 0 || containsFold(alert.Lines, line) {
						count++
					}
				}
				statuses = append(statuses, map[string]any{"line": line, "alert_count": count})
			}
			data["line_statuses"] = statuses
			result.Summary = fmt.Sprintf("Mobility status: %d alert(s).", len(alerts))
		}
	}
	if includes["accessibility"] {
		incidents, err := a.Provider.Incidents(ctx, input.Lines)
		if err != nil {
			result.Warn("SOURCE_UNAVAILABLE", "tcl_accessibility", "Accessibility data is unavailable.", true)
		} else {
			data["accessibility_incidents"] = incidents
		}
	}

	areaLabels, areaPoints, unresolved := a.resolveMobilityAreas(ctx, input.Areas)
	for _, ref := range unresolved {
		result.Warn("PLACE_NOT_FOUND", "areas", "Mobility area could not be resolved: "+mobilityRefLabel(ref)+".", false)
	}
	if includes["traffic"] {
		conditions := []model.TrafficCondition{}
		if len(areaPoints) > len(areaLabels) {
			result.Warn("PARTIAL_RESULT", "traffic", "Traffic conditions cannot be filtered by coordinate-only areas.", false)
		}
		if len(input.Areas) == 0 || len(areaLabels) != 0 {
			labels := areaLabels
			if len(labels) == 0 {
				labels = []string{""}
			}
			for _, label := range labels {
				values, err := a.Provider.Traffic(ctx, label)
				if err != nil {
					result.Warn("SOURCE_UNAVAILABLE", "traffic", "Traffic conditions are unavailable.", true)
					continue
				}
				conditions = append(conditions, values...)
			}
			data["traffic_conditions"] = uniqueTraffic(conditions)
		}
	}
	if includes["roadworks"] {
		events := []model.RoadEvent{}
		if len(input.Areas) != 0 && len(areaPoints) == 0 {
			data["road_events"] = events
			return result
		}
		values, err := a.Provider.RoadEvents(ctx, "")
		if err != nil {
			result.Warn("SOURCE_UNAVAILABLE", "road_events", "Road events are unavailable.", true)
		} else {
			events = append(events, values...)
		}
		events = uniqueRoadEvents(events)
		if len(areaPoints) != 0 {
			var omitted int
			events, omitted = filterRoadEvents(events, areaPoints, 1500)
			if omitted != 0 {
				result.Warn("PARTIAL_RESULT", "road_events", "Road events without matching coordinates were omitted.", false)
			}
		}
		data["road_events"] = events
	}
	return result
}

func (a *App) resolveMobilityAreas(ctx context.Context, refs []model.PlaceRef) ([]string, []model.Point, []model.PlaceRef) {
	labels := []string{}
	points := []model.Point{}
	unresolved := []model.PlaceRef{}
	for _, ref := range refs {
		point, err := a.ResolvePoint(ctx, ref)
		if err != nil || point == nil {
			unresolved = append(unresolved, ref)
			continue
		}
		points = append(points, *point)
		label := strings.TrimSpace(ref.Query)
		if label != "" {
			labels = append(labels, label)
		}
	}
	return uniqueMobilityStrings(labels), points, unresolved
}

func mobilityRefLabel(ref model.PlaceRef) string {
	if value := strings.TrimSpace(ref.Query); value != "" {
		return value
	}
	if value := strings.TrimSpace(ref.ProfilePlace); value != "" {
		return value
	}
	if value := strings.TrimSpace(ref.PlaceID); value != "" {
		return value
	}
	if ref.Latitude != nil && ref.Longitude != nil {
		return fmt.Sprintf("%g,%g", *ref.Latitude, *ref.Longitude)
	}
	return "invalid reference"
}

func filterRoadEvents(events []model.RoadEvent, areas []model.Point, radiusM float64) ([]model.RoadEvent, int) {
	result := []model.RoadEvent{}
	omitted := 0
	for _, event := range events {
		if event.Latitude == nil || event.Longitude == nil {
			omitted++
			continue
		}
		matched := false
		for _, area := range areas {
			if model.Distance(area, model.Point{Latitude: *event.Latitude, Longitude: *event.Longitude}) <= radiusM {
				matched = true
				break
			}
		}
		if matched {
			result = append(result, event)
		} else {
			omitted++
		}
	}
	return result, omitted
}

func uniqueAlerts(values []model.TransitAlert) []model.TransitAlert {
	seen := map[string]bool{}
	result := []model.TransitAlert{}
	for _, value := range values {
		if seen[value.ID] {
			continue
		}
		seen[value.ID] = true
		result = append(result, value)
	}
	return result
}

func uniqueTraffic(values []model.TrafficCondition) []model.TrafficCondition {
	seen := map[string]bool{}
	result := []model.TrafficCondition{}
	for _, value := range values {
		if seen[value.ID] {
			continue
		}
		seen[value.ID] = true
		result = append(result, value)
	}
	return result
}

func uniqueRoadEvents(values []model.RoadEvent) []model.RoadEvent {
	seen := map[string]bool{}
	result := []model.RoadEvent{}
	for _, value := range values {
		if seen[value.ID] {
			continue
		}
		seen[value.ID] = true
		result = append(result, value)
	}
	return result
}

func containsFold(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(value, expected) {
			return true
		}
	}
	return false
}

func uniqueMobilityStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func mobilityStringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
