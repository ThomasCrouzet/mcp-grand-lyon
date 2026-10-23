package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func (a *App) VelovStations(ctx context.Context, ref model.PlaceRef, radiusM float64, minimumBikes, limit int) model.Envelope {
	if radiusM <= 0 {
		radiusM = 1000
	}
	if limit <= 0 {
		limit = 10
	}
	point, err := a.ResolvePoint(ctx, ref)
	if err != nil || point == nil {
		return model.NewEnvelope("not_found", "Place not found for Vélo'v.", map[string]any{"stations": []model.VelovStation{}})
	}
	if a.Provider == nil {
		result := model.NewEnvelope("unavailable", "Vélo'v source is unavailable.", map[string]any{"stations": []model.VelovStation{}})
		result.Warn("SOURCE_UNAVAILABLE", "velov_realtime", "Vélo'v provider is not configured.", true)
		return result
	}
	stations, err := a.Provider.Stations(ctx, *point, radiusM, limit*2)
	if err != nil {
		result := model.NewEnvelope("unavailable", "Vélo'v data is unavailable.", map[string]any{"stations": []model.VelovStation{}})
		result.Warn("SOURCE_UNAVAILABLE", "velov_realtime", "Vélo'v retrieval failed.", true)
		return result
	}
	filtered := make([]model.VelovStation, 0, min(limit, len(stations)))
	for _, station := range stations {
		distance := model.Distance(*point, model.Point{Latitude: station.Latitude, Longitude: station.Longitude})
		if distance > radiusM || station.BikesAvailable < minimumBikes || station.Status == "closed" {
			continue
		}
		station.DistanceM = &distance
		filtered = append(filtered, station)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return model.Value(filtered[i].DistanceM) < model.Value(filtered[j].DistanceM) })
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	result := model.NewEnvelope("ok", fmt.Sprintf("%d Vélo'v station(s) found.", len(filtered)), map[string]any{"stations": filtered})
	now := model.Now()
	maximumAge := a.velovMaximumStaleSeconds(ctx)
	var oldestObserved *time.Time
	oldestAge := 0
	missingObserved := false
	staleObserved := false
	for _, station := range filtered {
		if station.ObservedAt == nil {
			missingObserved = true
			continue
		}
		age := stationAge(station, now)
		if oldestObserved == nil || age > oldestAge {
			observed := station.ObservedAt.In(model.Paris)
			oldestObserved = &observed
			oldestAge = age
		}
		if age > maximumAge {
			staleObserved = true
		}
	}
	if staleObserved {
		result.Warn("STALE_DATA", "velov_realtime", fmt.Sprintf("Vélo'v observations exceed the %d-second freshness limit.", maximumAge), false)
	}
	if missingObserved {
		result.Warn("PARTIAL_RESULT", "velov_realtime", "Some Vélo'v stations have no observation timestamp.", false)
	}
	result.Sources = append(result.Sources, model.SourceProvenance{
		Provider: "DataGrandLyon", SourceID: "velov_realtime", Dataset: "velov_realtime",
		Attribution: "Métropole de Lyon / JCDecaux", License: "unknown",
		ObservedAt: oldestObserved, RetrievedAt: model.Ptr(now), AgeSeconds: observedAge(oldestObserved, oldestAge),
		Realtime: true, Stale: staleObserved,
	})
	if a.Store != nil {
		for _, station := range filtered {
			if station.ObservedAt == nil {
				continue
			}
			if err := a.Store.RecordVelov(ctx, station); err != nil {
				result.Warn("PARTIAL_RESULT", "velov_history", "Vélo'v history was not recorded.", true)
				return result
			}
		}
	}
	return result
}

func (a *App) velovMaximumStaleSeconds(ctx context.Context) int {
	const fallback = 300
	if a.Store == nil {
		return fallback
	}
	source, err := a.Store.GetSource(ctx, "velov_realtime")
	if err != nil || source == nil {
		return fallback
	}
	value := briefingIntValue(source["maximum_stale_seconds"])
	if value <= 0 {
		return fallback
	}
	return value
}

func observedAge(observed *time.Time, age int) *int {
	if observed == nil {
		return nil
	}
	return model.Ptr(age)
}

func scoreStation(bikes, docks, capacity int, distanceM, maxWalkingM float64, ageSeconds, maxAge int, reliability float64, sampleCount int) model.VelovScore {
	availableBase := min(5, max(1, capacity))
	availability := math.Min(1, float64(bikes)/float64(availableBase))
	distance := math.Max(0, 1-distanceM/math.Max(maxWalkingM, 1))
	freshness := math.Max(0, 1-float64(ageSeconds)/float64(max(maxAge, 1)))
	historical := 0.0
	if sampleCount >= 5 {
		historical = reliability
	}
	score := 0.4*availability + 0.3*distance + 0.15*freshness + 0.15*historical
	return model.VelovScore{
		Score: math.Max(0, math.Min(1, score)), AvailabilityScore: availability,
		DistanceScore: distance, FreshnessScore: freshness, HistoricalReliabilityScore: historical,
	}
}

func stationAge(station model.VelovStation, now time.Time) int {
	if station.ObservedAt == nil {
		return 0
	}
	return max(0, int(now.Sub(*station.ObservedAt).Seconds()))
}
