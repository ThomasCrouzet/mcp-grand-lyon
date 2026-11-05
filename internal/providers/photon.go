package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

// ParsePhoton converts decoded Photon GeoJSON data to place candidates.
func ParsePhoton(payload any) ([]model.PlaceCandidate, error) {
	if !providerJSONContainer(payload) {
		return nil, errors.New("Photon root must be an object or array")
	}
	var features []any
	switch typed := payload.(type) {
	case map[string]any:
		if raw, ok := providerJSONFirst(typed, "features").([]any); ok {
			features = raw
		}
	case []any:
		features = typed
	}
	result := []model.PlaceCandidate{}
	for _, rawFeature := range features {
		feature, ok := rawFeature.(map[string]any)
		if !ok {
			continue
		}
		properties, _ := providerJSONFirst(feature, "properties").(map[string]any)
		geometry, _ := providerJSONFirst(feature, "geometry").(map[string]any)
		coordinates, _ := providerJSONFirst(geometry, "coordinates").([]any)
		if len(coordinates) < 2 {
			continue
		}
		longitude, longitudeOK := providerJSONFloat(coordinates[0])
		latitude, latitudeOK := providerJSONFloat(coordinates[1])
		if !latitudeOK || !longitudeOK || math.IsNaN(latitude) || math.IsNaN(longitude) {
			continue
		}
		name := providerJSONText(providerJSONFirst(properties, "name", "street", "city"))
		if name == "" {
			name = "unknown"
		}
		city := providerJSONText(providerJSONFirst(properties, "city", "locality"))
		postcode := providerJSONText(providerJSONFirst(properties, "postcode"))
		label := name
		locality := strings.TrimSpace(strings.TrimSpace(postcode) + " " + strings.TrimSpace(city))
		if locality != "" {
			label += ", " + locality
		}
		identifier := providerJSONText(providerJSONFirst(properties, "osm_id", "osm_value"))
		objectType := providerJSONText(providerJSONFirst(properties, "osm_type"))
		if identifier == "" {
			identifier = providerJSONText(providerJSONFirst(properties, "name"))
			if identifier == "" {
				identifier = "unknown"
			}
			// Without an upstream object ID, coordinates and type keep distinct
			// objects with the same display name from sharing one logical ID.
			digest := sha256.Sum256([]byte(identifier + "|" + objectType + "|" + strconv.FormatFloat(latitude, 'f', 6, 64) + "|" + strconv.FormatFloat(longitude, 'f', 6, 64)))
			identifier = hex.EncodeToString(digest[:8])
		} else if objectType != "" {
			identifier = objectType + ":" + identifier
		}
		confidence := 0.7
		if value, ok := providerJSONFloat(providerJSONFirst(properties, "confidence")); ok {
			confidence = value
			if confidence > 1 {
				confidence = math.Min(1, confidence/100)
			}
		}
		result = append(result, model.PlaceCandidate{
			ID: "photon:" + identifier, Name: name, Label: label, Type: "point_of_interest",
			Latitude: latitude, Longitude: longitude, Confidence: confidence,
		})
	}
	return result, nil
}

func providerJSONFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		result, err := typed.Float64()
		return result, err == nil
	case string:
		result, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return result, err == nil
	default:
		return 0, false
	}
}
