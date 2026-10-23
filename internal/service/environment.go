package service

import (
	"context"
	"fmt"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func (app *App) EnvironmentBrief(ctx context.Context, input model.EnvironmentBriefInput) model.Envelope {
	if err := input.Location.Validate(); err != nil {
		return model.NewEnvelope("invalid_request", "Invalid location: "+err.Error(), environmentData(nil))
	}
	requested := input.Indicators
	if requested == nil {
		requested = []string{"pollen", "air_quality", "heat"}
	}
	if len(requested) == 0 {
		return model.NewEnvelope("invalid_request", "At least one environmental indicator is required.", environmentData(nil))
	}
	point, err := app.ResolvePoint(ctx, input.Location)
	if err != nil || point == nil {
		return model.NewEnvelope("not_found", "The environmental location could not be resolved.", environmentData(nil))
	}
	if app.Provider == nil {
		envelope := model.NewEnvelope("unavailable", "No environmental source is available.", environmentData(nil))
		for _, indicator := range requested {
			envelope.Warn("UNSUPPORTED_INDICATOR", indicator, "No source is available for this indicator.", false)
		}
		envelope.Status = "unavailable"
		return envelope
	}
	at := model.Now()
	if input.At != nil {
		at = *input.At
	}
	values, err := app.Provider.Indicators(ctx, *point, requested, at)
	if err != nil {
		envelope := model.NewEnvelope("unavailable", "Environmental indicators are unavailable.", environmentData(nil))
		envelope.Warn("SOURCE_UNAVAILABLE", "environment", "The environmental source failed.", true)
		envelope.Status = "unavailable"
		return envelope
	}

	wanted := make(map[string]bool, len(requested))
	for _, indicator := range requested {
		wanted[indicator] = true
	}
	seen := map[string]bool{}
	supported := make([]model.IndicatorValue, 0, len(values))
	envelope := model.NewEnvelope("ok", "Environmental indicators are available.", nil)
	for _, value := range values {
		if !wanted[value.Indicator] {
			continue
		}
		seen[value.Indicator] = true
		if !value.Supported {
			envelope.Warn("UNSUPPORTED_INDICATOR", value.Indicator, "No resolved source supports this indicator.", false)
			continue
		}
		if value.Value == nil {
			envelope.Warn("SOURCE_UNAVAILABLE", value.Indicator, "The source returned no value for this indicator.", false)
			continue
		}
		supported = append(supported, value)
	}
	for _, indicator := range requested {
		if !seen[indicator] {
			envelope.Warn("UNSUPPORTED_INDICATOR", indicator, "The source returned no value for this indicator.", false)
		}
	}
	envelope.Data = environmentData(supported)
	envelope.Summary = fmt.Sprintf("Found %d environmental indicators.", len(supported))
	if len(supported) == 0 {
		envelope.Status = "unavailable"
	}
	return envelope
}

func environmentData(values []model.IndicatorValue) map[string]any {
	if values == nil {
		values = []model.IndicatorValue{}
	}
	return map[string]any{"indicators": values}
}
