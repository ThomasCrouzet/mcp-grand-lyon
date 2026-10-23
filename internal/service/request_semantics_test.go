package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/config"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/storage"
)

type semanticProvider struct {
	model.Provider
	search          func(context.Context, string, *model.Point, []string, int) ([]model.PlaceCandidate, error)
	incidents       func(context.Context, []string) ([]model.AccessibilityIncident, error)
	checkStop       func(context.Context, string) (string, error)
	facilities      func(context.Context, model.Point, []string, float64, int) ([]model.Facility, error)
	indicators      func(context.Context, model.Point, []string, time.Time) ([]model.IndicatorValue, error)
	wasteFacilities func(context.Context, model.Point, string, float64, int) ([]model.WasteFacility, error)
	plan            func(context.Context, model.Point, model.Point, time.Time, []string, model.TripPreferences) ([]model.TripOption, error)
}

func (provider *semanticProvider) Search(ctx context.Context, query string, near *model.Point, types []string, limit int) ([]model.PlaceCandidate, error) {
	if provider.search != nil {
		return provider.search(ctx, query, near, types, limit)
	}
	return nil, nil
}

func (provider *semanticProvider) Incidents(ctx context.Context, lines []string) ([]model.AccessibilityIncident, error) {
	if provider.incidents != nil {
		return provider.incidents(ctx, lines)
	}
	return nil, nil
}

func (provider *semanticProvider) CheckStop(ctx context.Context, stopID string) (string, error) {
	if provider.checkStop != nil {
		return provider.checkStop(ctx, stopID)
	}
	return "unknown", nil
}

func (provider *semanticProvider) Facilities(ctx context.Context, point model.Point, categories []string, radiusM float64, limit int) ([]model.Facility, error) {
	if provider.facilities != nil {
		return provider.facilities(ctx, point, categories, radiusM, limit)
	}
	return nil, nil
}

func (provider *semanticProvider) Indicators(ctx context.Context, point model.Point, indicators []string, at time.Time) ([]model.IndicatorValue, error) {
	if provider.indicators != nil {
		return provider.indicators(ctx, point, indicators, at)
	}
	return nil, nil
}

func (provider *semanticProvider) WasteFacilities(ctx context.Context, point model.Point, category string, radiusM float64, limit int) ([]model.WasteFacility, error) {
	if provider.wasteFacilities != nil {
		return provider.wasteFacilities(ctx, point, category, radiusM, limit)
	}
	return nil, nil
}

func (provider *semanticProvider) Plan(ctx context.Context, origin, destination model.Point, departure time.Time, modes []string, preferences model.TripPreferences) ([]model.TripOption, error) {
	if provider.plan != nil {
		return provider.plan(ctx, origin, destination, departure, modes, preferences)
	}
	return nil, errors.New("routing unavailable")
}

func serviceTestApp(t *testing.T, provider model.Provider) *App {
	t.Helper()
	store, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &App{
		Config: config.Config{
			TransitousEnabled: true,
			JourneyScoring: model.JourneyScoringWeights{
				Duration: 0.35, Reliability: 0.25, Disruptions: 0.15,
				Walking: 0.10, Availability: 0.10, UserPreference: 0.05,
			},
		},
		Store:    store,
		Provider: provider,
	}
}

func coordinateRef(latitude, longitude float64) model.PlaceRef {
	return model.PlaceRef{Latitude: model.Ptr(latitude), Longitude: model.Ptr(longitude)}
}

func TestTripOptionsEnforcesPublicConstraints(t *testing.T) {
	t.Parallel()
	departure := time.Date(2026, 7, 20, 8, 0, 0, 0, model.Paris)
	deadline := departure.Add(11 * time.Minute)
	duration := 10 * 60
	accessible := "accessible"
	provider := &semanticProvider{plan: func(context.Context, model.Point, model.Point, time.Time, []string, model.TripPreferences) ([]model.TripOption, error) {
		return []model.TripOption{
			{Mode: "tcl", Score: 0.9, EstimatedDurationSeconds: &duration, Accessibility: &accessible, Alerts: []model.TransitAlert{{ID: "incident", Title: "Line interrupted"}}, Summary: "disrupted"},
			{Mode: "tcl", Score: 0.7, EstimatedDurationSeconds: &duration, Accessibility: &accessible, Alerts: []model.TransitAlert{}, Summary: "verified"},
		}, nil
	}}
	app := serviceTestApp(t, provider)

	envelope := app.TripOptions(context.Background(), model.TripOptionsInput{
		Origin: coordinateRef(45.7606, 4.8618), Destination: coordinateRef(45.7578, 4.8320),
		DepartureAt: &departure, ArrivalBefore: &deadline, Modes: []string{"tcl"},
		Preferences: &model.TripPreferences{AvoidDisruptions: true, Wheelchair: true},
	})
	if envelope.Status != "ok" {
		t.Fatalf("status=%s warnings=%+v", envelope.Status, envelope.Warnings)
	}
	options := envelope.Data["options"].([]model.TripOption)
	if len(options) != 1 || options[0].Summary != "verified" || options[0].ArrivalAt == nil {
		t.Fatalf("options=%+v", options)
	}
	if options[0].ArrivalAt.After(deadline) {
		t.Fatalf("arrival=%s deadline=%s", options[0].ArrivalAt, deadline)
	}

	invalid := app.TripOptions(context.Background(), model.TripOptionsInput{
		Origin: coordinateRef(45.76, 4.86), Destination: coordinateRef(45.75, 4.83), Modes: []string{"teleport"},
	})
	if invalid.Status != "invalid_request" {
		t.Fatalf("unknown mode status=%s", invalid.Status)
	}
}

func TestTripOptionsDoesNotRecommendUnverifiedWheelchairRoute(t *testing.T) {
	t.Parallel()
	duration := 300
	provider := &semanticProvider{plan: func(context.Context, model.Point, model.Point, time.Time, []string, model.TripPreferences) ([]model.TripOption, error) {
		return []model.TripOption{{Mode: "tcl", Score: 0.8, EstimatedDurationSeconds: &duration, Summary: "unknown access"}}, nil
	}}
	app := serviceTestApp(t, provider)
	envelope := app.TripOptions(context.Background(), model.TripOptionsInput{
		Origin: coordinateRef(45.76, 4.86), Destination: coordinateRef(45.75, 4.83), Modes: []string{"tcl"},
		Preferences: &model.TripPreferences{Wheelchair: true},
	})
	if envelope.Status != "unavailable" || envelope.Data["recommended_mode"] != nil {
		t.Fatalf("envelope=%+v", envelope)
	}
}

func TestNearbyFacilitiesAppliesOpeningHoursAndErrors(t *testing.T) {
	t.Parallel()
	hours := "08:00-20:00"
	provider := &semanticProvider{facilities: func(context.Context, model.Point, []string, float64, int) ([]model.Facility, error) {
		return []model.Facility{{ID: "known", Name: "Known", Category: "toilet", Latitude: 45.777, Longitude: 4.855, OpenStatus: "unknown", OpeningHoursRaw: &hours}, {ID: "unknown", Name: "Unknown", Category: "toilet", Latitude: 45.777, Longitude: 4.855, OpenStatus: "unknown"}}, nil
	}}
	app := serviceTestApp(t, provider)
	at := time.Date(2026, 7, 20, 9, 0, 0, 0, model.Paris)
	envelope := app.NearbyFacilities(context.Background(), model.NearbyFacilitiesInput{
		Location: coordinateRef(45.777, 4.855), Categories: []string{"toilet"}, OpenAt: &at,
	})
	items := envelope.Data["items"].([]model.Facility)
	if envelope.Status != "partial" || len(items) != 1 || items[0].ID != "known" || items[0].OpenStatus != "open" {
		t.Fatalf("envelope=%+v", envelope)
	}

	provider.facilities = func(context.Context, model.Point, []string, float64, int) ([]model.Facility, error) {
		return nil, errors.New("facility source failed")
	}
	failed := app.NearbyFacilities(context.Background(), model.NearbyFacilitiesInput{
		Location: coordinateRef(45.777, 4.855), Categories: []string{"toilet"},
	})
	if failed.Status != "unavailable" || !failed.Degraded {
		t.Fatalf("failed=%+v", failed)
	}
}

func TestAccessibilityCheckPreservesUncertainty(t *testing.T) {
	t.Parallel()
	provider := &semanticProvider{
		search: func(context.Context, string, *model.Point, []string, int) ([]model.PlaceCandidate, error) {
			return []model.PlaceCandidate{{ID: "gtfs:stop:BEL1", Name: "Bellecour", Latitude: 45.7578, Longitude: 4.832}}, nil
		},
		incidents: func(context.Context, []string) ([]model.AccessibilityIncident, error) { return nil, nil },
		checkStop: func(context.Context, string) (string, error) { return "accessible", nil },
	}
	app := serviceTestApp(t, provider)
	envelope := app.AccessibilityCheck(context.Background(), model.AccessibilityCheckInput{
		Origin: &model.PlaceRef{Query: "Bellecour"}, Needs: []string{"step_free"},
	})
	if envelope.Status != "partial" || envelope.Data["overall_status"] == "accessible" || !envelope.Degraded {
		t.Fatalf("envelope=%+v", envelope)
	}
	segments := envelope.Data["segments"].([]model.AccessibilitySegment)
	if len(segments) != 1 || len(segments[0].Evidence) != 1 {
		t.Fatalf("segments=%+v", segments)
	}
	claim := segments[0].Evidence[0]
	if claim["source"] != "provider" || claim["fact"] != "check_stop_accessible_claim" || claim["confidence"] != "low" || claim["detail"] != "claim ignored without structural proof" || claim["status"] != "unknown" {
		t.Fatalf("claim evidence=%+v", claim)
	}

	invalid := app.AccessibilityCheck(context.Background(), model.AccessibilityCheckInput{Needs: []string{"hearing"}})
	if invalid.Status != "invalid_request" {
		t.Fatalf("invalid=%+v", invalid)
	}
}

func TestAccessibilityEvidenceKeepsStrongIncident(t *testing.T) {
	t.Parallel()
	provider := &semanticProvider{
		search: func(context.Context, string, *model.Point, []string, int) ([]model.PlaceCandidate, error) {
			return []model.PlaceCandidate{{ID: "BEL1", Name: "Bellecour", Latitude: 45.7578, Longitude: 4.832}}, nil
		},
		incidents: func(context.Context, []string) ([]model.AccessibilityIncident, error) {
			return []model.AccessibilityIncident{{ID: "lift", Location: "Bellecour", Status: "inaccessible", Description: "Ascenseur quai direction Perrache"}}, nil
		},
	}
	app := serviceTestApp(t, provider)
	if _, err := app.Store.DB.Exec(`INSERT INTO gtfs_stops(stop_id,stop_name,stop_lat,stop_lon,wheelchair_boarding) VALUES ('BEL1','Bellecour',45.7578,4.832,1)`); err != nil {
		t.Fatal(err)
	}
	envelope := app.AccessibilityCheck(context.Background(), model.AccessibilityCheckInput{
		Origin: &model.PlaceRef{Query: "Bellecour"}, Needs: []string{"wheelchair"},
	})
	segments := envelope.Data["segments"].([]model.AccessibilitySegment)
	if len(segments) != 1 || segments[0].Status != "inaccessible" || len(segments[0].Evidence) != 2 {
		t.Fatalf("segments=%+v", segments)
	}
	if !strings.Contains(segments[0].Notes, "incident") {
		t.Fatalf("strong incident notes were lost: %q", segments[0].Notes)
	}
	incident := segments[0].Evidence[0]
	if incident["source"] != "tcl_accessibility_alerts" || incident["fact"] != "incident" || incident["confidence"] != "high" || incident["detail"] != "Ascenseur quai direction Perrache" || incident["status"] != "inaccessible" {
		t.Fatalf("incident evidence=%+v", incident)
	}
	gtfs := segments[0].Evidence[1]
	if gtfs["source"] != "gtfs_wheelchair" || gtfs["fact"] != "wheelchair_boarding=1" || gtfs["confidence"] != "low" || gtfs["status"] != "partially_accessible" {
		t.Fatalf("GTFS evidence=%+v", gtfs)
	}
}

func TestAccessibilityProviderFailureIsNotAccessible(t *testing.T) {
	t.Parallel()
	provider := &semanticProvider{
		search: func(context.Context, string, *model.Point, []string, int) ([]model.PlaceCandidate, error) {
			return []model.PlaceCandidate{{ID: "stop", Name: "Bellecour", Latitude: 45.7578, Longitude: 4.832}}, nil
		},
		incidents: func(context.Context, []string) ([]model.AccessibilityIncident, error) {
			return nil, errors.New("incident source failed")
		},
		checkStop: func(context.Context, string) (string, error) { return "", errors.New("stop source failed") },
	}
	envelope := serviceTestApp(t, provider).AccessibilityCheck(context.Background(), model.AccessibilityCheckInput{
		Origin: &model.PlaceRef{Query: "Bellecour"}, Needs: []string{"wheelchair"},
	})
	if envelope.Status != "partial" || envelope.Data["overall_status"] == "accessible" || !envelope.Degraded {
		t.Fatalf("envelope=%+v", envelope)
	}
}

func TestEnvironmentBriefSeparatesUnsupportedAndFailedSources(t *testing.T) {
	t.Parallel()
	value := "moderate"
	provider := &semanticProvider{indicators: func(context.Context, model.Point, []string, time.Time) ([]model.IndicatorValue, error) {
		return []model.IndicatorValue{{Indicator: "pollen", Value: &value, Supported: true}, {Indicator: "heat", Supported: false}}, nil
	}}
	app := serviceTestApp(t, provider)
	envelope := app.EnvironmentBrief(context.Background(), model.EnvironmentBriefInput{
		Location: coordinateRef(45.76, 4.85), Indicators: []string{"pollen", "heat"},
	})
	values := envelope.Data["indicators"].([]model.IndicatorValue)
	if envelope.Status != "partial" || len(values) != 1 || values[0].Indicator != "pollen" {
		t.Fatalf("envelope=%+v", envelope)
	}

	provider.indicators = func(context.Context, model.Point, []string, time.Time) ([]model.IndicatorValue, error) {
		return nil, errors.New("environment source failed")
	}
	failed := app.EnvironmentBrief(context.Background(), model.EnvironmentBriefInput{
		Location: coordinateRef(45.76, 4.85), Indicators: []string{"pollen"},
	})
	if failed.Status != "unavailable" || !failed.Degraded {
		t.Fatalf("failed=%+v", failed)
	}
}

func TestWasteDropoffClassifiesAndHonorsConstraints(t *testing.T) {
	t.Parallel()
	hours := "08:00-18:00"
	provider := &semanticProvider{wasteFacilities: func(context.Context, model.Point, string, float64, int) ([]model.WasteFacility, error) {
		return []model.WasteFacility{
			{ID: "verified", Name: "Verified hours", Latitude: 45.76, Longitude: 4.85, AcceptedCategories: []string{"portable_battery"}, OpenStatus: "unknown", OpeningHoursRaw: &hours},
			{ID: "current", Name: "Current status only", Latitude: 45.76, Longitude: 4.85, AcceptedCategories: []string{"portable_battery"}, OpenStatus: "open"},
			{ID: "unknown", Name: "Unknown acceptance", Latitude: 45.76, Longitude: 4.85, OpenStatus: "open"},
		}, nil
	}}
	app := serviceTestApp(t, provider)
	withoutTime := app.WasteDropoff(context.Background(), model.WasteDropoffInput{
		Item: "pile", Location: model.Ptr(coordinateRef(45.76, 4.85)), Transport: "car",
	})
	if withoutTime.Status != "partial" || !withoutTime.Degraded {
		t.Fatalf("unknown acceptance envelope=%+v", withoutTime)
	}
	unknownAcceptance := false
	for _, item := range withoutTime.Warnings {
		unknownAcceptance = unknownAcceptance || item.Source != nil && *item.Source == "waste_acceptance"
	}
	if !unknownAcceptance {
		t.Fatalf("warnings=%+v", withoutTime.Warnings)
	}

	at := time.Date(2026, 7, 20, 12, 0, 0, 0, model.Paris)
	envelope := app.WasteDropoff(context.Background(), model.WasteDropoffInput{
		Item: "pile", Location: model.Ptr(coordinateRef(45.76, 4.85)), Transport: "car", OpenAt: &at,
	})
	classification := envelope.Data["classification"].(model.WasteClassification)
	facilities := envelope.Data["facilities"].([]model.WasteFacility)
	if envelope.Status != "partial" || classification.Category != "portable_battery" || !classification.Hazardous || len(facilities) != 1 || facilities[0].ID != "verified" {
		t.Fatalf("envelope=%+v", envelope)
	}
	closedAt := time.Date(2026, 7, 20, 20, 0, 0, 0, model.Paris)
	closed := app.WasteDropoff(context.Background(), model.WasteDropoffInput{
		Item: "pile", Location: model.Ptr(coordinateRef(45.76, 4.85)), Transport: "car", OpenAt: &closedAt,
	})
	if facilities := closed.Data["facilities"].([]model.WasteFacility); len(facilities) != 0 {
		t.Fatalf("closed facilities=%+v", facilities)
	}

	invalid := app.WasteDropoff(context.Background(), model.WasteDropoffInput{Item: "pile", Transport: "walk"})
	if invalid.Status != "invalid_request" {
		t.Fatalf("invalid=%+v", invalid)
	}

	missing := app.WasteDropoff(context.Background(), model.WasteDropoffInput{
		Item: "pile", Location: &model.PlaceRef{Query: "missing"}, Transport: "car",
	})
	if missing.Status != "not_found" {
		t.Fatalf("missing=%+v", missing)
	}
}
