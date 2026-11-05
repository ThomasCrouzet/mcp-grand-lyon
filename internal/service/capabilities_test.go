package service

import (
	"context"
	"testing"
)

func TestSourceCapabilitiesSeparatePublicAndCredentialSources(t *testing.T) {
	t.Parallel()
	app := serviceTestApp(t, &freshnessProvider{})
	if err := app.Store.LoadSources(context.Background(), []byte(`sources:
  basic:
    provider: datapusher
    authentication: basic
    status: OK
  public:
    provider: ogc
    status: OK
  disabled:
    enabled: false
    authentication: basic
    status: DISABLED
  unresolved:
    authentication: basic
    status: UNRESOLVED
`)); err != nil {
		t.Fatal(err)
	}
	app.Config.Offline = false
	app.Config.Username = ""
	app.Config.Password = ""
	app.Config.TransitousEnabled = false
	capabilities, err := app.SourceCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]SourceCapability{}
	for _, capability := range capabilities {
		byID[capability.SourceID] = capability
	}
	if byID["basic"].Status != "CREDENTIALS_MISSING" || byID["unresolved"].Status != "CREDENTIALS_MISSING" {
		t.Fatalf("credential sources = %#v", byID)
	}
	if byID["public"].Status != "CONFIGURED" || byID["disabled"].Status != "DISABLED" {
		t.Fatalf("public/disabled sources = %#v", byID)
	}
	if byID["photon"].Status != "AVAILABLE" || byID["transitous"].Status != "DISABLED" || byID["gtfs_tcl"].Status != "MISSING" {
		t.Fatalf("public/local sources = %#v", byID)
	}

	app.Config.Username = "account"
	app.Config.Password = "password"
	capabilities, err = app.SourceCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range capabilities {
		if capability.SourceID == "basic" && capability.Status != "CONFIGURED" {
			t.Fatalf("configured source = %#v", capability)
		}
	}

	app.Config.Offline = true
	capabilities, err = app.SourceCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range capabilities {
		if capability.Status != "OFFLINE_FIXTURE" {
			t.Fatalf("offline capability = %#v", capability)
		}
	}
}

func TestObservedSourceHealthMapsToActionableStates(t *testing.T) {
	t.Parallel()
	app := serviceTestApp(t, &freshnessProvider{})
	app.Config.Offline = false
	app.Config.Username = "account"
	app.Config.Password = "password"
	if err := app.Store.LoadSources(context.Background(), []byte(`sources:
  auth:
    authentication: basic
    status: OK
  permission:
    authentication: basic
    status: OK
  timeout:
    authentication: basic
    status: OK
  malformed:
    authentication: basic
    status: OK
  stale:
    authentication: basic
    status: OK
  verified:
    authentication: basic
    status: OK
`)); err != nil {
		t.Fatal(err)
	}
	for source, status := range map[string]string{
		"auth": "AUTH_ERROR", "permission": "PERMISSION_DENIED", "timeout": "UNAVAILABLE",
		"malformed": "MALFORMED", "stale": "STALE", "verified": "VERIFIED",
	} {
		execCapabilityHealth(t, app, source, status)
	}
	capabilities, err := app.SourceCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]SourceCapability{}
	for _, capability := range capabilities {
		byID[capability.SourceID] = capability
	}
	want := map[string]string{
		"auth": "AUTH_FAILED", "permission": "PERMISSION_DENIED", "timeout": "UNAVAILABLE",
		"malformed": "MALFORMED", "stale": "STALE_DATA", "verified": "VERIFIED",
	}
	for source, status := range want {
		if byID[source].Status != status || byID[source].ObservedAt == "" || byID[source].LastStatus == "" {
			t.Fatalf("%s capability = %#v, want %s", source, byID[source], status)
		}
	}
}

func execCapabilityHealth(t *testing.T, app *App, source, status string) {
	t.Helper()
	_, err := app.Store.DB.Exec(`INSERT INTO source_health(source_id,last_success_at,last_failure_at,consecutive_failures,last_status)
		VALUES (?,?,?,?,?) ON CONFLICT(source_id) DO UPDATE SET last_success_at=excluded.last_success_at,
		last_failure_at=excluded.last_failure_at,last_status=excluded.last_status`, source,
		"2026-11-05T00:00:00Z", "2026-11-05T00:00:00Z", 1, status)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredCapabilityDoesNotClaimHealth(t *testing.T) {
	t.Parallel()
	app := serviceTestApp(t, &freshnessProvider{})
	app.Config.Offline = false
	app.Config.Username = "account"
	app.Config.Password = "password"
	if err := app.Store.LoadSources(context.Background(), []byte(`sources:
  resolved:
    authentication: basic
    status: OK
`)); err != nil {
		t.Fatal(err)
	}
	capabilities, err := app.SourceCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range capabilities {
		if capability.SourceID == "resolved" {
			if capability.Status != "CONFIGURED" || capability.Reason == "" {
				t.Fatalf("configured source claimed health: %#v", capability)
			}
			return
		}
	}
	t.Fatal("resolved capability is absent")
}
