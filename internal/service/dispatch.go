package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/assets"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	resolved    *jsonschema.Resolved
}

var definitions = sync.OnceValues(func() (map[string]ToolDefinition, error) {
	data, err := assets.Files.ReadFile("schemas.json")
	if err != nil {
		return nil, err
	}
	tools := map[string]ToolDefinition{}
	if err = json.Unmarshal(data, &tools); err != nil {
		return nil, err
	}
	for name, tool := range tools {
		var schema jsonschema.Schema
		if err = json.Unmarshal(tool.InputSchema, &schema); err != nil {
			return nil, err
		}
		tool.resolved, err = schema.Resolve(nil)
		if err != nil {
			return nil, err
		}
		tools[name] = tool
	}
	return tools, nil
})

func Tools() ([]ToolDefinition, error) {
	definitions, err := definitions()
	if err != nil {
		return nil, err
	}
	result := make([]ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, definition)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func validatePlaces(value reflect.Value) error {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return validatePlaces(value.Elem())
	}
	if value.Type() == reflect.TypeFor[model.PlaceRef]() {
		return value.Interface().(model.PlaceRef).Validate()
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if value.Type().Field(i).IsExported() {
				if err := validatePlaces(value.Field(i)); err != nil {
					return err
				}
			}
		}
	case reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			if err := validatePlaces(value.Index(i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeInput[T any](raw []byte) (T, error) {
	var input T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, errors.New("invalid input types or timestamp; use RFC3339 with a UTC offset")
	}
	return input, validatePlaces(reflect.ValueOf(input))
}

func invoke[T any](ctx context.Context, raw []byte, handler func(context.Context, T) model.Envelope) model.Envelope {
	input, err := decodeInput[T](raw)
	if err != nil {
		return model.NewEnvelope("invalid_request", err.Error(), nil)
	}
	return handler(ctx, input)
}

func applyTripPreferenceDefaults(values map[string]any) {
	preferences, ok := values["preferences"].(map[string]any)
	if !ok {
		return
	}
	defaults := map[string]any{
		"max_walking_m":       800,
		"minimum_velov_bikes": 3,
		"minimum_velov_docks": 3,
		"avoid_disruptions":   true,
		"wheelchair":          false,
	}
	for name, value := range defaults {
		if _, exists := preferences[name]; !exists {
			preferences[name] = value
		}
	}
}

func (app *App) Call(ctx context.Context, name string, raw json.RawMessage) (result model.Envelope) {
	defer func() {
		if app.Config.Offline && result.Status != "invalid_request" {
			markOfflineFixture(&result)
		}
	}()
	tools, err := definitions()
	if err != nil {
		return model.NewEnvelope("unavailable", "Tool schemas are unavailable.", nil)
	}
	tool, exists := tools[name]
	if !exists {
		return model.NewEnvelope("invalid_request", "Unknown tool.", nil)
	}
	if len(raw) > 128*1024 {
		return model.NewEnvelope("invalid_request", "Request arguments exceed the size limit.", nil)
	}
	var values map[string]any
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err = json.Unmarshal(raw, &values); err != nil || values == nil {
		return model.NewEnvelope("invalid_request", "Request arguments must be a JSON object.", nil)
	}
	if err = tool.resolved.Validate(values); err != nil {
		return model.NewEnvelope("invalid_request", "Arguments do not match the tool schema.", nil)
	}
	if err = tool.resolved.ApplyDefaults(&values); err != nil {
		return model.NewEnvelope("invalid_request", "Cannot apply tool defaults.", nil)
	}
	if name == "lyon_trip_options" {
		applyTripPreferenceDefaults(values)
	}
	raw, err = json.Marshal(values)
	if err != nil {
		return model.NewEnvelope("invalid_request", "Invalid JSON arguments.", nil)
	}
	switch name {
	case "lyon_resolve_place":
		return invoke(ctx, raw, app.ResolvePlace)
	case "lyon_next_departures":
		return invoke(ctx, raw, app.NextDepartures)
	case "lyon_mobility_status":
		return invoke(ctx, raw, app.MobilityStatus)
	case "lyon_trip_options":
		return invoke(ctx, raw, app.TripOptions)
	case "lyon_parking_options":
		return invoke(ctx, raw, app.ParkingOptions)
	case "lyon_accessibility_check":
		return invoke(ctx, raw, app.AccessibilityCheck)
	case "lyon_nearby_facilities":
		return invoke(ctx, raw, app.NearbyFacilities)
	case "lyon_environment_brief":
		return invoke(ctx, raw, app.EnvironmentBrief)
	case "lyon_waste_dropoff":
		return invoke(ctx, raw, app.WasteDropoff)
	case "lyon_personal_briefing":
		return invoke(ctx, raw, app.PersonalBriefing)
	}
	return model.NewEnvelope("invalid_request", "Unknown tool.", nil)
}

func markOfflineFixture(result *model.Envelope) {
	// Copy response data before changing flags. Providers can share cached values.
	if data, err := json.Marshal(result.Data); err == nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var copied map[string]any
		if decoder.Decode(&copied) == nil {
			clearRealtime(copied)
			result.Data = copied
		}
	}
	for index := range result.Sources {
		source := &result.Sources[index]
		source.Realtime = false
		if source.Provider != "GTFS" {
			source.Provider = "Fixtures"
			source.Dataset = "offline_demo"
			source.ObservedAt = nil
			source.AgeSeconds = nil
		}
	}
	result.Sources = append(result.Sources, model.SourceProvenance{
		Provider: "Fixtures", SourceID: "offline_fixtures", Dataset: "offline_demo",
		Attribution: "Bundled demonstration data", License: "See ATTRIBUTIONS.md",
		RetrievedAt: model.Ptr(result.GeneratedAt),
	})
	result.Warn("OFFLINE_FIXTURE", "offline_fixtures", "Offline mode uses demonstration fixtures. These results do not describe current conditions.", false)
}

func clearRealtime(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "realtime" {
				value[key] = false
			} else {
				clearRealtime(child)
			}
		}
	case []any:
		for _, child := range value {
			clearRealtime(child)
		}
	}
}

func contains(values []string, query string) bool {
	for _, value := range values {
		if strings.EqualFold(value, query) {
			return true
		}
	}
	return false
}
