package providers

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func (live *Live) Plan(ctx context.Context, origin, destination model.Point, departure time.Time, modes []string, preferences model.TripPreferences) ([]model.TripOption, error) {
	if !live.cfg.TransitousEnabled {
		return []model.TripOption{}, nil
	}
	if len(modes) != 0 {
		hasTransit := false
		for _, mode := range modes {
			hasTransit = hasTransit || mode == "tcl"
		}
		if !hasTransit {
			return []model.TripOption{}, nil
		}
	}
	base := strings.TrimRight(live.cfg.TransitousBaseURL, "/")
	if base == "" {
		base = "https://api.transitous.org/api"
	}
	values := url.Values{
		"fromPlace": {strconv.FormatFloat(origin.Latitude, 'f', -1, 64) + "," + strconv.FormatFloat(origin.Longitude, 'f', -1, 64)},
		"toPlace":   {strconv.FormatFloat(destination.Latitude, 'f', -1, 64) + "," + strconv.FormatFloat(destination.Longitude, 'f', -1, 64)},
		"time":      {departure.Format(time.RFC3339)},
	}
	decoded, err := live.getJSON(ctx, base+"/v5/plan?"+values.Encode(), false)
	if err != nil {
		return nil, err
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return nil, errors.New("Transitous response is not an object")
	}
	raw := root["itineraries"]
	if raw == nil {
		raw = root["plans"]
	}
	items, _ := raw.([]any)
	result := make([]model.TripOption, 0, min(5, len(items)))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		duration := integer(entry, 0, "duration", "durationSeconds")
		if duration <= 0 {
			continue
		}
		arrival := departure.Add(time.Duration(duration) * time.Second)
		result = append(result, model.TripOption{
			Mode:                     "tcl",
			Score:                    0.7,
			EstimatedDurationSeconds: &duration,
			DepartureAt:              &departure,
			ArrivalAt:                &arrival,
			Summary:                  firstText(entry, "Itinéraire Transitous", "summary"),
			Sources:                  []string{"transitous"},
			Alerts:                   []model.TransitAlert{},
		})
		if len(result) == 5 {
			break
		}
	}
	_ = preferences
	return result, nil
}
