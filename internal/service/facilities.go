package service

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

var (
	supportedFacilityCategories = map[string]bool{
		"toilet": true, "drinking_water": true, "park": true, "garden": true,
		"bike_pump": true, "velov_station": true, "waste_facility": true,
	}
	simpleOpeningHours = regexp.MustCompile(`^\s*([01]?\d|2[0-3]):([0-5]\d)\s*-\s*([01]?\d|2[0-3]):([0-5]\d)\s*$`)
)

func (app *App) NearbyFacilities(ctx context.Context, input model.NearbyFacilitiesInput) model.Envelope {
	if err := input.Location.Validate(); err != nil {
		return model.NewEnvelope("invalid_request", "Invalid location: "+err.Error(), facilityData(nil, nil, input.OpenAt))
	}
	if len(input.Categories) == 0 {
		return model.NewEnvelope("invalid_request", "At least one facility category is required.", facilityData(nil, nil, input.OpenAt))
	}
	for _, category := range input.Categories {
		if !supportedFacilityCategories[category] {
			return model.NewEnvelope("invalid_request", "Unsupported facility category: "+category, facilityData(nil, nil, input.OpenAt))
		}
	}
	radius := input.RadiusM
	if radius == 0 {
		radius = 1000
	}
	limit := input.LimitPerCategory
	if limit == 0 {
		limit = 5
	}
	if radius < 50 || radius > 5000 || limit < 1 || limit > 20 {
		return model.NewEnvelope("invalid_request", "Facility radius or limit is outside the supported range.", facilityData(nil, nil, input.OpenAt))
	}
	point, err := app.ResolvePoint(ctx, input.Location)
	if err != nil || point == nil {
		return model.NewEnvelope("not_found", "The facility location could not be resolved.", facilityData(nil, nil, input.OpenAt))
	}
	if app.Provider == nil {
		envelope := model.NewEnvelope("unavailable", "The facility provider is unavailable.", facilityData(nil, nil, input.OpenAt))
		envelope.Warn("SOURCE_UNAVAILABLE", "facilities", "The facility provider is unavailable.", true)
		envelope.Status = "unavailable"
		return envelope
	}
	facilities, err := app.Provider.Facilities(ctx, *point, input.Categories, float64(radius), limit)
	if err != nil && len(facilities) == 0 {
		envelope := model.NewEnvelope("unavailable", "Facilities are unavailable.", facilityData(nil, nil, input.OpenAt))
		envelope.Warn("SOURCE_UNAVAILABLE", "facilities", "The facility source failed.", true)
		envelope.Status = "unavailable"
		return envelope
	}

	allowed := make(map[string]bool, len(input.Categories))
	for _, category := range input.Categories {
		allowed[category] = true
	}
	counts := map[string]int{}
	filtered := make([]model.Facility, 0, len(facilities))
	unknownHours := 0
	for _, facility := range facilities {
		if !allowed[facility.Category] || counts[facility.Category] >= limit {
			continue
		}
		distance := model.Distance(*point, model.Point{Latitude: facility.Latitude, Longitude: facility.Longitude})
		if distance > float64(radius) {
			continue
		}
		facility.DistanceM = model.Ptr(distance)
		if input.OpenAt != nil {
			status := openingStatusAt(facility.OpeningHoursRaw, *input.OpenAt)
			if status == "unknown" {
				unknownHours++
				continue
			}
			if status == "closed" {
				continue
			}
			facility.OpenStatus = "open"
		}
		counts[facility.Category]++
		filtered = append(filtered, facility)
	}
	envelope := model.NewEnvelope("ok", fmt.Sprintf("Found %d facilities.", len(filtered)), facilityData(filtered, input.Categories, input.OpenAt))
	if err != nil {
		envelope.Warn("PARTIAL_RESULT", "facilities", "Some requested facility sources are unavailable.", true)
	}
	if unknownHours != 0 {
		envelope.Warn("PARTIAL_RESULT", "facility_opening_hours", fmt.Sprintf("Omitted %d facilities with unverified opening hours.", unknownHours), false)
	}
	return envelope
}

func facilityData(items []model.Facility, categories []string, openAt *time.Time) map[string]any {
	if items == nil {
		items = []model.Facility{}
	}
	grouped := map[string][]model.Facility{}
	for _, category := range categories {
		grouped[category] = []model.Facility{}
	}
	for _, item := range items {
		grouped[item.Category] = append(grouped[item.Category], item)
	}
	return map[string]any{"facilities": grouped, "items": items, "open_at": openAt}
}

func openingStatusAt(raw *string, at time.Time) string {
	if raw == nil {
		return "unknown"
	}
	parts := simpleOpeningHours.FindStringSubmatch(*raw)
	if len(parts) != 5 {
		return "unknown"
	}
	numbers := make([]int, 4)
	for index := range numbers {
		numbers[index], _ = strconv.Atoi(parts[index+1])
	}
	currentTime := at.In(model.Paris)
	current := currentTime.Hour()*60 + currentTime.Minute()
	start := numbers[0]*60 + numbers[1]
	end := numbers[2]*60 + numbers[3]
	open := start <= current && current < end
	if start > end {
		open = current >= start || current < end
	}
	if open {
		return "open"
	}
	return "closed"
}
