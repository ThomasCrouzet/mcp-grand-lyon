package service

import (
	"context"
	"fmt"
)

type SourceCapability struct {
	SourceID           string `json:"source_id"`
	Provider           string `json:"provider"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	CredentialRequired bool   `json:"credential_required"`
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

// SourceCapabilities reports configuration and availability per source. It does
// not claim that a configured source is currently healthy: verified live data
// requires a bounded observation through the diagnostic command.
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
	credentials := app.LiveCredentialsPresent()
	for _, source := range sources {
		capability := SourceCapability{
			SourceID: fmt.Sprint(source["source_id"]), Provider: fmt.Sprint(source["provider"]),
			CredentialRequired: fmt.Sprint(source["authentication"]) == "basic",
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
