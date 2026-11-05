package providers

import "testing"

func TestParsePhotonSupportsFeatureListAndSkipsInvalidCoordinates(t *testing.T) {
	payload := []any{
		map[string]any{
			"geometry": map[string]any{"coordinates": []any{4.8618, 45.7606}},
			"properties": map[string]any{
				"name": "Gare Part-Dieu", "city": "Lyon", "postcode": "69003",
				"osm_id": float64(12345), "confidence": float64(91),
			},
		},
		map[string]any{
			"geometry":   map[string]any{"coordinates": []any{"bad", 45.0}},
			"properties": map[string]any{"name": "Invalid"},
		},
	}

	places, err := ParsePhoton(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 1 {
		t.Fatalf("places = %d, want 1", len(places))
	}
	place := places[0]
	if place.ID != "photon:12345" || place.Label != "Gare Part-Dieu, 69003 Lyon" {
		t.Fatalf("unexpected place: %#v", place)
	}
	if place.Confidence != 0.91 || place.Longitude != 4.8618 || place.Latitude != 45.7606 {
		t.Fatalf("unexpected place values: %#v", place)
	}
}

func TestParsePhotonPreservesZeroCoordinates(t *testing.T) {
	payload := map[string]any{
		"features": []any{map[string]any{
			"geometry":   map[string]any{"coordinates": []any{float64(0), float64(0)}},
			"properties": map[string]any{"city": "Null Island"},
		}},
	}

	places, err := ParsePhoton(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 1 || places[0].Latitude != 0 || places[0].Longitude != 0 {
		t.Fatalf("zero coordinates were lost: %#v", places)
	}
}

func TestParsePhotonIncludesObjectTypeInIdentity(t *testing.T) {
	feature := func(objectType string, osmID float64, latitude, longitude float64) map[string]any {
		properties := map[string]any{"name": "Twin", "confidence": float64(0.8)}
		if objectType != "" {
			properties["osm_type"] = objectType
		}
		if osmID != 0 {
			properties["osm_id"] = osmID
		}
		return map[string]any{
			"geometry":   map[string]any{"coordinates": []any{longitude, latitude}},
			"properties": properties,
		}
	}
	places, err := ParsePhoton([]any{
		feature("node", 9001, 45.75, 4.85),
		feature("way", 9001, 45.77, 4.87),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 2 || places[0].ID == places[1].ID {
		t.Fatalf("object types were not part of identity: %#v", places)
	}
	if places[0].ID != "photon:node:9001" || places[1].ID != "photon:way:9001" {
		t.Fatalf("unexpected typed IDs: %#v", places)
	}

	unnamed, err := ParsePhoton([]any{
		feature("", 0, 45.75, 4.85),
		feature("", 0, 45.77, 4.87),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(unnamed) != 2 || unnamed[0].ID == unnamed[1].ID {
		t.Fatalf("same-name objects without an upstream ID collided: %#v", unnamed)
	}
}

func TestParsePhotonRejectsInvalidRoot(t *testing.T) {
	if _, err := ParsePhoton(true); err == nil {
		t.Fatal("Photon accepted a scalar root")
	}
}
