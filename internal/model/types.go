package model

import "time"

type PlaceRef struct {
	PlaceID      string   `json:"place_id" yaml:"place_id"`
	Query        string   `json:"query" yaml:"query"`
	Latitude     *float64 `json:"latitude" yaml:"latitude"`
	Longitude    *float64 `json:"longitude" yaml:"longitude"`
	ProfilePlace string   `json:"profile_place" yaml:"profile_place"`
}

type SourceProvenance struct {
	Provider    string     `json:"provider" yaml:"provider"`
	SourceID    string     `json:"source_id" yaml:"source_id"`
	Dataset     string     `json:"dataset" yaml:"dataset"`
	Attribution string     `json:"attribution" yaml:"attribution"`
	License     string     `json:"license" yaml:"license"`
	ObservedAt  *time.Time `json:"observed_at" yaml:"observed_at"`
	RetrievedAt *time.Time `json:"retrieved_at" yaml:"retrieved_at"`
	AgeSeconds  *int       `json:"age_seconds" yaml:"age_seconds"`
	Realtime    bool       `json:"realtime" yaml:"realtime"`
	Stale       bool       `json:"stale" yaml:"stale"`
}

type WarningItem struct {
	Code      string  `json:"code" yaml:"code"`
	Source    *string `json:"source" yaml:"source"`
	Message   string  `json:"message" yaml:"message"`
	Retryable bool    `json:"retryable" yaml:"retryable"`
}

type Envelope struct {
	SchemaVersion string             `json:"schema_version" yaml:"schema_version"`
	RequestID     string             `json:"request_id" yaml:"request_id"`
	Status        string             `json:"status" yaml:"status"`
	GeneratedAt   time.Time          `json:"generated_at" yaml:"generated_at"`
	Summary       string             `json:"summary" yaml:"summary"`
	Data          map[string]any     `json:"data" yaml:"data"`
	Sources       []SourceProvenance `json:"sources" yaml:"sources"`
	Warnings      []WarningItem      `json:"warnings" yaml:"warnings"`
	Degraded      bool               `json:"degraded" yaml:"degraded"`
}

type PlaceCandidate struct {
	ID         string   `json:"id" yaml:"id"`
	Name       string   `json:"name" yaml:"name"`
	Label      string   `json:"label" yaml:"label"`
	Type       string   `json:"type" yaml:"type"`
	Latitude   float64  `json:"latitude" yaml:"latitude"`
	Longitude  float64  `json:"longitude" yaml:"longitude"`
	DistanceM  *float64 `json:"distance_m" yaml:"distance_m"`
	Confidence float64  `json:"confidence" yaml:"confidence"`
}

type ResolvedPlace struct {
	ID         string  `json:"id" yaml:"id"`
	Name       string  `json:"name" yaml:"name"`
	Label      string  `json:"label" yaml:"label"`
	Type       string  `json:"type" yaml:"type"`
	Latitude   float64 `json:"latitude" yaml:"latitude"`
	Longitude  float64 `json:"longitude" yaml:"longitude"`
	Confidence float64 `json:"confidence" yaml:"confidence"`
}

type Departure struct {
	LineID       string     `json:"line_id" yaml:"line_id"`
	LineName     string     `json:"line_name" yaml:"line_name"`
	Destination  string     `json:"destination" yaml:"destination"`
	Platform     *string    `json:"platform" yaml:"platform"`
	StopRef      *string    `json:"stop_ref" yaml:"stop_ref"`
	ScheduledAt  *time.Time `json:"scheduled_at" yaml:"scheduled_at"`
	ExpectedAt   *time.Time `json:"expected_at" yaml:"expected_at"`
	DelaySeconds *int       `json:"delay_seconds" yaml:"delay_seconds"`
	Realtime     bool       `json:"realtime" yaml:"realtime"`
	Cancelled    bool       `json:"cancelled" yaml:"cancelled"`
}

type TransitAlert struct {
	ID          string     `json:"id" yaml:"id"`
	Severity    string     `json:"severity" yaml:"severity"`
	Title       string     `json:"title" yaml:"title"`
	Description string     `json:"description" yaml:"description"`
	Lines       []string   `json:"lines" yaml:"lines"`
	StartAt     *time.Time `json:"start_at" yaml:"start_at"`
	EndAt       *time.Time `json:"end_at" yaml:"end_at"`
}

type ParkingOption struct {
	ID                string     `json:"id" yaml:"id"`
	Name              string     `json:"name" yaml:"name"`
	Type              string     `json:"type" yaml:"type"`
	Latitude          float64    `json:"latitude" yaml:"latitude"`
	Longitude         float64    `json:"longitude" yaml:"longitude"`
	Capacity          *int       `json:"capacity" yaml:"capacity"`
	AvailableSpaces   *int       `json:"available_spaces" yaml:"available_spaces"`
	OccupiedSpaces    *int       `json:"occupied_spaces" yaml:"occupied_spaces"`
	AvailabilityRatio *float64   `json:"availability_ratio" yaml:"availability_ratio"`
	Status            string     `json:"status" yaml:"status"`
	DistanceM         *float64   `json:"distance_m" yaml:"distance_m"`
	Realtime          bool       `json:"realtime" yaml:"realtime"`
	ObservedAt        *time.Time `json:"observed_at" yaml:"observed_at"`
}

type VelovStation struct {
	ID             string     `json:"id" yaml:"id"`
	Name           string     `json:"name" yaml:"name"`
	Latitude       float64    `json:"latitude" yaml:"latitude"`
	Longitude      float64    `json:"longitude" yaml:"longitude"`
	BikesAvailable int        `json:"bikes_available" yaml:"bikes_available"`
	DocksAvailable int        `json:"docks_available" yaml:"docks_available"`
	Capacity       int        `json:"capacity" yaml:"capacity"`
	Status         string     `json:"status" yaml:"status"`
	ObservedAt     *time.Time `json:"observed_at" yaml:"observed_at"`
	DistanceM      *float64   `json:"distance_m" yaml:"distance_m"`
}

type ReliabilityScore struct {
	Score                float64 `json:"score" yaml:"score"`
	SampleCount          int     `json:"sample_count" yaml:"sample_count"`
	Confidence           string  `json:"confidence" yaml:"confidence"`
	ProbabilityAvailable float64 `json:"probability_available" yaml:"probability_available"`
}

type VelovScore struct {
	StationID                  string  `json:"station_id" yaml:"station_id"`
	Score                      float64 `json:"score" yaml:"score"`
	AvailabilityScore          float64 `json:"availability_score" yaml:"availability_score"`
	DistanceScore              float64 `json:"distance_score" yaml:"distance_score"`
	FreshnessScore             float64 `json:"freshness_score" yaml:"freshness_score"`
	HistoricalReliabilityScore float64 `json:"historical_reliability_score" yaml:"historical_reliability_score"`
}

type Facility struct {
	ID              string   `json:"id" yaml:"id"`
	Name            string   `json:"name" yaml:"name"`
	Category        string   `json:"category" yaml:"category"`
	Latitude        float64  `json:"latitude" yaml:"latitude"`
	Longitude       float64  `json:"longitude" yaml:"longitude"`
	DistanceM       *float64 `json:"distance_m" yaml:"distance_m"`
	OpenStatus      string   `json:"open_status" yaml:"open_status"`
	OpeningHoursRaw *string  `json:"opening_hours_raw" yaml:"opening_hours_raw"`
}

type WasteClassification struct {
	Input       string   `json:"input" yaml:"input"`
	Category    string   `json:"category" yaml:"category"`
	Hazardous   bool     `json:"hazardous" yaml:"hazardous"`
	Confidence  float64  `json:"confidence" yaml:"confidence"`
	MatchedRule *string  `json:"matched_rule" yaml:"matched_rule"`
	Candidates  []string `json:"candidates" yaml:"candidates"`
}

type WasteFacility struct {
	ID                 string   `json:"id" yaml:"id"`
	Name               string   `json:"name" yaml:"name"`
	Latitude           float64  `json:"latitude" yaml:"latitude"`
	Longitude          float64  `json:"longitude" yaml:"longitude"`
	AcceptedCategories []string `json:"accepted_categories" yaml:"accepted_categories"`
	DistanceM          *float64 `json:"distance_m" yaml:"distance_m"`
	OpenStatus         string   `json:"open_status" yaml:"open_status"`
	OpeningHoursRaw    *string  `json:"opening_hours_raw" yaml:"opening_hours_raw"`
}

type TrafficCondition struct {
	ID          string     `json:"id" yaml:"id"`
	Area        string     `json:"area" yaml:"area"`
	Level       string     `json:"level" yaml:"level"`
	Description string     `json:"description" yaml:"description"`
	ObservedAt  *time.Time `json:"observed_at" yaml:"observed_at"`
}

type RoadEvent struct {
	ID          string     `json:"id" yaml:"id"`
	Type        string     `json:"type" yaml:"type"`
	Title       string     `json:"title" yaml:"title"`
	Description string     `json:"description" yaml:"description"`
	Latitude    *float64   `json:"latitude" yaml:"latitude"`
	Longitude   *float64   `json:"longitude" yaml:"longitude"`
	StartAt     *time.Time `json:"start_at" yaml:"start_at"`
	EndAt       *time.Time `json:"end_at" yaml:"end_at"`
}

type AccessibilityIncident struct {
	ID          string     `json:"id" yaml:"id"`
	Location    string     `json:"location" yaml:"location"`
	Status      string     `json:"status" yaml:"status"`
	Description string     `json:"description" yaml:"description"`
	StartAt     *time.Time `json:"start_at" yaml:"start_at"`
	EndAt       *time.Time `json:"end_at" yaml:"end_at"`
}

type AccessibilitySegment struct {
	Name     string              `json:"name" yaml:"name"`
	Status   string              `json:"status" yaml:"status"`
	Notes    string              `json:"notes" yaml:"notes"`
	Unknowns []string            `json:"unknowns" yaml:"unknowns"`
	Evidence []map[string]string `json:"evidence" yaml:"evidence"`
}

type IndicatorValue struct {
	Indicator  string     `json:"indicator" yaml:"indicator"`
	Value      any        `json:"value" yaml:"value"`
	Unit       *string    `json:"unit" yaml:"unit"`
	Zone       *string    `json:"zone" yaml:"zone"`
	ObservedAt *time.Time `json:"observed_at" yaml:"observed_at"`
	SourceID   *string    `json:"source_id" yaml:"source_id"`
	Supported  bool       `json:"supported" yaml:"supported"`
}

type TripOption struct {
	Mode                     string         `json:"mode" yaml:"mode"`
	Score                    float64        `json:"score" yaml:"score"`
	EstimatedDurationSeconds *int           `json:"estimated_duration_seconds" yaml:"estimated_duration_seconds"`
	DepartureAt              *time.Time     `json:"departure_at" yaml:"departure_at"`
	ArrivalAt                *time.Time     `json:"arrival_at" yaml:"arrival_at"`
	WalkingDistanceM         *int           `json:"walking_distance_m" yaml:"walking_distance_m"`
	Transfers                int            `json:"transfers" yaml:"transfers"`
	Availability             *string        `json:"availability" yaml:"availability"`
	Reliability              *float64       `json:"reliability" yaml:"reliability"`
	Accessibility            *string        `json:"accessibility" yaml:"accessibility"`
	Alerts                   []TransitAlert `json:"alerts" yaml:"alerts"`
	Realtime                 bool           `json:"realtime" yaml:"realtime"`
	Summary                  string         `json:"summary" yaml:"summary"`
	Sources                  []string       `json:"sources" yaml:"sources"`
}

type BriefingChange struct {
	Key      string  `json:"key" yaml:"key"`
	Previous *string `json:"previous" yaml:"previous"`
	Current  *string `json:"current" yaml:"current"`
}

type BriefingResult struct {
	Profile string           `json:"profile" yaml:"profile"`
	At      time.Time        `json:"at" yaml:"at"`
	Summary string           `json:"summary" yaml:"summary"`
	Facts   map[string]any   `json:"facts" yaml:"facts"`
	Changes []BriefingChange `json:"changes" yaml:"changes"`
}

type ResolvePlaceInput struct {
	Query string    `json:"query" yaml:"query"`
	Near  *PlaceRef `json:"near" yaml:"near"`
	Types []string  `json:"types" yaml:"types"`
	Limit int       `json:"limit" yaml:"limit"`
}

type NextDeparturesInput struct {
	Stop      PlaceRef   `json:"stop" yaml:"stop"`
	Line      string     `json:"line" yaml:"line"`
	Direction string     `json:"direction" yaml:"direction"`
	At        *time.Time `json:"at" yaml:"at"`
	Limit     int        `json:"limit" yaml:"limit"`
}

type MobilityStatusInput struct {
	Lines   []string   `json:"lines" yaml:"lines"`
	Areas   []PlaceRef `json:"areas" yaml:"areas"`
	Include []string   `json:"include" yaml:"include"`
}

type TripPreferences struct {
	MaxWalkingM       int  `json:"max_walking_m" yaml:"max_walking_m"`
	MinimumVelovBikes int  `json:"minimum_velov_bikes" yaml:"minimum_velov_bikes"`
	MinimumVelovDocks int  `json:"minimum_velov_docks" yaml:"minimum_velov_docks"`
	AvoidDisruptions  bool `json:"avoid_disruptions" yaml:"avoid_disruptions"`
	Wheelchair        bool `json:"wheelchair" yaml:"wheelchair"`
}

type TripOptionsInput struct {
	Origin        PlaceRef         `json:"origin" yaml:"origin"`
	Destination   PlaceRef         `json:"destination" yaml:"destination"`
	DepartureAt   *time.Time       `json:"departure_at" yaml:"departure_at"`
	ArrivalBefore *time.Time       `json:"arrival_before" yaml:"arrival_before"`
	Modes         []string         `json:"modes" yaml:"modes"`
	Preferences   *TripPreferences `json:"preferences" yaml:"preferences"`
}

type ParkingOptionsInput struct {
	Destination   PlaceRef `json:"destination" yaml:"destination"`
	Types         []string `json:"types" yaml:"types"`
	RadiusM       int      `json:"radius_m" yaml:"radius_m"`
	MinimumSpaces int      `json:"minimum_spaces" yaml:"minimum_spaces"`
	Limit         int      `json:"limit" yaml:"limit"`
}

type AccessibilityCheckInput struct {
	Origin      *PlaceRef  `json:"origin" yaml:"origin"`
	Destination *PlaceRef  `json:"destination" yaml:"destination"`
	Stops       []PlaceRef `json:"stops" yaml:"stops"`
	Needs       []string   `json:"needs" yaml:"needs"`
}

type NearbyFacilitiesInput struct {
	Location         PlaceRef   `json:"location" yaml:"location"`
	Categories       []string   `json:"categories" yaml:"categories"`
	RadiusM          int        `json:"radius_m" yaml:"radius_m"`
	OpenAt           *time.Time `json:"open_at" yaml:"open_at"`
	LimitPerCategory int        `json:"limit_per_category" yaml:"limit_per_category"`
}

type EnvironmentBriefInput struct {
	Location   PlaceRef   `json:"location" yaml:"location"`
	At         *time.Time `json:"at" yaml:"at"`
	Indicators []string   `json:"indicators" yaml:"indicators"`
}

type WasteDropoffInput struct {
	Item      string     `json:"item" yaml:"item"`
	Location  *PlaceRef  `json:"location" yaml:"location"`
	Transport string     `json:"transport" yaml:"transport"`
	OpenAt    *time.Time `json:"open_at" yaml:"open_at"`
	RadiusM   int        `json:"radius_m" yaml:"radius_m"`
	Limit     int        `json:"limit" yaml:"limit"`
}

type PersonalBriefingInput struct {
	Profile             string     `json:"profile" yaml:"profile"`
	At                  *time.Time `json:"at" yaml:"at"`
	CompareWithPrevious bool       `json:"compare_with_previous" yaml:"compare_with_previous"`
}

type JourneyScoringWeights struct {
	Duration       float64 `json:"duration" yaml:"duration"`
	Reliability    float64 `json:"reliability" yaml:"reliability"`
	Disruptions    float64 `json:"disruptions" yaml:"disruptions"`
	Walking        float64 `json:"walking" yaml:"walking"`
	Availability   float64 `json:"availability" yaml:"availability"`
	UserPreference float64 `json:"user_preference" yaml:"user_preference"`
}
