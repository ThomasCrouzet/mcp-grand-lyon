package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

func TestToolBudgetWarnsAndKeepsCompletedBranches(t *testing.T) {
	t.Parallel()
	provider := &semanticProvider{plan: func(ctx context.Context, _ model.Point, _ model.Point, _ time.Time, _ []string, _ model.TripPreferences) ([]model.TripOption, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	app := serviceTestApp(t, provider)
	app.Config.ToolTimeout = 30 * time.Millisecond
	raw, err := json.Marshal(map[string]any{
		"origin":      map[string]any{"latitude": 45.76, "longitude": 4.86},
		"destination": map[string]any{"latitude": 45.75, "longitude": 4.83},
		"modes":       []string{"walk", "tcl"},
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope := app.Call(context.Background(), "lyon_trip_options", raw)
	if !hasWarning(envelope, "TOOL_TIMEOUT", "tool_execution") {
		t.Fatalf("budget expiry was not reported: %#v", envelope)
	}
	if len(envelope.Data["options"].([]model.TripOption)) == 0 {
		t.Fatalf("completed walk branch was discarded: %#v", envelope)
	}
}

func TestToolBudgetRespectsEarlierCallerDeadline(t *testing.T) {
	t.Parallel()
	if _, err := Tools(); err != nil {
		t.Fatal(err)
	}
	provider := &semanticProvider{plan: func(ctx context.Context, _ model.Point, _ model.Point, _ time.Time, _ []string, _ model.TripPreferences) ([]model.TripOption, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	app := serviceTestApp(t, provider)
	app.Config.ToolTimeout = time.Second
	raw, err := json.Marshal(map[string]any{
		"origin":      map[string]any{"latitude": 45.76, "longitude": 4.86},
		"destination": map[string]any{"latitude": 45.75, "longitude": 4.83},
		"modes":       []string{"tcl"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	envelope := app.Call(ctx, "lyon_trip_options", raw)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("earlier caller deadline was ignored: %v", elapsed)
	}
	if !hasWarning(envelope, "TOOL_TIMEOUT", "tool_execution") {
		t.Fatalf("caller deadline was not reported: %#v", envelope)
	}
}
