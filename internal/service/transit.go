package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func (app *App) NextDepartures(ctx context.Context, input model.NextDeparturesInput) model.Envelope {
	if input.Limit <= 0 {
		input.Limit = 6
	}
	generated := model.Now()
	at := generated
	if input.At != nil {
		at = *input.At
	}
	departures := []model.Departure{}
	result := model.NewEnvelope("ok", "", map[string]any{"stop": map[string]any{}, "departures": departures})
	stops, err := app.ResolveRef(ctx, input.Stop, 5)
	if err != nil || len(stops) == 0 {
		result.Status = "not_found"
		result.Summary = "Stop not found."
		return result
	}
	if stops[0].Type != "transport_stop" {
		point := model.Point{Latitude: stops[0].Latitude, Longitude: stops[0].Longitude}
		stops, err = app.Store.NearEntities(ctx, point, 500, []string{"transport_stop"}, 5)
		if err != nil {
			result.Status = "unavailable"
			result.Summary = "Nearby stops are unavailable."
			result.Warn("SOURCE_UNAVAILABLE", "entities", "The local stop index could not be read.", true)
			return result
		}
		if len(stops) == 0 {
			indexed, indexErr := app.Store.HasTransitIndex(ctx)
			if indexErr != nil || !indexed {
				result.Status = "unavailable"
				result.Summary = "The local stop index is unavailable."
				result.Warn("SOURCE_UNAVAILABLE", "gtfs_tcl", "Import GTFS data before requesting departures near a place.", false)
				return result
			}
			result.Status = "not_found"
			result.Summary = "No imported stop was found within 500 metres of this place."
			return result
		}
	}
	stop := stops[0]
	result.Data["stop"] = stop
	if len(stops) > 1 && model.Normalize(stop.Name) != model.Normalize(input.Stop.Query) {
		result.Warn("AMBIGUOUS_PLACE", "places", "Several stops match; the first candidate is used.", false)
	}
	filter := func(raw []model.Departure) []model.Departure {
		matched := []model.Departure{}
		for _, departure := range raw {
			if !model.LineMatches(input.Line, departure.LineName, departure.LineID) {
				continue
			}
			if input.Direction != "" && !strings.Contains(strings.ToLower(departure.Destination), strings.ToLower(input.Direction)) {
				continue
			}
			when := departure.ExpectedAt
			if when == nil {
				when = departure.ScheduledAt
			}
			if when == nil || when.Before(at) || !when.Before(at.Add(24*time.Hour)) {
				continue
			}
			matched = append(matched, departure)
		}
		return matched
	}
	useRealtime := app.Config.Offline || input.At == nil || at.Sub(generated).Abs() <= 5*time.Minute
	if app.Provider != nil && useRealtime {
		raw, err := app.Provider.Departures(ctx, stop.ID, input.Line, input.Direction, 100)
		if err != nil {
			result.Warn("SOURCE_UNAVAILABLE", "tcl_departures", "Realtime departures are unavailable; use GTFS when possible.", true)
		} else {
			departures = filter(raw)
		}
	}
	if len(departures) == 0 {
		raw, err := app.Store.ScheduledDepartures(ctx, stop.ID, at, input.Line, input.Direction, input.Limit)
		if err != nil {
			result.Warn("SOURCE_UNAVAILABLE", "gtfs", "Scheduled departures are unavailable.", true)
		} else {
			departures = filter(raw)
			for i := range departures {
				departures[i].Realtime = false
			}
			result.Sources = append(result.Sources, model.SourceProvenance{Provider: "GTFS", SourceID: "gtfs_tcl", Dataset: "GTFS_TCL", Attribution: "SYTRAL Mobilités", License: "unknown", RetrievedAt: model.Ptr(generated), AgeSeconds: model.Ptr(0)})
			if len(departures) > 0 {
				result.Warn("STALE_DATA", "gtfs_tcl", "These are scheduled GTFS departures, not realtime observations.", false)
			}
		}
	} else {
		result.Sources = append(result.Sources, model.SourceProvenance{Provider: "DataGrandLyon", SourceID: "tcl_departures", Dataset: "prochains_passages", Attribution: "SYTRAL Mobilités", License: "unknown", ObservedAt: model.Ptr(generated), RetrievedAt: model.Ptr(generated), AgeSeconds: model.Ptr(0), Realtime: true})
	}
	sort.SliceStable(departures, func(i, j int) bool {
		a, b := departures[i].ExpectedAt, departures[j].ExpectedAt
		if a == nil {
			a = departures[i].ScheduledAt
		}
		if b == nil {
			b = departures[j].ScheduledAt
		}
		return a.Before(*b)
	})
	if len(departures) > input.Limit {
		departures = departures[:input.Limit]
	}
	result.Data["departures"] = departures
	result.Summary = fmt.Sprintf("%d departure(s) for %s.", len(departures), stop.Name)
	if len(departures) == 0 {
		result.Status = "not_found"
		result.Degraded = true
		if len(result.Warnings) > 0 {
			result.Status = "unavailable"
		}
	}
	return result
}
