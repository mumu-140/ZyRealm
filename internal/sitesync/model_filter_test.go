package sitesync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestSyncSiteModelsByGroupWithGlobalFilterCoversPrimaryAndFallbackSources(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "primary helper discovery", source: siteModelSourceSync},
		{name: "managed session fallback", source: siteModelSourceSyncFallback},
		{name: "site-specific fallback", source: "site_fallback"},
		{name: "sub2api discovery", source: "sub2api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models, results := syncSiteModelsByGroupWithGlobalFilter(
				context.Background(),
				nil,
				nil,
				"",
				[]model.SiteToken{{Token: "test-key", GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true}},
				0,
				tt.source,
				`^gpt-`,
				func(token model.SiteToken, allowGlobalFallback bool) (siteModelFetchResult, error) {
					return siteModelFetchResult{
						names:         []string{"claude-3-5-sonnet", "gpt-4o", "gpt-4.1"},
						source:        tt.source,
						authoritative: true,
					}, nil
				},
			)
			if len(models) != 2 || models[0].ModelName != "gpt-4.1" || models[1].ModelName != "gpt-4o" {
				t.Fatalf("filtered models = %+v, want gpt-4.1 and gpt-4o", models)
			}
			if len(results) != 1 || results[0].Status != siteGroupSyncStatusSynced || results[0].ModelCount != 2 {
				t.Fatalf("unexpected group results: %+v", results)
			}
		})
	}
}

func TestSyncSiteModelsByGroupWithGlobalFilterValidZeroIsAuthoritativeEmpty(t *testing.T) {
	models, results := syncSiteModelsByGroupWithGlobalFilter(
		context.Background(),
		nil,
		nil,
		"",
		[]model.SiteToken{{Token: "test-key", GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true}},
		0,
		siteModelSourceSync,
		`^gpt-`,
		func(token model.SiteToken, allowGlobalFallback bool) (siteModelFetchResult, error) {
			return siteModelFetchResult{names: []string{"claude-3-5-sonnet"}, source: siteModelSourceSync, authoritative: true}, nil
		},
	)
	if len(models) != 0 {
		t.Fatalf("models = %+v, want authoritative empty", models)
	}
	if len(results) != 1 || results[0].Status != siteGroupSyncStatusEmpty || !results[0].Authoritative {
		t.Fatalf("unexpected group results: %+v", results)
	}
}

func TestSyncSiteModelsByGroupWithGlobalFilterInvalidPatternIsNonAuthoritativeFailure(t *testing.T) {
	models, results := syncSiteModelsByGroupWithGlobalFilter(
		context.Background(),
		nil,
		nil,
		"",
		[]model.SiteToken{{Token: "test-key", GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true}},
		0,
		siteModelSourceSync,
		`(`,
		func(token model.SiteToken, allowGlobalFallback bool) (siteModelFetchResult, error) {
			return siteModelFetchResult{names: []string{"gpt-4o"}, source: siteModelSourceSync, authoritative: true}, nil
		},
	)
	if len(models) != 0 {
		t.Fatalf("models = %+v, want none on invalid filter", models)
	}
	if len(results) != 1 || results[0].Status != siteGroupSyncStatusFailed || results[0].Authoritative {
		t.Fatalf("invalid filter must be a non-authoritative failure, got %+v", results)
	}
}

func setupSiteModelFilterSyncTest(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	if err := dbpkg.InitDB("sqlite", filepath.Join(t.TempDir(), "site-model-filter.db"), false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })
	return context.Background()
}

func createDirectSiteModelFilterFixture(t *testing.T, ctx context.Context, serverURL string) (*model.Site, *model.SiteAccount) {
	t.Helper()
	site := &model.Site{
		Name:             "model-filter-site",
		Platform:         model.SitePlatformAPI,
		BaseURL:          serverURL,
		DefaultRouteType: model.SiteModelRouteTypeOpenAIChat,
		Enabled:          true,
	}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "model-filter-account",
		CredentialType: model.SiteCredentialTypeAPIKey,
		APIKey:         "test-key",
		Enabled:        true,
		AutoSync:       true,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}
	return site, account
}

func newDirectSiteModelServer(t *testing.T, names ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" && r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		data := make([]map[string]string, 0, len(names))
		for _, name := range names {
			data = append(data, map[string]string{"id": name})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestSyncAccountAppliesGlobalFilterBeforeDirectProjection(t *testing.T) {
	ctx := setupSiteModelFilterSyncTest(t)
	if err := op.SettingSetString(model.SettingKeyModelFilterRegex, `^gpt-`); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	server := newDirectSiteModelServer(t, "claude-3-5-sonnet", "gpt-4o", "gpt-4.1")
	_, account := createDirectSiteModelFilterFixture(t, ctx, server.URL)

	result, err := SyncAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("SyncAccount returned error: %v", err)
	}
	if len(result.Models) != 2 || result.Models[0] != "gpt-4.1" || result.Models[1] != "gpt-4o" {
		t.Fatalf("result models = %+v, want only globally admitted gpt models", result.Models)
	}
	var persisted []model.SiteModel
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Order("model_name ASC").Find(&persisted).Error; err != nil {
		t.Fatalf("load persisted models failed: %v", err)
	}
	if len(persisted) != 2 || persisted[0].ModelName != "gpt-4.1" || persisted[1].ModelName != "gpt-4o" {
		t.Fatalf("persisted models = %+v, want only globally admitted gpt models", persisted)
	}
}

func TestSyncAccountValidZeroFilterIsAuthoritativeAndClearsHistoricalModels(t *testing.T) {
	ctx := setupSiteModelFilterSyncTest(t)
	if err := op.SettingSetString(model.SettingKeyModelFilterRegex, `^gpt-`); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	server := newDirectSiteModelServer(t, "claude-3-5-sonnet")
	_, account := createDirectSiteModelFilterFixture(t, ctx, server.URL)
	old := model.SiteModel{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ModelName: "old-gpt", Source: "sync"}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&old).Error; err != nil {
		t.Fatalf("create historical model failed: %v", err)
	}

	result, err := SyncAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("SyncAccount returned error for valid zero-result policy: %v", err)
	}
	if result.ModelCount != 0 || len(result.Models) != 0 {
		t.Fatalf("result = %+v, want authoritative empty model set", result)
	}
	var count int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteModel{}).Where("site_account_id = ?", account.ID).Count(&count).Error; err != nil {
		t.Fatalf("count persisted models failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("persisted model count = %d, want 0", count)
	}
}

func TestSyncAccountInvalidGlobalFilterPreservesHistoricalProjection(t *testing.T) {
	ctx := setupSiteModelFilterSyncTest(t)
	if err := op.SettingSetString(model.SettingKeyModelFilterRegex, `(`); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	server := newDirectSiteModelServer(t, "gpt-4.1")
	_, account := createDirectSiteModelFilterFixture(t, ctx, server.URL)
	old := model.SiteModel{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ModelName: "preserve-me", Source: "sync"}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&old).Error; err != nil {
		t.Fatalf("create historical model failed: %v", err)
	}

	if _, err := SyncAccount(ctx, account.ID); err == nil {
		t.Fatal("SyncAccount accepted invalid runtime global filter")
	}
	var persisted []model.SiteModel
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Find(&persisted).Error; err != nil {
		t.Fatalf("load persisted models failed: %v", err)
	}
	if len(persisted) != 1 || persisted[0].ModelName != "preserve-me" {
		t.Fatalf("invalid filter mutated historical models: %+v", persisted)
	}
}
