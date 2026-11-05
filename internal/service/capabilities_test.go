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
