package service

import (
	"context"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
)

const defaultMaximumStaleSeconds = 300

// maximumStaleSeconds reads the configured freshness limit for one source.
// Missing or invalid configuration keeps the conservative default.
func (a *App) maximumStaleSeconds(ctx context.Context, sourceID string) int {
	if a.Store == nil {
		return defaultMaximumStaleSeconds
	}
	source, err := a.Store.GetSource(ctx, sourceID)
	if err != nil || source == nil {
		return defaultMaximumStaleSeconds
	}
	value := briefingIntValue(source["maximum_stale_seconds"])
	if value <= 0 {
		return defaultMaximumStaleSeconds
	}
	return value
}

// observationFreshness keeps upstream observation time, retrieval time, and age
// separate. A missing observation timestamp stays unknown instead of becoming a
// fresh zero-age observation.
func observationFreshness(observed, retrieved *time.Time, now time.Time, maximumStaleSeconds int) (ageSeconds *int, stale, missing bool) {
	if observed == nil || observed.IsZero() {
		return nil, false, true
	}
	age := int(now.Sub(*observed).Seconds())
	if age < 0 {
		age = 0
	}
	stale = maximumStaleSeconds > 0 && age > maximumStaleSeconds
	return &age, stale, false
}

func retrievalTime(retrieved *time.Time, fallback time.Time) time.Time {
	if retrieved != nil && !retrieved.IsZero() {
		return *retrieved
	}
	return fallback
}

func oldestObservation(values ...*time.Time) *time.Time {
	var oldest *time.Time
	for _, value := range values {
		if value == nil || value.IsZero() {
			continue
		}
		if oldest == nil || value.Before(*oldest) {
			copied := *value
			oldest = &copied
		}
	}
	return oldest
}

func latestRetrieval(values ...*time.Time) *time.Time {
	var latest *time.Time
	for _, value := range values {
		if value == nil || value.IsZero() {
			continue
		}
		if latest == nil || value.After(*latest) {
			copied := *value
			latest = &copied
		}
	}
	return latest
}

func uniqueSources(values []model.SourceProvenance) []model.SourceProvenance {
	seen := map[string]bool{}
	result := []model.SourceProvenance{}
	for _, value := range values {
		key := value.Provider + "|" + value.SourceID + "|" + value.Dataset
		if !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	return result
}
