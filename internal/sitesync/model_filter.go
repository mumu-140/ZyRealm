package sitesync

import (
	"context"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/modelmatch"
)

type globalModelFilterContextKey struct{}

func withGlobalModelFilter(ctx context.Context, pattern string) context.Context {
	return context.WithValue(ctx, globalModelFilterContextKey{}, pattern)
}

func globalModelFilterFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	pattern, _ := ctx.Value(globalModelFilterContextKey{}).(string)
	return pattern
}

func applyGlobalModelFilterToSiteFetchResult(result siteModelFetchResult, pattern string) (siteModelFetchResult, error) {
	filtered, err := modelmatch.Filter(result.names, pattern)
	if err != nil {
		result.names = nil
		result.authoritative = false
		result.message = "全局模型过滤规则无效，本次保留历史模型"
		return result, fmt.Errorf("invalid global model filter: %w", err)
	}
	result.names = filtered
	if result.authoritative {
		result.message = fmt.Sprintf("同步到 %d 个模型", len(filtered))
	}
	return result, nil
}

// syncSiteModelsByGroupWithGlobalFilter is the explicit testable boundary for
// site-discovery admission. Production SyncAccount injects the same pattern
// into the operation context before entering platform-specific sync code.
func syncSiteModelsByGroupWithGlobalFilter(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
	groupTokens []model.SiteToken,
	platformUserID int,
	source string,
	globalFilter string,
	fetcher func(token model.SiteToken, allowGlobalFallback bool) (siteModelFetchResult, error),
) ([]model.SiteModel, []siteGroupSyncResult) {
	return syncSiteModelsByGroup(
		withGlobalModelFilter(ctx, globalFilter),
		siteRecord,
		account,
		accessToken,
		groupTokens,
		platformUserID,
		source,
		fetcher,
	)
}

// applyGlobalModelFilterToSnapshot is a final admission guard for direct-token
// sync paths that do not pass through syncSiteModelsByGroup. Grouped paths are
// already filtered earlier; applying the same regex twice is idempotent.
func applyGlobalModelFilterToSnapshot(snapshot *syncSnapshot, pattern string) error {
	if snapshot == nil {
		return nil
	}

	names := make([]string, 0, len(snapshot.models))
	byName := make(map[string][]model.SiteModel, len(snapshot.models))
	for _, item := range snapshot.models {
		name := strings.TrimSpace(item.ModelName)
		names = append(names, name)
		byName[name] = append(byName[name], item)
	}
	filteredNames, err := modelmatch.Filter(names, pattern)
	if err != nil {
		return fmt.Errorf("invalid global model filter: %w", err)
	}

	remainingByName := make(map[string]int, len(filteredNames))
	for _, name := range filteredNames {
		remainingByName[name]++
	}
	filteredModels := make([]model.SiteModel, 0, len(filteredNames))
	for _, item := range snapshot.models {
		name := strings.TrimSpace(item.ModelName)
		if remainingByName[name] <= 0 {
			continue
		}
		filteredModels = append(filteredModels, item)
		remainingByName[name]--
	}
	snapshot.models = filteredModels

	modelCounts := make(map[string]int)
	for _, item := range snapshot.models {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		if strings.TrimSpace(item.ModelName) != "" {
			modelCounts[groupKey]++
		}
	}
	for i := range snapshot.groupResults {
		item := &snapshot.groupResults[i]
		if !item.Authoritative {
			continue
		}
		switch item.Status {
		case siteGroupSyncStatusFailed, siteGroupSyncStatusUnresolved, siteGroupSyncStatusMissingKey, siteGroupSyncStatusRemoved:
			continue
		}
		count := modelCounts[model.NormalizeSiteGroupKey(item.GroupKey)]
		item.ModelCount = count
		if count == 0 {
			item.Status = siteGroupSyncStatusEmpty
			item.Message = "全局模型过滤后该分组当前没有可用模型"
		} else {
			item.Status = siteGroupSyncStatusSynced
			item.Message = fmt.Sprintf("同步到 %d 个模型", count)
		}
	}
	snapshot.status = buildSyncSnapshotStatus(snapshot.groupResults)
	snapshot.message = buildSyncSnapshotMessage(snapshot.groupResults)
	return nil
}
