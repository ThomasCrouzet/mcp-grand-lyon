package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func TestEntityIndexHandlesLiteralFTSInputAndRadius(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	entity := model.PlaceCandidate{
		ID:         "lyon:test:1",
		Name:       "Toilettes Test",
		Label:      "Toilettes Test",
		Type:       "toilet",
		Latitude:   45.75,
		Longitude:  4.85,
		Confidence: 0.9,
	}
	if err := store.UpsertEntity(ctx, entity, "fixture", "1", nil, []string{"wc test"}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetEntity(ctx, entity.ID)
	if err != nil || got == nil || got.Name != entity.Name {
		t.Fatalf("GetEntity = %#v, %v", got, err)
	}
	results, err := store.SearchEntities(ctx, `Toilettes" OR x`, 5)
	if err != nil {
		t.Fatalf("literal FTS query failed: %v", err)
	}
	if len(results) != 1 || results[0].ID != entity.ID {
		t.Fatalf("SearchEntities = %#v", results)
	}
	near, err := store.NearEntities(ctx, model.Point{Latitude: 45.75, Longitude: 4.85}, 500, []string{"toilet"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(near) != 1 || near[0].DistanceM == nil || *near[0].DistanceM > 1 {
		t.Fatalf("NearEntities = %#v", near)
	}
}
