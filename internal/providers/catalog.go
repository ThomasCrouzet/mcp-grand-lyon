package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/assets"
	"go.yaml.in/yaml/v3"
)

type CatalogStatus struct {
	Auth    string
	Catalog string
	Detail  string
}

func ClassifyCatalogStatus(status int) CatalogStatus {
	switch status {
	case http.StatusOK:
		return CatalogStatus{Auth: "OK", Catalog: "OK", Detail: "full list accessible"}
	case http.StatusUnauthorized:
		return CatalogStatus{Auth: "ERROR", Catalog: "ERROR", Detail: "authentication failed (401)"}
	case http.StatusForbidden:
		return CatalogStatus{Auth: "OK", Catalog: "FORBIDDEN", Detail: "catalog permission limit (403)"}
	default:
		return CatalogStatus{Auth: "UNKNOWN", Catalog: "ERROR", Detail: fmt.Sprintf("HTTP %d", status)}
	}
}

type knownSourcesFile struct {
	Tables map[string]struct {
		Service   string `yaml:"service"`
		TableName string `yaml:"table_name"`
	} `yaml:"tables"`
}

func knownSources() map[string][2]string {
	result := map[string][2]string{}
	body, err := assets.Files.ReadFile("config/sources.known.yaml")
	if err != nil {
		return result
	}
	var decoded knownSourcesFile
	if yaml.Unmarshal(body, &decoded) != nil {
		return result
	}
	for id, source := range decoded.Tables {
		if (source.Service == "rdata" || source.Service == "grandlyon") && tableNamePattern.MatchString(source.TableName) {
			result[id] = [2]string{source.Service, source.TableName}
		}
	}
	return result
}

func (live *Live) CatalogScan(ctx context.Context, mode string) (map[string]any, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "full" && mode != "known" && mode != "ogc" {
		return nil, fmt.Errorf("unknown catalog mode %q", mode)
	}
	result := map[string]any{"catalog_mode": mode, "auth": "OK"}
	catalogUsable := false
	catalogForbidden := false
	catalogSuccesses := 0
	catalogFailures := 0
	if mode == "auto" || mode == "full" {
		for _, service := range []string{"rdata", "grandlyon"} {
			entries, status, err := live.fetchCatalog(ctx, service)
			classification := ClassifyCatalogStatus(status)
			if err != nil {
				catalogFailures++
				result[service] = map[string]any{"status": classification.Catalog, "error": err.Error()}
				catalogForbidden = catalogForbidden || classification.Catalog == "FORBIDDEN"
				if classification.Auth == "ERROR" {
					result["auth"] = "ERROR"
				}
				continue
			}
			catalogUsable = true
			catalogSuccesses++
			result[service] = map[string]any{"status": "OK", "count": len(entries)}
			live.resolveCatalogSources(ctx, service, entries, result)
		}
		if catalogSuccesses > 0 && catalogFailures > 0 {
			result["catalog_list"] = "PARTIAL"
			result["status"] = "PARTIAL"
			result["note"] = "one catalog service is unavailable"
		} else if catalogUsable {
			result["catalog_list"] = "OK"
		} else if catalogForbidden {
			result["catalog_list"] = "FORBIDDEN"
			if mode == "auto" {
				result["note"] = "catalog access is limited; used known tables and OGC candidates"
			}
		} else if catalogFailures > 0 {
			result["catalog_list"] = "ERROR"
		}
		if mode == "full" && !catalogUsable {
			result["status"] = "PARTIAL"
			result["note"] = "full catalog scan failed; use auto, known, or ogc mode"
		}
	}
	if mode == "known" || (mode == "auto" && !catalogUsable) {
		sources := map[string]any{}
		known := knownSources()
		registry, err := live.sourceRegistry(ctx)
		if err != nil {
			return nil, fmt.Errorf("list source registry: %w", err)
		}
		ids := make([]string, 0, len(known))
		for id := range known {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			source := known[id]
			registrySource, registered := registry[id]
			if registered {
				if enabled, ok := registrySource["enabled"].(bool); ok && !enabled {
					entry := map[string]any{"table": source[1], "status": "DISABLED"}
					if updateErr := live.setSourceStatus(ctx, id, "DISABLED", source[1]); updateErr != nil {
						entry["registry_error"] = updateErr.Error()
					}
					sources[id] = entry
					continue
				}
			}
			rows, err := live.queryTable(ctx, source[0], source[1], 1)
			status := "OK"
			entry := map[string]any{"table": source[1], "sample_rows": len(rows)}
			if err != nil {
				status, entry["error"] = catalogProbeStatus(err), err.Error()
			}
			entry["status"] = status
			sources[id] = entry
			if registered {
				if updateErr := live.setSourceStatus(ctx, id, status, source[1]); updateErr != nil {
					entry["registry_error"] = updateErr.Error()
				}
			}
		}
		result["sources"] = sources
		if mode == "auto" {
			result["catalog_mode"] = "known_tables_fallback"
		}
	}
	if mode == "ogc" || (mode == "auto" && !catalogUsable) {
		collections, err := live.listCollections(ctx)
		if err != nil {
			result["ogc"] = map[string]any{"status": "ERROR", "error": err.Error()}
		} else {
			result["ogc"] = map[string]any{"status": "OK", "collections": len(collections)}
			result["ogc_candidates"] = ogcCandidates(collections, 30)
		}
	}
	return result, nil
}

func (live *Live) sourceRegistry(ctx context.Context) (map[string]map[string]any, error) {
	result := map[string]map[string]any{}
	if live.store == nil {
		return result, nil
	}
	sources, err := live.store.ListSources(ctx)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if id, ok := source["source_id"].(string); ok && id != "" {
			result[id] = source
		}
	}
	return result, nil
}

func catalogProbeStatus(err error) string {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "403") || strings.Contains(message, "forbidden") {
		return "UNRESOLVED"
	}
	return "ERROR"
}

func ogcCandidates(collections []map[string]any, limit int) []string {
	keywords := []string{"parking", "velov", "toilet", "dechet", "chantier"}
	result := []string{}
	for _, collection := range collections {
		id := strings.ToLower(text(collection, "id", "name"))
		title := strings.ToLower(text(collection, "title"))
		blob := id + " " + title
		matched := false
		for _, keyword := range keywords {
			if strings.Contains(blob, keyword) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		candidate := id
		if candidate == "" {
			candidate = title
		}
		if candidate != "" {
			result = append(result, candidate)
		}
		if len(result) == bound(limit, 1, 30) {
			break
		}
	}
	return result
}

func (live *Live) resolveCatalogSources(ctx context.Context, service string, entries []map[string]any, result map[string]any) {
	if live.store == nil {
		return
	}
	sources, err := live.store.ListSources(ctx)
	if err != nil {
		result["registry"] = map[string]any{"status": "ERROR", "error": err.Error()}
		return
	}
	for _, source := range sources {
		id, _ := source["source_id"].(string)
		sourceService, _ := source["service"].(string)
		if sourceService == "" {
			sourceService = "rdata"
		}
		if id == "" || sourceService != service {
			continue
		}
		if enabled, ok := source["enabled"].(bool); ok && !enabled {
			entry := map[string]any{"status": "DISABLED", "candidates": 0}
			if updateErr := live.setSourceStatus(ctx, id, "DISABLED", ""); updateErr != nil {
				entry["registry_error"] = updateErr.Error()
			}
			result[id] = entry
			continue
		}
		metadata := map[string]any{}
		if raw, ok := source["metadata_json"].(string); ok && raw != "" {
			if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
				result[id] = map[string]any{"status": "ERROR", "error": "invalid source metadata"}
				continue
			}
		}
		hints := stringValues(metadata["exact_table_hints"])
		if table, ok := source["table_name"].(string); ok && tableNamePattern.MatchString(table) {
			hints = append(hints, table)
		}
		keywords := stringValues(metadata["search_keywords"])
		candidates := searchCatalog(entries, keywords, hints)
		selected := resolveCatalogCandidate(candidates, hints)
		status := "UNRESOLVED"
		if selected != "" {
			status = "OK"
		}
		entry := map[string]any{"status": status, "candidates": len(candidates)}
		if selected != "" {
			entry["table"] = selected
		}
		result[id] = entry
		if updateErr := live.setSourceStatus(ctx, id, status, selected); updateErr != nil {
			entry["registry_error"] = updateErr.Error()
		}
	}
}

type scoredCatalogEntry struct {
	score float64
	entry map[string]any
}

func searchCatalog(entries []map[string]any, keywords, hints []string) []map[string]any {
	scored := make([]scoredCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		name := strings.ToLower(catalogEntryName(entry))
		title := strings.ToLower(text(entry, "title", "label"))
		blob := name + " " + title
		score := 0.0
		for _, hint := range hints {
			if normalized := strings.ToLower(strings.TrimSpace(hint)); normalized != "" && strings.Contains(name, normalized) {
				score += 100
			}
		}
		for _, keyword := range keywords {
			if normalized := strings.ToLower(strings.TrimSpace(keyword)); normalized != "" && strings.Contains(blob, normalized) {
				score += 10
			}
		}
		if score > 0 {
			scored = append(scored, scoredCatalogEntry{score: score, entry: entry})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	result := make([]map[string]any, 0, len(scored))
	for _, candidate := range scored {
		result = append(result, candidate.entry)
	}
	return result
}

func resolveCatalogCandidate(candidates []map[string]any, hints []string) string {
	exact := []string{}
	for _, candidate := range candidates {
		name := catalogEntryName(candidate)
		normalizedName := strings.ToLower(name)
		for _, hint := range hints {
			normalizedHint := strings.ToLower(strings.TrimSpace(hint))
			if normalizedHint != "" && (normalizedName == normalizedHint || strings.HasSuffix(normalizedName, "."+normalizedHint) || strings.Contains(normalizedName, normalizedHint)) {
				exact = append(exact, name)
				break
			}
		}
	}
	if len(exact) == 1 && tableNamePattern.MatchString(exact[0]) {
		return exact[0]
	}
	if len(candidates) == 1 {
		name := catalogEntryName(candidates[0])
		if tableNamePattern.MatchString(name) {
			return name
		}
	}
	return ""
}

func catalogEntryName(entry map[string]any) string {
	return text(entry, "name", "id", "table", "layer")
}

func stringValues(value any) []string {
	result := []string{}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" {
				result = append(result, value)
			}
		}
	case []string:
		result = append(result, typed...)
	}
	return result
}

func (live *Live) CatalogValidate(ctx context.Context) (map[string]any, error) {
	result := map[string]any{}
	if live.store != nil {
		sources, err := live.store.ListSources(ctx)
		if err != nil {
			return nil, fmt.Errorf("list source registry: %w", err)
		}
		for _, source := range sources {
			id, _ := source["source_id"].(string)
			status, _ := source["status"].(string)
			service, _ := source["service"].(string)
			table, _ := source["table_name"].(string)
			if id == "" {
				continue
			}
			if enabled, ok := source["enabled"].(bool); ok && !enabled {
				entry := map[string]any{"status": "DISABLED"}
				if updateErr := live.setSourceStatus(ctx, id, "DISABLED", table); updateErr != nil {
					entry["registry_error"] = updateErr.Error()
				}
				result[id] = entry
				continue
			}
			if (status != "OK" && status != "ERROR") || !tableNamePattern.MatchString(table) {
				if status == "" {
					status = "UNRESOLVED"
				}
				result[id] = map[string]any{"status": status}
				continue
			}
			if service == "" {
				service = "rdata"
			}
			rows, queryErr := live.queryTable(ctx, service, table, 2)
			if queryErr != nil {
				entry := map[string]any{"status": "ERROR", "error": queryErr.Error()}
				if updateErr := live.setSourceStatus(ctx, id, "ERROR", table); updateErr != nil {
					entry["registry_error"] = updateErr.Error()
				}
				result[id] = entry
				continue
			}
			entry := map[string]any{"status": "OK", "sample_rows": len(rows)}
			if updateErr := live.setSourceStatus(ctx, id, "OK", table); updateErr != nil {
				entry["registry_error"] = updateErr.Error()
			}
			result[id] = entry
		}
		return result, nil
	}
	for id, source := range knownSources() {
		rows, err := live.queryTable(ctx, source[0], source[1], 2)
		if err != nil {
			result[id] = map[string]any{"status": "ERROR", "error": err.Error()}
			continue
		}
		result[id] = map[string]any{"status": "OK", "sample_rows": len(rows)}
	}
	return result, nil
}

func (live *Live) fetchCatalog(ctx context.Context, service string) ([]map[string]any, int, error) {
	response, err := live.client.Get(ctx, dataPusherBase+"/"+service+"/all.json", true, jsonLimit)
	if err != nil {
		return nil, 0, err
	}
	classification := ClassifyCatalogStatus(response.StatusCode)
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, fmt.Errorf("catalog: %s", classification.Detail)
	}
	var decoded any
	if err := json.Unmarshal(response.Body, &decoded); err != nil {
		return nil, response.StatusCode, err
	}
	return extractCatalogRows(decoded), response.StatusCode, nil
}

func extractCatalogRows(decoded any) []map[string]any {
	if values, ok := decoded.([]any); ok {
		return objectRows(values, false)
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return []map[string]any{}
	}
	for _, key := range []string{"results", "values", "features", "records", "datasets"} {
		if values, ok := root[key].([]any); ok {
			return objectRows(values, key == "features")
		}
	}
	return []map[string]any{}
}

func (live *Live) listCollections(ctx context.Context) ([]map[string]any, error) {
	decoded, err := live.getJSON(ctx, "https://data.grandlyon.com/geoserver/ogc/features/v1/collections", false)
	if err != nil {
		return nil, err
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return []map[string]any{}, nil
	}
	values, _ := root["collections"].([]any)
	return objectRows(values, false), nil
}

func (live *Live) setSourceStatus(ctx context.Context, id, status, table string) error {
	if live.store == nil {
		return nil
	}
	return live.store.SetSourceStatus(ctx, id, status, table)
}
