package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func (s *Store) UpsertEntity(ctx context.Context, entity model.PlaceCandidate, sourceID, providerID string, properties map[string]any, aliases []string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsertEntityTx(ctx, tx, entity, sourceID, providerID, properties, aliases); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertEntityTx(ctx context.Context, tx *sql.Tx, entity model.PlaceCandidate, sourceID, providerID string, properties map[string]any, aliases []string) error {
	if entity.ID == "" || entity.Name == "" {
		return fmt.Errorf("entity ID and name are required")
	}
	if !validCoordinates(entity.Latitude, entity.Longitude) {
		return fmt.Errorf("entity coordinates are invalid")
	}
	payload, err := json.Marshal(properties)
	if err != nil {
		return fmt.Errorf("encode entity properties: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO entities
		(logical_id,source_id,provider_id,entity_type,name,normalized_name,latitude,longitude,properties_json,indexed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT(logical_id) DO UPDATE SET
		source_id=excluded.source_id,provider_id=excluded.provider_id,entity_type=excluded.entity_type,
		name=excluded.name,normalized_name=excluded.normalized_name,latitude=excluded.latitude,
		longitude=excluded.longitude,properties_json=excluded.properties_json,indexed_at=excluded.indexed_at`,
		entity.ID, sourceID, providerID, entity.Type, entity.Name, model.Normalize(entity.Name),
		entity.Latitude, entity.Longitude, string(payload), now)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM entity_aliases WHERE logical_id=?", entity.ID); err != nil {
		return err
	}
	for _, alias := range aliases {
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO entity_aliases(logical_id,alias) VALUES (?,?)", entity.ID, alias); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM entity_fts WHERE logical_id=?", entity.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO entity_fts(logical_id,name,aliases,normalized_name) VALUES (?,?,?,?)",
		entity.ID, entity.Name, strings.Join(aliases, " "), model.Normalize(entity.Name)); err != nil {
		return err
	}
	rtreeID := stableEntityID(entity.ID)
	if _, err = tx.ExecContext(ctx, "DELETE FROM entity_rtree WHERE id=?", rtreeID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO entity_rtree(id,minx,maxx,miny,maxy) VALUES (?,?,?,?,?)",
		rtreeID, entity.Longitude, entity.Longitude, entity.Latitude, entity.Latitude); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO entity_rtree_map(rtree_id,logical_id) VALUES (?,?)", rtreeID, entity.ID); err != nil {
		return err
	}
	return nil
}

func deleteEntitiesBySourceTx(ctx context.Context, tx *sql.Tx, sourceID string) error {
	statements := []string{
		`DELETE FROM entity_fts WHERE logical_id IN (SELECT logical_id FROM entities WHERE source_id=?)`,
		`DELETE FROM entity_aliases WHERE logical_id IN (SELECT logical_id FROM entities WHERE source_id=?)`,
		`DELETE FROM entity_rtree WHERE id IN (
			SELECT rtree_id FROM entity_rtree_map WHERE logical_id IN (
				SELECT logical_id FROM entities WHERE source_id=?
			)
		)`,
		`DELETE FROM entity_rtree_map WHERE logical_id IN (SELECT logical_id FROM entities WHERE source_id=?)`,
		`DELETE FROM entities WHERE source_id=?`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement, sourceID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetEntity(ctx context.Context, id string) (*model.PlaceCandidate, error) {
	return scanCandidate(s.DB.QueryRowContext(ctx, `SELECT logical_id,name,entity_type,latitude,longitude FROM entities WHERE logical_id=?`, id))
}

func (s *Store) HasTransitIndex(ctx context.Context) (bool, error) {
	var present bool
	err := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM entities WHERE entity_type='transport_stop')").Scan(&present)
	return present, err
}

func (s *Store) SearchEntities(ctx context.Context, query string, limit int) ([]model.PlaceCandidate, error) {
	limit = boundedLimit(limit, 10, 100)
	tokens := safeSearchTokens(query)
	if len(tokens) == 0 {
		return []model.PlaceCandidate{}, nil
	}
	match := make([]string, len(tokens))
	for i, token := range tokens {
		match[i] = `"` + strings.ReplaceAll(token, `"`, ``) + `"*`
	}
	results, err := s.queryCandidates(ctx, `SELECT e.logical_id,e.name,e.entity_type,e.latitude,e.longitude
		FROM entity_fts f JOIN entities e ON e.logical_id=f.logical_id
		WHERE entity_fts MATCH ? LIMIT ?`, strings.Join(match, " "), limit)
	if err != nil {
		return nil, err
	}
	if len(results) != 0 {
		return results, nil
	}
	return s.queryCandidates(ctx, `SELECT logical_id,name,entity_type,latitude,longitude FROM entities
		WHERE normalized_name LIKE ? LIMIT ?`, "%"+tokens[0]+"%", limit)
}

func (s *Store) NearEntities(ctx context.Context, point model.Point, radiusM float64, entityTypes []string, limit int) ([]model.PlaceCandidate, error) {
	if !validCoordinates(point.Latitude, point.Longitude) || radiusM < 0 {
		return nil, fmt.Errorf("invalid proximity query")
	}
	limit = boundedLimit(limit, 20, 200)
	latDelta := radiusM / 111320.0
	lonScale := math.Cos(point.Latitude * math.Pi / 180)
	if math.Abs(lonScale) < 0.01 {
		lonScale = 0.01
	}
	lonDelta := radiusM / (111320.0 * lonScale)
	args := []any{point.Longitude + lonDelta, point.Longitude - lonDelta, point.Latitude + latDelta, point.Latitude - latDelta}
	filter := ""
	if len(entityTypes) != 0 {
		placeholders := make([]string, len(entityTypes))
		for i, entityType := range entityTypes {
			placeholders[i] = "?"
			args = append(args, entityType)
		}
		filter = " AND e.entity_type IN (" + strings.Join(placeholders, ",") + ")"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT e.logical_id,e.name,e.entity_type,e.latitude,e.longitude
		FROM entity_rtree r JOIN entity_rtree_map m ON m.rtree_id=r.id
		JOIN entities e ON e.logical_id=m.logical_id
		WHERE r.minx<=? AND r.maxx>=? AND r.miny<=? AND r.maxy>=?`+filter, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []model.PlaceCandidate{}
	for rows.Next() {
		candidate, err := scanCandidateRows(rows)
		if err != nil {
			return nil, err
		}
		distance := model.Distance(point, model.Point{Latitude: candidate.Latitude, Longitude: candidate.Longitude})
		if distance <= radiusM {
			candidate.DistanceM = &distance
			results = append(results, *candidate)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortCandidatesByDistance(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanCandidate(row rowScanner) (*model.PlaceCandidate, error) {
	candidate := &model.PlaceCandidate{Confidence: 0.9}
	err := row.Scan(&candidate.ID, &candidate.Name, &candidate.Type, &candidate.Latitude, &candidate.Longitude)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	candidate.Label = candidate.Name
	return candidate, nil
}

func scanCandidateRows(rows *sql.Rows) (*model.PlaceCandidate, error) {
	return scanCandidate(rows)
}

func (s *Store) queryCandidates(ctx context.Context, query string, args ...any) ([]model.PlaceCandidate, error) {
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []model.PlaceCandidate{}
	for rows.Next() {
		candidate, err := scanCandidateRows(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, *candidate)
	}
	return results, rows.Err()
}

func safeSearchTokens(query string) []string {
	cleaned := strings.ReplaceAll(model.Normalize(query), `"`, " ")
	return strings.Fields(cleaned)
}

func stableEntityID(id string) int64 {
	hash := sha256.Sum256([]byte(id))
	return int64(binary.BigEndian.Uint64(hash[:8]) & math.MaxInt64)
}

func validCoordinates(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsNaN(longitude) && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}

func boundedLimit(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func sortCandidatesByDistance(values []model.PlaceCandidate) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && *values[j].DistanceM < *values[j-1].DistanceM; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
