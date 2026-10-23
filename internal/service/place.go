package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

var ErrPlaceNotFound = errors.New("place not found")

func (app *App) ResolvePoint(ctx context.Context, ref model.PlaceRef) (*model.Point, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if ref.Latitude != nil && ref.Longitude != nil {
		return &model.Point{Latitude: *ref.Latitude, Longitude: *ref.Longitude}, nil
	}
	candidates, err := app.ResolveRef(ctx, ref, 1)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrPlaceNotFound
	}
	return &model.Point{Latitude: candidates[0].Latitude, Longitude: candidates[0].Longitude}, nil
}

func (app *App) ResolveRef(ctx context.Context, ref model.PlaceRef, limit int) ([]model.PlaceCandidate, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 5
	}
	if ref.Query != "" {
		candidates, _ := app.searchPlaces(ctx, ref.Query, nil, nil, limit)
		if len(candidates) == 0 {
			return nil, ErrPlaceNotFound
		}
		return candidates, nil
	}
	if ref.PlaceID != "" {
		candidate, err := app.Store.GetEntity(ctx, ref.PlaceID)
		if err != nil {
			return nil, err
		}
		if candidate == nil && strings.HasPrefix(ref.PlaceID, "gtfs:") {
			candidate, err = app.Store.GetStop(ctx, ref.PlaceID)
		}
		if err != nil {
			return nil, err
		}
		if candidate == nil {
			return nil, ErrPlaceNotFound
		}
		return []model.PlaceCandidate{*candidate}, nil
	}
	if ref.ProfilePlace != "" {
		place, err := app.Store.GetPlace(ctx, ref.ProfilePlace)
		if err != nil {
			return nil, err
		}
		if place == nil {
			return nil, ErrPlaceNotFound
		}
		lat, latOK := place["latitude"].(float64)
		lon, lonOK := place["longitude"].(float64)
		if latOK && lonOK {
			label, _ := place["label"].(string)
			return []model.PlaceCandidate{{ID: ref.ProfilePlace, Name: label, Label: label, Type: "point_of_interest", Latitude: lat, Longitude: lon, Confidence: 1}}, nil
		}
		if address, ok := place["address"].(string); ok && strings.TrimSpace(address) != "" {
			return app.ResolveRef(ctx, model.PlaceRef{Query: address}, limit)
		}
		return nil, ErrPlaceNotFound
	}
	return []model.PlaceCandidate{{ID: "coord", Name: "Point", Label: fmt.Sprintf("%g,%g", *ref.Latitude, *ref.Longitude), Type: "point_of_interest", Latitude: *ref.Latitude, Longitude: *ref.Longitude, Confidence: 1}}, nil
}

func placeSimilarity(query, name string) float64 {
	left, right := strings.Fields(model.Normalize(query)), strings.Fields(model.Normalize(name))
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	shared := 0
	for _, word := range left {
		if contains(right, word) {
			shared++
		}
	}
	if shared == len(left) || shared == len(right) {
		return 1
	}
	return float64(2*shared) / float64(len(left)+len(right))
}

func (app *App) searchPlaces(ctx context.Context, query string, near *model.Point, types []string, limit int) ([]model.PlaceCandidate, []model.WarningItem) {
	candidates := []model.PlaceCandidate{}
	warnings := []model.WarningItem{}
	addWarning := func(source string) {
		warnings = append(warnings, model.WarningItem{Code: "SOURCE_UNAVAILABLE", Source: model.Ptr(source), Message: "Place source unavailable.", Retryable: true})
	}
	local, err := app.Store.SearchEntities(ctx, query, limit*2)
	if err != nil {
		addWarning("entities")
	} else {
		candidates = append(candidates, local...)
	}
	stops, err := app.Store.SearchStops(ctx, query, limit*2)
	if err != nil {
		addWarning("gtfs")
	} else {
		candidates = append(candidates, stops...)
	}
	if len(candidates) < limit && app.Provider != nil {
		remote, err := app.Provider.Search(ctx, query, near, types, limit)
		if err != nil {
			addWarning("photon")
		} else {
			candidates = append(candidates, remote...)
		}
	}
	unique := []model.PlaceCandidate{}
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.ID] || (len(types) > 0 && !contains(types, c.Type)) {
			continue
		}
		seen[c.ID] = true
		c.Confidence = math.Max(c.Confidence, placeSimilarity(query, c.Name))
		if near != nil {
			c.DistanceM = model.Ptr(model.Distance(*near, model.Point{Latitude: c.Latitude, Longitude: c.Longitude}))
		}
		unique = append(unique, c)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		a, b := unique[i], unique[j]
		aExact, bExact := model.Normalize(a.Name) == model.Normalize(query), model.Normalize(b.Name) == model.Normalize(query)
		if aExact != bExact {
			return aExact
		}
		if a.Confidence != b.Confidence {
			return a.Confidence > b.Confidence
		}
		if near != nil {
			return model.Value(a.DistanceM) < model.Value(b.DistanceM)
		}
		return false
	})
	if len(unique) > limit {
		unique = unique[:limit]
	}
	return unique, warnings
}

func (app *App) ResolvePlace(ctx context.Context, input model.ResolvePlaceInput) model.Envelope {
	if strings.TrimSpace(input.Query) == "" {
		return model.NewEnvelope("invalid_request", "A place query is required.", map[string]any{"candidates": []model.PlaceCandidate{}})
	}
	if input.Limit <= 0 {
		input.Limit = 5
	}
	var near *model.Point
	var nearErr error
	if input.Near != nil {
		near, nearErr = app.ResolvePoint(ctx, *input.Near)
	}
	candidates, warnings := app.searchPlaces(ctx, input.Query, near, input.Types, input.Limit)
	status := "ok"
	if len(candidates) == 0 {
		status = "not_found"
	} else if len(candidates) > 1 && model.Normalize(candidates[0].Name) != model.Normalize(input.Query) && math.Abs(candidates[0].Confidence-candidates[1].Confidence) < 0.05 {
		status = "ambiguous"
	}
	envelope := model.NewEnvelope(status, fmt.Sprintf("%d place candidate(s).", len(candidates)), map[string]any{"candidates": candidates})
	envelope.Warnings = warnings
	envelope.Degraded = len(warnings) > 0
	if status == "ambiguous" {
		envelope.Warn("AMBIGUOUS_PLACE", "places", "Several places match this query.", false)
	}
	if nearErr != nil {
		envelope.Warn("PLACE_NOT_FOUND", "places", "The nearby reference could not be resolved.", false)
	}
	return envelope
}
