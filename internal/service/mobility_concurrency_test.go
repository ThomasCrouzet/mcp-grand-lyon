package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

type concurrencyProvider struct {
	model.Provider
	current atomic.Int32
	peak    atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (provider *concurrencyProvider) enter() {
	value := provider.current.Add(1)
	for {
		peak := provider.peak.Load()
		if value <= peak || provider.peak.CompareAndSwap(peak, value) {
			break
		}
	}
	provider.started <- struct{}{}
}

func (provider *concurrencyProvider) leave() { provider.current.Add(-1) }

func (provider *concurrencyProvider) Alerts(context.Context, []string) ([]model.TransitAlert, error) {
	provider.enter()
	defer provider.leave()
	<-provider.release
	return []model.TransitAlert{{ID: "alert", Title: "Alert"}}, nil
}

func (provider *concurrencyProvider) Incidents(context.Context, []string) ([]model.AccessibilityIncident, error) {
	provider.enter()
	defer provider.leave()
	<-provider.release
	return []model.AccessibilityIncident{{ID: "incident"}}, nil
}

func (provider *concurrencyProvider) Traffic(context.Context, string) ([]model.TrafficCondition, error) {
	provider.enter()
	defer provider.leave()
	<-provider.release
	return []model.TrafficCondition{{ID: "traffic"}}, nil
}

func (provider *concurrencyProvider) RoadEvents(context.Context, string) ([]model.RoadEvent, error) {
	provider.enter()
	defer provider.leave()
	<-provider.release
	return []model.RoadEvent{{ID: "event"}}, nil
}

func TestMobilityRunsIndependentBranchesConcurrently(t *testing.T) {
	t.Parallel()
	provider := &concurrencyProvider{started: make(chan struct{}, 4), release: make(chan struct{})}
	app := serviceTestApp(t, provider)
	app.Config.MaxParallel = 4
	done := make(chan model.Envelope, 1)
	go func() {
		done <- app.MobilityStatus(context.Background(), model.MobilityStatusInput{
			Include: []string{"transit", "accessibility", "traffic", "roadworks"},
		})
	}()
	for index := 0; index < 4; index++ {
		select {
		case <-provider.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d branches started", index)
		}
	}
	close(provider.release)
	envelope := <-done
	if provider.peak.Load() != 4 {
		t.Fatalf("peak concurrency = %d, want 4", provider.peak.Load())
	}
	if envelope.Status != "ok" {
		t.Fatalf("envelope = %#v", envelope)
	}
	if len(envelope.Data["alerts"].([]model.TransitAlert)) != 1 ||
		len(envelope.Data["accessibility_incidents"].([]model.AccessibilityIncident)) != 1 ||
		len(envelope.Data["traffic_conditions"].([]model.TrafficCondition)) != 1 ||
		len(envelope.Data["road_events"].([]model.RoadEvent)) != 1 {
		t.Fatalf("branch data = %#v", envelope.Data)
	}
}
