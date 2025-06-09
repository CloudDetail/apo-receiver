package tenancy

import (
	"context"

	lru "github.com/hashicorp/golang-lru/v2"
)

var TenantCache *tenantCache // traceID -> tenant

type tenantCache struct {
	cache *lru.Cache[string, UserInfo] // traceID -> tenant
}

func init() {
	cache, err := lru.New[string, UserInfo](10000)
	if err != nil {

	}
	TenantCache = &tenantCache{
		cache: cache,
	}
}

func (t *tenantCache) StoreTenantFromCtx(traceID string, ctx context.Context) {
	tenantInfo := GetTenant(ctx)
	t.StoreTenant(traceID, tenantInfo)
}

func (t *tenantCache) StoreTenant(traceID string, tenant UserInfo) {
	t.cache.Add(traceID, tenant)
}
func (t *tenantCache) UpdateLastUsed(traceID string) {
	// Do nothing but update last used
	t.cache.Get(traceID)
}

func (t *tenantCache) GetTenant(traceID string) (UserInfo, bool) {
	return t.cache.Get(traceID)
}

func (t *tenantCache) GetTenantCtx(ctx context.Context, traceID string) (context.Context, bool) {
	tenant, find := t.GetTenant(traceID)
	if !find {
		return ctx, false
	}
	return WithTenant(ctx, &tenant), true
}
