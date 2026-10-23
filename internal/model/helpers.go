package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type Point struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

var Paris = func() *time.Location {
	location, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		panic(err)
	}
	return location
}()

func Now() time.Time        { return time.Now().In(Paris) }
func Ptr[T any](value T) *T { return &value }
func Value[T any](value *T) T {
	if value != nil {
		return *value
	}
	var zero T
	return zero
}
func NewEnvelope(status, summary string, data map[string]any) Envelope {
	var id [16]byte
	_, _ = rand.Read(id[:])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	hexID := hex.EncodeToString(id[:])
	if data == nil {
		data = map[string]any{}
	}
	return Envelope{SchemaVersion: "1.0", RequestID: fmt.Sprintf("%s-%s-%s-%s-%s", hexID[:8], hexID[8:12], hexID[12:16], hexID[16:20], hexID[20:]), Status: status, GeneratedAt: Now(), Summary: summary, Data: data, Sources: []SourceProvenance{}, Warnings: []WarningItem{}}
}
func (envelope *Envelope) Warn(code, source, message string, retryable bool) {
	envelope.Warnings = append(envelope.Warnings, WarningItem{Code: code, Source: Ptr(source), Message: message, Retryable: retryable})
	envelope.Degraded = true
	if envelope.Status == "ok" {
		envelope.Status = "partial"
	}
}
func (ref PlaceRef) Validate() error {
	modes := 0
	for _, text := range []string{ref.PlaceID, ref.Query, ref.ProfilePlace} {
		if strings.TrimSpace(text) != "" {
			modes++
		}
	}
	if ref.Latitude != nil || ref.Longitude != nil {
		modes++
		if ref.Latitude == nil || ref.Longitude == nil {
			return errors.New("latitude and longitude are both required")
		}
		if math.IsNaN(*ref.Latitude) || math.IsNaN(*ref.Longitude) || *ref.Latitude < -90 || *ref.Latitude > 90 || *ref.Longitude < -180 || *ref.Longitude > 180 {
			return errors.New("coordinates are outside their valid range")
		}
	}
	if modes != 1 {
		return errors.New("exactly one place mode is required")
	}
	if len([]rune(ref.PlaceID)) > 200 || len([]rune(ref.Query)) > 200 || len([]rune(ref.ProfilePlace)) > 64 {
		return errors.New("place reference is too long")
	}
	return nil
}
func Normalize(value string) string {
	value = norm.NFD.String(value)
	value = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		if r == '-' || r == '\'' {
			return ' '
		}
		return unicode.ToLower(r)
	}, value)
	return strings.Join(strings.Fields(value), " ")
}
func Distance(a, b Point) float64 {
	const radians = math.Pi / 180
	dlat, dlon := (b.Latitude-a.Latitude)*radians, (b.Longitude-a.Longitude)*radians
	h := math.Pow(math.Sin(dlat/2), 2) + math.Cos(a.Latitude*radians)*math.Cos(b.Latitude*radians)*math.Pow(math.Sin(dlon/2), 2)
	return 2 * 6371000 * math.Asin(math.Sqrt(math.Min(1, h)))
}
func LineMatches(query string, identifiers ...string) bool {
	clean := func(s string) string {
		s = strings.ToUpper(strings.Join(strings.Fields(s), " "))
		for {
			before := s
			for _, prefix := range []string{"METRO ", "MÉTRO ", "LIGNE ", "BUS ", "TRAM ", "TRAMWAY ", "LINE "} {
				s = strings.TrimPrefix(s, prefix)
			}
			if s == before {
				break
			}
		}
		parts := strings.Split(s, ":")
		for i := len(parts) - 1; i >= 0; i-- {
			part := strings.TrimSpace(parts[i])
			switch part {
			case "", "SYTRAL", "TCL", "LINE", "ACTIV", "MDL", "METRO", "MÉTRO", "TRAM", "BUS", "FUNICULAIRE", "FUNI", "LIGNE", "LINEID", "ROUTE":
				continue
			}
			s = part
			break
		}
		if value, err := strconv.Atoi(s); err == nil {
			return strconv.Itoa(value)
		}
		return s
	}
	if strings.TrimSpace(query) == "" {
		return true
	}
	for _, candidate := range identifiers {
		if clean(query) == clean(candidate) {
			return true
		}
	}
	return false
}
func ParseGTFSTime(value string, serviceDate time.Time) (time.Time, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return time.Time{}, errors.New("invalid GTFS time")
	}
	n := [3]int{}
	for i, part := range parts {
		var err error
		n[i], err = strconv.Atoi(part)
		if err != nil {
			return time.Time{}, errors.New("invalid GTFS time")
		}
	}
	if n[0] < 0 || n[0] > 99 || n[1] < 0 || n[1] > 59 || n[2] < 0 || n[2] > 59 {
		return time.Time{}, errors.New("invalid GTFS time")
	}
	day := serviceDate.In(Paris)
	noon := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, Paris)
	return noon.Add(-12*time.Hour + time.Duration(n[0]*3600+n[1]*60+n[2])*time.Second), nil
}
