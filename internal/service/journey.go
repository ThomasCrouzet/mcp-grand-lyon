package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

var supportedTravelModes = map[string]bool{
	"tcl": true, "velov": true, "park_and_ride": true, "car": true, "walk": true,
}

func (app *App) TripOptions(ctx context.Context, input model.TripOptionsInput) model.Envelope {
	if err := input.Origin.Validate(); err != nil {
		return invalidTrip("invalid origin: " + err.Error())
	}
	if err := input.Destination.Validate(); err != nil {
		return invalidTrip("invalid destination: " + err.Error())
	}
	modes := input.Modes
	if modes == nil {
		modes = []string{"tcl", "velov", "park_and_ride", "car", "walk"}
	}
	if len(modes) == 0 {
		return invalidTrip("modes must contain at least one mode")
	}
	seenModes := map[string]bool{}
	for _, mode := range modes {
		if !supportedTravelModes[mode] {
			return invalidTrip("unsupported travel mode: " + mode)
		}
		seenModes[mode] = true
	}

	departure := model.Now()
	if input.DepartureAt != nil {
		departure = *input.DepartureAt
	}
	if input.ArrivalBefore != nil && !input.ArrivalBefore.After(departure) {
		return invalidTrip("arrival_before must be after departure_at")
	}
	origin, err := app.ResolvePoint(ctx, input.Origin)
	if err != nil || origin == nil {
		return model.NewEnvelope("not_found", "The origin could not be resolved.", tripData(nil, departure, input.ArrivalBefore))
	}
	destination, err := app.ResolvePoint(ctx, input.Destination)
	if err != nil || destination == nil {
		return model.NewEnvelope("not_found", "The destination could not be resolved.", tripData(nil, departure, input.ArrivalBefore))
	}

	preferences := defaultTripPreferences(input.Preferences)
	distance := model.Distance(*origin, *destination)
	options := make([]model.TripOption, 0, len(modes))
	envelope := model.NewEnvelope("ok", "Trip options are available.", nil)

	if seenModes["walk"] {
		duration := int(distance / 1.4)
		walking := int(distance)
		options = append(options, model.TripOption{
			Mode: "walk", Score: app.tripScore(duration, 0.95, 0, walking, 0.5),
			EstimatedDurationSeconds: &duration, WalkingDistanceM: &walking,
			Summary: fmt.Sprintf("Walk about %d m", walking), Realtime: false,
			Alerts: []model.TransitAlert{}, Sources: []string{"haversine"},
		})
	}
	if seenModes["car"] {
		duration := int(distance / 8.0)
		options = append(options, model.TripOption{
			Mode: "car", Score: app.tripScore(duration, 0.7, 0.2, 0, 0.5),
			EstimatedDurationSeconds: &duration,
			Summary:                  fmt.Sprintf("Estimated car trip over %d m straight-line distance", int(distance)),
			Alerts:                   []model.TransitAlert{}, Sources: []string{"estimate"},
		})
	}
	if seenModes["velov"] {
		app.appendVelovOption(ctx, &envelope, &options, *origin, *destination, preferences)
	}
	if seenModes["park_and_ride"] {
		app.appendParkAndRideOption(ctx, &envelope, &options, *destination, distance)
	}
	if seenModes["tcl"] {
		app.appendTransitOptions(ctx, &envelope, &options, *origin, *destination, departure, preferences)
	}

	options = applyTripTiming(options, departure)
	if preferences.AvoidDisruptions {
		options = filterTripOptions(options, func(option model.TripOption) bool { return len(option.Alerts) == 0 })
	}
	if preferences.Wheelchair {
		options = filterTripOptions(options, func(option model.TripOption) bool {
			if option.Accessibility == nil {
				return false
			}
			switch strings.ToLower(*option.Accessibility) {
			case "accessible", "wheelchair", "wheelchair_accessible":
				return true
			default:
				return false
			}
		})
		if len(options) == 0 {
			envelope.Warn("PARTIAL_RESULT", "journey_planner", "No option has verified wheelchair accessibility.", false)
		}
	}
	if input.ArrivalBefore != nil {
		deadline := *input.ArrivalBefore
		options = filterTripOptions(options, func(option model.TripOption) bool {
			return option.ArrivalAt != nil && !option.ArrivalAt.After(deadline)
		})
	}

	sort.SliceStable(options, func(i, j int) bool { return options[i].Score > options[j].Score })
	status := envelope.Status
	var recommended any
	if len(options) != 0 {
		recommended = options[0].Mode
	} else if preferences.Wheelchair {
		status = "unavailable"
	} else if input.ArrivalBefore != nil || preferences.AvoidDisruptions {
		status = "not_found"
	} else if len(envelope.Warnings) != 0 {
		status = "unavailable"
	} else {
		status = "not_found"
	}
	envelope.Status = status
	envelope.Data = tripData(options, departure, input.ArrivalBefore)
	envelope.Data["recommended_mode"] = recommended
	if recommended == nil {
		envelope.Summary = "No trip option satisfies the request."
	} else {
		envelope.Summary = fmt.Sprintf("Recommended option: %s.", recommended)
	}
	return envelope
}

func invalidTrip(message string) model.Envelope {
	envelope := model.NewEnvelope("invalid_request", message, tripData(nil, time.Time{}, nil))
	envelope.Warn("INVALID_REQUEST", "journey_service", message, false)
	envelope.Status = "invalid_request"
	return envelope
}

func tripData(options []model.TripOption, departure time.Time, deadline *time.Time) map[string]any {
	if options == nil {
		options = []model.TripOption{}
	}
	var departureValue any
	if !departure.IsZero() {
		departureValue = departure
	}
	return map[string]any{
		"recommended_mode": nil, "options": options,
		"departure_at": departureValue, "arrival_before": deadline,
	}
}

func defaultTripPreferences(input *model.TripPreferences) model.TripPreferences {
	if input != nil {
		return *input
	}
	return model.TripPreferences{
		MaxWalkingM: 800, MinimumVelovBikes: 3, MinimumVelovDocks: 3,
		AvoidDisruptions: true,
	}
}

func (app *App) appendVelovOption(ctx context.Context, envelope *model.Envelope, options *[]model.TripOption, origin, destination model.Point, preferences model.TripPreferences) {
	if app.Provider == nil {
		envelope.Warn("SOURCE_UNAVAILABLE", "velov_realtime", "The Velov provider is unavailable.", true)
		return
	}
	radius := preferences.MaxWalkingM
	originStations, originErr := app.Provider.Stations(ctx, origin, float64(radius), 5)
	destinationStations, destinationErr := app.Provider.Stations(ctx, destination, float64(radius), 5)
	if originErr != nil || destinationErr != nil {
		envelope.Warn("SOURCE_UNAVAILABLE", "velov_realtime", "Velov stations are unavailable for this trip.", true)
		return
	}
	originStations = filterStations(originStations, preferences.MinimumVelovBikes, true)
	destinationStations = filterStations(destinationStations, preferences.MinimumVelovDocks, false)
	if len(originStations) == 0 || len(destinationStations) == 0 {
		envelope.Warn("PARTIAL_RESULT", "velov_realtime", "No station pair satisfies the requested availability.", false)
		return
	}
	bikeDistance := model.Distance(
		model.Point{Latitude: originStations[0].Latitude, Longitude: originStations[0].Longitude},
		model.Point{Latitude: destinationStations[0].Latitude, Longitude: destinationStations[0].Longitude},
	)
	walking := int(model.Value(originStations[0].DistanceM) + model.Value(destinationStations[0].DistanceM))
	if walking > preferences.MaxWalkingM {
		envelope.Warn("PARTIAL_RESULT", "velov_realtime", "No station pair satisfies the requested walking limit.", false)
		return
	}
	duration := int(bikeDistance/4.0) + walking
	availability := fmt.Sprintf("%d bikes / %d docks", originStations[0].BikesAvailable, destinationStations[0].DocksAvailable)
	*options = append(*options, model.TripOption{
		Mode: "velov", Score: app.tripScore(duration, 0.75, 0, walking, 0.8),
		EstimatedDurationSeconds: &duration, WalkingDistanceM: &walking,
		Availability: &availability, Summary: fmt.Sprintf("Velov %s to %s", originStations[0].Name, destinationStations[0].Name),
		Realtime: true, Alerts: []model.TransitAlert{}, Sources: []string{"velov_realtime"},
	})
}

func filterStations(stations []model.VelovStation, minimum int, bikes bool) []model.VelovStation {
	result := make([]model.VelovStation, 0, len(stations))
	for _, station := range stations {
		available := station.DocksAvailable
		if bikes {
			available = station.BikesAvailable
		}
		if available >= minimum {
			result = append(result, station)
		}
	}
	return result
}

func (app *App) appendParkAndRideOption(ctx context.Context, envelope *model.Envelope, options *[]model.TripOption, destination model.Point, distance float64) {
	if app.Provider == nil {
		envelope.Warn("SOURCE_UNAVAILABLE", "parking", "The parking provider is unavailable.", true)
		return
	}
	parkings, err := app.Provider.Parkings(ctx, destination, []string{"park_and_ride"}, 3000, 0, 3)
	if err != nil && len(parkings) == 0 {
		envelope.Warn("SOURCE_UNAVAILABLE", "parking", "Park-and-ride data is unavailable.", true)
		return
	}
	if err != nil {
		envelope.Warn("PARTIAL_RESULT", "parking", "Some park-and-ride data is unavailable.", true)
	}
	if len(parkings) == 0 {
		return
	}
	var selected *model.ParkingOption
	for index := range parkings {
		if parkings[index].AvailableSpaces != nil && *parkings[index].AvailableSpaces > 0 {
			selected = &parkings[index]
			break
		}
	}
	if selected == nil {
		for index := range parkings {
			if parkings[index].AvailableSpaces == nil {
				selected = &parkings[index]
				break
			}
		}
	}
	if selected == nil {
		return
	}
	walking := 200
	if selected.DistanceM != nil {
		walking = int(*selected.DistanceM)
	}
	duration := int(distance / 9.0)
	availabilityScore := 0.2
	var availability *string
	if selected.AvailableSpaces == nil {
		envelope.Warn("PARTIAL_RESULT", "parking_availability", "Park-and-ride availability is unknown.", false)
	} else {
		text := fmt.Sprintf("%d spaces", *selected.AvailableSpaces)
		availability = &text
		availabilityScore = 0.5
		if *selected.AvailableSpaces > 5 {
			availabilityScore = 0.7
		}
	}
	*options = append(*options, model.TripOption{
		Mode: "park_and_ride", Score: app.tripScore(duration, 0.7, 0.1, walking, availabilityScore),
		EstimatedDurationSeconds: &duration, WalkingDistanceM: &walking,
		Availability: availability,
		Summary:      "Park-and-ride at " + selected.Name, Realtime: selected.Realtime,
		Alerts: []model.TransitAlert{}, Sources: []string{"parking"},
	})
}

func (app *App) appendTransitOptions(ctx context.Context, envelope *model.Envelope, options *[]model.TripOption, origin, destination model.Point, departure time.Time, preferences model.TripPreferences) {
	if app.Provider == nil || !app.Config.TransitousEnabled {
		appendTransitFallback(envelope, options, app)
		return
	}
	planned, err := app.Provider.Plan(ctx, origin, destination, departure, []string{"tcl"}, preferences)
	if err != nil || len(planned) == 0 {
		appendTransitFallback(envelope, options, app)
		return
	}
	for _, option := range planned {
		if option.EstimatedDurationSeconds == nil {
			walking := model.Value(option.WalkingDistanceM)
			if walking == 0 {
				walking = 400
			}
			option.Score = app.tripScoreUnknownDuration(0.75, 0.1, walking, 0.4)
			if option.Summary == "" {
				option.Summary = "TCL duration is unavailable."
			}
		}
		if option.Alerts == nil {
			option.Alerts = []model.TransitAlert{}
		}
		if option.Sources == nil {
			option.Sources = []string{"transitous"}
		}
		*options = append(*options, option)
	}
}

func appendTransitFallback(envelope *model.Envelope, options *[]model.TripOption, app *App) {
	envelope.Warn("ROUTING_UNAVAILABLE", "journey_planner", "A reliable TCL door-to-door duration is unavailable.", true)
	walking := 400
	*options = append(*options, model.TripOption{
		Mode: "tcl", Score: app.tripScoreUnknownDuration(0.75, 0.1, walking, 0.4),
		WalkingDistanceM: &walking, Summary: "TCL duration is unavailable.",
		Alerts: []model.TransitAlert{}, Sources: []string{},
	})
}

func applyTripTiming(options []model.TripOption, departure time.Time) []model.TripOption {
	for index := range options {
		if options[index].DepartureAt == nil {
			options[index].DepartureAt = model.Ptr(departure)
		}
		if options[index].ArrivalAt == nil && options[index].EstimatedDurationSeconds != nil {
			arrival := options[index].DepartureAt.Add(time.Duration(*options[index].EstimatedDurationSeconds) * time.Second)
			options[index].ArrivalAt = &arrival
		}
	}
	return options
}

func filterTripOptions(options []model.TripOption, keep func(model.TripOption) bool) []model.TripOption {
	result := make([]model.TripOption, 0, len(options))
	for _, option := range options {
		if keep(option) {
			result = append(result, option)
		}
	}
	return result
}

func (app *App) tripScore(duration int, reliability, disruptions float64, walking int, availability float64) float64 {
	weights := app.journeyWeights()
	durationScore := math.Max(0, 1-float64(duration)/5400)
	return boundedScore(weights.Duration*durationScore + weights.Reliability*reliability + weights.Disruptions*math.Max(0, 1-disruptions) + weights.Walking*math.Max(0, 1-float64(walking)/2000) + weights.Availability*availability + weights.UserPreference*0.5)
}

func (app *App) tripScoreUnknownDuration(reliability, disruptions float64, walking int, availability float64) float64 {
	weights := app.journeyWeights()
	return boundedScore(weights.Duration*0.15 + weights.Reliability*reliability + weights.Disruptions*math.Max(0, 1-disruptions) + weights.Walking*math.Max(0, 1-float64(walking)/2000) + weights.Availability*availability + weights.UserPreference*0.5)
}

func (app *App) journeyWeights() model.JourneyScoringWeights {
	weights := app.Config.JourneyScoring
	if weights.Duration+weights.Reliability+weights.Disruptions+weights.Walking+weights.Availability+weights.UserPreference == 0 {
		return model.JourneyScoringWeights{Duration: 0.35, Reliability: 0.25, Disruptions: 0.15, Walking: 0.10, Availability: 0.10, UserPreference: 0.05}
	}
	return weights
}

func boundedScore(value float64) float64 { return math.Min(1, math.Max(0, value)) }
