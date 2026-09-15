package sitesync

import (
	"context"

	"github.com/bestruirui/octopus/internal/model"
)

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
