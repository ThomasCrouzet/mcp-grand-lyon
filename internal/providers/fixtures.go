package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

// ErrFixtureRoutingUnavailable reports that offline fixtures contain no routing engine.
var ErrFixtureRoutingUnavailable = errors.New("fixture routing is unavailable")

// Fixtures supplies deterministic offline data from an fs.FS rooted at fixtures/.
type Fixtures struct {
	files fs.FS
}

var _ model.Provider = (*Fixtures)(nil)

// NewFixtures creates an offline provider from packaged or override fixtures.
func NewFixtures(files fs.FS) *Fixtures {
	return &Fixtures{files: files}
}

func (fixtures *Fixtures) Search(ctx context.Context, query string, near *model.Point, types []string, limit int) ([]model.PlaceCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, err := fixtures.readJSON("photon/part_dieu.json")
	if err != nil || !found {
		return []model.PlaceCandidate{}, err
	}
	candidates, err := ParsePhoton(payload)
	if err != nil {
		return nil, err
	}
	query = model.Normalize(query)
	if query == "" {
		return []model.PlaceCandidate{}, nil
	}
	allowedTypes := providerStringSet(types)
	result := []model.PlaceCandidate{}
	for _, candidate := range candidates {
		if !strings.Contains(model.Normalize(candidate.Name), query) && !strings.Contains(model.Normalize(candidate.Label), query) {
			continue
		}
		if len(allowedTypes) > 0 && !allowedTypes[candidate.Type] {
			continue
		}
		if near != nil {
			distance := model.Distance(*near, model.Point{Latitude: candidate.Latitude, Longitude: candidate.Longitude})
			candidate.DistanceM = model.Ptr(distance)
		}
		result = append(result, candidate)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].DistanceM != nil && result[j].DistanceM != nil {
			return *result[i].DistanceM < *result[j].DistanceM
		}
		return result[i].Confidence > result[j].Confidence
	})
	return providerLimit(result, limit), nil
}

func (fixtures *Fixtures) Reverse(ctx context.Context, point model.Point) (*model.PlaceCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &model.PlaceCandidate{
		ID: "photon:reverse", Name: "Lyon", Label: "Lyon", Type: "point_of_interest",
		Latitude: point.Latitude, Longitude: point.Longitude, Confidence: 0.5,
	}, nil
}

func (fixtures *Fixtures) Departures(ctx context.Context, stopID, line, direction string, limit int) ([]model.Departure, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, err := fixtures.readJSON("siri/estimated_timetable.json")
	if err != nil {
		return nil, err
	}
	var departures []model.Departure
	if found {
		departures, err = ParseSIRIDepartures(payload)
	} else {
		departures, err = fixtures.dataPusherDepartures()
	}
	if err != nil {
		return nil, err
	}
	result := []model.Departure{}
	for _, departure := range departures {
		if departure.StopRef == nil || !providerStopMatches(stopID, *departure.StopRef) {
			continue
		}
		if !model.LineMatches(line, departure.LineName, departure.LineID) {
			continue
		}
		if direction != "" && !strings.Contains(model.Normalize(departure.Destination), model.Normalize(direction)) {
			continue
		}
		result = append(result, departure)
	}
	return providerLimit(result, limit), nil
}

func (fixtures *Fixtures) dataPusherDepartures() ([]model.Departure, error) {
	payload, found, err := fixtures.readJSON("datagrandlyon/tcl_departures.json")
	if err != nil || !found {
		return []model.Departure{}, err
	}
	result := []model.Departure{}
	for _, row := range providerFixtureRows(payload) {
		line := providerJSONText(providerJSONFirst(row, "ligne", "line"))
		stop := providerJSONText(providerJSONFirst(row, "stop_id", "stop_ref", "arret_id", "arret"))
		destination := providerJSONText(providerJSONFirst(row, "direction", "destination"))
		expected := providerJSONTime(providerJSONFirst(row, "heurepassage", "expected", "delaipassage"))
		departure := model.Departure{
			LineID: "tcl:" + line, LineName: line, Destination: destination,
			ScheduledAt: expected, ExpectedAt: expected, Realtime: true,
		}
		if stop != "" {
			departure.StopRef = model.Ptr(stop)
		}
		result = append(result, departure)
	}
	return result, nil
}

func (fixtures *Fixtures) Alerts(ctx context.Context, lines []string) ([]model.TransitAlert, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, siriErr := fixtures.readJSON("siri/situation_exchange.json")
	if found {
		var alerts []model.TransitAlert
		if siriErr == nil {
			alerts, siriErr = ParseSIRIAlerts(payload)
		}
		if siriErr == nil {
			return filterFixtureAlerts(alerts, lines), nil
		}
	}
	alerts, fallbackErr := fixtures.dataPusherAlerts()
	if fallbackErr != nil {
		if siriErr != nil {
			return nil, errors.Join(siriErr, fallbackErr)
		}
		return nil, fallbackErr
	}
	return filterFixtureAlerts(alerts, lines), nil
}

func filterFixtureAlerts(alerts []model.TransitAlert, lines []string) []model.TransitAlert {
	if len(lines) == 0 {
		return alerts
	}
	result := []model.TransitAlert{}
	for _, alert := range alerts {
		if len(alert.Lines) == 0 || providerAlertMatchesLines(alert, lines) {
			result = append(result, alert)
		}
	}
	return result
}

func (fixtures *Fixtures) dataPusherAlerts() ([]model.TransitAlert, error) {
	payload, found, err := fixtures.readJSON("datagrandlyon/tcl_alerts.json")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("required fixture datagrandlyon/tcl_alerts.json is missing")
	}
	result := []model.TransitAlert{}
	for index, row := range providerFixtureRows(payload) {
		identifier := providerJSONText(providerJSONFirst(row, "id"))
		if identifier == "" {
			identifier = fmt.Sprint(index)
		}
		severity := "minor"
		if strings.Contains(model.Normalize(providerJSONText(providerJSONFirst(row, "type"))), "majeur") {
			severity = "major"
		}
		result = append(result, model.TransitAlert{
			ID: identifier, Severity: severity,
			Title:       providerFixtureDefault(providerJSONText(providerJSONFirst(row, "title", "titre")), "Alerte"),
			Description: providerJSONText(providerJSONFirst(row, "description")),
			Lines:       providerStringList(providerJSONFirst(row, "lines")),
		})
	}
	return result, nil
}

func (fixtures *Fixtures) Incidents(ctx context.Context, lines []string) ([]model.AccessibilityIncident, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, err := fixtures.readJSON("datagrandlyon/tcl_accessibility.json")
	if err != nil || !found {
		return []model.AccessibilityIncident{}, err
	}
	result := []model.AccessibilityIncident{}
	for index, row := range providerFixtureRows(payload) {
		identifier := providerJSONText(providerJSONFirst(row, "id"))
		if identifier == "" {
			identifier = fmt.Sprint(index)
		}
		status := "partially_accessible"
		if strings.Contains(model.Normalize(providerJSONText(providerJSONFirst(row, "status"))), "indispo") {
			status = "inaccessible"
		}
		result = append(result, model.AccessibilityIncident{
			ID: identifier, Location: providerFixtureDefault(providerJSONText(providerJSONFirst(row, "location", "arret")), "unknown"),
			Status: status, Description: providerJSONText(providerJSONFirst(row, "description", "message")),
		})
	}
	return result, nil
}

func (fixtures *Fixtures) CheckStop(ctx context.Context, stopID string) (string, error) {
	incidents, err := fixtures.Incidents(ctx, nil)
	if err != nil {
		return "unknown", err
	}
	requested := model.Normalize(stopID)
	for _, incident := range incidents {
		location := model.Normalize(incident.Location)
		if requested != "" && location != "" && (strings.Contains(requested, location) || strings.Contains(location, requested)) {
			return incident.Status, nil
		}
	}
	return "unknown", nil
}

func (fixtures *Fixtures) Stations(ctx context.Context, point model.Point, radiusM float64, limit int) ([]model.VelovStation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, err := fixtures.readJSON("datagrandlyon/velov.json")
	if err != nil {
		return []model.VelovStation{}, err
	}
	if !found {
		return []model.VelovStation{}, errors.New("required fixture datagrandlyon/velov.json is missing")
	}
	result := []model.VelovStation{}
	for _, row := range providerFixtureRows(payload) {
		latitude, _ := providerFixtureFloat(row, "lat", "latitude")
		longitude, _ := providerFixtureFloat(row, "lng", "lon", "longitude")
		distance := model.Distance(point, model.Point{Latitude: latitude, Longitude: longitude})
		if distance > radiusM {
			continue
		}
		status := "closed"
		if strings.EqualFold(providerFixtureDefault(providerJSONText(providerJSONFirst(row, "status")), "OPEN"), "OPEN") {
			status = "open"
		}
		result = append(result, model.VelovStation{
			ID:       providerFixtureDefault(providerJSONText(providerJSONFirst(row, "number", "id", "name")), "station"),
			Name:     providerFixtureDefault(providerJSONText(providerJSONFirst(row, "name")), "Vélo'v"),
			Latitude: latitude, Longitude: longitude,
			BikesAvailable: providerFixtureIntDefault(row, 0, "available_bikes", "bikes"),
			DocksAvailable: providerFixtureIntDefault(row, 0, "available_bike_stands", "docks"),
			Capacity:       providerFixtureIntDefault(row, 0, "bike_stands", "capacity"),
			Status:         status,
			ObservedAt:     providerJSONTime(providerJSONFirst(row, "observed_at", "last_update", "updated_at")),
			DistanceM:      model.Ptr(distance),
		})
	}
	sort.SliceStable(result, func(i, j int) bool { return *result[i].DistanceM < *result[j].DistanceM })
	return providerLimit(result, limit), nil
}

func (fixtures *Fixtures) Parkings(ctx context.Context, point model.Point, types []string, radiusM float64, minimumSpaces, limit int) ([]model.ParkingOption, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, err := fixtures.readJSON("datagrandlyon/parkings.json")
	if err != nil || !found {
		return []model.ParkingOption{}, err
	}
	allowed := providerStringSet(types)
	result := []model.ParkingOption{}
	for _, row := range providerFixtureRows(payload) {
		name := providerFixtureDefault(providerJSONText(providerJSONFirst(row, "name")), "Parking")
		parkingType := "public_parking"
		if strings.Contains(model.Normalize(providerJSONText(providerJSONFirst(row, "type"))), "relais") || strings.Contains(strings.ToLower(name), "p+r") {
			parkingType = "park_and_ride"
		}
		if len(allowed) > 0 && !allowed[parkingType] {
			continue
		}
		latitude, _ := providerFixtureFloat(row, "lat", "latitude")
		longitude, _ := providerFixtureFloat(row, "lon", "longitude", "lng")
		distance := model.Distance(point, model.Point{Latitude: latitude, Longitude: longitude})
		if distance > radiusM {
			continue
		}
		available := providerFixtureOptionalInt(row, "available", "places_disponibles")
		if available != nil && *available < minimumSpaces {
			continue
		}
		capacity := providerFixtureOptionalInt(row, "capacity", "capacite")
		occupied := providerFixtureOptionalInt(row, "occupied", "occupied_spaces")
		var ratio *float64
		if capacity != nil && *capacity > 0 && available != nil {
			value := float64(*available) / float64(*capacity)
			ratio = &value
		}
		status := "unknown"
		if available != nil && *available == 0 {
			status = "full"
		} else if available != nil {
			status = "open"
		}
		parking := model.ParkingOption{
			ID:   providerFixtureDefault(providerJSONText(providerJSONFirst(row, "id", "name")), name),
			Name: name, Type: parkingType, Latitude: latitude, Longitude: longitude,
			Capacity: capacity, AvailableSpaces: available, OccupiedSpaces: occupied,
			AvailabilityRatio: ratio, Status: status, DistanceM: model.Ptr(distance), Realtime: available != nil,
		}
		if parking.Realtime {
			parking.ObservedAt = model.Ptr(model.Now())
		}
		result = append(result, parking)
	}
	sort.SliceStable(result, func(i, j int) bool { return *result[i].DistanceM < *result[j].DistanceM })
	return providerLimit(result, limit), nil
}

func (fixtures *Fixtures) Traffic(ctx context.Context, area string) ([]model.TrafficCondition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, err := fixtures.readJSON("datagrandlyon/traffic.json")
	if err != nil || !found {
		return []model.TrafficCondition{}, err
	}
	result := []model.TrafficCondition{}
	for index, row := range providerFixtureRows(payload) {
		itemArea := providerFixtureDefault(providerJSONText(providerJSONFirst(row, "area", "secteur")), "Lyon")
		if area != "" && !strings.Contains(model.Normalize(itemArea), model.Normalize(area)) {
			continue
		}
		result = append(result, model.TrafficCondition{
			ID:   providerFixtureDefault(providerJSONText(providerJSONFirst(row, "id")), fmt.Sprint(index)),
			Area: itemArea, Level: providerFixtureDefault(providerJSONText(providerJSONFirst(row, "level", "etat")), "unknown"),
			Description: providerJSONText(providerJSONFirst(row, "description")),
		})
	}
	return result, nil
}

func (fixtures *Fixtures) RoadEvents(ctx context.Context, area string) ([]model.RoadEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, err := fixtures.readJSON("datagrandlyon/road_events.json")
	if err != nil || !found {
		return []model.RoadEvent{}, err
	}
	result := []model.RoadEvent{}
	for index, row := range providerFixtureRows(payload) {
		title := providerFixtureDefault(providerJSONText(providerJSONFirst(row, "title", "nom")), "Événement")
		if area != "" && !strings.Contains(model.Normalize(title+" "+providerJSONText(providerJSONFirst(row, "description"))), model.Normalize(area)) {
			continue
		}
		eventType := "incident"
		if strings.Contains(model.Normalize(providerJSONText(providerJSONFirst(row, "type"))), "chantier") {
			eventType = "roadworks"
		}
		result = append(result, model.RoadEvent{
			ID:   providerFixtureDefault(providerJSONText(providerJSONFirst(row, "id")), fmt.Sprint(index)),
			Type: eventType, Title: title, Description: providerJSONText(providerJSONFirst(row, "description")),
			Latitude: providerFixtureOptionalFloat(row, "lat", "latitude"), Longitude: providerFixtureOptionalFloat(row, "lon", "longitude"),
		})
	}
	return result, nil
}

func (fixtures *Fixtures) Facilities(ctx context.Context, point model.Point, categories []string, radiusM float64, limitPerCategory int) ([]model.Facility, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, err := fixtures.readJSON("datagrandlyon/facilities.json")
	if err != nil || !found {
		return []model.Facility{}, err
	}
	allowed := providerStringSet(categories)
	counts := map[string]int{}
	result := []model.Facility{}
	for _, row := range providerFixtureRows(payload) {
		category := providerFixtureCategory(providerJSONText(providerJSONFirst(row, "category", "type")))
		if category == "" || (len(allowed) > 0 && !allowed[category]) || counts[category] >= limitPerCategory {
			continue
		}
		latitude, _ := providerFixtureFloat(row, "lat", "latitude")
		longitude, _ := providerFixtureFloat(row, "lon", "longitude")
		distance := model.Distance(point, model.Point{Latitude: latitude, Longitude: longitude})
		if distance > radiusM {
			continue
		}
		facility := model.Facility{
			ID:       providerFixtureDefault(providerJSONText(providerJSONFirst(row, "id", "name")), category),
			Name:     providerFixtureDefault(providerJSONText(providerJSONFirst(row, "name")), category),
			Category: category, Latitude: latitude, Longitude: longitude,
			DistanceM: model.Ptr(distance), OpenStatus: "unknown",
		}
		if hours := providerJSONText(providerJSONFirst(row, "hours", "opening_hours")); hours != "" {
			facility.OpeningHoursRaw = model.Ptr(hours)
		}
		counts[category]++
		result = append(result, facility)
	}
	sort.SliceStable(result, func(i, j int) bool { return *result[i].DistanceM < *result[j].DistanceM })
	return result, nil
}

func (fixtures *Fixtures) Indicators(ctx context.Context, point model.Point, indicators []string, at time.Time) ([]model.IndicatorValue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, err := fixtures.readJSON("datagrandlyon/environment.json")
	if err != nil {
		return nil, err
	}
	object, _ := payload.(map[string]any)
	supported := providerStringSet(providerStringList(providerJSONFirst(object, "supported")))
	if at.IsZero() {
		at = model.Now()
	}
	result := make([]model.IndicatorValue, 0, len(indicators))
	for _, indicator := range indicators {
		block, exists := object[indicator]
		if !found || (!supported[indicator] && !exists) {
			result = append(result, model.IndicatorValue{Indicator: indicator, Supported: false})
			continue
		}
		value := block
		var unit, zone string
		if details, ok := block.(map[string]any); ok {
			value = providerJSONFirst(details, "value")
			unit = providerJSONText(providerJSONFirst(details, "unit"))
			zone = providerFixtureDefault(providerJSONText(providerJSONFirst(details, "zone")), "Lyon")
		}
		item := model.IndicatorValue{
			Indicator: indicator, Supported: true, ObservedAt: model.Ptr(at),
			SourceID: model.Ptr("env_" + indicator),
		}
		switch value := value.(type) {
		case string, float64, json.Number:
			item.Value = value
		}
		if unit != "" {
			item.Unit = model.Ptr(unit)
		}
		if zone != "" {
			item.Zone = model.Ptr(zone)
		}
		result = append(result, item)
	}
	return result, nil
}

func (fixtures *Fixtures) WasteFacilities(ctx context.Context, point model.Point, category string, radiusM float64, limit int) ([]model.WasteFacility, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, found, err := fixtures.readJSON("datagrandlyon/waste_facilities.json")
	if err != nil || !found {
		return []model.WasteFacility{}, err
	}
	result := []model.WasteFacility{}
	for _, row := range providerFixtureRows(payload) {
		accepted := providerStringList(providerJSONFirst(row, "categories", "accepted_categories"))
		if len(accepted) > 0 && category != "unknown" && !providerStringSet(accepted)[category] {
			continue
		}
		latitude, _ := providerFixtureFloat(row, "lat", "latitude")
		longitude, _ := providerFixtureFloat(row, "lon", "longitude")
		distance := model.Distance(point, model.Point{Latitude: latitude, Longitude: longitude})
		if distance > radiusM {
			continue
		}
		facility := model.WasteFacility{
			ID:       providerFixtureDefault(providerJSONText(providerJSONFirst(row, "id", "name")), "facility"),
			Name:     providerFixtureDefault(providerJSONText(providerJSONFirst(row, "name")), "Déchèterie"),
			Latitude: latitude, Longitude: longitude, AcceptedCategories: accepted,
			DistanceM: model.Ptr(distance), OpenStatus: providerFixtureDefault(providerJSONText(providerJSONFirst(row, "open_status")), "unknown"),
		}
		if hours := providerJSONText(providerJSONFirst(row, "opening_hours", "hours")); hours != "" {
			facility.OpeningHoursRaw = model.Ptr(hours)
		}
		result = append(result, facility)
	}
	sort.SliceStable(result, func(i, j int) bool { return *result[i].DistanceM < *result[j].DistanceM })
	return providerLimit(result, limit), nil
}

func (fixtures *Fixtures) Plan(ctx context.Context, origin, destination model.Point, departure time.Time, modes []string, preferences model.TripPreferences) ([]model.TripOption, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrFixtureRoutingUnavailable
}

func (fixtures *Fixtures) readJSON(name string) (any, bool, error) {
	if fixtures.files == nil {
		return nil, false, errors.New("fixture filesystem is nil")
	}
	data, err := fs.ReadFile(fixtures.files, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read fixture %s: %w", name, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, true, fmt.Errorf("decode fixture %s: %w", name, err)
	}
	return payload, true, nil
}

func providerFixtureRows(payload any) []map[string]any {
	if object, ok := payload.(map[string]any); ok {
		return providerJSONMaps(providerJSONFirst(object, "values"))
	}
	return providerJSONMaps(payload)
}

func providerFixtureFloat(object map[string]any, keys ...string) (float64, bool) {
	return providerJSONFloat(providerJSONFirst(object, keys...))
}

func providerFixtureOptionalFloat(object map[string]any, keys ...string) *float64 {
	value, ok := providerFixtureFloat(object, keys...)
	if !ok {
		return nil
	}
	return &value
}

func providerFixtureOptionalInt(object map[string]any, keys ...string) *int {
	value := providerJSONFirst(object, keys...)
	if value == nil {
		return nil
	}
	number, ok := providerJSONFloat(value)
	if !ok {
		return nil
	}
	result := int(number)
	return &result
}

func providerFixtureIntDefault(object map[string]any, fallback int, keys ...string) int {
	if value := providerFixtureOptionalInt(object, keys...); value != nil {
		return *value
	}
	return fallback
}

func providerFixtureDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func providerFixtureCategory(value string) string {
	switch model.Normalize(value) {
	case "toilet", "toilettes":
		return "toilet"
	case "fontaine", "drinking water":
		return "drinking_water"
	case "parc", "park":
		return "park"
	case "bike pump", "pompe a velo":
		return "bike_pump"
	case "velov station", "station velov":
		return "velov_station"
	default:
		return ""
	}
}

func providerStringList(value any) []string {
	result := []string{}
	for _, item := range providerJSONItems(value) {
		text := providerJSONText(item)
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func providerStringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func providerStopMatches(requested, observed string) bool {
	trim := func(value string) string {
		value = strings.TrimRight(strings.TrimSpace(value), ":")
		if index := strings.LastIndex(value, ":"); index >= 0 {
			value = value[index+1:]
		}
		return strings.ToLower(value)
	}
	return trim(requested) != "" && trim(requested) == trim(observed)
}

func providerAlertMatchesLines(alert model.TransitAlert, requested []string) bool {
	for _, query := range requested {
		for _, line := range alert.Lines {
			if model.LineMatches(query, line, line) {
				return true
			}
		}
	}
	return false
}

func providerLimit[T any](values []T, limit int) []T {
	if limit <= 0 || limit >= len(values) {
		return values
	}
	return values[:limit]
}
