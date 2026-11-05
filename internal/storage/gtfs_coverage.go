package storage

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

type GTFSImportInfo struct {
	SourceURL  string
	ImportedAt string
	FileHash   string
	Status     string
}

// ActiveGTFSImport returns the latest successful import identity, or nil.
func (s *Store) ActiveGTFSImport(ctx context.Context) (map[string]any, error) {
	var id int64
	var sourceURL, importedAt, fileHash, status string
	err := s.DB.QueryRowContext(ctx, `SELECT id,COALESCE(source_url,''),imported_at,COALESCE(file_hash,''),status
		FROM gtfs_imports WHERE status='ok' ORDER BY id DESC LIMIT 1`).Scan(&id, &sourceURL, &importedAt, &fileHash, &status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "source_url": sourceURL, "imported_at": importedAt, "file_hash": fileHash, "status": status}, nil
}

// GTFSCoverage reports whether the imported theoretical timetable covers a date,
// including calendar start/end bounds and date exceptions.
func (s *Store) GTFSCoverage(ctx context.Context, date time.Time) (map[string]any, error) {
	result := map[string]any{
		"checked_date": date.In(model.Paris).Format("2006-01-02"),
		"realtime":     false,
		"status":       "missing",
		"imported":     false,
	}
	var importedAt, sourceURL, fileHash, status sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT imported_at,source_url,file_hash,status FROM gtfs_imports ORDER BY id DESC LIMIT 1`).Scan(&importedAt, &sourceURL, &fileHash, &status)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	result["imported"] = true
	result["imported_at"] = importedAt.String
	result["source_url"] = sourceURL.String
	result["file_hash"] = fileHash.String
	result["import_status"] = status.String

	calendars, err := s.loadGTFSCalendars(ctx)
	if err != nil {
		return nil, err
	}
	exceptionRows, err := s.DB.QueryContext(ctx, `SELECT service_id,date,exception_type FROM gtfs_calendar_dates ORDER BY date`)
	if err != nil {
		return nil, err
	}
	defer exceptionRows.Close()
	exceptions := map[string]int{}
	var firstDate, lastDate string
	if len(calendars) != 0 {
		for _, calendar := range calendars {
			if firstDate == "" || calendar.startDate < firstDate {
				firstDate = calendar.startDate
			}
			if lastDate == "" || calendar.endDate > lastDate {
				lastDate = calendar.endDate
			}
		}
	}
	for exceptionRows.Next() {
		var serviceID, exceptionDate string
		var exceptionType int
		if err := exceptionRows.Scan(&serviceID, &exceptionDate, &exceptionType); err != nil {
			return nil, err
		}
		exceptions[serviceID+"\x00"+exceptionDate] = exceptionType
		if exceptionType == 1 {
			if firstDate == "" || exceptionDate < firstDate {
				firstDate = exceptionDate
			}
			if lastDate == "" || exceptionDate > lastDate {
				lastDate = exceptionDate
			}
		}
	}
	if err := exceptionRows.Err(); err != nil {
		return nil, err
	}
	if firstDate == "" {
		return result, nil
	}
	result["first_service_date"] = formatGTFSDate(firstDate)
	result["last_service_date"] = formatGTFSDate(lastDate)
	dateKey := date.In(model.Paris).Format("20060102")
	active := []string{}
	for serviceID := range calendars {
		if serviceRuns(serviceID, date, dateKey, calendars, exceptions) {
			active = append(active, serviceID)
		}
	}
	for key, exceptionType := range exceptions {
		if exceptionType != 1 {
			continue
		}
		serviceID := key[:len(key)-len(dateKey)-1]
		if key[len(key)-len(dateKey):] == dateKey && !containsStringValue(active, serviceID) {
			active = append(active, serviceID)
		}
	}
	sort.Strings(active)
	result["active_service_count"] = len(active)
	result["active_services"] = active
	switch {
	case len(active) != 0:
		result["status"] = "current"
	case dateKey < firstDate:
		result["status"] = "future_only"
	case dateKey > lastDate:
		result["status"] = "expired"
	default:
		result["status"] = "covered_no_service"
	}
	return result, nil
}

func formatGTFSDate(value string) string {
	parsed, err := time.Parse("20060102", value)
	if err != nil {
		return value
	}
	return parsed.Format("2006-01-02")
}

func containsStringValue(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
