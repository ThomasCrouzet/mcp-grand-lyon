package service

import (
	"context"
	"fmt"
	"strings"
)

type SourceCapability struct {
	SourceID           string `json:"source_id"`
	Provider           string `json:"provider"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	CredentialRequired bool   `json:"credential_required"`
	RegistryStatus     string `json:"registry_status,omitempty"`
	ObservedAt         string `json:"observed_at,omitempty"`
	LastStatus         string `json:"last_status,omitempty"`
}

func (app *App) RequestedMode() string {
	if app.Config.Offline {
		return "offline"
	}
	return "live"
}

func (app *App) EffectiveMode() string {
	return app.RequestedMode()
}

func (app *App) LiveCredentialsPresent() bool {
	return app.Config.Username != "" && app.Config.Password != ""
}

func (app *App) SourceCapabilities(ctx context.Context) ([]SourceCapability, error) {
	result := []SourceCapability{}
	if app.Config.Offline {
		sources, err := app.Store.ListSources(ctx)
		if err != nil {
			return nil, err
		}
		for _, source := range sources {
			result = append(result, SourceCapability{
				SourceID: fmt.Sprint(source["source_id"]), Provider: fmt.Sprint(source["provider"]),
				Status: "OFFLINE_FIXTURE", Reason: "explicit offline mode uses demonstration fixtures",
				CredentialRequired: fmt.Sprint(source["authentication"]) == "basic",
			})
		}
		result = append(result,
			SourceCapability{SourceID: "photon", Provider: "public", Status: "OFFLINE_FIXTURE", Reason: "explicit offline mode uses demonstration fixtures"},
			SourceCapability{SourceID: "gtfs_tcl", Provider: "local", Status: "OFFLINE_FIXTURE", Reason: "explicit offline mode uses demonstration fixtures"},
			SourceCapability{SourceID: "transitous", Provider: "public", Status: "OFFLINE_FIXTURE", Reason: "explicit offline mode uses demonstration fixtures"},
		)
		return result, nil
	}
	sources, err := app.Store.ListSources(ctx)
	if err != nil {
		return nil, err
	}
	health, err := app.Store.SourceHealth(ctx)
	if err != nil {
		return nil, err
	}
	credentials := app.LiveCredentialsPresent()
	for _, source := range sources {
		capability := SourceCapability{
			SourceID: fmt.Sprint(source["source_id"]), Provider: fmt.Sprint(source["provider"]),
			CredentialRequired: fmt.Sprint(source["authentication"]) == "basic",
			RegistryStatus:     fmt.Sprint(source["status"]),
		}
		switch {
		case source["enabled"] == false:
			capability.Status, capability.Reason = "DISABLED", "source is disabled in the registry"
		case capability.CredentialRequired && !credentials:
			capability.Status, capability.Reason = "CREDENTIALS_MISSING", "DataGrandLyon credentials are not configured"
		case source["status"] == "UNRESOLVED":
			capability.Status, capability.Reason = "UNRESOLVED", "no unique source table or collection is resolved"
		case source["status"] == "DISABLED":
			capability.Status, capability.Reason = "DISABLED", "source status is disabled"
		default:
			capability.Status, capability.Reason = "CONFIGURED", "configuration is resolved; current availability requires a live probe"
			if observed := health[capability.SourceID]; observed != nil {
				if value, ok := observed["last_status"].(string); ok && value != "" {
					capability.LastStatus = value
					capability.Status, capability.Reason = observedStatus(value)
					capability.ObservedAt, _ = observed["last_success_at"].(string)
					if capability.ObservedAt == "" {
						capability.ObservedAt, _ = observed["last_failure_at"].(string)
					}
				}
			}
		}
		result = append(result, capability)
	}
	photon := SourceCapability{SourceID: "photon", Provider: "public", Status: "AVAILABLE", Reason: "public geocoding does not require DataGrandLyon credentials"}
	result = append(result, photon)
	if app.Config.TransitousEnabled {
		result = append(result, SourceCapability{SourceID: "transitous", Provider: "public", Status: "AVAILABLE", Reason: "optional public journey planner is enabled"})
	} else {
		result = append(result, SourceCapability{SourceID: "transitous", Provider: "public", Status: "DISABLED", Reason: "optional journey planner is disabled"})
	}
	indexed, err := app.Store.HasTransitIndex(ctx)
	if err != nil {
		return nil, err
	}
	if indexed {
		result = append(result, SourceCapability{SourceID: "gtfs_tcl", Provider: "local", Status: "AVAILABLE", Reason: "imported theoretical timetable is indexed; departures remain realtime=false"})
	} else {
		result = append(result, SourceCapability{SourceID: "gtfs_tcl", Provider: "local", Status: "MISSING", Reason: "no imported GTFS timetable is indexed"})
	}
	return result, nil
}

// SourceCapabilities reports configuration and availability per source. It does
// not claim that a configured source is currently healthy: verified live data
// requires a bounded observation through the diagnostic command.
func observedStatus(status string) (string, string) {
	switch strings.ToUpper(status) {
	case "AUTH_ERROR", "AUTHENTICATION_FAILED", "401":
		return "AUTH_FAILED", "the last observed probe was rejected for authentication"
	case "PERMISSION_DENIED", "FORBIDDEN", "403":
		return "PERMISSION_DENIED", "the last observed probe lacked catalogue permission"
	case "TIMEOUT", "UNAVAILABLE", "ERROR", "500", "503":
		return "UNAVAILABLE", "the last observed probe timed out or returned an upstream failure"
	case "MALFORMED", "INVALID":
		return "MALFORMED", "the last observed probe returned malformed data"
	case "STALE":
		return "STALE_DATA", "the last observation exceeded its freshness limit"
	case "VERIFIED", "OK":
		return "VERIFIED", "a recent bounded probe observed usable data"
	default:
		return "CONFIGURED", "an observed status is recorded but does not map to a supported state"
	}
}
