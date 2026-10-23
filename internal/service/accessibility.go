package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func (app *App) AccessibilityCheck(ctx context.Context, input model.AccessibilityCheckInput) model.Envelope {
	needs := input.Needs
	if needs == nil {
		needs = []string{"wheelchair"}
	}
	if len(needs) == 0 {
		return invalidAccessibility("At least one accessibility need is required.", needs)
	}
	for _, need := range needs {
		if need != "wheelchair" && need != "step_free" {
			return invalidAccessibility("Unsupported accessibility need: "+need, needs)
		}
	}
	refs := append([]model.PlaceRef{}, input.Stops...)
	if input.Origin != nil {
		refs = append(refs, *input.Origin)
	}
	if input.Destination != nil {
		refs = append(refs, *input.Destination)
	}
	if len(refs) == 0 {
		return invalidAccessibility("origin, destination, or stops is required.", needs)
	}

	envelope := model.NewEnvelope("ok", "Accessibility data is available.", nil)
	if contains(needs, "step_free") {
		envelope.Warn("PARTIAL_RESULT", "accessibility", "Available sources do not verify the complete step-free path.", false)
	}
	incidents := []model.AccessibilityIncident{}
	if app.Provider == nil {
		envelope.Warn("SOURCE_UNAVAILABLE", "accessibility", "The accessibility provider is unavailable.", true)
	} else {
		var err error
		incidents, err = app.Provider.Incidents(ctx, nil)
		if err != nil {
			incidents = []model.AccessibilityIncident{}
			envelope.Warn("SOURCE_UNAVAILABLE", "accessibility_incidents", "Accessibility incidents are unavailable.", true)
		}
	}

	segments := make([]model.AccessibilitySegment, 0, len(refs))
	unknowns := []string{}
	statuses := make([]string, 0, len(refs))
	for _, ref := range refs {
		segment := app.accessibilitySegment(ctx, ref, incidents, &envelope)
		segments = append(segments, segment)
		statuses = append(statuses, segment.Status)
		if segment.Status == "unknown" {
			unknowns = append(unknowns, segment.Name)
		}
	}
	overall := aggregateAccessibility(statuses)
	if overall == "unknown" {
		envelope.Status = "partial"
		envelope.Degraded = true
	}
	envelope.Data = map[string]any{
		"overall_status": overall, "segments": segments, "incidents": incidents,
		"unknowns": unknowns, "requested_needs": needs,
	}
	switch overall {
	case "inaccessible":
		envelope.Summary = "The request includes an inaccessible stop or incident."
	case "partially_accessible":
		envelope.Summary = "Accessibility is partial or only declarative."
	case "accessible":
		envelope.Summary = "Accessibility is verified."
	default:
		envelope.Summary = "Accessibility is unknown."
	}
	return envelope
}

func invalidAccessibility(message string, needs []string) model.Envelope {
	return model.NewEnvelope("invalid_request", message, map[string]any{
		"overall_status": "unknown", "segments": []model.AccessibilitySegment{},
		"incidents": []model.AccessibilityIncident{}, "unknowns": []string{},
		"requested_needs": needs,
	})
}

func (app *App) accessibilitySegment(ctx context.Context, ref model.PlaceRef, incidents []model.AccessibilityIncident, envelope *model.Envelope) model.AccessibilitySegment {
	segment := model.AccessibilitySegment{Status: "unknown", Unknowns: []string{}, Evidence: []map[string]string{}}
	candidates, err := app.ResolveRef(ctx, ref, 1)
	if err != nil || len(candidates) == 0 {
		segment.Name = accessibilityLabel(ref)
		segment.Notes = "The stop could not be resolved."
		segment.Unknowns = []string{"place_not_found"}
		envelope.Warn("PLACE_NOT_FOUND", "place", "An accessibility stop could not be resolved.", false)
		return segment
	}
	candidate := candidates[0]
	segment.Name = candidate.Name
	notes := []string{}
	matched := matchingIncidents(incidents, candidate)
	for _, incident := range matched {
		detail := incident.Description
		if detail == "" {
			detail = incident.Location
		}
		segment.Evidence = append(segment.Evidence, accessibilityEvidence(
			"tcl_accessibility_alerts", "incident", "high", detail, "inaccessible",
		))
	}
	if len(matched) > 0 {
		segment.Status = "inaccessible"
		notes = append(notes, fmt.Sprintf("%d accessibility incidents match this stop.", len(matched)))
	}

	if app.Provider != nil && segment.Status == "unknown" {
		status, checkErr := app.Provider.CheckStop(ctx, candidate.ID)
		if checkErr != nil {
			envelope.Warn("SOURCE_UNAVAILABLE", "accessibility_check", "The stop accessibility check failed.", true)
		} else {
			switch status {
			case "inaccessible":
				segment.Status = "inaccessible"
				segment.Evidence = append(segment.Evidence, accessibilityEvidence(
					"tcl_accessibility_alerts", "check_stop", "high", status, "inaccessible",
				))
				notes = append(notes, "The provider reports an accessibility incident.")
			case "partially_accessible":
				segment.Status = "partially_accessible"
				segment.Evidence = append(segment.Evidence, accessibilityEvidence(
					"provider", "check_stop", "medium", status, "partially_accessible",
				))
				notes = append(notes, "The provider reports partial accessibility.")
			case "accessible":
				segment.Evidence = append(segment.Evidence, accessibilityEvidence(
					"provider", "check_stop_accessible_claim", "low", "claim ignored without structural proof", "unknown",
				))
				notes = append(notes, "The provider claim lacks structural verification.")
			}
		}
	}

	if app.Store != nil {
		boarding, boardingErr := app.Store.WheelchairBoarding(ctx, candidate.ID)
		if boardingErr != nil {
			envelope.Warn("SOURCE_UNAVAILABLE", "gtfs_wheelchair", "GTFS wheelchair data is unavailable.", true)
		} else if boarding != nil {
			switch *boarding {
			case 2:
				segment.Status = "inaccessible"
				segment.Evidence = append(segment.Evidence, accessibilityEvidence(
					"gtfs_wheelchair", "wheelchair_boarding=2", "medium", "GTFS declares that wheelchair boarding is unavailable.", "inaccessible",
				))
				notes = append(notes, "GTFS declares that wheelchair boarding is unavailable.")
			case 1:
				segment.Evidence = append(segment.Evidence, accessibilityEvidence(
					"gtfs_wheelchair", "wheelchair_boarding=1", "low", "GTFS declares possible access without live verification.", "partially_accessible",
				))
				if segment.Status == "unknown" {
					segment.Status = "partially_accessible"
				}
				notes = append(notes, "GTFS provides declarative wheelchair access only.")
			case 0:
				segment.Evidence = append(segment.Evidence, accessibilityEvidence(
					"gtfs_wheelchair", "wheelchair_boarding=0", "low", "GTFS provides no wheelchair boarding information.", "unknown",
				))
			}
		}
	}
	if segment.Status == "unknown" {
		segment.Unknowns = []string{"no_declared_data"}
		if len(notes) == 0 {
			notes = append(notes, "No source verifies structural accessibility.")
		}
	}
	segment.Notes = strings.Join(notes, " ")
	return segment
}

func accessibilityEvidence(source, fact, confidence, detail, status string) map[string]string {
	return map[string]string{
		"source": source, "fact": fact, "confidence": confidence,
		"detail": detail, "status": status,
	}
}

func matchingIncidents(incidents []model.AccessibilityIncident, candidate model.PlaceCandidate) []model.AccessibilityIncident {
	name := model.Normalize(candidate.Name)
	id := strings.ToLower(candidate.ID)
	matched := make([]model.AccessibilityIncident, 0)
	for _, incident := range incidents {
		location := model.Normalize(incident.Location)
		rawLocation := strings.ToLower(incident.Location)
		if name != "" && location != "" && (strings.Contains(name, location) || strings.Contains(location, name)) {
			matched = append(matched, incident)
			continue
		}
		if id != "" && rawLocation != "" && (strings.Contains(id, rawLocation) || strings.Contains(rawLocation, id)) {
			matched = append(matched, incident)
		}
	}
	return matched
}

func accessibilityLabel(ref model.PlaceRef) string {
	for _, label := range []string{ref.Query, ref.PlaceID, ref.ProfilePlace} {
		if label != "" {
			return label
		}
	}
	return "stop"
}

func aggregateAccessibility(statuses []string) string {
	if len(statuses) == 0 {
		return "unknown"
	}
	hasUnknown := false
	hasPartial := false
	for _, status := range statuses {
		switch status {
		case "inaccessible":
			return "inaccessible"
		case "unknown":
			hasUnknown = true
		case "partially_accessible":
			hasPartial = true
		}
	}
	if hasUnknown && !hasPartial {
		allUnknown := true
		for _, status := range statuses {
			allUnknown = allUnknown && status == "unknown"
		}
		if allUnknown {
			return "unknown"
		}
	}
	if hasUnknown || hasPartial {
		return "partially_accessible"
	}
	return "accessible"
}
