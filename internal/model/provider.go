package model

import (
	"context"
	"time"
)

// Provider supplies normalized data. Services apply public request constraints.
// Aggregate methods can return usable rows with an error after a partial source failure.
// Callers must retain nonempty results and report the partial failure.
type Provider interface {
	Search(context.Context, string, *Point, []string, int) ([]PlaceCandidate, error)
	Reverse(context.Context, Point) (*PlaceCandidate, error)
	Departures(context.Context, string, string, string, int) ([]Departure, error)
	Alerts(context.Context, []string) ([]TransitAlert, error)
	Incidents(context.Context, []string) ([]AccessibilityIncident, error)
	CheckStop(context.Context, string) (string, error)
	Stations(context.Context, Point, float64, int) ([]VelovStation, error)
	Parkings(context.Context, Point, []string, float64, int, int) ([]ParkingOption, error)
	Traffic(context.Context, string) ([]TrafficCondition, error)
	RoadEvents(context.Context, string) ([]RoadEvent, error)
	Facilities(context.Context, Point, []string, float64, int) ([]Facility, error)
	Indicators(context.Context, Point, []string, time.Time) ([]IndicatorValue, error)
	WasteFacilities(context.Context, Point, string, float64, int) ([]WasteFacility, error)
	Plan(context.Context, Point, Point, time.Time, []string, TripPreferences) ([]TripOption, error)
}
