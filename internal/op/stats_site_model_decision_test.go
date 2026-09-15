package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestStatsSiteModelHourlyRecordAttemptsIgnoresDecisionOnly(t *testing.T) {
	siteModelHourlyCacheLock.Lock()
	original := siteModelHourlyCache
	siteModelHourlyCache = make(map[siteModelHourlyKey]*model.StatsSiteModelHourly)
	siteModelHourlyCacheLock.Unlock()
	defer func() {
		siteModelHourlyCacheLock.Lock()
		siteModelHourlyCache = original
		siteModelHourlyCacheLock.Unlock()
	}()

	StatsSiteModelHourlyRecordAttempts([]model.ChannelAttempt{{
		ChannelID:   99,
		ModelName:   "filtered-model",
		Status:      model.AttemptSkipped,
		AttemptKind: "decision_only",
	}}, "fallback-model")

	siteModelHourlyCacheLock.Lock()
	got := len(siteModelHourlyCache)
	siteModelHourlyCacheLock.Unlock()
	if got != 0 {
		t.Fatalf("decision-only envelope created site-model traffic: cache entries=%d", got)
	}
}
