package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/config"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/httpclient"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/storage"
)

type mappedTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (transport mappedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	mapped := request.Clone(request.Context())
	mappedURL := *request.URL
	mappedURL.Scheme = transport.target.Scheme
	mappedURL.Host = transport.target.Host
	mapped.URL = &mappedURL
	mapped.Host = request.URL.Host
	response, err := transport.base.RoundTrip(mapped)
	if response != nil {
		response.Request = request
	}
	return response, err
}

func testLive(t *testing.T, handler http.Handler, mutate func(*config.Config)) *Live {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		VerifyTLS:         true,
		MaxParallel:       4,
		ConnectTimeout:    time.Second,
		ReadTimeout:       time.Second,
		Username:          "fixture-user",
		Password:          "fixture-password",
		TransitousBaseURL: "https://api.transitous.org/api/",
	}
	if mutate != nil {
		mutate(&cfg)
	}
	client := httpclient.NewWithTransport(cfg, mappedTransport{target: target, base: server.Client().Transport})
	t.Cleanup(func() { _ = client.Close() })
	return NewLive(client, nil, cfg)
}

func testLiveWithStore(t *testing.T, handler http.Handler, store *storage.Store) *Live {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{VerifyTLS: true, MaxParallel: 4, ConnectTimeout: time.Second, ReadTimeout: time.Second}
	client := httpclient.NewWithTransport(cfg, mappedTransport{target: target, base: server.Client().Transport})
	t.Cleanup(func() { _ = client.Close() })
	return NewLive(client, store, cfg)
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func TestLiveParkingPreservesZeroAndMissingAvailability(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "/ogc/features/v1/collections/"):
			writeJSON(t, writer, map[string]any{"features": []any{}})
		case strings.Contains(request.URL.Path, "pvoparkingdispo"):
			writeJSON(t, writer, map[string]any{"values": []any{}})
		case strings.Contains(request.URL.Path, "pvoparking/all.json"):
			writeJSON(t, writer, map[string]any{"values": []any{
				map[string]any{"gid": "zero", "nom": "Zero", "lat": 45.76, "lon": 4.85, "capacite": 80, "available": 0},
				map[string]any{"gid": "capacity", "nom": "Capacity", "lat": 45.7601, "lon": 4.8501, "capacite": 200},
				map[string]any{"gid": "mercator", "nom": "Mercator", "x": 539958.0, "y": 5741471.0, "crs": "EPSG:3857", "capacite": 20},
				map[string]any{"gid": "invalid", "nom": "Invalid", "x": 840000.0, "y": 6520000.0, "capacite": 20},
			}})
		case strings.Contains(request.URL.Path, "tclparcrelaistr"):
			writeJSON(t, writer, map[string]any{"values": []any{}})
		default:
			http.NotFound(writer, request)
		}
	})
	live := testLive(t, handler, nil)
	options, err := live.Parkings(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, nil, 500, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 3 {
		t.Fatalf("options = %#v", options)
	}
	byID := map[string]model.ParkingOption{}
	for _, option := range options {
		byID[option.ID] = option
	}
	if byID["zero"].AvailableSpaces == nil || *byID["zero"].AvailableSpaces != 0 || byID["zero"].Status != "full" {
		t.Fatalf("zero option = %#v", byID["zero"])
	}
	if byID["capacity"].AvailableSpaces != nil || byID["capacity"].Realtime {
		t.Fatalf("capacity option = %#v", byID["capacity"])
	}
	if byID["mercator"].Longitude < 4 || byID["mercator"].Longitude > 5 || byID["mercator"].Latitude < 45 || byID["mercator"].Latitude > 46 {
		t.Fatalf("mercator option = %#v", byID["mercator"])
	}
	if _, ok := byID["invalid"]; ok {
		t.Fatalf("invalid projected option = %#v", byID["invalid"])
	}
}

// Failure mode: the official OGC API Features endpoint can return HTTP 500
// while the bounded WFS endpoint for the same collection remains available.
func TestLiveParkingFallsBackToOfficialWFS(t *testing.T) {
	t.Parallel()
	observedAt := "2026-10-23T10:40:00+02:00"
	var wfsRequests atomic.Int32
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "/ogc/features/v1/collections/"):
			http.Error(writer, "upstream failure", http.StatusInternalServerError)
		case request.URL.Path == "/geoserver/ows":
			wfsRequests.Add(1)
			if request.URL.Query().Get("service") != "WFS" || request.URL.Query().Get("request") != "GetFeature" || request.URL.Query().Get("count") != "100" {
				t.Errorf("unexpected WFS query: %s", request.URL.RawQuery)
			}
			writeJSON(t, writer, map[string]any{
				"features": []any{
					map[string]any{
						"geometry": map[string]any{"type": "Point", "coordinates": []any{4.85, 45.76}},
						"properties": map[string]any{
							"id": "live", "nom": "Official Live Name", "nb_places": 100, "places_disponibles": 7, "last_update": observedAt,
						},
					},
				},
			})
		case strings.Contains(request.URL.Path, "pvoparkingdispo"):
			http.NotFound(writer, request)
		case strings.Contains(request.URL.Path, "pvoparking/all.json"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{
				"gid": "static", "nom": "Legacy Static Name", "lat": 45.7601, "lon": 4.8501, "capacite": 100,
			}}})
		case strings.Contains(request.URL.Path, "tclparcrelaistr"):
			writeJSON(t, writer, map[string]any{"values": []any{}})
		default:
			http.NotFound(writer, request)
		}
	}), nil)
	options, err := live.Parkings(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, nil, 500, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 || options[0].ID != "live" || options[0].AvailableSpaces == nil || *options[0].AvailableSpaces != 7 || wfsRequests.Load() != 1 {
		t.Fatalf("options, WFS requests = (%#v, %d)", options, wfsRequests.Load())
	}
	wantObserved, err := time.Parse(time.RFC3339, observedAt)
	if err != nil {
		t.Fatal(err)
	}
	if options[0].ObservedAt == nil || !options[0].ObservedAt.Equal(wantObserved) {
		t.Fatalf("observed_at = %v, want %v", options[0].ObservedAt, wantObserved)
	}
}

func TestLiveParkingsReportCompleteStructuralFailure(t *testing.T) {
	t.Parallel()
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.NotFound(writer, request)
	}), nil)
	_, err := live.Parkings(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, nil, 500, 0, 10)
	if err == nil {
		t.Fatal("expected a complete parking source failure")
	}
}

func TestLiveParkingsReturnRowsWithPartialSourceError(t *testing.T) {
	t.Parallel()
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "/ogc/features/v1/collections/"):
			writeJSON(t, writer, map[string]any{"features": []any{}})
		case strings.Contains(request.URL.Path, "pvoparkingdispo"):
			writeJSON(t, writer, map[string]any{"values": []any{}})
		case strings.Contains(request.URL.Path, "pvoparking/all.json"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"gid": "valid", "nom": "Valid", "lat": 45.76, "lon": 4.85}}})
		case strings.Contains(request.URL.Path, "tclparcrelaistr"):
			http.NotFound(writer, request)
		default:
			http.NotFound(writer, request)
		}
	}), nil)
	options, err := live.Parkings(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, nil, 500, 0, 10)
	if err == nil || len(options) != 1 || options[0].ID != "valid" {
		t.Fatalf("options, error = (%#v, %v)", options, err)
	}
}

func TestLiveParkAndRideSkipsPublicAvailabilitySources(t *testing.T) {
	t.Parallel()
	var availabilityRequests atomic.Int32
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "/ogc/features/v1/collections/"), strings.Contains(request.URL.Path, "pvoparkingdispo"):
			availabilityRequests.Add(1)
			http.NotFound(writer, request)
		case strings.Contains(request.URL.Path, "tclparcrelaistr"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"id": "pr", "nom": "P+R", "lat": 45.76, "lon": 4.85, "nb_tot_place_dispo": 4}}})
		default:
			http.NotFound(writer, request)
		}
	}), nil)
	options, err := live.Parkings(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, []string{"park_and_ride"}, 500, 0, 10)
	if err != nil || len(options) != 1 || availabilityRequests.Load() != 0 {
		t.Fatalf("options, error, availability requests = (%#v, %v, %d)", options, err, availabilityRequests.Load())
	}
}

func TestLiveFacilitiesReturnRowsWithPartialSourceError(t *testing.T) {
	t.Parallel()
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "adrtoilettepublique"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"gid": "toilet", "adresse": "Place", "lat": 45.76, "lon": 4.85}}})
		case strings.Contains(request.URL.Path, "adrbornefontaine"):
			http.NotFound(writer, request)
		default:
			http.NotFound(writer, request)
		}
	}), nil)
	values, err := live.Facilities(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, []string{"toilet", "drinking_water"}, 500, 5)
	if err == nil || len(values) != 1 || values[0].ID != "toilet" {
		t.Fatalf("facilities, error = (%#v, %v)", values, err)
	}
}

func TestLiveFacilitiesMapVelovStationsAndRejectUnresolvedCategory(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		switch {
		case strings.Contains(request.URL.Path, "jcdvelov"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"number": "station", "name": "Station", "lat": 45.76, "lng": 4.85, "status": "OPEN"}}})
		case strings.Contains(request.URL.Path, "gipdecheterie"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"identifiant": "waste", "nom": "Waste", "lat": 45.7601, "lon": 4.8501}}})
		default:
			http.NotFound(writer, request)
		}
	}), nil)
	values, err := live.Facilities(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, []string{"velov_station", "waste_facility"}, 500, 5)
	if err != nil || len(values) != 2 || values[0].ID != "station" || values[0].Category != "velov_station" || values[1].ID != "waste" || values[1].Category != "waste_facility" {
		t.Fatalf("facilities, error = (%#v, %v)", values, err)
	}
	before := requests.Load()
	if values, err = live.Facilities(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, []string{"park"}, 500, 5); err == nil || len(values) != 0 {
		t.Fatalf("unresolved facilities, error = (%#v, %v)", values, err)
	}
	if requests.Load() != before {
		t.Fatalf("unresolved category issued %d HTTP request(s)", requests.Load()-before)
	}
}

func TestLiveRoadEventsApplyAreaFilter(t *testing.T) {
	t.Parallel()
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(t, writer, map[string]any{"values": []any{
			map[string]any{"gid": "lyon", "nom": "Travaux Garibaldi", "commune1": "Lyon 3", "descripchantierinternet": "Voie réduite"},
			map[string]any{"gid": "bron", "nom": "Travaux route", "commune1": "Bron", "descripchantierinternet": "Route barrée"},
		}})
	}), nil)
	events, err := live.RoadEvents(context.Background(), "Lyon 3")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "lyon" {
		t.Fatalf("events = %#v", events)
	}
}

func TestLiveWasteDoesNotInventAcceptedCategories(t *testing.T) {
	t.Parallel()
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(t, writer, map[string]any{"values": []any{
			map[string]any{"identifiant": "unknown", "nom": "Unknown", "lat": 45.76, "lon": 4.85},
			map[string]any{"identifiant": "known", "nom": "Known", "lat": 45.7601, "lon": 4.8501, "allowed_waste": "paint; portable_battery"},
		}})
	}), nil)
	facilities, err := live.WasteFacilities(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, "paint", 500, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facilities) != 2 {
		t.Fatalf("facilities = %#v", facilities)
	}
	byID := map[string]model.WasteFacility{}
	for _, facility := range facilities {
		byID[facility.ID] = facility
	}
	if len(byID["unknown"].AcceptedCategories) != 0 {
		t.Fatalf("unknown categories = %#v", byID["unknown"].AcceptedCategories)
	}
	if len(byID["known"].AcceptedCategories) != 2 {
		t.Fatalf("known categories = %#v", byID["known"].AcceptedCategories)
	}
}

func TestLiveVelovCoalescesConcurrentRefresh(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		time.Sleep(20 * time.Millisecond)
		writeJSON(t, writer, map[string]any{"values": []any{
			map[string]any{"number": "1", "name": "Station", "lat": 45.76, "lng": 4.85, "available_bikes": 5, "available_bike_stands": 5, "bike_stands": 10, "status": "OPEN"},
		}})
	}), nil)
	done := make(chan error, 8)
	for range 8 {
		go func() {
			_, err := live.Stations(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, 500, 5)
			done <- err
		}()
	}
	for range 8 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestLiveVelovUsesSourceTimestampAndRegistryTTL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/velov-ttl.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.LoadSources(ctx, []byte(`sources:
  velov_realtime:
    service: rdata
    table_name: jcd_jcdecaux.jcdvelov
    status: OK
    ttl_seconds: 0
`)); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	live := testLiveWithStore(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writeJSON(t, writer, map[string]any{"values": []any{
			map[string]any{"number": "dated", "name": "Dated", "lat": 45.76, "lng": 4.85, "status": "OPEN", "last_update": "2026-10-23 10:38:37+02:00"},
			map[string]any{"number": "fme", "name": "FME", "lat": 45.7602, "lng": 4.8502, "status": "OPEN", "last_update_fme": "2026-10-23 10:39:41.102903+02:00"},
			map[string]any{"number": "undated", "name": "Undated", "lat": 45.7601, "lng": 4.8501, "status": "OPEN"},
		}})
	}), store)
	stations, err := live.Stations(ctx, model.Point{Latitude: 45.76, Longitude: 4.85}, 500, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.Stations(ctx, model.Point{Latitude: 45.76, Longitude: 4.85}, 500, 5); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
	byID := map[string]model.VelovStation{}
	for _, station := range stations {
		byID[station.ID] = station
	}
	want := time.Date(2026, 10, 23, 10, 38, 37, 0, time.FixedZone("CEST", 2*60*60))
	if byID["dated"].ObservedAt == nil || !byID["dated"].ObservedAt.Equal(want) {
		t.Fatalf("dated observation = %#v", byID["dated"].ObservedAt)
	}
	wantFME := time.Date(2026, 10, 23, 10, 39, 41, 102903000, time.FixedZone("CEST", 2*60*60))
	if byID["fme"].ObservedAt == nil || !byID["fme"].ObservedAt.Equal(wantFME) {
		t.Fatalf("FME observation = %#v", byID["fme"].ObservedAt)
	}
	if byID["undated"].ObservedAt != nil {
		t.Fatalf("undated observation = %#v", byID["undated"].ObservedAt)
	}
}

func TestLiveParkingAvailabilityUsesRegistryTTL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/parking-ttl.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.LoadSources(ctx, []byte(`sources:
  parking_realtime:
    service: grandlyon
    table_name: pvo_patrimoine_voirie.pvoparking
    status: OK
    ttl_seconds: 0
`)); err != nil {
		t.Fatal(err)
	}
	var availabilityRequests atomic.Int32
	live := testLiveWithStore(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "/ogc/features/v1/collections/"):
			availabilityRequests.Add(1)
			writeJSON(t, writer, map[string]any{"features": []any{map[string]any{"properties": map[string]any{"nom": "Valid", "places_disponibles": 3}}}})
		case strings.Contains(request.URL.Path, "pvoparking/all.json"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"gid": "valid", "nom": "Valid", "lat": 45.76, "lon": 4.85}}})
		default:
			http.NotFound(writer, request)
		}
	}), store)
	for range 2 {
		if _, err := live.Parkings(ctx, model.Point{Latitude: 45.76, Longitude: 4.85}, []string{"public_parking"}, 500, 0, 5); err != nil {
			t.Fatal(err)
		}
	}
	if availabilityRequests.Load() != 2 {
		t.Fatalf("availability requests = %d, want 2", availabilityRequests.Load())
	}
}

func TestLiveDisabledSourcesDoNotIssueRequestsAndKeepActiveAggregateRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/disabled-business.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.LoadSources(ctx, []byte(`sources:
  velov_realtime:
    enabled: false
    service: rdata
    table_name: blocked.velov
  tcl_departures:
    enabled: false
    service: rdata
    table_name: blocked.departures
  parking_capacity:
    enabled: true
    service: rdata
    table_name: custom.capacity
    status: ERROR
  parking_realtime:
    enabled: false
    service: grandlyon
    table_name: blocked.availability
  park_and_ride:
    enabled: false
    service: rdata
    table_name: blocked.pr
  toilets:
    enabled: false
    service: grandlyon
    table_name: blocked.toilets
  drinking_water:
    enabled: true
    service: rdata
    table_name: custom.water
    status: ERROR
`)); err != nil {
		t.Fatal(err)
	}
	var paths []string
	var pathMu sync.Mutex
	live := testLiveWithStore(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		pathMu.Lock()
		paths = append(paths, request.URL.Path)
		pathMu.Unlock()
		switch {
		case strings.Contains(request.URL.Path, "custom.capacity"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"gid": "capacity", "nom": "Capacity", "lat": 45.76, "lon": 4.85}}})
		case strings.Contains(request.URL.Path, "custom.water"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"gid": "water", "nom": "Water", "lat": 45.76, "lon": 4.85}}})
		default:
			http.NotFound(writer, request)
		}
	}), store)
	if _, err := live.Stations(ctx, model.Point{Latitude: 45.76, Longitude: 4.85}, 500, 5); err == nil {
		t.Fatal("expected disabled Vélo'v source error")
	}
	if _, err := live.Departures(ctx, "BEL1", "A", "", 5); err == nil {
		t.Fatal("expected disabled departures source error")
	}
	parkings, parkingErr := live.Parkings(ctx, model.Point{Latitude: 45.76, Longitude: 4.85}, nil, 500, 0, 5)
	if parkingErr == nil || len(parkings) != 1 || parkings[0].ID != "capacity" {
		t.Fatalf("parkings, error = (%#v, %v)", parkings, parkingErr)
	}
	facilities, facilityErr := live.Facilities(ctx, model.Point{Latitude: 45.76, Longitude: 4.85}, []string{"toilet", "drinking_water"}, 500, 5)
	if facilityErr == nil || len(facilities) != 1 || facilities[0].ID != "water" {
		t.Fatalf("facilities, error = (%#v, %v)", facilities, facilityErr)
	}
	pathMu.Lock()
	defer pathMu.Unlock()
	if len(paths) != 2 || !containsPath(paths, "custom.capacity") || !containsPath(paths, "custom.water") {
		t.Fatalf("HTTP paths = %#v", paths)
	}
}

func TestLiveOtherDisabledSourcesDoNotIssueRequests(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/disabled-other.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.LoadSources(ctx, []byte(`sources:
  photon:
    enabled: false
  tcl_alerts:
    enabled: false
    service: rdata
    table_name: blocked.alerts
  tcl_accessibility_alerts:
    enabled: false
    service: rdata
    table_name: blocked.accessibility
  road_events:
    enabled: false
    service: grandlyon
    table_name: blocked.roads
  waste_facilities:
    enabled: false
    service: grandlyon
    table_name: blocked.waste
`)); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	live := testLiveWithStore(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		http.NotFound(writer, request)
	}), store)
	point := model.Point{Latitude: 45.76, Longitude: 4.85}
	checks := []func() error{
		func() error { _, err := live.Search(ctx, "Bellecour", nil, nil, 5); return err },
		func() error { _, err := live.Reverse(ctx, point); return err },
		func() error { _, err := live.Alerts(ctx, nil); return err },
		func() error { _, err := live.Incidents(ctx, nil); return err },
		func() error { _, err := live.CheckStop(ctx, "BEL1"); return err },
		func() error { _, err := live.RoadEvents(ctx, "Lyon"); return err },
		func() error { _, err := live.WasteFacilities(ctx, point, "paint", 500, 5); return err },
	}
	for index, check := range checks {
		if err := check(); err == nil {
			t.Fatalf("check %d did not report a disabled source", index)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("HTTP requests = %d, want 0", requests.Load())
	}
}

func TestLiveUsesResolvedTablesWhileSourceStatusIsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/resolved-business.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.LoadSources(ctx, []byte(`sources:
  velov_realtime:
    enabled: true
    status: ERROR
    service: grandlyon
    table_name: custom.velov
    ttl_seconds: 0
  tcl_departures:
    enabled: true
    status: ERROR
    service: grandlyon
    table_name: custom.departures
`)); err != nil {
		t.Fatal(err)
	}
	var paths []string
	var pathMu sync.Mutex
	live := testLiveWithStore(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		pathMu.Lock()
		paths = append(paths, request.URL.Path)
		pathMu.Unlock()
		switch {
		case strings.Contains(request.URL.Path, "custom.velov"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"number": "v", "name": "V", "lat": 45.76, "lng": 4.85}}})
		case strings.Contains(request.URL.Path, "estimated-timetables"):
			writeJSON(t, writer, map[string]any{"EstimatedVehicleJourney": []any{}})
		case strings.Contains(request.URL.Path, "custom.departures"):
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"ligne": "A", "stop_id": "BEL1", "direction": "Vaulx", "heurepassage": "2026-07-20T08:07:00+02:00"}}})
		default:
			http.NotFound(writer, request)
		}
	}), store)
	if stations, err := live.Stations(ctx, model.Point{Latitude: 45.76, Longitude: 4.85}, 500, 5); err != nil || len(stations) != 1 {
		t.Fatalf("stations, error = (%#v, %v)", stations, err)
	}
	if departures, err := live.Departures(ctx, "BEL1", "A", "", 5); err != nil || len(departures) != 1 {
		t.Fatalf("departures, error = (%#v, %v)", departures, err)
	}
	pathMu.Lock()
	defer pathMu.Unlock()
	if !containsPath(paths, "/grandlyon/custom.velov/") || !containsPath(paths, "/grandlyon/custom.departures/") {
		t.Fatalf("HTTP paths = %#v", paths)
	}
}

func containsPath(paths []string, fragment string) bool {
	for _, path := range paths {
		if strings.Contains(path, fragment) {
			return true
		}
	}
	return false
}

func TestLiveDeparturesKeepStrictStopAndLineFilters(t *testing.T) {
	t.Parallel()
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "estimated-timetables"):
			writeJSON(t, writer, map[string]any{"EstimatedVehicleJourney": []any{
				map[string]any{"LineRef": "A", "DestinationName": "Vaulx", "EstimatedCalls": map[string]any{"EstimatedCall": []any{
					map[string]any{"StopPointRef": "BEL1", "ExpectedDepartureTime": "2026-07-20T08:05:00+02:00"},
				}}},
				map[string]any{"LineRef": "D", "DestinationName": "Vénissieux", "EstimatedCalls": map[string]any{"EstimatedCall": []any{
					map[string]any{"StopPointRef": "BEL1", "ExpectedDepartureTime": "2026-07-20T08:06:00+02:00"},
				}}},
			}})
		case strings.Contains(request.URL.Path, "tclpassagearret"):
			writeJSON(t, writer, map[string]any{"values": []any{
				map[string]any{"ligne": "A", "stop_id": "OTHER", "direction": "Vaulx", "heurepassage": "2026-07-20T08:07:00+02:00"},
			}})
		default:
			http.NotFound(writer, request)
		}
	}), nil)
	departures, err := live.Departures(context.Background(), "gtfs:stop:BEL1", "A", "Vaulx", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(departures) != 1 || departures[0].LineName != "A" || departures[0].StopRef == nil || *departures[0].StopRef != "BEL1" {
		t.Fatalf("departures = %#v", departures)
	}
	other, err := live.Departures(context.Background(), "gtfs:stop:OTHER", "A", "Vaulx", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].StopRef == nil || *other[0].StopRef != "OTHER" {
		t.Fatalf("other stop departures = %#v", other)
	}
}

func TestLiveDeparturesReportMalformedSIRIWhenFallbackFails(t *testing.T) {
	t.Parallel()
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "estimated-timetables"):
			writeJSON(t, writer, "malformed")
		case strings.Contains(request.URL.Path, "tclpassagearret"):
			http.Error(writer, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(writer, request)
		}
	}), nil)
	if _, err := live.Departures(context.Background(), "BEL1", "A", "", 4); err == nil {
		t.Fatal("expected malformed SIRI and fallback errors")
	}
}

func TestLiveDeparturesUseResolvedStopCoordinates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/departures.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	stop := model.PlaceCandidate{ID: "gtfs:stop:BEL1", Name: "Bellecour", Type: "stop", Latitude: 45.7578, Longitude: 4.832}
	if err := store.UpsertEntity(ctx, stop, "gtfs_tcl", "BEL1", nil, nil); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "estimated-timetables"):
			writeJSON(t, writer, map[string]any{"EstimatedVehicleJourney": []any{}})
		case strings.Contains(request.URL.Path, "tclpassagearret"):
			writeJSON(t, writer, map[string]any{"values": []any{
				map[string]any{"ligne": "A", "direction": "Vaulx", "lat": 45.7579, "lon": 4.8321, "heurepassage": "2026-07-20T08:07:00+02:00"},
			}})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{VerifyTLS: true, MaxParallel: 2, ConnectTimeout: time.Second, ReadTimeout: time.Second}
	client := httpclient.NewWithTransport(cfg, mappedTransport{target: target, base: server.Client().Transport})
	t.Cleanup(func() { _ = client.Close() })
	departures, err := NewLive(client, store, cfg).Departures(ctx, stop.ID, "A", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(departures) != 1 || departures[0].LineName != "A" {
		t.Fatalf("departures = %#v", departures)
	}
}

func TestLiveAlertsReportMalformedSIRI(t *testing.T) {
	t.Parallel()
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "tclalertetrafic"):
			writeJSON(t, writer, map[string]any{"values": []any{}})
		case strings.Contains(request.URL.Path, "situation-exchange"):
			writeJSON(t, writer, 42)
		default:
			http.NotFound(writer, request)
		}
	}), nil)
	if _, err := live.Alerts(context.Background(), nil); err == nil {
		t.Fatal("expected malformed SIRI error")
	}
}

func TestTransitousRespectsFeatureFlagAndMapsItinerary(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writeJSON(t, writer, map[string]any{"itineraries": []any{
			map[string]any{"duration": 900, "summary": "Metro A"},
		}})
	})
	disabled := testLive(t, handler, nil)
	options, err := disabled.Plan(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, model.Point{Latitude: 45.75, Longitude: 4.83}, time.Now(), []string{"tcl"}, model.TripPreferences{})
	if err != nil || len(options) != 0 || requests.Load() != 0 {
		t.Fatalf("disabled = (%#v, %v, %d requests)", options, err, requests.Load())
	}
	enabled := testLive(t, handler, func(cfg *config.Config) { cfg.TransitousEnabled = true })
	options, err = enabled.Plan(context.Background(), model.Point{Latitude: 45.76, Longitude: 4.85}, model.Point{Latitude: 45.75, Longitude: 4.83}, time.Now(), []string{"tcl"}, model.TripPreferences{})
	if err != nil || len(options) != 1 || options[0].EstimatedDurationSeconds == nil || *options[0].EstimatedDurationSeconds != 900 {
		t.Fatalf("enabled = (%#v, %v)", options, err)
	}
}

func TestQueryTableIsBoundedAndDecodesFeatureProperties(t *testing.T) {
	t.Parallel()
	var rawQuery string
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		rawQuery = request.URL.RawQuery
		writeJSON(t, writer, map[string]any{"features": []any{
			map[string]any{"properties": map[string]any{"id": "one"}},
		}})
	}), nil)
	rows, err := live.queryTable(context.Background(), "grandlyon", "safe.table", 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != "one" {
		t.Fatalf("rows = %#v", rows)
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("maxfeatures") != "200" || values.Get("compact") != "false" {
		t.Fatalf("query = %q", rawQuery)
	}
}

func TestCatalogStatusSeparatesAuthenticationAndPermission(t *testing.T) {
	t.Parallel()
	if got := ClassifyCatalogStatus(http.StatusUnauthorized); got.Auth != "ERROR" || got.Catalog != "ERROR" {
		t.Fatalf("401 = %#v", got)
	}
	if got := ClassifyCatalogStatus(http.StatusForbidden); got.Auth != "OK" || got.Catalog != "FORBIDDEN" {
		t.Fatalf("403 = %#v", got)
	}
	if got := ClassifyCatalogStatus(http.StatusOK); got.Auth != "OK" || got.Catalog != "OK" {
		t.Fatalf("200 = %#v", got)
	}
}

func TestCatalogScanKnownModeKeepsRequestedModeAndBoundsProbes(t *testing.T) {
	t.Parallel()
	var probes atomic.Int32
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("maxfeatures") != "1" {
			t.Errorf("maxfeatures = %q", request.URL.Query().Get("maxfeatures"))
		}
		probes.Add(1)
		writeJSON(t, writer, map[string]any{"values": []any{}})
	}), nil)
	result, err := live.CatalogScan(context.Background(), "known")
	if err != nil {
		t.Fatal(err)
	}
	if result["catalog_mode"] != "known" {
		t.Fatalf("catalog_mode = %#v", result["catalog_mode"])
	}
	sources, ok := result["sources"].(map[string]any)
	if !ok || len(sources) == 0 || probes.Load() != int32(len(sources)) {
		t.Fatalf("sources = %#v, probes = %d", result["sources"], probes.Load())
	}
}

func TestCatalogScanAutoFallsBackAfterForbiddenCatalog(t *testing.T) {
	t.Parallel()
	var catalogRequests atomic.Int32
	var boundedProbes atomic.Int32
	var collectionRequests atomic.Int32
	live := testLive(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/rdata/all.json"), strings.HasSuffix(request.URL.Path, "/grandlyon/all.json"):
			catalogRequests.Add(1)
			http.Error(writer, "forbidden", http.StatusForbidden)
		case strings.HasSuffix(request.URL.Path, "/collections"):
			collectionRequests.Add(1)
			writeJSON(t, writer, map[string]any{"collections": []any{
				map[string]any{"id": "parking-live"},
				map[string]any{"id": "unrelated"},
			}})
		default:
			if request.URL.Query().Get("maxfeatures") == "1" {
				boundedProbes.Add(1)
			}
			writeJSON(t, writer, map[string]any{"values": []any{}})
		}
	}), nil)
	result, err := live.CatalogScan(context.Background(), "auto")
	if err != nil {
		t.Fatal(err)
	}
	if result["catalog_list"] != "FORBIDDEN" || result["catalog_mode"] != "known_tables_fallback" {
		t.Fatalf("result = %#v", result)
	}
	candidates, ok := result["ogc_candidates"].([]string)
	if !ok || len(candidates) != 1 || candidates[0] != "parking-live" {
		t.Fatalf("ogc_candidates = %#v", result["ogc_candidates"])
	}
	if catalogRequests.Load() != 2 || boundedProbes.Load() == 0 || collectionRequests.Load() != 1 {
		t.Fatalf("requests = catalog:%d probes:%d collections:%d", catalogRequests.Load(), boundedProbes.Load(), collectionRequests.Load())
	}
}

func TestCatalogScanFullResolvesOnlyUnambiguousSources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.LoadSources(ctx, []byte(`sources:
  exact:
    service: rdata
    exact_table_hints: [safe.exact]
    search_keywords: [station]
  ambiguous:
    service: rdata
    search_keywords: [traffic]
`)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/rdata/all.json") {
			writeJSON(t, writer, map[string]any{"values": []any{
				map[string]any{"name": "safe.exact", "title": "Station exact"},
				map[string]any{"name": "safe.station_other", "title": "Station other"},
				map[string]any{"name": "safe.traffic_east", "title": "Traffic east"},
				map[string]any{"name": "safe.traffic_west", "title": "Traffic west"},
			}})
			return
		}
		writeJSON(t, writer, map[string]any{"datasets": []any{map[string]any{"name": "safe.other"}}})
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{VerifyTLS: true, MaxParallel: 2, ConnectTimeout: time.Second, ReadTimeout: time.Second}
	client := httpclient.NewWithTransport(cfg, mappedTransport{target: target, base: server.Client().Transport})
	t.Cleanup(func() { _ = client.Close() })
	live := NewLive(client, store, cfg)
	result, err := live.CatalogScan(ctx, "full")
	if err != nil {
		t.Fatal(err)
	}
	grandlyon, ok := result["grandlyon"].(map[string]any)
	if !ok || grandlyon["count"] != 1 {
		t.Fatalf("grandlyon = %#v", result["grandlyon"])
	}
	exact, ok := result["exact"].(map[string]any)
	if !ok || exact["status"] != "OK" || exact["candidates"] != 2 {
		t.Fatalf("exact = %#v", result["exact"])
	}
	ambiguous, ok := result["ambiguous"].(map[string]any)
	if !ok || ambiguous["status"] != "UNRESOLVED" || ambiguous["candidates"] != 2 {
		t.Fatalf("ambiguous = %#v", result["ambiguous"])
	}
	storedExact, err := store.GetSource(ctx, "exact")
	if err != nil || storedExact["status"] != "OK" || storedExact["table_name"] != "safe.exact" {
		t.Fatalf("stored exact = (%#v, %v)", storedExact, err)
	}
	storedAmbiguous, err := store.GetSource(ctx, "ambiguous")
	if err != nil || storedAmbiguous["status"] != "UNRESOLVED" || storedAmbiguous["table_name"] != nil {
		t.Fatalf("stored ambiguous = (%#v, %v)", storedAmbiguous, err)
	}
}

func TestCatalogValidateProbesOnlyResolvedRegistrySources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/validate.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.LoadSources(ctx, []byte(`sources:
  resolved:
    service: rdata
    table_name: safe.exact
    status: OK
  unresolved:
    service: rdata
    status: UNRESOLVED
`)); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if !strings.Contains(request.URL.Path, "safe.exact") || request.URL.Query().Get("maxfeatures") != "2" {
			t.Errorf("unexpected validation request: %s", request.URL.String())
		}
		writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"id": "sample"}}})
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{VerifyTLS: true, MaxParallel: 2, ConnectTimeout: time.Second, ReadTimeout: time.Second}
	client := httpclient.NewWithTransport(cfg, mappedTransport{target: target, base: server.Client().Transport})
	t.Cleanup(func() { _ = client.Close() })
	result, err := NewLive(client, store, cfg).CatalogValidate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved, ok := result["resolved"].(map[string]any)
	if !ok || resolved["status"] != "OK" || resolved["sample_rows"] != 1 {
		t.Fatalf("resolved = %#v", result["resolved"])
	}
	unresolved, ok := result["unresolved"].(map[string]any)
	if !ok || unresolved["status"] != "UNRESOLVED" {
		t.Fatalf("unresolved = %#v", result["unresolved"])
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d", requests.Load())
	}
}

func TestCatalogKnownScanSkipsDisabledSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/known-disabled.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.LoadSources(ctx, []byte(`sources:
  velov_realtime:
    enabled: false
    service: rdata
    table_name: jcd_jcdecaux.jcdvelov
`)); err != nil {
		t.Fatal(err)
	}
	var disabledRequests atomic.Int32
	live := testLiveWithStore(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "jcdvelov") {
			disabledRequests.Add(1)
		}
		writeJSON(t, writer, map[string]any{"values": []any{}})
	}), store)
	result, err := live.CatalogScan(ctx, "known")
	if err != nil {
		t.Fatal(err)
	}
	sources := result["sources"].(map[string]any)
	disabled := sources["velov_realtime"].(map[string]any)
	if disabled["status"] != "DISABLED" || disabledRequests.Load() != 0 {
		t.Fatalf("disabled source, requests = (%#v, %d)", disabled, disabledRequests.Load())
	}
}

func TestCatalogValidateSkipsDisabledAndRetriesTransientFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/validate-retry.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.LoadSources(ctx, []byte(`sources:
  disabled:
    enabled: false
    service: rdata
    table_name: safe.disabled
    status: OK
  recover:
    service: rdata
    table_name: safe.recover
    status: OK
`)); err != nil {
		t.Fatal(err)
	}
	var disabledRequests atomic.Int32
	var recoverRequests atomic.Int32
	live := testLiveWithStore(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "safe.disabled"):
			disabledRequests.Add(1)
			writeJSON(t, writer, map[string]any{"values": []any{}})
		case strings.Contains(request.URL.Path, "safe.recover"):
			if recoverRequests.Add(1) == 1 {
				http.NotFound(writer, request)
				return
			}
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"id": "ok"}}})
		default:
			http.NotFound(writer, request)
		}
	}), store)
	first, err := live.CatalogValidate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := live.CatalogValidate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first["disabled"].(map[string]any)["status"] != "DISABLED" || disabledRequests.Load() != 0 {
		t.Fatalf("disabled = %#v, requests = %d", first["disabled"], disabledRequests.Load())
	}
	if first["recover"].(map[string]any)["status"] != "ERROR" || second["recover"].(map[string]any)["status"] != "OK" || recoverRequests.Load() != 2 {
		t.Fatalf("recover first, second, requests = (%#v, %#v, %d)", first["recover"], second["recover"], recoverRequests.Load())
	}
}

func TestCatalogFullReportsPartialAndKeepsExplicitTable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir()+"/catalog-partial.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.LoadSources(ctx, []byte(`sources:
  explicit:
    service: rdata
    table_name: safe.explicit
`)); err != nil {
		t.Fatal(err)
	}
	live := testLiveWithStore(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/rdata/all.json") {
			writeJSON(t, writer, map[string]any{"values": []any{map[string]any{"name": "safe.explicit"}}})
			return
		}
		http.Error(writer, "forbidden", http.StatusForbidden)
	}), store)
	result, err := live.CatalogScan(ctx, "full")
	if err != nil {
		t.Fatal(err)
	}
	explicit := result["explicit"].(map[string]any)
	if result["catalog_list"] != "PARTIAL" || result["status"] != "PARTIAL" || explicit["status"] != "OK" || explicit["table"] != "safe.explicit" {
		t.Fatalf("result = %#v", result)
	}
}

func TestJSONResponseBodyIsClosedByHTTPClient(t *testing.T) {
	t.Parallel()
	var closed atomic.Bool
	client := httpclient.NewWithTransport(config.Config{VerifyTLS: true, MaxParallel: 1, ConnectTimeout: time.Second, ReadTimeout: time.Second}, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &closeReader{Reader: strings.NewReader(`{"values":[]}`), closed: &closed}}, nil
	}))
	t.Cleanup(func() { _ = client.Close() })
	live := NewLive(client, nil, config.Config{})
	_, err := live.queryTable(context.Background(), "rdata", "safe.table", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !closed.Load() {
		t.Fatal("response body was not closed")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type closeReader struct {
	io.Reader
	closed *atomic.Bool
}

func (reader *closeReader) Close() error {
	reader.closed.Store(true)
	return nil
}
