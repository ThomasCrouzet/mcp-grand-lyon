package service

import (
	"reflect"
	"testing"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func TestNormalizeParkingPreservesZeroAndUnknownAvailability(t *testing.T) {
	zero := 0
	capacity := 800
	options, warning := normalizeParkingOptions(
		model.Point{Latitude: 45.7675, Longitude: 4.8355},
		[]model.ParkingOption{
			{ID: "full", Type: "public_parking", Latitude: 45.7675, Longitude: 4.8355, AvailableSpaces: &zero, Status: "open", Realtime: true},
			{ID: "capacity", Type: "public_parking", Latitude: 45.7676, Longitude: 4.8355, Capacity: &capacity, Status: "open", Realtime: true},
		},
		[]string{"public_parking"}, 100, 0, 10,
	)

	if len(options) != 2 {
		t.Fatalf("got %d options, want 2", len(options))
	}
	if options[0].AvailableSpaces == nil || *options[0].AvailableSpaces != 0 || options[0].Status != "full" {
		t.Fatalf("zero availability was not preserved: %#v", options[0])
	}
	if options[1].AvailableSpaces != nil || options[1].Status != "unknown" || options[1].Realtime {
		t.Fatalf("capacity-only parking claims availability: %#v", options[1])
	}
	if warning != parkingAvailabilityPartial {
		t.Fatalf("warning = %q, want %q", warning, parkingAvailabilityPartial)
	}
}

func TestNormalizeParkingEnforcesDistanceTypeAndMinimum(t *testing.T) {
	one := 1
	zero := 0
	options, _ := normalizeParkingOptions(
		model.Point{Latitude: 45.7675, Longitude: 4.8355},
		[]model.ParkingOption{
			{ID: "valid", Type: "park_and_ride", Latitude: 45.7675, Longitude: 4.8355, AvailableSpaces: &one},
			{ID: "full", Type: "park_and_ride", Latitude: 45.7675, Longitude: 4.8355, AvailableSpaces: &zero},
			{ID: "wrong-type", Type: "public_parking", Latitude: 45.7675, Longitude: 4.8355, AvailableSpaces: &one},
			{ID: "far", Type: "park_and_ride", Latitude: 45.80, Longitude: 4.90, AvailableSpaces: &one},
		},
		[]string{"park_and_ride"}, 100, 1, 10,
	)

	if len(options) != 1 || options[0].ID != "valid" {
		t.Fatalf("constraint enforcement returned %#v", options)
	}
}

func TestFilterRoadEventsUsesEveryResolvedArea(t *testing.T) {
	nearFirstLat, nearFirstLon := 45.75, 4.85
	nearSecondLat, nearSecondLon := 45.80, 4.90
	unknownLat := 45.77
	events := []model.RoadEvent{
		{ID: "first", Latitude: &nearFirstLat, Longitude: &nearFirstLon},
		{ID: "second", Latitude: &nearSecondLat, Longitude: &nearSecondLon},
		{ID: "unknown", Latitude: &unknownLat},
	}
	areas := []model.Point{
		{Latitude: 45.75, Longitude: 4.85},
		{Latitude: 45.80, Longitude: 4.90},
	}

	got, omitted := filterRoadEvents(events, areas, 1500)
	ids := []string{}
	for _, event := range got {
		ids = append(ids, event.ID)
	}
	if !reflect.DeepEqual(ids, []string{"first", "second"}) {
		t.Fatalf("filtered IDs = %#v", ids)
	}
	if omitted != 1 {
		t.Fatalf("omitted = %d, want 1", omitted)
	}
}

func TestCompareBriefingFactsReadsNestedSnapshot(t *testing.T) {
	previous := map[string]any{
		"facts": map[string]any{
			"mobility": map[string]any{"alerts": []any{map[string]any{"id": "old"}}},
		},
	}
	current := map[string]any{
		"mobility": map[string]any{"alerts": []any{map[string]any{"id": "new"}}},
	}

	changes := compareBriefingFacts(previous, current)
	if len(changes) != 1 || changes[0].Key != "mobility" {
		t.Fatalf("changes = %#v", changes)
	}
	if equal := compareBriefingFacts(map[string]any{"facts": current}, current); len(equal) != 0 {
		t.Fatalf("equal facts produced changes: %#v", equal)
	}
}

func TestScoreStationRequiresReliableHistory(t *testing.T) {
	withoutHistory := scoreStation(3, 3, 12, 100, 500, 0, 60, 1, 4)
	withHistory := scoreStation(3, 3, 12, 100, 500, 0, 60, 1, 5)
	if withoutHistory.HistoricalReliabilityScore != 0 {
		t.Fatalf("insufficient history influenced score: %#v", withoutHistory)
	}
	if withHistory.HistoricalReliabilityScore != 1 || withHistory.Score <= withoutHistory.Score {
		t.Fatalf("reliable history did not influence score: without=%#v with=%#v", withoutHistory, withHistory)
	}
}
