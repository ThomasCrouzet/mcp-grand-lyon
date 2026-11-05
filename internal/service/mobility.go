package service

import (
	"context"
	"fmt"
	"strings"
	"sync"

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
	areaLabels, areaPoints, unresolved := a.resolveMobilityAreas(ctx, input.Areas)
	type branchResult struct {
		alerts         []model.TransitAlert
		alertsErr      error
		incidents      []model.AccessibilityIncident
		incidentsErr   error
		conditions     []model.TrafficCondition
		trafficErr     error
		trafficBlocked bool
		events         []model.RoadEvent
		eventsErr      error
		roadBlocked    bool
	}
	branch := branchResult{}
	branch.trafficBlocked = false
	branch.roadBlocked = len(input.Areas) != 0 && len(areaPoints) == 0
	limit := a.Config.MaxParallel
	if limit < 1 {
		limit = 1
	}
	if limit > 4 {
		limit = 4
	}
	sem := make(chan struct{}, limit)
	var waitGroup sync.WaitGroup
	run := func(operation func()) {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			operation()
		}()
	}
	if includes["transit"] {
		run(func() { branch.alerts, branch.alertsErr = a.Provider.Alerts(ctx, input.Lines) })
	}
	if includes["accessibility"] {
		run(func() { branch.incidents, branch.incidentsErr = a.Provider.Incidents(ctx, input.Lines) })
	}
	if includes["traffic"] {
		run(func() {
			labels := areaLabels
			if len(labels) == 0 {
				labels = []string{""}
			}
			for _, label := range labels {
				values, err := a.Provider.Traffic(ctx, label)
				if err != nil {
					branch.trafficErr = err
					continue
				}
				branch.conditions = append(branch.conditions, values...)
			}
		})
	}
	if includes["roadworks"] && !branch.roadBlocked {
		run(func() { branch.events, branch.eventsErr = a.Provider.RoadEvents(ctx, "") })
	}
	waitGroup.Wait()

	for _, ref := range unresolved {
		result.Warn("PLACE_NOT_FOUND", "areas", "Mobility area could not be resolved: "+mobilityRefLabel(ref)+".", false)
	}
	if includes["transit"] {
		if branch.alertsErr != nil {
			if len(branch.alerts) == 0 {
				result.Warn("SOURCE_UNAVAILABLE", "tcl_alerts", "TCL alerts are unavailable.", true)
			} else {
				result.Warn("PARTIAL_RESULT", "tcl_alerts", "TCL alerts are incomplete because a later source page failed.", true)
			}
		}
		if len(branch.alerts) != 0 || branch.alertsErr == nil {
			alerts := uniqueAlerts(branch.alerts)
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
		if branch.incidentsErr != nil {
			if len(branch.incidents) == 0 {
				result.Warn("SOURCE_UNAVAILABLE", "tcl_accessibility", "Accessibility data is unavailable.", true)
			} else {
				result.Warn("PARTIAL_RESULT", "tcl_accessibility", "Accessibility data is incomplete because a later source page failed.", true)
			}
		}
		if len(branch.incidents) != 0 || branch.incidentsErr == nil {
			data["accessibility_incidents"] = branch.incidents
		}
	}
	if includes["traffic"] {
		if len(areaPoints) > len(areaLabels) {
			result.Warn("PARTIAL_RESULT", "traffic", "Traffic conditions cannot be filtered by coordinate-only areas.", false)
		}
		if branch.trafficErr != nil {
			result.Warn("SOURCE_UNAVAILABLE", "traffic", "Traffic conditions are unavailable.", true)
		}
		data["traffic_conditions"] = uniqueTraffic(branch.conditions)
	}
	if includes["roadworks"] {
		if branch.roadBlocked {
			data["road_events"] = []model.RoadEvent{}
		} else {
			if branch.eventsErr != nil {
				if len(branch.events) == 0 {
					result.Warn("SOURCE_UNAVAILABLE", "road_events", "Road events are unavailable.", true)
				} else {
					result.Warn("PARTIAL_RESULT", "road_events", "Road events are incomplete because a later source page failed.", true)
				}
			}
			events := uniqueRoadEvents(branch.events)
			if len(areaPoints) != 0 {
				var omitted int
				events, omitted = filterRoadEvents(events, areaPoints, 1500)
				if omitted != 0 {
					result.Warn("PARTIAL_RESULT", "road_events", "Road events without matching coordinates were omitted.", false)
				}
			}
			data["road_events"] = events
		}
	}
	retrieved := model.Ptr(model.Now())
	for _, source := range []struct {
		include bool
		id      string
		dataset string
		attrib  string
	}{
		{includes["transit"], "tcl_alerts", "tcl_alerts", "SYTRAL Mobilités"},
		{includes["accessibility"], "tcl_accessibility", "tcl_accessibility", "SYTRAL Mobilités"},
		{includes["traffic"], "traffic", "traffic", "Métropole de Lyon"},
		{includes["roadworks"], "road_events", "road_events", "Métropole de Lyon"},
	} {
		if source.include {
			result.Sources = append(result.Sources, model.SourceProvenance{
				Provider: "DataGrandLyon", SourceID: source.id, Dataset: source.dataset,
				Attribution: source.attrib, License: "unknown", RetrievedAt: retrieved,
			})
		}
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
