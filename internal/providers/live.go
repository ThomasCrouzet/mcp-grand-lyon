// Package providers connects normalized domain data to approved upstream services.
package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/config"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/httpclient"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/storage"
)

const (
	dataPusherBase = "https://data.grandlyon.com/fr/datapusher/ws"
	photonURL      = "https://download.data.grandlyon.com/geocoding/photon-bal/api"
	siriETURL      = "https://data.grandlyon.com/siri-lite/2.0/estimated-timetables.json"
	siriSXURL      = "https://data.grandlyon.com/siri-lite/2.0/situation-exchange.json"
	parkingOGC     = "metropole-de-lyon:parkings-de-la-metropole-de-lyon-disponibilites-temps-reel-v2"
	jsonLimit      = 8 * 1024 * 1024
)

var tableNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+\.[A-Za-z0-9_]+$`)
var collectionNamePattern = regexp.MustCompile(`^[a-z0-9-]+(?::[a-z0-9_.-]+)?$`)

var errSourceDisabled = errors.New("source is disabled")

type resolvedSource struct {
	Service       string
	Table         string
	Collection    string
	TableResolved bool
}

// Live implements model.Provider with live, bounded, read-only sources.
type Live struct {
	client *httpclient.Client
	store  *storage.Store
	cfg    config.Config

	velovMu         sync.Mutex
	velov           []model.VelovStation
	velovExpires    time.Time
	parkingMu       sync.Mutex
	parkingLive     map[string]int
	parkingRows     []map[string]any
	parkingObserved *time.Time
	parkingExpires  time.Time
	parkingErr      error
}

func NewLive(client *httpclient.Client, store *storage.Store, cfg config.Config) *Live {
	return &Live{client: client, store: store, cfg: cfg}
}

func (live *Live) resolveSource(ctx context.Context, sourceID, defaultService, defaultTable string) (resolvedSource, error) {
	result := resolvedSource{Service: defaultService, Table: defaultTable}
	if live.store == nil {
		return result, nil
	}
	source, err := live.store.GetSource(ctx, sourceID)
	if err != nil {
		return resolvedSource{}, fmt.Errorf("read source %s: %w", sourceID, err)
	}
	if source == nil {
		return result, nil
	}
	if enabled, ok := source["enabled"].(bool); ok && !enabled {
		return resolvedSource{}, fmt.Errorf("%s: %w", sourceID, errSourceDisabled)
	}
	if service, ok := source["service"].(string); ok && strings.TrimSpace(service) != "" {
		result.Service = strings.TrimSpace(service)
	}
	if result.Service != "" && result.Service != "rdata" && result.Service != "grandlyon" {
		return resolvedSource{}, fmt.Errorf("source %s has an invalid service", sourceID)
	}
	if table, ok := source["table_name"].(string); ok && strings.TrimSpace(table) != "" {
		result.Table = strings.TrimSpace(table)
		result.TableResolved = true
	}
	if result.Table != "" && !tableNamePattern.MatchString(result.Table) {
		return resolvedSource{}, fmt.Errorf("source %s has an invalid table", sourceID)
	}
	if collection, ok := source["collection"].(string); ok && strings.TrimSpace(collection) != "" {
		result.Collection = strings.TrimSpace(collection)
		if !collectionNamePattern.MatchString(result.Collection) {
			return resolvedSource{}, fmt.Errorf("source %s has an invalid collection", sourceID)
		}
	}
	return result, nil
}

func (live *Live) requireSource(ctx context.Context, sourceID string) error {
	_, err := live.resolveSource(ctx, sourceID, "", "")
	return err
}

func (live *Live) querySourceTable(ctx context.Context, sourceID, service, table string, maximum int) ([]map[string]any, error) {
	source, err := live.resolveSource(ctx, sourceID, service, table)
	if err != nil {
		return nil, err
	}
	return live.queryTable(ctx, source.Service, source.Table, maximum)
}

func (live *Live) resolvePublicParkingSource(ctx context.Context) (resolvedSource, error) {
	if live.store != nil {
		source, err := live.store.GetSource(ctx, "parking_capacity")
		if err != nil {
			return resolvedSource{}, fmt.Errorf("read source parking_capacity: %w", err)
		}
		if source != nil {
			return live.resolveSource(ctx, "parking_capacity", "grandlyon", "pvo_patrimoine_voirie.pvoparking")
		}
	}
	return live.resolveSource(ctx, "parking_realtime", "grandlyon", "pvo_patrimoine_voirie.pvoparking")
}

func (live *Live) getJSON(ctx context.Context, rawURL string, auth bool) (any, error) {
	response, err := live.client.Get(ctx, rawURL, auth, jsonLimit)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upstream returned HTTP %d", response.StatusCode)
	}
	var decoded any
	if err := json.Unmarshal(response.Body, &decoded); err != nil {
		return nil, fmt.Errorf("decode upstream JSON: %w", err)
	}
	return decoded, nil
}

func (live *Live) queryTable(ctx context.Context, service, table string, maximum int) ([]map[string]any, error) {
	if service != "rdata" && service != "grandlyon" {
		return nil, errors.New("unsupported DataPusher service")
	}
	if !tableNamePattern.MatchString(table) {
		return nil, errors.New("invalid DataPusher table")
	}
	maximum = bound(maximum, 1, 200)
	query := url.Values{"compact": {"false"}, "maxfeatures": {strconv.Itoa(maximum)}}
	rawURL := dataPusherBase + "/" + service + "/" + url.PathEscape(table) + "/all.json?" + query.Encode()
	decoded, err := live.getJSON(ctx, rawURL, true)
	if err != nil {
		return nil, err
	}
	return extractRows(decoded), nil
}

func extractRows(decoded any) []map[string]any {
	if values, ok := decoded.([]any); ok {
		return objectRows(values, false)
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return []map[string]any{}
	}
	for _, key := range []string{"values", "results", "features"} {
		if values, ok := root[key].([]any); ok {
			return objectRows(values, key == "features")
		}
	}
	return []map[string]any{}
}

func objectRows(values []any, featureProperties bool) []map[string]any {
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if featureProperties {
			properties, ok := row["properties"].(map[string]any)
			if !ok {
				continue
			}
			row = cloneRow(properties)
			if geometry, ok := value.(map[string]any)["geometry"].(map[string]any); ok && text(geometry, "type") == "Point" {
				coordinates, _ := geometry["coordinates"].([]any)
				if len(coordinates) >= 2 {
					longitude, longitudeOK := number(map[string]any{"value": coordinates[0]}, "value")
					latitude, latitudeOK := number(map[string]any{"value": coordinates[1]}, "value")
					if longitudeOK && latitudeOK && validGeographicCoordinates(latitude, longitude) {
						row["lon"], row["lat"] = longitude, latitude
					}
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func (live *Live) Search(ctx context.Context, query string, near *model.Point, types []string, limit int) ([]model.PlaceCandidate, error) {
	if err := live.requireSource(ctx, "photon"); err != nil {
		return nil, err
	}
	values := url.Values{"q": {query}, "limit": {strconv.Itoa(bound(limit, 1, 20))}}
	if near != nil {
		values.Set("lat", strconv.FormatFloat(near.Latitude, 'f', -1, 64))
		values.Set("lon", strconv.FormatFloat(near.Longitude, 'f', -1, 64))
	}
	decoded, err := live.getJSON(ctx, photonURL+"?"+values.Encode(), false)
	if err != nil {
		return nil, err
	}
	candidates, err := ParsePhoton(decoded)
	if err != nil {
		return nil, err
	}
	return filterCandidateTypes(candidates, types, bound(limit, 1, 20)), nil
}

func (live *Live) Reverse(ctx context.Context, point model.Point) (*model.PlaceCandidate, error) {
	if err := live.requireSource(ctx, "photon"); err != nil {
		return nil, err
	}
	values := url.Values{"lat": {strconv.FormatFloat(point.Latitude, 'f', -1, 64)}, "lon": {strconv.FormatFloat(point.Longitude, 'f', -1, 64)}, "limit": {"1"}}
	decoded, err := live.getJSON(ctx, photonURL+"?"+values.Encode(), false)
	if err != nil {
		return nil, err
	}
	candidates, err := ParsePhoton(decoded)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	return &candidates[0], nil
}

func filterCandidateTypes(values []model.PlaceCandidate, types []string, limit int) []model.PlaceCandidate {
	allowed := make(map[string]struct{}, len(types))
	for _, value := range types {
		allowed[value] = struct{}{}
	}
	result := make([]model.PlaceCandidate, 0, min(len(values), limit))
	for _, candidate := range values {
		if len(allowed) != 0 {
			if _, ok := allowed[candidate.Type]; !ok {
				continue
			}
		}
		result = append(result, candidate)
		if len(result) == limit {
			break
		}
	}
	return result
}

func (live *Live) Departures(ctx context.Context, stopID, line, direction string, limit int) ([]model.Departure, error) {
	limit = bound(limit, 1, 20)
	source, sourceErr := live.resolveSource(ctx, "tcl_departures", "rdata", "tcl_sytral.tclpassagearret")
	if sourceErr != nil {
		return nil, sourceErr
	}
	stop := live.resolveStop(ctx, stopID)
	decoded, siriErr := live.getJSON(ctx, siriETURL, true)
	departures := []model.Departure{}
	if siriErr == nil {
		departures, siriErr = ParseSIRIDepartures(decoded)
		if siriErr != nil {
			siriErr = fmt.Errorf("parse SIRI departures: %w", siriErr)
		} else {
			departures = filterDepartures(departures, stopID, stop, line, direction)
		}
	}
	if len(departures) < limit {
		rows, tableErr := live.queryTable(ctx, source.Service, source.Table, 80)
		if tableErr == nil {
			departures = append(departures, mapPassageRows(rows, stopID, stop, line, direction)...)
		} else if siriErr != nil {
			return nil, errors.Join(siriErr, tableErr)
		}
	}
	return dedupeDepartures(departures, limit), nil
}

func (live *Live) resolveStop(ctx context.Context, stopID string) *model.PlaceCandidate {
	if live.store == nil || strings.TrimSpace(stopID) == "" {
		return nil
	}
	stop, err := live.store.GetEntity(ctx, stopID)
	if err != nil || stop == nil {
		return nil
	}
	return stop
}

func filterDepartures(values []model.Departure, stopID string, stop *model.PlaceCandidate, line, direction string) []model.Departure {
	stopID = strings.TrimPrefix(stopID, "gtfs:stop:")
	result := []model.Departure{}
	for _, departure := range values {
		if stopID != "" {
			matched := departure.StopRef != nil && sameStop(stopID, *departure.StopRef)
			if !matched && stop != nil && departure.StopRef != nil {
				matched = sameStop(stop.Name, *departure.StopRef)
			}
			if !matched {
				continue
			}
		}
		if line != "" && !model.LineMatches(line, departure.LineName, departure.LineID) {
			continue
		}
		if direction != "" && !strings.Contains(strings.ToLower(departure.Destination), strings.ToLower(direction)) {
			continue
		}
		result = append(result, departure)
	}
	return result
}

func mapPassageRows(rows []map[string]any, stopID string, stop *model.PlaceCandidate, line, direction string) []model.Departure {
	result := []model.Departure{}
	requestedStop := strings.TrimPrefix(stopID, "gtfs:stop:")
	for _, row := range rows {
		lineName := text(row, "ligne", "line")
		destination := text(row, "direction", "destination")
		rowStop := text(row, "stop_id", "code_arret", "id", "arrets", "arret", "nom", "stop")
		if requestedStop != "" {
			matched := sameStop(requestedStop, rowStop)
			if !matched && stop != nil {
				matched = sameStop(stop.Name, rowStop)
			}
			if !matched && stop != nil {
				latitude, latitudeOK := number(row, "lat", "latitude")
				longitude, longitudeOK := number(row, "lon", "lng", "longitude")
				matched = latitudeOK && longitudeOK && validGeographicCoordinates(latitude, longitude) && model.Distance(model.Point{Latitude: stop.Latitude, Longitude: stop.Longitude}, model.Point{Latitude: latitude, Longitude: longitude}) <= 100
			}
			if !matched {
				continue
			}
		}
		if line != "" && !model.LineMatches(line, lineName) {
			continue
		}
		if direction != "" && !strings.Contains(strings.ToLower(destination), strings.ToLower(direction)) {
			continue
		}
		expected := parseTime(first(row, "heurepassage", "expected"))
		result = append(result, model.Departure{LineID: "tcl:" + lineName, LineName: lineName, Destination: destination, StopRef: optional(rowStop), ScheduledAt: expected, ExpectedAt: expected, DelaySeconds: parseDelay(first(row, "delaipassage")), Realtime: true})
	}
	return result
}

func sameStop(requested, actual string) bool {
	requested = model.Normalize(strings.TrimPrefix(requested, "gtfs:stop:"))
	actual = model.Normalize(strings.TrimPrefix(actual, "gtfs:stop:"))
	return requested != "" && actual != "" && (requested == actual || strings.HasSuffix(actual, ":"+requested))
}

func dedupeDepartures(values []model.Departure, limit int) []model.Departure {
	seen := map[string]struct{}{}
	result := []model.Departure{}
	for _, departure := range values {
		key := departure.LineName + "|" + departure.Destination + "|"
		if departure.ExpectedAt != nil {
			key += departure.ExpectedAt.Format(time.RFC3339Nano)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, departure)
		if len(result) == limit {
			break
		}
	}
	return result
}

func (live *Live) Alerts(ctx context.Context, lines []string) ([]model.TransitAlert, error) {
	source, sourceErr := live.resolveSource(ctx, "tcl_alerts", "rdata", "tcl_sytral.tclalertetrafic_2")
	if sourceErr != nil {
		return nil, sourceErr
	}
	rows, tableErr := live.queryTable(ctx, source.Service, source.Table, 50)
	alerts := mapAlertRows(rows)
	if len(alerts) == 0 {
		decoded, siriErr := live.getJSON(ctx, siriSXURL, true)
		if siriErr == nil {
			alerts, siriErr = ParseSIRIAlerts(decoded)
			if siriErr != nil {
				return nil, fmt.Errorf("parse SIRI alerts: %w", siriErr)
			}
		} else if tableErr != nil {
			return nil, errors.Join(tableErr, siriErr)
		}
	}
	return filterAlerts(alerts, lines), nil
}

func mapAlertRows(rows []map[string]any) []model.TransitAlert {
	result := make([]model.TransitAlert, 0, len(rows))
	for index, row := range rows {
		severity := map[string]string{"1": "info", "2": "minor", "3": "major", "4": "critical", "info": "info", "mineur": "minor", "majeur": "major", "critique": "critical"}[strings.ToLower(text(row, "niveauseverite", "severity", "type"))]
		if severity == "" {
			severity = "unknown"
		}
		lineText := strings.ReplaceAll(text(row, "ligne_cli", "ligne_com"), ";", ",")
		lines := []string{}
		for _, line := range strings.Split(lineText, ",") {
			if line = strings.TrimSpace(line); line != "" {
				lines = append(lines, line)
			}
		}
		result = append(result, model.TransitAlert{ID: firstText(row, strconv.Itoa(index), "n", "id"), Severity: severity, Title: firstText(row, "Alerte TCL", "titre", "title"), Description: text(row, "message", "cause", "description"), Lines: lines, StartAt: parseTime(first(row, "debut")), EndAt: parseTime(first(row, "fin"))})
	}
	return result
}

func filterAlerts(alerts []model.TransitAlert, lines []string) []model.TransitAlert {
	if len(lines) == 0 {
		return alerts
	}
	result := []model.TransitAlert{}
	for _, alert := range alerts {
		if len(alert.Lines) == 0 {
			result = append(result, alert)
			continue
		}
		for _, requested := range lines {
			matched := false
			for _, line := range alert.Lines {
				matched = matched || model.LineMatches(requested, line)
			}
			if matched {
				result = append(result, alert)
				break
			}
		}
	}
	return result
}

func (live *Live) Incidents(ctx context.Context, lines []string) ([]model.AccessibilityIncident, error) {
	rows, err := live.querySourceTable(ctx, "tcl_accessibility_alerts", "rdata", "tcl_sytral.tclalerteaccessibilite", 50)
	if err != nil {
		return nil, err
	}
	result := make([]model.AccessibilityIncident, 0, len(rows))
	for index, row := range rows {
		result = append(result, model.AccessibilityIncident{ID: firstText(row, strconv.Itoa(index), "id", "gid"), Location: firstText(row, "unknown", "code_station", "code_lieu", "equipement"), Status: "inaccessible", Description: text(row, "consequence", "cause", "equipement"), StartAt: parseTime(first(row, "debut_indispo")), EndAt: parseTime(first(row, "fin_indispo"))})
	}
	return result, nil
}

func (live *Live) CheckStop(ctx context.Context, stopID string) (string, error) {
	incidents, err := live.Incidents(ctx, nil)
	if err != nil {
		return "unknown", err
	}
	for _, incident := range incidents {
		if strings.Contains(model.Normalize(incident.Location), model.Normalize(stopID)) || strings.Contains(model.Normalize(stopID), model.Normalize(incident.Location)) {
			return "inaccessible", nil
		}
	}
	return "unknown", nil
}

func (live *Live) Stations(ctx context.Context, point model.Point, radius float64, limit int) ([]model.VelovStation, error) {
	stations, err := live.allVelov(ctx)
	if err != nil {
		return nil, err
	}
	result := []model.VelovStation{}
	for _, station := range stations {
		distance := model.Distance(point, model.Point{Latitude: station.Latitude, Longitude: station.Longitude})
		if distance <= radius {
			station.DistanceM = &distance
			result = append(result, station)
		}
	}
	sort.Slice(result, func(i, j int) bool { return *result[i].DistanceM < *result[j].DistanceM })
	return result[:min(len(result), bound(limit, 1, 20))], nil
}

func (live *Live) allVelov(ctx context.Context) ([]model.VelovStation, error) {
	live.velovMu.Lock()
	defer live.velovMu.Unlock()
	if time.Now().Before(live.velovExpires) {
		return append([]model.VelovStation(nil), live.velov...), nil
	}
	rows, err := live.querySourceTable(ctx, "velov_realtime", "rdata", "jcd_jcdecaux.jcdvelov", 200)
	if err != nil {
		return nil, err
	}
	result := []model.VelovStation{}
	for _, row := range rows {
		latitude, latitudeOK := number(row, "lat", "latitude")
		longitude, longitudeOK := number(row, "lng", "lon", "longitude")
		if !latitudeOK || !longitudeOK {
			continue
		}
		status := "closed"
		if value := strings.ToUpper(firstText(row, "OPEN", "status", "availability")); value == "OPEN" || value == "DISPONIBLE" || value == "GREEN" || value == "1" {
			status = "open"
		}
		observed := parseTime(first(row, "observed_at", "last_update", "last_update_fme", "last_update_gl", "last_updated", "updated_at", "lastupdate", "timestamp"))
		result = append(result, model.VelovStation{ID: firstText(row, "", "number", "id", "name"), Name: firstText(row, "Vélo'v", "name"), Latitude: latitude, Longitude: longitude, BikesAvailable: integer(row, 0, "available_bikes", "bikes"), DocksAvailable: integer(row, 0, "available_bike_stands", "docks"), Capacity: integer(row, 0, "bike_stands", "capacity"), Status: status, ObservedAt: observed})
	}
	live.velov = result
	live.velovExpires = time.Now().Add(live.sourceTTL(ctx, "velov_realtime", 45*time.Second))
	return append([]model.VelovStation(nil), result...), nil
}

func (live *Live) Traffic(context.Context, string) ([]model.TrafficCondition, error) {
	return nil, errors.New("live traffic conditions are unresolved")
}

func (live *Live) RoadEvents(ctx context.Context, area string) ([]model.RoadEvent, error) {
	rows, err := live.querySourceTable(ctx, "road_events", "grandlyon", "pvo_patrimoine_voirie.pvochantierperturbant", 50)
	if err != nil {
		return nil, err
	}
	result := []model.RoadEvent{}
	area = strings.ToLower(strings.TrimSpace(area))
	for index, row := range rows {
		title := firstText(row, "Chantier", "nom", "title")
		description := text(row, "descripchantierinternet", "description")
		commune := text(row, "commune1", "commune")
		if area != "" && !strings.Contains(strings.ToLower(title+" "+description+" "+commune), area) {
			continue
		}
		latitude, latitudeOK := number(row, "lat", "latitude", "y")
		longitude, longitudeOK := number(row, "lon", "lng", "longitude", "x")
		result = append(result, model.RoadEvent{ID: firstText(row, strconv.Itoa(index), "gid", "id"), Type: "roadworks", Title: title, Description: description, Latitude: numberPointer(latitude, latitudeOK), Longitude: numberPointer(longitude, longitudeOK), StartAt: parseTime(first(row, "debutchantier", "start")), EndAt: parseTime(first(row, "finchantier", "end"))})
	}
	return result, nil
}

func (live *Live) Indicators(context.Context, model.Point, []string, time.Time) ([]model.IndicatorValue, error) {
	return nil, errors.New("live environment indicators are unresolved")
}

func (live *Live) Facilities(ctx context.Context, point model.Point, categories []string, radius float64, limitPerCategory int) ([]model.Facility, error) {
	tables := map[string][4]string{
		"toilet":         {"toilets", "grandlyon", "adr_voie_lieu.adrtoilettepublique_latest", "adresse"},
		"drinking_water": {"drinking_water", "grandlyon", "adr_voie_lieu.adrbornefontaine_latest", "identifiant"},
		"bike_pump":      {"bike_pumps", "grandlyon", "pvo_patrimoine_voirie.pvostationvelovpompe", "nom"},
	}
	result := []model.Facility{}
	requestedSources := 0
	successfulSources := 0
	var sourceErrors []error
	for _, category := range categories {
		if category == "velov_station" {
			requestedSources++
			stations, err := live.Stations(ctx, point, radius, limitPerCategory)
			if err != nil {
				sourceErrors = append(sourceErrors, err)
				continue
			}
			successfulSources++
			for _, station := range stations {
				result = append(result, model.Facility{ID: station.ID, Name: station.Name, Category: category, Latitude: station.Latitude, Longitude: station.Longitude, DistanceM: station.DistanceM, OpenStatus: station.Status})
			}
			continue
		}
		if category == "waste_facility" {
			requestedSources++
			facilities, err := live.WasteFacilities(ctx, point, "unknown", radius, limitPerCategory)
			if err != nil {
				sourceErrors = append(sourceErrors, err)
				continue
			}
			successfulSources++
			for _, facility := range facilities {
				result = append(result, model.Facility{ID: facility.ID, Name: facility.Name, Category: category, Latitude: facility.Latitude, Longitude: facility.Longitude, DistanceM: facility.DistanceM, OpenStatus: facility.OpenStatus, OpeningHoursRaw: facility.OpeningHoursRaw})
			}
			continue
		}
		table, ok := tables[category]
		if !ok {
			requestedSources++
			sourceErrors = append(sourceErrors, fmt.Errorf("live facility category %s is unresolved", category))
			continue
		}
		requestedSources++
		rows, err := live.querySourceTable(ctx, table[0], table[1], table[2], 100)
		if err != nil {
			sourceErrors = append(sourceErrors, err)
			continue
		}
		successfulSources++
		count := 0
		for _, row := range rows {
			latitude, latitudeOK := number(row, "lat", "latitude")
			longitude, longitudeOK := number(row, "lon", "lng", "longitude")
			if !latitudeOK || !longitudeOK {
				continue
			}
			distance := model.Distance(point, model.Point{Latitude: latitude, Longitude: longitude})
			if distance > radius || count >= bound(limitPerCategory, 1, 20) {
				continue
			}
			name := firstText(row, facilityDefault(category), table[3], "adresse", "nom", "name")
			result = append(result, model.Facility{ID: firstText(row, category+"-"+strconv.Itoa(count), "gid", "id", table[3]), Name: name, Category: category, Latitude: latitude, Longitude: longitude, DistanceM: &distance, OpenStatus: "unknown", OpeningHoursRaw: optional(text(row, "opening_hours", "openinghoursspecification", "horaires"))})
			count++
		}
	}
	sort.Slice(result, func(i, j int) bool { return *result[i].DistanceM < *result[j].DistanceM })
	if len(sourceErrors) != 0 {
		if requestedSources != 0 && successfulSources == 0 {
			return nil, errors.Join(sourceErrors...)
		}
		return result, errors.Join(sourceErrors...)
	}
	return result, nil
}

func (live *Live) WasteFacilities(ctx context.Context, point model.Point, category string, radius float64, limit int) ([]model.WasteFacility, error) {
	rows, err := live.querySourceTable(ctx, "waste_facilities", "grandlyon", "gip_proprete.gipdecheterie_3_0_0", 50)
	if err != nil {
		return nil, err
	}
	result := []model.WasteFacility{}
	for _, row := range rows {
		latitude, latitudeOK := number(row, "lat", "latitude")
		longitude, longitudeOK := number(row, "lon", "lng", "longitude")
		if !latitudeOK || !longitudeOK {
			continue
		}
		distance := model.Distance(point, model.Point{Latitude: latitude, Longitude: longitude})
		if distance > radius {
			continue
		}
		accepted := parseCategories(first(row, "allowed_waste", "accepted_categories", "categories"))
		result = append(result, model.WasteFacility{ID: firstText(row, "", "identifiant", "gid", "id", "adresse"), Name: facilityLabel(row), Latitude: latitude, Longitude: longitude, AcceptedCategories: accepted, DistanceM: &distance, OpenStatus: firstText(row, "unknown", "open_status"), OpeningHoursRaw: optional(text(row, "opening_hours", "openinghoursspecification", "horaires"))})
	}
	sort.Slice(result, func(i, j int) bool { return *result[i].DistanceM < *result[j].DistanceM })
	return result[:min(len(result), bound(limit, 1, 20))], nil
}

func (live *Live) Parkings(ctx context.Context, point model.Point, types []string, radius float64, minimumSpaces, limit int) ([]model.ParkingOption, error) {
	allowed := map[string]bool{"public_parking": len(types) == 0, "park_and_ride": len(types) == 0}
	for _, value := range types {
		allowed[value] = true
	}
	liveMap := map[string]int{}
	liveRows := []map[string]any{}
	var observed *time.Time
	var availabilityErr error
	if allowed["public_parking"] {
		liveMap, liveRows, observed, availabilityErr = live.parkingAvailability(ctx)
	}
	result := []model.ParkingOption{}
	requestedSources := 0
	successfulSources := 0
	sourceErrors := []error{}
	if availabilityErr != nil {
		sourceErrors = append(sourceErrors, availabilityErr)
	}
	if allowed["public_parking"] {
		requestedSources++
		publicSucceeded := len(liveRows) != 0
		seenNames := map[string]bool{}
		for _, row := range liveRows {
			option, ok := live.mapParking(ctx, row, "public_parking", point, radius, nil, nil)
			if !ok {
				continue
			}
			result = append(result, option)
			seenNames[model.Normalize(option.Name)] = true
		}
		source, sourceErr := live.resolvePublicParkingSource(ctx)
		var rows []map[string]any
		var err error
		if sourceErr != nil {
			err = sourceErr
		} else {
			rows, err = live.queryTable(ctx, source.Service, source.Table, 100)
		}
		if err != nil {
			sourceErrors = append(sourceErrors, err)
		} else {
			publicSucceeded = true
		}
		for _, row := range rows {
			option, ok := live.mapParking(ctx, row, "public_parking", point, radius, liveMap, observed)
			if ok && !seenNames[model.Normalize(option.Name)] {
				result = append(result, option)
			}
		}
		if publicSucceeded {
			successfulSources++
		}
	}
	if allowed["park_and_ride"] {
		requestedSources++
		rows, err := live.querySourceTable(ctx, "park_and_ride", "rdata", "tcl_sytral.tclparcrelaistr", 50)
		if err != nil {
			sourceErrors = append(sourceErrors, err)
		} else {
			successfulSources++
		}
		for _, row := range rows {
			option, ok := live.mapParking(ctx, row, "park_and_ride", point, radius, nil, nil)
			if ok {
				result = append(result, option)
			}
		}
	}
	if requestedSources != 0 && successfulSources == 0 {
		return nil, errors.Join(sourceErrors...)
	}
	filtered := result[:0]
	for _, option := range result {
		if option.AvailableSpaces != nil && *option.AvailableSpaces < minimumSpaces {
			continue
		}
		filtered = append(filtered, option)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if (filtered[i].AvailableSpaces != nil) != (filtered[j].AvailableSpaces != nil) {
			return filtered[i].AvailableSpaces != nil
		}
		return *filtered[i].DistanceM < *filtered[j].DistanceM
	})
	filtered = filtered[:min(len(filtered), bound(limit, 1, 20))]
	if len(sourceErrors) != 0 {
		return filtered, errors.Join(sourceErrors...)
	}
	return filtered, nil
}

func (live *Live) mapParking(ctx context.Context, row map[string]any, kind string, point model.Point, radius float64, liveMap map[string]int, observed *time.Time) (model.ParkingOption, bool) {
	latitude, longitude, coordinatesOK := parkingCoordinates(row)
	name := firstText(row, parkingDefault(kind), "nom", "name")
	if !coordinatesOK && kind == "park_and_ride" && live.store != nil {
		entities, err := live.store.SearchEntities(ctx, name, 1)
		if err == nil && len(entities) != 0 {
			latitude, longitude, coordinatesOK = entities[0].Latitude, entities[0].Longitude, true
		}
	}
	if !coordinatesOK {
		return model.ParkingOption{}, false
	}
	distance := model.Distance(point, model.Point{Latitude: latitude, Longitude: longitude})
	if distance > radius {
		return model.ParkingOption{}, false
	}
	capacity := optionalInteger(row, "capacite", "capacity", "nb_places")
	available := optionalInteger(row, "places_disponibles", "nb_places_disponibles", "available", "dispo")
	if kind == "park_and_ride" {
		available = optionalInteger(row, "nb_tot_place_dispo", "available")
	}
	if value, ok := liveMap[model.Normalize(name)]; ok {
		available = &value
	}
	status := "unknown"
	if available != nil && *available > 0 {
		status = "open"
	} else if available != nil {
		status = "full"
	}
	rowObserved := parseTime(first(row, "observed_at", "last_update", "last_updated", "updated_at"))
	if rowObserved == nil {
		rowObserved = observed
	}
	return model.ParkingOption{ID: firstText(row, name, "gid", "id", "nom", "name"), Name: name, Type: kind, Latitude: latitude, Longitude: longitude, Capacity: capacity, AvailableSpaces: available, Status: status, DistanceM: &distance, Realtime: available != nil, ObservedAt: chooseObserved(available, rowObserved)}, true
}

func parkingCoordinates(row map[string]any) (float64, float64, bool) {
	latitude, latitudeOK := number(row, "lat", "latitude")
	longitude, longitudeOK := number(row, "lon", "lng", "longitude")
	if latitudeOK && longitudeOK && validGeographicCoordinates(latitude, longitude) {
		return latitude, longitude, true
	}
	x, xOK := number(row, "x")
	y, yOK := number(row, "y")
	if !xOK || !yOK {
		return 0, 0, false
	}
	crs := strings.ToLower(text(row, "crs", "srs", "epsg"))
	if strings.Contains(crs, "3857") {
		const earthRadius = 6378137.0
		longitude = x / earthRadius * 180 / math.Pi
		latitude = (2*math.Atan(math.Exp(y/earthRadius)) - math.Pi/2) * 180 / math.Pi
		return latitude, longitude, validGeographicCoordinates(latitude, longitude)
	}
	if crs != "" && !strings.Contains(crs, "4326") && !strings.Contains(crs, "crs84") && !strings.Contains(crs, "wgs84") {
		return 0, 0, false
	}
	return y, x, validGeographicCoordinates(y, x)
}

func validGeographicCoordinates(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsNaN(longitude) && !math.IsInf(latitude, 0) && !math.IsInf(longitude, 0) && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}

func (live *Live) parkingAvailability(ctx context.Context) (map[string]int, []map[string]any, *time.Time, error) {
	live.parkingMu.Lock()
	defer live.parkingMu.Unlock()
	if time.Now().Before(live.parkingExpires) {
		return cloneIntMap(live.parkingLive), cloneRows(live.parkingRows), live.parkingObserved, live.parkingErr
	}
	source, sourceErr := live.resolveSource(ctx, "parking_realtime", "grandlyon", "pvo_patrimoine_voirie.pvoparkingdispo")
	if sourceErr != nil {
		return map[string]int{}, nil, nil, sourceErr
	}
	collection := source.Collection
	if collection == "" {
		collection = parkingOGC
	}
	mapping := map[string]int{}
	liveRows := []map[string]any{}
	var observed *time.Time
	sourceSucceeded := false
	sourceErrors := []error{}
	items, err := live.fetchOGCItems(ctx, collection, 100)
	if err == nil {
		sourceSucceeded = true
		addParkingAvailability(items, mapping, &liveRows, &observed)
	} else {
		sourceErrors = append(sourceErrors, err)
	}
	if len(mapping) == 0 {
		items, wfsErr := live.fetchWFSItems(ctx, collection, 100)
		if wfsErr == nil {
			sourceSucceeded = true
			addParkingAvailability(items, mapping, &liveRows, &observed)
		} else {
			sourceErrors = append(sourceErrors, wfsErr)
		}
	}
	if len(mapping) == 0 {
		candidates := [][2]string{{"grandlyon", "pvo_patrimoine_voirie.pvoparkingdispo"}, {"rdata", "pvo_patrimoine_voirie.pvoparkingdispo"}}
		if source.TableResolved {
			candidates = [][2]string{{source.Service, source.Table}}
		}
		for _, candidate := range candidates {
			rows, queryErr := live.queryTable(ctx, candidate[0], candidate[1], 100)
			if queryErr != nil {
				sourceErrors = append(sourceErrors, queryErr)
				continue
			}
			sourceSucceeded = true
			for _, row := range rows {
				name := firstText(row, "", "nom", "name", "id")
				if available := optionalInteger(row, "places_disponibles", "available", "nb_places_disponibles"); name != "" && available != nil {
					mapping[model.Normalize(name)] = *available
					observed = laterTime(observed, parseTime(first(row, "observed_at", "last_update", "last_updated", "updated_at")))
				}
			}
			if len(mapping) != 0 {
				break
			}
		}
	}
	live.parkingErr = nil
	if !sourceSucceeded {
		live.parkingErr = errors.Join(sourceErrors...)
	}
	live.parkingLive, live.parkingRows, live.parkingObserved = mapping, cloneRows(liveRows), observed
	live.parkingExpires = time.Now().Add(live.sourceTTL(ctx, "parking_realtime", time.Minute))
	return cloneIntMap(mapping), cloneRows(liveRows), observed, live.parkingErr
}

func addParkingAvailability(items []map[string]any, mapping map[string]int, liveRows *[]map[string]any, observed **time.Time) {
	for _, item := range items {
		name := firstText(item, "", "nom", "name", "idparking", "id")
		available := optionalInteger(item, "places_disponibles", "available", "nb_places_disponibles")
		if name == "" || available == nil {
			continue
		}
		mapping[model.Normalize(name)] = *available
		*liveRows = append(*liveRows, cloneRow(item))
		*observed = laterTime(*observed, parseTime(first(item, "observed_at", "last_update", "last_updated", "updated_at")))
	}
}

func laterTime(current, candidate *time.Time) *time.Time {
	if candidate == nil || current != nil && !candidate.After(*current) {
		return current
	}
	value := *candidate
	return &value
}

func (live *Live) sourceTTL(ctx context.Context, sourceID string, fallback time.Duration) time.Duration {
	if live.store == nil {
		return fallback
	}
	source, err := live.store.GetSource(ctx, sourceID)
	if err != nil || source == nil {
		return fallback
	}
	seconds, ok := source["ttl_seconds"].(int)
	if !ok || seconds < 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func cloneIntMap(input map[string]int) map[string]int {
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneRows(input []map[string]any) []map[string]any {
	output := make([]map[string]any, 0, len(input))
	for _, row := range input {
		output = append(output, cloneRow(row))
	}
	return output
}

func cloneRow(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func chooseObserved(available *int, observed *time.Time) *time.Time {
	if available == nil {
		return nil
	}
	if observed != nil {
		value := *observed
		return &value
	}
	return nil
}

func (live *Live) fetchOGCItems(ctx context.Context, collection string, limit int) ([]map[string]any, error) {
	if !collectionNamePattern.MatchString(collection) {
		return nil, errors.New("invalid OGC collection")
	}
	values := url.Values{"f": {"application/json"}, "limit": {strconv.Itoa(bound(limit, 1, 200))}}
	decoded, err := live.getJSON(ctx, "https://data.grandlyon.com/geoserver/ogc/features/v1/collections/"+collection+"/items?"+values.Encode(), false)
	if err != nil {
		return nil, err
	}
	return extractRows(decoded), nil
}

func (live *Live) fetchWFSItems(ctx context.Context, collection string, limit int) ([]map[string]any, error) {
	if !collectionNamePattern.MatchString(collection) {
		return nil, errors.New("invalid WFS collection")
	}
	values := url.Values{
		"service":      {"WFS"},
		"version":      {"2.0.0"},
		"request":      {"GetFeature"},
		"typeNames":    {collection},
		"count":        {strconv.Itoa(bound(limit, 1, 200))},
		"outputFormat": {"application/json"},
	}
	decoded, err := live.getJSON(ctx, "https://data.grandlyon.com/geoserver/ows?"+values.Encode(), true)
	if err != nil {
		return nil, err
	}
	return extractRows(decoded), nil
}

func bound(value, fallback, maximum int) int {
	if value < 1 {
		return fallback
	}
	return min(value, maximum)
}

func first(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := row[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func text(row map[string]any, keys ...string) string {
	value := first(row, keys...)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstText(row map[string]any, fallback string, keys ...string) string {
	if value := text(row, keys...); value != "" {
		return value
	}
	return fallback
}

func number(row map[string]any, keys ...string) (float64, bool) {
	value := first(row, keys...)
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func integer(row map[string]any, fallback int, keys ...string) int {
	if value := optionalInteger(row, keys...); value != nil {
		return *value
	}
	return fallback
}

func optionalInteger(row map[string]any, keys ...string) *int {
	value := first(row, keys...)
	if value == nil {
		return nil
	}
	var parsed int
	var err error
	switch typed := value.(type) {
	case float64:
		parsed = int(typed)
	case json.Number:
		var converted int64
		converted, err = typed.Int64()
		parsed = int(converted)
	case string:
		parsed, err = strconv.Atoi(strings.TrimSpace(typed))
	default:
		err = errors.New("not an integer")
	}
	if err != nil {
		return nil
	}
	return &parsed
}

func parseTime(value any) *time.Time {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05Z07:00"} {
		parsed, err := time.Parse(layout, text)
		if err == nil {
			return &parsed
		}
	}
	if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", text, model.Paris); err == nil {
		return &parsed
	}
	return nil
}

func parseDelay(value any) *int {
	text := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	if text == "" || text == "proche" || text == "approaching" || text == "<nil>" {
		return nil
	}
	text = strings.TrimSpace(strings.TrimSuffix(text, " min"))
	minutes, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil
	}
	seconds := int(minutes * 60)
	return &seconds
}

func optional(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func numberPointer(value float64, present bool) *float64 {
	if !present {
		return nil
	}
	return &value
}

func facilityDefault(category string) string {
	return map[string]string{"toilet": "Toilettes", "drinking_water": "Fontaine", "bike_pump": "Pompe à vélo"}[category]
}

func parkingDefault(kind string) string {
	if kind == "park_and_ride" {
		return "P+R"
	}
	return "Parking"
}

func facilityLabel(row map[string]any) string {
	if value := firstText(row, "", "nom", "name", "title", "identifiant"); value != "" {
		return value
	}
	if address, ok := first(row, "address", "adresse").(map[string]any); ok {
		return strings.Join(strings.Fields(text(address, "streetAddress")+" "+text(address, "postalCode")+" "+text(address, "addressLocality")), " ")
	}
	return firstText(row, "Déchèterie", "address", "adresse")
}

func parseCategories(value any) []string {
	result := []string{}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				result = append(result, text)
			}
		}
	case []string:
		result = append(result, typed...)
	case string:
		for _, item := range regexp.MustCompile(`[,;|]`).Split(typed, -1) {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
	}
	return result
}
