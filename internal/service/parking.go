package service

import (
	"context"
	"fmt"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

const (
	parkingAvailabilityComplete = ""
	parkingAvailabilityMissing  = "missing"
	parkingAvailabilityPartial  = "partial"
)

func (a *App) ParkingOptions(ctx context.Context, input model.ParkingOptionsInput) model.Envelope {
	if input.RadiusM <= 0 {
		input.RadiusM = 1500
	}
	if input.Limit <= 0 {
		input.Limit = 10
	}
	point, err := a.ResolvePoint(ctx, input.Destination)
	if err != nil || point == nil {
		return model.NewEnvelope("not_found", "Destination not found.", map[string]any{"options": []model.ParkingOption{}})
	}
	if a.Provider == nil {
		result := model.NewEnvelope("unavailable", "Parking source is unavailable.", map[string]any{"options": []model.ParkingOption{}})
		result.Degraded = true
		return result
	}
	options, err := a.Provider.Parkings(ctx, *point, input.Types, float64(input.RadiusM), input.MinimumSpaces, input.Limit*2)
	if err != nil && len(options) == 0 {
		result := model.NewEnvelope("unavailable", "Parking data is unavailable.", map[string]any{"options": []model.ParkingOption{}})
		result.Warn("SOURCE_UNAVAILABLE", "parking", "Parking retrieval failed.", true)
		return result
	}
	options, availability := normalizeParkingOptions(*point, options, input.Types, float64(input.RadiusM), input.MinimumSpaces, input.Limit)
	maximumAge := a.maximumStaleSeconds(ctx, "parking_realtime")
	now := model.Now()
	staleObserved := false
	missingObserved := false
	observations := []*time.Time{}
	retrievals := []*time.Time{}
	for index := range options {
		option := &options[index]
		if option.RetrievedAt == nil {
			option.RetrievedAt = model.Ptr(now)
		}
		retrievals = append(retrievals, option.RetrievedAt)
		if option.AvailableSpaces == nil {
			continue
		}
		_, stale, missing := observationFreshness(option.ObservedAt, option.RetrievedAt, now, maximumAge)
		option.Stale = stale
		staleObserved = staleObserved || stale
		missingObserved = missingObserved || missing
		observations = append(observations, option.ObservedAt)
	}
	status := "ok"
	if len(options) == 0 {
		status = "not_found"
	}
	result := model.NewEnvelope(status, fmt.Sprintf("%d parking option(s) found.", len(options)), map[string]any{"options": options})
	if len(options) != 0 {
		observed := oldestObservation(observations...)
		retrieved := latestRetrieval(retrievals...)
		if retrieved == nil {
			retrieved = model.Ptr(now)
		}
		age, _, _ := observationFreshness(observed, retrieved, now, maximumAge)
		result.Sources = append(result.Sources, model.SourceProvenance{
			Provider: "DataGrandLyon", SourceID: "parking_realtime", Dataset: "parking_availability",
			Attribution: "Métropole de Lyon", License: "unknown",
			ObservedAt: observed, RetrievedAt: retrieved, AgeSeconds: age,
			Realtime: true, Stale: staleObserved,
		})
	}
	if err != nil {
		result.Warn("PARTIAL_RESULT", "parking", "Some requested parking sources are unavailable.", true)
	}
	switch availability {
	case parkingAvailabilityMissing:
		result.Warn("PARTIAL_RESULT", "parking_realtime", "No validated realtime availability is available.", true)
	case parkingAvailabilityPartial:
		result.Warn("PARTIAL_RESULT", "parking_realtime", "Realtime availability is incomplete.", false)
	}
	if missingObserved {
		result.Warn("PARTIAL_RESULT", "parking_realtime", "Some availability values have no observation timestamp, so their age is unknown.", false)
	}
	if staleObserved {
		result.Warn("STALE_DATA", "parking_realtime", fmt.Sprintf("Parking observations exceed the %d-second freshness limit.", maximumAge), false)
	}
	return result
}

func normalizeParkingOptions(point model.Point, options []model.ParkingOption, types []string, radiusM float64, minimumSpaces, limit int) ([]model.ParkingOption, string) {
	if limit <= 0 {
		limit = 10
	}
	allowed := make(map[string]bool, len(types))
	for _, value := range types {
		allowed[value] = true
	}
	result := make([]model.ParkingOption, 0, min(limit, len(options)))
	hasLive := false
	hasUnknown := false
	for _, option := range options {
		distance := model.Distance(point, model.Point{Latitude: option.Latitude, Longitude: option.Longitude})
		if distance > radiusM || len(allowed) != 0 && !allowed[option.Type] {
			continue
		}
		if option.AvailableSpaces != nil && *option.AvailableSpaces < minimumSpaces {
			continue
		}
		option.DistanceM = &distance
		if option.AvailableSpaces == nil {
			hasUnknown = true
			option.Realtime = false
			option.AvailabilityRatio = nil
			if option.Status != "closed" {
				option.Status = "unknown"
			}
		} else {
			hasLive = true
			if *option.AvailableSpaces == 0 && option.Status != "closed" {
				option.Status = "full"
			}
		}
		result = append(result, option)
		if len(result) == limit {
			break
		}
	}
	if len(result) == 0 || !hasUnknown {
		return result, parkingAvailabilityComplete
	}
	if hasLive {
		return result, parkingAvailabilityPartial
	}
	return result, parkingAvailabilityMissing
}
