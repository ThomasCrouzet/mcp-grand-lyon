package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"go.yaml.in/yaml/v3"
)

type profileDocument struct {
	Places    map[string]map[string]any `yaml:"places"`
	Commutes  map[string]map[string]any `yaml:"commutes"`
	Briefings map[string]map[string]any `yaml:"briefings"`
}

func (s *Store) LoadProfiles(ctx context.Context, body []byte) error {
	var document profileDocument
	if err := yaml.Unmarshal(body, &document); err != nil {
		return fmt.Errorf("parse profiles: %w", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for key, place := range document.Places {
		label, _ := place["label"].(string)
		if label == "" {
			label = key
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO user_places
			(place_key,label,address,latitude,longitude,updated_at) VALUES (?,?,?,?,?,?)
			ON CONFLICT(place_key) DO UPDATE SET label=excluded.label,address=excluded.address,
			latitude=excluded.latitude,longitude=excluded.longitude,updated_at=excluded.updated_at`,
			key, label, nullableString(place["address"]), nullableFloat(place["latitude"]), nullableFloat(place["longitude"]), now)
		if err != nil {
			return err
		}
	}
	for key, value := range document.Commutes {
		if err := upsertJSONProfile(ctx, tx, "commute_profiles", key, value, now); err != nil {
			return err
		}
	}
	for key, value := range document.Briefings {
		if err := upsertJSONProfile(ctx, tx, "briefing_profiles", key, value, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func upsertJSONProfile(ctx context.Context, tx *sql.Tx, table, key string, value map[string]any, now string) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`INSERT INTO %s(profile_key,config_json,updated_at) VALUES (?,?,?)
		ON CONFLICT(profile_key) DO UPDATE SET config_json=excluded.config_json,updated_at=excluded.updated_at`, table)
	_, err = tx.ExecContext(ctx, query, key, string(payload), now)
	return err
}

func (s *Store) GetPlace(ctx context.Context, key string) (map[string]any, error) {
	var label string
	var address sql.NullString
	var latitude, longitude sql.NullFloat64
	var updated string
	err := s.DB.QueryRowContext(ctx, `SELECT label,address,latitude,longitude,updated_at FROM user_places WHERE place_key=?`, key).
		Scan(&label, &address, &latitude, &longitude, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value := map[string]any{"place_key": key, "label": label, "updated_at": updated}
	if address.Valid {
		value["address"] = address.String
	}
	if latitude.Valid {
		value["latitude"] = latitude.Float64
	}
	if longitude.Valid {
		value["longitude"] = longitude.Float64
	}
	return value, nil
}

func (s *Store) GetCommute(ctx context.Context, key string) (map[string]any, error) {
	return s.getJSONProfile(ctx, "commute_profiles", key)
}

func (s *Store) GetBriefing(ctx context.Context, key string) (map[string]any, error) {
	return s.getJSONProfile(ctx, "briefing_profiles", key)
}

func (s *Store) getJSONProfile(ctx context.Context, table, key string) (map[string]any, error) {
	var payload string
	err := s.DB.QueryRowContext(ctx, "SELECT config_json FROM "+table+" WHERE profile_key=?", key).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value := map[string]any{}
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Store) SaveSnapshot(ctx context.Context, key string, value map[string]any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO briefing_snapshots(profile_key,captured_at,payload_json) VALUES (?,?,?)`,
		key, time.Now().UTC().Format(time.RFC3339Nano), string(payload))
	return err
}

func (s *Store) LastSnapshot(ctx context.Context, key string) (map[string]any, error) {
	var payload string
	err := s.DB.QueryRowContext(ctx, `SELECT payload_json FROM briefing_snapshots WHERE profile_key=? ORDER BY id DESC LIMIT 1`, key).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value := map[string]any{}
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Store) RecordVelov(ctx context.Context, station model.VelovStation) error {
	if station.ID == "" || !validCoordinates(station.Latitude, station.Longitude) {
		return fmt.Errorf("invalid Velov station")
	}
	observed := time.Now().In(model.Paris)
	if station.ObservedAt != nil {
		observed = station.ObservedAt.In(model.Paris)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO velov_stations(station_id,name,latitude,longitude,capacity,updated_at)
		VALUES (?,?,?,?,?,?) ON CONFLICT(station_id) DO UPDATE SET name=excluded.name,latitude=excluded.latitude,
		longitude=excluded.longitude,capacity=excluded.capacity,updated_at=excluded.updated_at`,
		station.ID, station.Name, station.Latitude, station.Longitude, station.Capacity, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO velov_snapshots
		(station_id,observed_at,bikes_available,docks_available,hour_slot) VALUES (?,?,?,?,?)`,
		station.ID, observed.Format(time.RFC3339Nano), station.BikesAvailable, station.DocksAvailable, observed.Format("15"))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Reliability(ctx context.Context, stationID, hour string, minimumBikes int) (model.ReliabilityScore, error) {
	query := "SELECT bikes_available FROM velov_snapshots WHERE station_id=?"
	args := []any{stationID}
	if hour != "" {
		query += " AND hour_slot=?"
		args = append(args, hour)
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return model.ReliabilityScore{}, err
	}
	defer rows.Close()
	total, available := 0, 0
	for rows.Next() {
		var bikes int
		if err := rows.Scan(&bikes); err != nil {
			return model.ReliabilityScore{}, err
		}
		total++
		if bikes >= minimumBikes {
			available++
		}
	}
	if err := rows.Err(); err != nil {
		return model.ReliabilityScore{}, err
	}
	confidence := "low"
	if total >= 20 {
		confidence = "high"
	} else if total >= 5 {
		confidence = "medium"
	}
	probability := 0.0
	if total != 0 {
		probability = float64(available) / float64(total)
	}
	return model.ReliabilityScore{Score: probability, SampleCount: total, Confidence: confidence, ProbabilityAvailable: probability}, nil
}

func nullableString(value any) any {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return nil
}

func nullableFloat(value any) any {
	switch number := value.(type) {
	case float64:
		return number
	case int:
		return float64(number)
	default:
		return nil
	}
}
