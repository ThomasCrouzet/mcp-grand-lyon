package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"go.yaml.in/yaml/v3"
)

type sourceDocument struct {
	Sources map[string]map[string]any `yaml:"sources"`
}

func (s *Store) LoadSources(ctx context.Context, body []byte) error {
	var document sourceDocument
	if err := yaml.Unmarshal(body, &document); err != nil {
		return fmt.Errorf("parse sources: %w", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, config := range document.Sources {
		if err := upsertSource(ctx, tx, id, config); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func upsertSource(ctx context.Context, tx *sql.Tx, id string, config map[string]any) error {
	payload, err := json.Marshal(config)
	if err != nil {
		return err
	}
	var previousMetadata string
	err = tx.QueryRowContext(ctx, "SELECT metadata_json FROM source_registry WHERE source_id=?", id).Scan(&previousMetadata)
	if err == nil && equivalentJSON(previousMetadata, payload) {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	enabled := boolValue(config["enabled"], true)
	status := stringValue(config["status"], "")
	if status == "" {
		if enabled {
			status = "UNRESOLVED"
		} else {
			status = "DISABLED"
		}
	}
	tableName := config["table_name"]
	if tableName == nil {
		if hints, ok := config["exact_table_hints"].([]any); ok && len(hints) != 0 {
			tableName = hints[0]
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO source_registry
		(source_id,provider,enabled,required,status,service,schema_name,table_name,collection,resolved_url,
		parser,realtime,ttl_seconds,maximum_stale_seconds,authentication,attribution,license,metadata_json,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(source_id) DO UPDATE SET
		provider=excluded.provider,enabled=excluded.enabled,required=excluded.required,status=excluded.status,
		service=excluded.service,schema_name=excluded.schema_name,table_name=excluded.table_name,
		collection=excluded.collection,resolved_url=excluded.resolved_url,parser=excluded.parser,
		realtime=excluded.realtime,ttl_seconds=excluded.ttl_seconds,
		maximum_stale_seconds=excluded.maximum_stale_seconds,authentication=excluded.authentication,
		attribution=excluded.attribution,license=excluded.license,metadata_json=excluded.metadata_json,
		updated_at=excluded.updated_at`, id, stringValue(config["provider"], "datapusher"), boolInt(enabled),
		boolInt(boolValue(config["required"], false)), status, config["service"], config["schema_name"], tableName,
		config["collection"], config["resolved_url"], config["parser"], boolInt(boolValue(config["realtime"], false)),
		intValue(config["ttl_seconds"], 60), intValue(config["maximum_stale_seconds"], 300), config["authentication"],
		config["attribution"], stringValue(config["license"], "unknown"), string(payload), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func equivalentJSON(previous string, current []byte) bool {
	var left, right any
	if json.Unmarshal([]byte(previous), &left) != nil || json.Unmarshal(current, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func (s *Store) ListSources(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx, sourceSelect+" ORDER BY source_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []map[string]any{}
	for rows.Next() {
		value, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) GetSource(ctx context.Context, id string) (map[string]any, error) {
	value, err := scanSource(s.DB.QueryRowContext(ctx, sourceSelect+" WHERE source_id=?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return value, err
}

func (s *Store) SetSourceStatus(ctx context.Context, id, status, table string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE source_registry SET status=?,table_name=COALESCE(NULLIF(?,''),table_name),updated_at=? WHERE source_id=?`,
		status, table, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("source %q does not exist", id)
	}
	return nil
}

const sourceSelect = `SELECT source_id,provider,enabled,required,status,service,schema_name,table_name,
	collection,resolved_url,parser,realtime,ttl_seconds,maximum_stale_seconds,authentication,attribution,
	license,metadata_json,updated_at FROM source_registry`

func scanSource(row rowScanner) (map[string]any, error) {
	var id, provider, status, license, metadata, updated string
	var enabled, required, realtime int
	var ttl, stale int
	var service, schemaName, tableName, collection, resolvedURL, parser, authentication, attribution sql.NullString
	err := row.Scan(&id, &provider, &enabled, &required, &status, &service, &schemaName, &tableName,
		&collection, &resolvedURL, &parser, &realtime, &ttl, &stale, &authentication, &attribution,
		&license, &metadata, &updated)
	if err != nil {
		return nil, err
	}
	value := map[string]any{
		"source_id": id, "provider": provider, "enabled": enabled != 0, "required": required != 0,
		"status": status, "realtime": realtime != 0, "ttl_seconds": ttl,
		"maximum_stale_seconds": stale, "license": license, "metadata_json": metadata, "updated_at": updated,
	}
	putNullString(value, "service", service)
	putNullString(value, "schema_name", schemaName)
	putNullString(value, "table_name", tableName)
	putNullString(value, "collection", collection)
	putNullString(value, "resolved_url", resolvedURL)
	putNullString(value, "parser", parser)
	putNullString(value, "authentication", authentication)
	putNullString(value, "attribution", attribution)
	return value, nil
}

func putNullString(value map[string]any, key string, item sql.NullString) {
	if item.Valid {
		value[key] = item.String
	} else {
		value[key] = nil
	}
}

func stringValue(value any, fallback string) string {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return fallback
}

func boolValue(value any, fallback bool) bool {
	if result, ok := value.(bool); ok {
		return result
	}
	return fallback
}

func intValue(value any, fallback int) int {
	switch result := value.(type) {
	case int:
		return result
	case float64:
		return int(result)
	default:
		return fallback
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
