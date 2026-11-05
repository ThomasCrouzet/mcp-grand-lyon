package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

type freshnessProvider struct {
	model.Provider
	stations   []model.VelovStation
	departures []model.Departure
	parkings   []model.ParkingOption
}

func (provider *freshnessProvider) Stations(context.Context, model.Point, float64, int) ([]model.VelovStation, error) {
	return provider.stations, nil
}

func (provider *freshnessProvider) Departures(context.Context, string, string, string, int) ([]model.Departure, error) {
	return provider.departures, nil
}

func (provider *freshnessProvider) Parkings(context.Context, model.Point, []string, float64, int, int) ([]model.ParkingOption, error) {
	return provider.parkings, nil
}

func (provider *freshnessProvider) Plan(context.Context, model.Point, model.Point, time.Time, []string, model.TripPreferences) ([]model.TripOption, error) {
	return nil, errors.New("routing unavailable")
}

func TestObservationFreshnessKeepsUnknownAndStaleDistinct(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, model.Paris)
	retrieved := now.Add(-time.Minute)
	fresh := now.Add(-10 * time.Second)
	boundary := now.Add(-300 * time.Second)
	expired := now.Add(-301 * time.Second)

	age, stale, missing := observationFreshness(&fresh, &retrieved, now, 300)
	if missing || stale || age == nil || *age != 10 {
		t.Fatalf("fresh observation = age %v stale %v missing %v", age, stale, missing)
	}
	age, stale, missing = observationFreshness(&boundary, &retrieved, now, 300)
	if missing || stale || age == nil || *age != 300 {
		t.Fatalf("boundary observation = age %v stale %v missing %v", age, stale, missing)
	}
	age, stale, missing = observationFreshness(&expired, &retrieved, now, 300)
	if missing || !stale || age == nil || *age != 301 {
		t.Fatalf("expired observation = age %v stale %v missing %v", age, stale, missing)
	}
	age, stale, missing = observationFreshness(nil, &retrieved, now, 300)
	if !missing || stale || age != nil {
		t.Fatalf("missing observation = age %v stale %v missing %v", age, stale, missing)
	}
	if !retrievalTime(&retrieved, now).Equal(retrieved) || !retrievalTime(nil, now).Equal(now) {
		t.Fatal("retrieval time fell back incorrectly")
	}
}

func TestClosedVelovStationIsNeverSelected(t *testing.T) {
	t.Parallel()
	observed := model.Now().Add(-10 * time.Second)
	retrieved := observed
	provider := &freshnessProvider{stations: []model.VelovStation{
		{ID: "closed", Name: "Closed", Latitude: 45.7600, Longitude: 4.8600, BikesAvailable: 20, DocksAvailable: 20, Status: "closed", ObservedAt: &observed, RetrievedAt: &retrieved},
		{ID: "open", Name: "Open", Latitude: 45.7601, Longitude: 4.8600, BikesAvailable: 10, DocksAvailable: 10, Status: "open", ObservedAt: &observed, RetrievedAt: &retrieved},
		{ID: "dest", Name: "Destination", Latitude: 45.7580, Longitude: 4.8330, BikesAvailable: 10, DocksAvailable: 10, Status: "open", ObservedAt: &observed, RetrievedAt: &retrieved},
	}}
	app := serviceTestApp(t, provider)
	envelope := app.TripOptions(context.Background(), model.TripOptionsInput{
		Origin: coordinateRef(45.7600, 4.8600), Destination: coordinateRef(45.7580, 4.8330),
		Modes: []string{"velov"}, Preferences: &model.TripPreferences{MaxWalkingM: 5000, MinimumVelovBikes: 1, MinimumVelovDocks: 1},
	})
	options := envelope.Data["options"].([]model.TripOption)
	if len(options) != 1 || options[0].Mode != "velov" {
		t.Fatalf("options = %#v warnings=%#v", options, envelope.Warnings)
	}
	if options[0].Summary != "Velov Open to Destination" {
		t.Fatalf("closed station was selected: %#v", options[0])
	}
}

func TestStaleOrUnobservedParkingCannotSupportRecommendation(t *testing.T) {
	t.Parallel()
	now := model.Now()
	expired := now.Add(-time.Hour)
	retrieved := now
	available := 10
	for name, observed := range map[string]*time.Time{
		"stale":   &expired,
		"missing": nil,
	} {
		t.Run(name, func(t *testing.T) {
			provider := &freshnessProvider{parkings: []model.ParkingOption{{
				ID: "pr", Name: "P+R", Type: "park_and_ride", Latitude: 45.7600, Longitude: 4.8500,
				AvailableSpaces: &available, Status: "open", Realtime: true,
				ObservedAt: observed, RetrievedAt: &retrieved,
			}}}
			app := serviceTestApp(t, provider)
			parking := app.ParkingOptions(context.Background(), model.ParkingOptionsInput{
				Destination: coordinateRef(45.7600, 4.8500), Types: []string{"park_and_ride"}, RadiusM: 500,
			})
			items := parking.Data["options"].([]model.ParkingOption)
			if len(items) != 1 {
				t.Fatalf("parking = %#v", parking)
			}
			if name == "stale" && !items[0].Stale {
				t.Fatalf("stale parking was not marked: %#v", items[0])
			}
			trip := app.TripOptions(context.Background(), model.TripOptionsInput{
				Origin: coordinateRef(45.7500, 4.8400), Destination: coordinateRef(45.7600, 4.8500),
				Modes: []string{"park_and_ride"}, Preferences: &model.TripPreferences{MaxWalkingM: 5000},
			})
			tripOptions := trip.Data["options"].([]model.TripOption)
			if len(tripOptions) != 1 || tripOptions[0].Eligible || trip.Data["recommended_mode"] != nil {
				t.Fatalf("stale/unobserved parking was recommended: %#v", trip)
			}
		})
	}
}

func TestDepartureProvenanceUsesObservationTimeWithoutResetting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := model.Now()
	observed := now.Add(-100 * time.Second)
	expected := now.Add(5 * time.Minute)
	provider := &freshnessProvider{departures: []model.Departure{{
		LineID: "tcl:A", LineName: "A", Destination: "Vaulx", StopRef: model.Ptr("X"),
		ScheduledAt: &expected, ExpectedAt: &expected, ObservedAt: &observed, Realtime: true,
	}}}
	app := serviceTestApp(t, provider)
	if err := app.Store.SaveResolvedPlace(ctx, model.PlaceCandidate{
		ID: "gtfs:stop:X", Name: "Stop X", Type: "transport_stop", Latitude: 45.76, Longitude: 4.85,
	}, "test", "X", nil); err != nil {
		t.Fatal(err)
	}
	first := app.NextDepartures(ctx, model.NextDeparturesInput{Stop: model.PlaceRef{PlaceID: "gtfs:stop:X"}, Line: "A", Limit: 3})
	if len(first.Sources) != 1 || first.Sources[0].ObservedAt == nil || !first.Sources[0].ObservedAt.Equal(observed) {
		t.Fatalf("departure provenance = %#v", first.Sources)
	}
	if first.Sources[0].AgeSeconds == nil || *first.Sources[0].AgeSeconds < 99 {
		t.Fatalf("departure age = %#v", first.Sources[0].AgeSeconds)
	}
	time.Sleep(2 * time.Millisecond)
	second := app.NextDepartures(ctx, model.NextDeparturesInput{Stop: model.PlaceRef{PlaceID: "gtfs:stop:X"}, Line: "A", Limit: 3})
	if second.Sources[0].ObservedAt == nil || !second.Sources[0].ObservedAt.Equal(observed) {
		t.Fatalf("second call reset observation time: %#v", second.Sources[0])
	}
	if second.Sources[0].AgeSeconds == nil || *second.Sources[0].AgeSeconds < *first.Sources[0].AgeSeconds {
		t.Fatalf("second call reset age: first=%v second=%v", first.Sources[0].AgeSeconds, second.Sources[0].AgeSeconds)
	}
}
