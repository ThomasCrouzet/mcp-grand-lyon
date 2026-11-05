package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/assets"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

type ToolDefinition struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
	resolved     *jsonschema.Resolved
	resolvedOut  *jsonschema.Resolved
}

type validationDetail struct {
	Code     string
	Field    string
	Expected string
}

type fieldError struct {
	field string
	err   error
}

func (failure fieldError) Error() string { return failure.field + ": " + failure.err.Error() }

type inputValidationError struct {
	detail  validationDetail
	message string
}

func (failure *inputValidationError) Error() string { return failure.message }

// IsExecutionError maps domain outcomes to the MCP execution status.
// Invalid arguments and complete execution failures set isError; partial,
// ambiguous, and legitimate empty/not-found results stay successful.
func IsExecutionError(status string) bool {
	return status == "invalid_request" || status == "unavailable"
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
		output, outputErr := OutputSchema(name)
		if outputErr != nil {
			return nil, outputErr
		}
		tool.OutputSchema, err = json.Marshal(output)
		if err != nil {
			return nil, err
		}
		tool.resolvedOut, err = output.Resolve(nil)
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
	return validatePlacesAt(value, "")
}

func validatePlacesAt(value reflect.Value, path string) error {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return validatePlacesAt(value.Elem(), path)
	}
	if value.Type() == reflect.TypeFor[model.PlaceRef]() {
		if err := value.Interface().(model.PlaceRef).Validate(); err != nil {
			return fieldError{field: path, err: err}
		}
		return nil
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if !value.Type().Field(i).IsExported() {
				continue
			}
			name := strings.Split(value.Type().Field(i).Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				name = value.Type().Field(i).Name
			}
			childPath := name
			if path != "" {
				childPath = path + "." + name
			}
			if err := validatePlacesAt(value.Field(i), childPath); err != nil {
				return err
			}
		}
	case reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			if err := validatePlacesAt(value.Index(i), childPath); err != nil {
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
		return input, &inputValidationError{
			detail:  validationDetail{Code: "invalid_argument", Expected: "use the declared JSON types and RFC3339 timestamps with a UTC offset"},
			message: "invalid input types or timestamp; use RFC3339 with a UTC offset",
		}
	}
	if err := validatePlaces(reflect.ValueOf(input)); err != nil {
		detail := validationDetail{Code: "invalid_place_reference", Expected: placeConstraint(err.Error())}
		message := err.Error()
		var failure fieldError
		if errors.As(err, &failure) {
			detail.Field = failure.field
		}
		return input, &inputValidationError{detail: detail, message: message}
	}
	return input, nil
}

func placeConstraint(message string) string {
	switch {
	case strings.Contains(message, "coordinates are outside"):
		return "latitude must be between -90 and 90 and longitude between -180 and 180"
	case strings.Contains(message, "both required"):
		return "provide both latitude and longitude"
	case strings.Contains(message, "exactly one place mode"):
		return "provide exactly one of place_id, query, coordinates, or profile_place"
	case strings.Contains(message, "too long"):
		return "place_id and query accept at most 200 characters; profile_place at most 64"
	default:
		return "use a supported place reference"
	}
}

func invalidRequestEnvelope(detail validationDetail, message string, data map[string]any) model.Envelope {
	envelope := model.NewEnvelope("invalid_request", message, data)
	if detail.Code == "" {
		detail.Code = "invalid_request"
	}
	if detail.Expected == "" {
		detail.Expected = "correct the request and retry"
	}
	envelope.Data["error"] = map[string]any{
		"code": detail.Code, "field": detail.Field, "expected": detail.Expected, "message": message,
	}
	envelope.Warn("INVALID_REQUEST", "request_validation", message, false)
	envelope.Status = "invalid_request"
	return envelope
}

func attachErrorDetail(envelope *model.Envelope) {
	if envelope.Status != "invalid_request" && envelope.Status != "unavailable" {
		return
	}
	if envelope.Data == nil {
		envelope.Data = map[string]any{}
	}
	if _, exists := envelope.Data["error"]; exists {
		return
	}
	code := "invalid_request"
	expected := "correct the request and retry"
	if envelope.Status == "unavailable" {
		code = "execution_failed"
		expected = "retry later or check source availability and diagnostics"
	}
	envelope.Data["error"] = map[string]any{
		"code": code, "field": "", "expected": expected, "message": envelope.Summary,
	}
}

func invoke[T any](ctx context.Context, raw []byte, handler func(context.Context, T) model.Envelope) model.Envelope {
	input, err := decodeInput[T](raw)
	if err != nil {
		var failure *inputValidationError
		if errors.As(err, &failure) {
			return invalidRequestEnvelope(failure.detail, failure.message, nil)
		}
		return invalidRequestEnvelope(validationDetail{Code: "invalid_request"}, err.Error(), nil)
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
		attachErrorDetail(&result)
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
		return invalidRequestEnvelope(validationDetail{Code: "unknown_tool", Field: "name", Expected: "use a tool advertised by tools/list"}, "Unknown tool.", nil)
	}
	if len(raw) > 128*1024 {
		return invalidRequestEnvelope(validationDetail{Code: "request_too_large", Expected: "request arguments must stay below 128 KiB"}, "Request arguments exceed the size limit.", nil)
	}
	var values map[string]any
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err = json.Unmarshal(raw, &values); err != nil || values == nil {
		return invalidRequestEnvelope(validationDetail{Code: "invalid_arguments", Expected: "send a JSON object as tool arguments"}, "Request arguments must be a JSON object.", nil)
	}
	if err = tool.resolved.Validate(values); err != nil {
		message := err.Error()
		return invalidRequestEnvelope(validationDetail{
			Code: "invalid_arguments", Field: schemaErrorField(message), Expected: schemaErrorExpectation(message),
		}, message, nil)
	}
	if err = tool.resolved.ApplyDefaults(&values); err != nil {
		return invalidRequestEnvelope(validationDetail{Code: "invalid_arguments", Expected: "use values accepted by the tool schema"}, "Cannot apply tool defaults.", nil)
	}
	if name == "lyon_trip_options" {
		applyTripPreferenceDefaults(values)
	}
	raw, err = json.Marshal(values)
	if err != nil {
		return invalidRequestEnvelope(validationDetail{Code: "invalid_arguments", Expected: "send valid JSON values"}, "Invalid JSON arguments.", nil)
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
	return invalidRequestEnvelope(validationDetail{Code: "unknown_tool", Field: "name", Expected: "use a tool advertised by tools/list"}, "Unknown tool.", nil)
}

func schemaErrorField(message string) string {
	const marker = "/properties/"
	field := ""
	for {
		index := strings.Index(message, marker)
		if index < 0 {
			break
		}
		message = message[index+len(marker):]
		end := strings.IndexAny(message, "/:")
		if end < 0 {
			end = len(message)
		}
		if candidate := message[:end]; candidate != "" && candidate != "$defs" {
			field = candidate
		}
	}
	return field
}

func schemaErrorExpectation(message string) string {
	switch {
	case strings.Contains(message, "unexpected additional properties"):
		return "remove unsupported fields"
	case strings.Contains(message, "minimum:"):
		return "use a value at or above the schema minimum"
	case strings.Contains(message, "maximum:"):
		return "use a value at or below the schema maximum"
	case strings.Contains(message, "required"):
		return "provide every required field"
	case strings.Contains(message, "enum"):
		return "use one of the supported values"
	case strings.Contains(message, "type:"):
		return "use the JSON type declared by the schema"
	default:
		return "value must satisfy the tool input schema"
	}
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
