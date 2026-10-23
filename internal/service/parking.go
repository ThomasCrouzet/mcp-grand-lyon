package service

import (
	"context"
	"fmt"

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
	status := "ok"
	if len(options) == 0 {
		status = "not_found"
	}
	result := model.NewEnvelope(status, fmt.Sprintf("%d parking option(s) found.", len(options)), map[string]any{"options": options})
	if err != nil {
		result.Warn("PARTIAL_RESULT", "parking", "Some requested parking sources are unavailable.", true)
	}
	switch availability {
	case parkingAvailabilityMissing:
		result.Warn("PARTIAL_RESULT", "parking_realtime", "No validated realtime availability is available.", true)
	case parkingAvailabilityPartial:
		result.Warn("PARTIAL_RESULT", "parking_realtime", "Realtime availability is incomplete.", false)
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
