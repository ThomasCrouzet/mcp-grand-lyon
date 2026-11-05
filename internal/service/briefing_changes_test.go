package service

import (
	"fmt"
	"testing"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func TestCompareBriefingFactsUsesStableIdentities(t *testing.T) {
	t.Parallel()
	previous := map[string]any{"facts": map[string]any{
		"departures": map[string]any{"departures": []any{
			map[string]any{"line_name": "A", "destination": "Vaulx", "expected_at": "2026-07-20T08:05:00+02:00", "cancelled": false},
			map[string]any{"line_name": "C3", "destination": "Part-Dieu", "expected_at": "2026-07-20T08:10:00+02:00", "cancelled": false},
		}},
		"journey": map[string]any{"options": []any{
			map[string]any{"mode": "park_and_ride", "availability": "5 spaces", "eligible": true},
		}},
	}}
	current := map[string]any{
		"departures": map[string]any{"departures": []any{
			map[string]any{"line_name": "C3", "destination": "Part-Dieu", "expected_at": "2026-07-20T08:10:00+02:00", "cancelled": false},
			map[string]any{"line_name": "A", "destination": "Vaulx", "expected_at": "2026-07-20T08:05:00+02:00", "cancelled": true},
		}},
		"journey": map[string]any{"options": []any{
			map[string]any{"mode": "park_and_ride", "availability": "0 spaces", "eligible": false},
		}},
	}
	changes, truncated := compareBriefingFacts(previous, current)
	if truncated {
		t.Fatal("small change list was truncated")
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v", changes)
	}
	byKey := map[string]model.BriefingChange{}
	for _, change := range changes {
		byKey[change.Key] = change
	}
	cancelled := byKey["departures:A|Vaulx|2026-07-20T08:05:00+02:00"]
	if cancelled.Kind != "updated" || cancelled.Message != "Departure was cancelled." {
		t.Fatalf("cancelled change = %#v", cancelled)
	}
	parking := byKey["journey:park_and_ride"]
	if parking.Kind != "updated" || parking.Previous == nil || parking.Current == nil ||
		parking.Message != "Availability changed from 5 spaces to 0 spaces." {
		t.Fatalf("parking change = %#v", parking)
	}
}

func TestCompareBriefingFactsDistinguishesLossAndTruncation(t *testing.T) {
	t.Parallel()
	previous := map[string]any{"facts": map[string]any{
		"velov": map[string]any{"stations": []any{map[string]any{"id": "station", "availability": "5 bikes"}}},
	}}
	changes, _ := compareBriefingFacts(previous, map[string]any{})
	if len(changes) != 1 || changes[0].Kind != "evidence_lost" || changes[0].Key != "velov" {
		t.Fatalf("lost evidence = %#v", changes)
	}

	stations := []any{}
	for index := 0; index < 60; index++ {
		stations = append(stations, map[string]any{"id": fmt.Sprintf("station-%d", index), "availability": "1 bike"})
	}
	previous = map[string]any{"facts": map[string]any{"velov": map[string]any{"stations": stations}}}
	currentStations := []any{}
	for index := 0; index < 60; index++ {
		currentStations = append(currentStations, map[string]any{"id": fmt.Sprintf("station-%d", index), "availability": "0 bikes"})
	}
	changes, truncated := compareBriefingFacts(previous, map[string]any{"velov": map[string]any{"stations": currentStations}})
	if !truncated || len(changes) != 50 {
		t.Fatalf("truncation = %d changes, %v", len(changes), truncated)
	}
}
