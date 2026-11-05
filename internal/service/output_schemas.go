package service

import (
	"strings"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

// OutputSchema builds the advertised result contract from the shared envelope
// and domain model types, avoiding a second hand-written schema definition.
func OutputSchema(name string) (*jsonschema.Schema, error) {
	envelope, err := jsonschema.For[model.Envelope](nil)
	if err != nil {
		return nil, err
	}
	data := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}, AdditionalProperties: &jsonschema.Schema{}}
	switch name {
	case "lyon_resolve_place":
		data.Properties["candidates"], err = sliceSchema[model.PlaceCandidate]()
		if err != nil {
			return nil, err
		}
	case "lyon_next_departures":
		data.Properties["stop"], err = valueSchema[model.PlaceCandidate]()
		if err != nil {
			return nil, err
		}
		data.Properties["departures"], err = sliceSchema[model.Departure]()
		if err != nil {
			return nil, err
		}
	case "lyon_mobility_status":
		for field, builder := range map[string]func() (*jsonschema.Schema, error){
			"alerts":                  sliceSchema[model.TransitAlert],
			"accessibility_incidents": sliceSchema[model.AccessibilityIncident],
			"road_events":             sliceSchema[model.RoadEvent],
			"traffic_conditions":      sliceSchema[model.TrafficCondition],
		} {
			schema, buildErr := builder()
			if buildErr != nil {
				return nil, buildErr
			}
			data.Properties[field] = schema
		}
		data.Properties["line_statuses"] = &jsonschema.Schema{Types: []string{"null", "array"}, Items: &jsonschema.Schema{Type: "object", AdditionalProperties: &jsonschema.Schema{}}}
	case "lyon_trip_options":
		data.Properties["recommended_mode"] = &jsonschema.Schema{Types: []string{"null", "string"}, Enum: []any{nil, "tcl", "velov", "park_and_ride", "car", "walk"}}
		data.Properties["options"], err = sliceSchema[model.TripOption]()
		if err != nil {
			return nil, err
		}
		data.Properties["departure_at"] = nullableDateTime()
		data.Properties["arrival_before"] = nullableDateTime()
	case "lyon_parking_options":
		data.Properties["options"], err = sliceSchema[model.ParkingOption]()
		if err != nil {
			return nil, err
		}
	case "lyon_accessibility_check":
		for field, schema := range map[string]*jsonschema.Schema{
			"overall_status":  {Type: "string"},
			"segments":        nil,
			"incidents":       nil,
			"unknowns":        {Types: []string{"null", "array"}, Items: &jsonschema.Schema{Type: "string"}},
			"requested_needs": {Types: []string{"null", "array"}, Items: &jsonschema.Schema{Type: "string"}},
		} {
			data.Properties[field] = schema
		}
		data.Properties["segments"], err = sliceSchema[model.AccessibilitySegment]()
		if err != nil {
			return nil, err
		}
		data.Properties["incidents"], err = sliceSchema[model.AccessibilityIncident]()
		if err != nil {
			return nil, err
		}
	case "lyon_nearby_facilities":
		data.Properties["facilities"] = &jsonschema.Schema{Type: "object", AdditionalProperties: &jsonschema.Schema{Types: []string{"null", "array"}, Items: &jsonschema.Schema{Type: "object", AdditionalProperties: &jsonschema.Schema{}}}}
		data.Properties["items"], err = sliceSchema[model.Facility]()
		if err != nil {
			return nil, err
		}
		data.Properties["open_at"] = nullableDateTime()
	case "lyon_environment_brief":
		data.Properties["indicators"], err = sliceSchema[model.IndicatorValue]()
		if err != nil {
			return nil, err
		}
	case "lyon_waste_dropoff":
		data.Properties["classification"], err = valueSchema[model.WasteClassification]()
		if err != nil {
			return nil, err
		}
		data.Properties["facilities"], err = sliceSchema[model.WasteFacility]()
		if err != nil {
			return nil, err
		}
		data.Properties["transport"] = &jsonschema.Schema{Type: "string"}
		data.Properties["open_at"] = nullableDateTime()
	case "lyon_personal_briefing":
		data.Properties["profile"] = &jsonschema.Schema{Type: "string"}
		data.Properties["at"] = &jsonschema.Schema{Type: "string", Format: "date-time"}
		data.Properties["summary"] = &jsonschema.Schema{Type: "string"}
		data.Properties["facts"] = &jsonschema.Schema{Type: "object", AdditionalProperties: &jsonschema.Schema{}}
		data.Properties["changes"], err = sliceSchema[model.BriefingChange]()
		if err != nil {
			return nil, err
		}
	}
	data.Properties["error"] = &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{
		"code": {Type: "string"}, "field": {Type: "string"}, "expected": {Type: "string"}, "message": {Type: "string"},
	}}
	envelope.Properties["data"] = data
	envelope.Title = name + " output"
	applyDateTimeFormats(envelope, "")
	return envelope, nil
}

func sliceSchema[T any]() (*jsonschema.Schema, error) { return jsonschema.For[[]T](nil) }
func valueSchema[T any]() (*jsonschema.Schema, error) { return jsonschema.For[T](nil) }

func nullableDateTime() *jsonschema.Schema {
	return &jsonschema.Schema{Types: []string{"null", "string"}, Format: "date-time"}
}

func applyDateTimeFormats(schema *jsonschema.Schema, field string) {
	if schema == nil {
		return
	}
	if (strings.HasSuffix(field, "_at") || field == "at") && schema.Format == "" {
		for _, candidate := range append([]*jsonschema.Schema{schema}, schema.AnyOf...) {
			if candidate.Type == "string" || containsString(candidate.Types, "string") {
				candidate.Format = "date-time"
			}
		}
	}
	for name, child := range schema.Properties {
		applyDateTimeFormats(child, name)
	}
	applyDateTimeFormats(schema.Items, field)
	for _, child := range schema.AnyOf {
		applyDateTimeFormats(child, field)
	}
	for _, child := range schema.AllOf {
		applyDateTimeFormats(child, field)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
