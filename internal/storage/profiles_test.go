package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func TestProfilesSnapshotsAndVelovHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	profiles := []byte(`
version: 1
places:
  home:
    label: Home
    latitude: 45.75
    longitude: 4.85
commutes:
  daily:
    origin: home
    destination: work
briefings:
  morning:
    commute: daily
`)
	if err := store.LoadProfiles(ctx, profiles); err != nil {
		t.Fatal(err)
	}
	place, err := store.GetPlace(ctx, "home")
	if err != nil || place["label"] != "Home" {
		t.Fatalf("GetPlace = %#v, %v", place, err)
	}
	commute, err := store.GetCommute(ctx, "daily")
	if err != nil || commute["origin"] != "home" {
		t.Fatalf("GetCommute = %#v, %v", commute, err)
	}
	briefing, err := store.GetBriefing(ctx, "morning")
	if err != nil || briefing["commute"] != "daily" {
		t.Fatalf("GetBriefing = %#v, %v", briefing, err)
	}
	if err := store.SaveSnapshot(ctx, "morning", map[string]any{"sequence": 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, "morning", map[string]any{"sequence": 2}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LastSnapshot(ctx, "morning")
	if err != nil || snapshot["sequence"] != float64(2) {
		t.Fatalf("LastSnapshot = %#v, %v", snapshot, err)
	}

	observed := time.Date(2026, 10, 23, 8, 0, 0, 0, model.Paris)
	for _, bikes := range []int{0, 2, 3, 0, 4} {
		station := model.VelovStation{
			ID: "v1", Name: "Test", Latitude: 45.75, Longitude: 4.85,
			BikesAvailable: bikes, DocksAvailable: 5, Capacity: 10,
			ObservedAt: &observed,
		}
		if err := store.RecordVelov(ctx, station); err != nil {
			t.Fatal(err)
		}
	}
	score, err := store.Reliability(ctx, "v1", "08", 2)
	if err != nil {
		t.Fatal(err)
	}
	if score.SampleCount != 5 || score.ProbabilityAvailable != 0.6 || score.Confidence != "medium" {
		t.Fatalf("Reliability = %#v", score)
	}
}
