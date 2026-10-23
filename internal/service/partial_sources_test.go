package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

type partialSourceProvider struct {
	model.Provider
	parking    []model.ParkingOption
	facilities []model.Facility
}

func (provider *partialSourceProvider) Parkings(context.Context, model.Point, []string, float64, int, int) ([]model.ParkingOption, error) {
	return provider.parking, errors.New("one parking source failed")
}

func (provider *partialSourceProvider) Facilities(context.Context, model.Point, []string, float64, int) ([]model.Facility, error) {
	return provider.facilities, errors.New("one facility source failed")
}

func TestServicesPreservePartialProviderRows(t *testing.T) {
	t.Parallel()
	available := 4
	provider := &partialSourceProvider{
		parking:    []model.ParkingOption{{ID: "parking", Name: "Parking", Type: "park_and_ride", Latitude: 45.76, Longitude: 4.85, AvailableSpaces: &available, Status: "open", Realtime: true}},
		facilities: []model.Facility{{ID: "toilet", Name: "Toilet", Category: "toilet", Latitude: 45.76, Longitude: 4.85, OpenStatus: "unknown"}},
	}
	app := serviceTestApp(t, provider)
	location := coordinateRef(45.76, 4.85)

	parking := app.ParkingOptions(context.Background(), model.ParkingOptionsInput{Destination: location, Types: []string{"park_and_ride"}, RadiusM: 500, Limit: 5})
	parkingItems := parking.Data["options"].([]model.ParkingOption)
	if len(parkingItems) != 1 || !hasWarning(parking, "PARTIAL_RESULT", "parking") {
		t.Fatalf("parking = %#v", parking)
	}

	facilities := app.NearbyFacilities(context.Background(), model.NearbyFacilitiesInput{Location: location, Categories: []string{"toilet"}, RadiusM: 500, LimitPerCategory: 5})
	facilityItems := facilities.Data["items"].([]model.Facility)
	if len(facilityItems) != 1 || !hasWarning(facilities, "PARTIAL_RESULT", "facilities") {
		t.Fatalf("facilities = %#v", facilities)
	}

	journey := app.TripOptions(context.Background(), model.TripOptionsInput{Origin: coordinateRef(45.75, 4.84), Destination: location, Modes: []string{"park_and_ride"}, Preferences: &model.TripPreferences{}})
	journeyItems := journey.Data["options"].([]model.TripOption)
	if len(journeyItems) != 1 || !hasWarning(journey, "PARTIAL_RESULT", "parking") {
		t.Fatalf("journey = %#v", journey)
	}
}

func hasWarning(envelope model.Envelope, code, source string) bool {
	for _, warning := range envelope.Warnings {
		if warning.Code == code && warning.Source != nil && *warning.Source == source {
			return true
		}
	}
	return false
}
