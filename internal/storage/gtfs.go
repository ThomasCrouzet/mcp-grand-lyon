package storage

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

const (
	maxGTFSArchiveBytes = int64(300 * 1024 * 1024)
	maxGTFSMemberBytes  = int64(1024 * 1024 * 1024)
	maxGTFSTotalBytes   = int64(2 * 1024 * 1024 * 1024)
)

var gtfsFiles = []string{
	"agency.txt", "routes.txt", "stops.txt", "trips.txt", "stop_times.txt",
	"calendar.txt", "calendar_dates.txt", "transfers.txt",
}

func (s *Store) ImportGTFS(ctx context.Context, path string) (map[string]int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxGTFSArchiveBytes {
		return nil, fmt.Errorf("GTFS archive exceeds %d bytes", maxGTFSArchiveBytes)
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open GTFS archive: %w", err)
	}
	defer reader.Close()
	members, err := selectGTFSMembers(reader.File)
	if err != nil {
		return nil, err
	}
	if err := validateGTFSMembers(members); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := deleteEntitiesBySourceTx(ctx, tx, "gtfs_tcl"); err != nil {
		return nil, fmt.Errorf("clear GTFS entity index: %w", err)
	}
	for _, table := range []string{"gtfs_transfers", "gtfs_stop_times", "gtfs_trips", "gtfs_stops", "gtfs_routes", "gtfs_agencies", "gtfs_calendar", "gtfs_calendar_dates"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return nil, err
		}
	}
	counts := map[string]int{}
	for _, name := range gtfsFiles {
		member := members[name]
		if member == nil {
			counts[gtfsCountKey(name)] = 0
			continue
		}
		count, err := importGTFSFile(ctx, tx, name, member)
		if err != nil {
			return nil, fmt.Errorf("import %s: %w", name, err)
		}
		counts[gtfsCountKey(name)] = count
	}
	if err := validateGTFSCounts(counts); err != nil {
		return nil, err
	}
	counts["entities_indexed"] = counts["stops"]
	hash, err := hashFile(path)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO gtfs_imports(source_url,imported_at,file_hash,status) VALUES (?,?,?,'ok')`,
		path, time.Now().UTC().Format(time.RFC3339Nano), hash); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return counts, nil
}

func validateGTFSMembers(members map[string]*zip.File) error {
	for _, name := range []string{"agency.txt", "routes.txt", "stops.txt", "trips.txt", "stop_times.txt"} {
		if members[name] == nil {
			return fmt.Errorf("GTFS archive is missing %s", name)
		}
	}
	if members["calendar.txt"] == nil && members["calendar_dates.txt"] == nil {
		return fmt.Errorf("GTFS archive is missing calendar data")
	}
	return nil
}

func validateGTFSCounts(counts map[string]int) error {
	for _, key := range []string{"agencies", "routes", "stops", "trips", "stop_times"} {
		if counts[key] == 0 {
			return fmt.Errorf("GTFS %s data is empty", key)
		}
	}
	if counts["calendar"] == 0 && counts["calendar_dates"] == 0 {
		return fmt.Errorf("GTFS calendar data is empty")
	}
	return nil
}

func selectGTFSMembers(files []*zip.File) (map[string]*zip.File, error) {
	selected := map[string]*zip.File{}
	var total int64
	for _, file := range files {
		name := filepath.Base(strings.ReplaceAll(file.Name, "\\", "/"))
		wanted := false
		for _, expected := range gtfsFiles {
			if name == expected {
				wanted = true
				break
			}
		}
		if !wanted {
			continue
		}
		if selected[name] != nil {
			return nil, fmt.Errorf("GTFS archive contains duplicate %s", name)
		}
		if file.UncompressedSize64 > uint64(maxGTFSMemberBytes) {
			return nil, fmt.Errorf("GTFS member %s exceeds %d bytes", file.Name, maxGTFSMemberBytes)
		}
		total += int64(file.UncompressedSize64)
		if total > maxGTFSTotalBytes {
			return nil, fmt.Errorf("GTFS expanded data exceeds %d bytes", maxGTFSTotalBytes)
		}
		selected[name] = file
	}
	return selected, nil
}

func importGTFSFile(ctx context.Context, tx *sql.Tx, name string, file *zip.File) (int, error) {
	stream, err := file.Open()
	if err != nil {
		return 0, err
	}
	defer stream.Close()
	limited := &io.LimitedReader{R: stream, N: maxGTFSMemberBytes + 1}
	reader := csv.NewReader(limited)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err == io.EOF {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	for i := range header {
		header[i] = strings.TrimSpace(strings.TrimPrefix(header[i], "\ufeff"))
	}
	count := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		row := make(map[string]string, len(header))
		for i, key := range header {
			if key != "" && i < len(record) {
				row[key] = strings.TrimSpace(record[i])
			}
		}
		if err := insertGTFSRow(ctx, tx, name, row); err != nil {
			return 0, fmt.Errorf("row %d: %w", count+2, err)
		}
		count++
	}
	if limited.N <= 0 {
		return 0, fmt.Errorf("member exceeds %d bytes", maxGTFSMemberBytes)
	}
	return count, nil
}

func insertGTFSRow(ctx context.Context, tx *sql.Tx, name string, row map[string]string) error {
	switch name {
	case "agency.txt":
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO gtfs_agencies(agency_id,agency_name,agency_url,agency_timezone) VALUES (?,?,?,?)`,
			valueOr(row["agency_id"], "default"), row["agency_name"], nullText(row["agency_url"]), nullText(row["agency_timezone"]))
		return err
	case "routes.txt":
		routeType, err := optionalInt(row["route_type"])
		if err != nil || row["route_id"] == "" {
			return fmt.Errorf("invalid route: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO gtfs_routes(route_id,agency_id,route_short_name,route_long_name,route_type,route_color) VALUES (?,?,?,?,?,?)`,
			row["route_id"], nullText(row["agency_id"]), nullText(row["route_short_name"]), nullText(row["route_long_name"]), routeType, nullText(row["route_color"]))
		return err
	case "stops.txt":
		latitude, err := floatOrZero(row["stop_lat"])
		if err != nil {
			return err
		}
		longitude, err := floatOrZero(row["stop_lon"])
		if err != nil || !validCoordinates(latitude, longitude) || row["stop_id"] == "" {
			return fmt.Errorf("invalid stop")
		}
		locationType, err := intOrZero(row["location_type"])
		if err != nil {
			return err
		}
		wheelchair, err := optionalInt(row["wheelchair_boarding"])
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO gtfs_stops
			(stop_id,stop_code,stop_name,stop_lat,stop_lon,location_type,parent_station,wheelchair_boarding) VALUES (?,?,?,?,?,?,?,?)`,
			row["stop_id"], nullText(row["stop_code"]), row["stop_name"], latitude, longitude, locationType, nullText(row["parent_station"]), wheelchair)
		if err != nil {
			return err
		}
		entity := model.PlaceCandidate{
			ID: "gtfs:stop:" + row["stop_id"], Name: row["stop_name"], Label: row["stop_name"],
			Type: "transport_stop", Latitude: latitude, Longitude: longitude, Confidence: 1,
		}
		return upsertEntityTx(ctx, tx, entity, "gtfs_tcl", row["stop_id"], nil, nil)
	case "trips.txt":
		direction, err := optionalInt(row["direction_id"])
		if err != nil || row["trip_id"] == "" || row["route_id"] == "" || row["service_id"] == "" {
			return fmt.Errorf("invalid trip: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO gtfs_trips(trip_id,route_id,service_id,trip_headsign,direction_id,shape_id) VALUES (?,?,?,?,?,?)`,
			row["trip_id"], row["route_id"], row["service_id"], nullText(row["trip_headsign"]), direction, nullText(row["shape_id"]))
		return err
	case "stop_times.txt":
		arrival := valueOr(row["arrival_time"], row["departure_time"])
		departure := valueOr(row["departure_time"], row["arrival_time"])
		if arrival == "" {
			arrival = "00:00:00"
		}
		if departure == "" {
			departure = "00:00:00"
		}
		if _, err := parseGTFSServiceTime(arrival, time.Date(2026, 1, 1, 0, 0, 0, 0, model.Paris)); err != nil {
			return err
		}
		if _, err := parseGTFSServiceTime(departure, time.Date(2026, 1, 1, 0, 0, 0, 0, model.Paris)); err != nil {
			return err
		}
		sequence, err := strconv.Atoi(row["stop_sequence"])
		if err != nil || row["trip_id"] == "" || row["stop_id"] == "" {
			return fmt.Errorf("invalid stop time")
		}
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO gtfs_stop_times(trip_id,arrival_time,departure_time,stop_id,stop_sequence) VALUES (?,?,?,?,?)`,
			row["trip_id"], arrival, departure, row["stop_id"], sequence)
		return err
	case "calendar.txt":
		values := make([]int, 7)
		for i, field := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
			value, err := intOrZero(row[field])
			if err != nil {
				return err
			}
			values[i] = value
		}
		if row["service_id"] == "" {
			return fmt.Errorf("calendar service ID is empty")
		}
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO gtfs_calendar
			(service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			row["service_id"], values[0], values[1], values[2], values[3], values[4], values[5], values[6], valueOr(row["start_date"], "19700101"), valueOr(row["end_date"], "20991231"))
		return err
	case "calendar_dates.txt":
		exception, err := strconv.Atoi(valueOr(row["exception_type"], "1"))
		if err != nil || row["service_id"] == "" || row["date"] == "" {
			return fmt.Errorf("invalid calendar exception")
		}
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO gtfs_calendar_dates(service_id,date,exception_type) VALUES (?,?,?)`, row["service_id"], row["date"], exception)
		return err
	case "transfers.txt":
		transferType, err := optionalInt(row["transfer_type"])
		if err != nil {
			return err
		}
		minimum, err := optionalInt(row["min_transfer_time"])
		if err != nil || row["from_stop_id"] == "" || row["to_stop_id"] == "" {
			return fmt.Errorf("invalid transfer")
		}
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO gtfs_transfers(from_stop_id,to_stop_id,transfer_type,min_transfer_time) VALUES (?,?,?,?)`,
			row["from_stop_id"], row["to_stop_id"], transferType, minimum)
		return err
	default:
		return fmt.Errorf("unsupported GTFS file %q", name)
	}
}

func (s *Store) SearchStops(ctx context.Context, query string, limit int) ([]model.PlaceCandidate, error) {
	limit = boundedLimit(limit, 10, 100)
	tokens := safeSearchTokens(query)
	if len(tokens) == 0 {
		return []model.PlaceCandidate{}, nil
	}
	pattern := "%" + tokens[0] + "%"
	rows, err := s.DB.QueryContext(ctx, `SELECT stop_id,stop_name,stop_lat,stop_lon FROM gtfs_stops
		WHERE lower(replace(replace(stop_name,'-',' '),'''',' ')) LIKE ? OR lower(stop_name) LIKE ? LIMIT ?`, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []model.PlaceCandidate{}
	for rows.Next() {
		var candidate model.PlaceCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Name, &candidate.Latitude, &candidate.Longitude); err != nil {
			return nil, err
		}
		candidate.ID = "gtfs:stop:" + candidate.ID
		candidate.Label = candidate.Name
		candidate.Type = "transport_stop"
		candidate.Confidence = 0.85
		results = append(results, candidate)
	}
	return results, rows.Err()
}

func (s *Store) GetStop(ctx context.Context, id string) (*model.PlaceCandidate, error) {
	id = strings.TrimPrefix(id, "gtfs:stop:")
	var candidate model.PlaceCandidate
	err := s.DB.QueryRowContext(ctx, `SELECT stop_id,stop_name,stop_lat,stop_lon FROM gtfs_stops WHERE stop_id=?`, id).
		Scan(&candidate.ID, &candidate.Name, &candidate.Latitude, &candidate.Longitude)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	candidate.ID = "gtfs:stop:" + candidate.ID
	candidate.Label = candidate.Name
	candidate.Type = "transport_stop"
	candidate.Confidence = 1
	return &candidate, nil
}

func (s *Store) WheelchairBoarding(ctx context.Context, id string) (*int, error) {
	id = strings.TrimPrefix(id, "gtfs:stop:")
	var value sql.NullInt64
	err := s.DB.QueryRowContext(ctx, "SELECT wheelchair_boarding FROM gtfs_stops WHERE stop_id=?", id).Scan(&value)
	if err == sql.ErrNoRows || !value.Valid {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := int(value.Int64)
	return &result, nil
}

type gtfsDepartureRow struct {
	departure, headsign, routeID, shortName, longName, serviceID string
}

type gtfsCalendar struct {
	weekdays           [7]int
	startDate, endDate string
}

func (s *Store) ScheduledDepartures(ctx context.Context, stopID string, at time.Time, line, direction string, limit int) ([]model.Departure, error) {
	stopID = strings.TrimPrefix(stopID, "gtfs:stop:")
	limit = boundedLimit(limit, 6, 100)
	rows, err := s.DB.QueryContext(ctx, `WITH RECURSIVE selected_stops(stop_id) AS (
		SELECT stop_id FROM gtfs_stops WHERE stop_id=?
		UNION
		SELECT child.stop_id FROM gtfs_stops child
		JOIN selected_stops parent ON child.parent_station=parent.stop_id
		JOIN gtfs_stops root ON root.stop_id=?
		WHERE COALESCE(root.location_type,0)=1 AND COALESCE(child.location_type,0)<>1
	)
		SELECT st.departure_time,COALESCE(t.trip_headsign,''),r.route_id,
		COALESCE(r.route_short_name,''),COALESCE(r.route_long_name,''),t.service_id
		FROM gtfs_stop_times st JOIN gtfs_trips t ON t.trip_id=st.trip_id
		JOIN gtfs_routes r ON r.route_id=t.route_id
		WHERE st.stop_id IN (SELECT stop_id FROM selected_stops)
		ORDER BY t.trip_id,st.stop_sequence`, stopID, stopID)
	if err != nil {
		return nil, err
	}
	departureRows := []gtfsDepartureRow{}
	for rows.Next() {
		var row gtfsDepartureRow
		if err := rows.Scan(&row.departure, &row.headsign, &row.routeID, &row.shortName, &row.longName, &row.serviceID); err != nil {
			rows.Close()
			return nil, err
		}
		departureRows = append(departureRows, row)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	calendars, err := s.loadGTFSCalendars(ctx)
	if err != nil {
		return nil, err
	}
	atParis := at.In(model.Paris)
	startUTC := atParis.UTC()
	endUTC := startUTC.Add(24 * time.Hour)
	firstDay := civilDate(atParis).AddDate(0, 0, -4)
	exceptions, err := s.loadGTFSExceptions(ctx, firstDay, firstDay.AddDate(0, 0, 5))
	if err != nil {
		return nil, err
	}
	results := []model.Departure{}
	for offset := 0; offset < 6; offset++ {
		serviceDate := firstDay.AddDate(0, 0, offset)
		dateKey := serviceDate.Format("20060102")
		for _, row := range departureRows {
			if !serviceRuns(row.serviceID, serviceDate, dateKey, calendars, exceptions) {
				continue
			}
			name := valueOr(row.shortName, valueOr(row.longName, row.routeID))
			if !model.LineMatches(line, name, row.routeID) {
				continue
			}
			if direction != "" && !strings.Contains(strings.ToLower(row.headsign), strings.ToLower(direction)) {
				continue
			}
			when, err := parseGTFSServiceTime(row.departure, serviceDate)
			if err != nil || when.UTC().Before(startUTC) || !when.UTC().Before(endUTC) {
				continue
			}
			scheduled := when
			results = append(results, model.Departure{LineID: "tcl:" + row.routeID, LineName: name, Destination: row.headsign, ScheduledAt: &scheduled, ExpectedAt: &scheduled})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].ScheduledAt.Before(*results[j].ScheduledAt)
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *Store) loadGTFSCalendars(ctx context.Context) (map[string]gtfsCalendar, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date FROM gtfs_calendar`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string]gtfsCalendar{}
	for rows.Next() {
		var id string
		var value gtfsCalendar
		if err := rows.Scan(&id, &value.weekdays[0], &value.weekdays[1], &value.weekdays[2], &value.weekdays[3], &value.weekdays[4], &value.weekdays[5], &value.weekdays[6], &value.startDate, &value.endDate); err != nil {
			return nil, err
		}
		values[id] = value
	}
	return values, rows.Err()
}

func (s *Store) loadGTFSExceptions(ctx context.Context, first, last time.Time) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT service_id,date,exception_type FROM gtfs_calendar_dates WHERE date BETWEEN ? AND ?`, first.Format("20060102"), last.Format("20060102"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string]int{}
	for rows.Next() {
		var id, date string
		var exception int
		if err := rows.Scan(&id, &date, &exception); err != nil {
			return nil, err
		}
		values[id+"\x00"+date] = exception
	}
	return values, rows.Err()
}

func serviceRuns(id string, day time.Time, dateKey string, calendars map[string]gtfsCalendar, exceptions map[string]int) bool {
	exception := exceptions[id+"\x00"+dateKey]
	if exception == 1 {
		return true
	}
	if exception == 2 {
		return false
	}
	calendar, ok := calendars[id]
	if !ok || dateKey < calendar.startDate || dateKey > calendar.endDate {
		return false
	}
	weekday := (int(day.Weekday()) + 6) % 7
	return calendar.weekdays[weekday] != 0
}

func parseGTFSServiceTime(value string, serviceDate time.Time) (time.Time, error) {
	return model.ParseGTFSTime(value, serviceDate)
}

func civilDate(value time.Time) time.Time {
	value = value.In(model.Paris)
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, model.Paris)
}

func gtfsCountKey(name string) string {
	key := strings.TrimSuffix(name, ".txt")
	if key == "agency" {
		return "agencies"
	}
	return key
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func optionalInt(value string) (any, error) {
	if value == "" {
		return nil, nil
	}
	result, err := strconv.Atoi(value)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func intOrZero(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func floatOrZero(value string) (float64, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.ParseFloat(value, 64)
}

func nullText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func valueOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
