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
			Mode: "walk", Score: app.tripScore(duration, 0.95, 0, walking, 0.5), Evidence: "estimated",
			EstimatedDurationSeconds: &duration, WalkingDistanceM: &walking,
			Summary: fmt.Sprintf("Walk about %d m", walking), Realtime: false,
			Alerts: []model.TransitAlert{}, Sources: []string{"haversine"},
		})
	}
	if seenModes["car"] {
		duration := int(distance / 8.0)
		walking := 0
		options = append(options, model.TripOption{
			Mode: "car", Score: app.tripScore(duration, 0.7, 0.2, walking, 0.5), Evidence: "estimated",
			EstimatedDurationSeconds: &duration, WalkingDistanceM: &walking,
			Summary: fmt.Sprintf("Estimated car trip over %d m straight-line distance", int(distance)),
			Alerts:  []model.TransitAlert{}, Sources: []string{"estimate"},
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
	evaluateTripOptions(options, preferences, input.ArrivalBefore)
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].Eligible != options[j].Eligible {
			return options[i].Eligible
		}
		return options[i].Score > options[j].Score
	})
	status := envelope.Status
	var recommended any
	var recommendedOption *model.TripOption
	for index := range options {
		if options[index].Eligible {
			recommended = options[index].Mode
			recommendedOption = &options[index]
			break
		}
	}
	if recommended == nil {
		status = "not_found"
		if preferences.Wheelchair || len(envelope.Warnings) != 0 || hasUnavailableOption(options) {
			status = "unavailable"
		}
	}
	envelope.Status = status
	envelope.Data = tripData(options, departure, input.ArrivalBefore)
	envelope.Data["recommended_mode"] = recommended
	if recommended == nil {
		envelope.Summary = "No eligible trip option satisfies the request."
	} else {
		envelope.Summary = fmt.Sprintf("Recommended option: %s.", recommendedOption.Mode)
	}
	return envelope
}

func evaluateTripOptions(options []model.TripOption, preferences model.TripPreferences, deadline *time.Time) {
	for index := range options {
		option := &options[index]
		reasons := []string{}
		switch option.Evidence {
		case "calculated", "estimated":
		case "unavailable":
			reasons = append(reasons, "no calculated itinerary is available")
		default:
			option.Evidence = "unavailable"
			reasons = append(reasons, "itinerary evidence is unavailable")
		}
		if option.WalkingDistanceM == nil {
			reasons = append(reasons, "total walking distance is unknown")
		} else if *option.WalkingDistanceM > preferences.MaxWalkingM {
			reasons = append(reasons, fmt.Sprintf("total walking distance exceeds max_walking_m (%d m)", preferences.MaxWalkingM))
		}
		if deadline != nil {
			if option.ArrivalAt == nil {
				reasons = append(reasons, "arrival time is unknown, so arrival_before cannot be verified")
			} else if option.ArrivalAt.After(*deadline) {
				reasons = append(reasons, "arrival is after arrival_before")
			}
		}
		if preferences.AvoidDisruptions && len(option.Alerts) != 0 {
			reasons = append(reasons, "known service alerts affect this option")
		}
		if preferences.Wheelchair {
			accessibility := ""
			if option.Accessibility != nil {
				accessibility = strings.ToLower(*option.Accessibility)
			}
			switch accessibility {
			case "accessible", "wheelchair", "wheelchair_accessible":
			default:
				reasons = append(reasons, "wheelchair accessibility is not verified")
			}
		}
		option.Eligible = len(reasons) == 0
		option.IneligibilityReasons = reasons
	}
}

func hasUnavailableOption(options []model.TripOption) bool {
	for _, option := range options {
		if option.Evidence == "unavailable" {
			return true
		}
	}
	return false
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
	var selectedOrigin, selectedDestination *model.VelovStation
	bestDuration := 0
	bestWalking := 0
	for originIndex := range originStations {
		for destinationIndex := range destinationStations {
			originStation := &originStations[originIndex]
			destinationStation := &destinationStations[destinationIndex]
			if originStation.DistanceM == nil || destinationStation.DistanceM == nil {
				continue
			}
			walking := int(*originStation.DistanceM + *destinationStation.DistanceM)
			if walking > preferences.MaxWalkingM {
				continue
			}
			bikeDistance := model.Distance(
				model.Point{Latitude: originStation.Latitude, Longitude: originStation.Longitude},
				model.Point{Latitude: destinationStation.Latitude, Longitude: destinationStation.Longitude},
			)
			duration := int(bikeDistance/4.0) + walking
			if selectedOrigin == nil || duration < bestDuration {
				selectedOrigin, selectedDestination = originStation, destinationStation
				bestDuration, bestWalking = duration, walking
			}
		}
	}
	if selectedOrigin == nil {
		envelope.Warn("PARTIAL_RESULT", "velov_realtime", "No station pair satisfies the requested walking limit.", false)
		return
	}
	duration := bestDuration
	walking := bestWalking
	availability := fmt.Sprintf("%d bikes / %d docks", selectedOrigin.BikesAvailable, selectedDestination.DocksAvailable)
	*options = append(*options, model.TripOption{
		Mode: "velov", Score: app.tripScore(duration, 0.75, 0, walking, 0.8), Evidence: "estimated",
		EstimatedDurationSeconds: &duration, WalkingDistanceM: &walking,
		Availability: &availability, Summary: fmt.Sprintf("Velov %s to %s", selectedOrigin.Name, selectedDestination.Name),
		Realtime: true, Alerts: []model.TransitAlert{}, Sources: []string{"velov_realtime"},
	})
}

func filterStations(stations []model.VelovStation, minimum int, bikes bool) []model.VelovStation {
	result := make([]model.VelovStation, 0, len(stations))
	for _, station := range stations {
		if station.Status == "closed" {
			continue
		}
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
	walking := 0
	var walkingDistance *int
	if selected.DistanceM != nil {
		walking = int(*selected.DistanceM)
		walkingDistance = &walking
	} else {
		envelope.Warn("PARTIAL_RESULT", "parking", "The park-and-ride walking distance is unknown.", false)
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
		Mode: "park_and_ride", Score: app.tripScore(duration, 0.7, 0.1, walking, availabilityScore), Evidence: "estimated",
		EstimatedDurationSeconds: &duration, WalkingDistanceM: walkingDistance,
		Availability: availability,
		Summary:      "Park-and-ride at " + selected.Name, Realtime: selected.Realtime,
		Alerts: []model.TransitAlert{}, Sources: []string{"parking"},
	})
}

func (app *App) appendTransitOptions(ctx context.Context, envelope *model.Envelope, options *[]model.TripOption, origin, destination model.Point, departure time.Time, preferences model.TripPreferences) {
	if app.Provider == nil || !app.Config.TransitousEnabled {
		appendTransitFallback(envelope, options)
		return
	}
	planned, err := app.Provider.Plan(ctx, origin, destination, departure, []string{"tcl"}, preferences)
	if err != nil || len(planned) == 0 {
		appendTransitFallback(envelope, options)
		return
	}
	for _, option := range planned {
		if option.Evidence == "" {
			option.Evidence = "calculated"
		}
		if option.EstimatedDurationSeconds == nil {
			option.Evidence = "unavailable"
			option.Score = 0
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

func appendTransitFallback(envelope *model.Envelope, options *[]model.TripOption) {
	envelope.Warn("ROUTING_UNAVAILABLE", "journey_planner", "A reliable TCL door-to-door duration is unavailable.", true)
	*options = append(*options, model.TripOption{
		Mode: "tcl", Evidence: "unavailable", Eligible: false,
		Summary: "TCL duration is unavailable; no itinerary or walking distance was calculated.",
		Alerts:  []model.TransitAlert{}, Sources: []string{},
	})
}

func applyTripTiming(options []model.TripOption, departure time.Time) []model.TripOption {
	for index := range options {
		if options[index].Evidence == "unavailable" {
			continue
		}
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

func (app *App) tripScore(duration int, reliability, disruptions float64, walking int, availability float64) float64 {
	weights := app.journeyWeights()
	durationScore := math.Max(0, 1-float64(duration)/5400)
	return boundedScore(weights.Duration*durationScore + weights.Reliability*reliability + weights.Disruptions*math.Max(0, 1-disruptions) + weights.Walking*math.Max(0, 1-float64(walking)/2000) + weights.Availability*availability + weights.UserPreference*0.5)
}

func (app *App) journeyWeights() model.JourneyScoringWeights {
	weights := app.Config.JourneyScoring
	if weights.Duration+weights.Reliability+weights.Disruptions+weights.Walking+weights.Availability+weights.UserPreference == 0 {
		return model.JourneyScoringWeights{Duration: 0.35, Reliability: 0.25, Disruptions: 0.15, Walking: 0.10, Availability: 0.10, UserPreference: 0.05}
	}
	return weights
}

func boundedScore(value float64) float64 { return math.Min(1, math.Max(0, value)) }
