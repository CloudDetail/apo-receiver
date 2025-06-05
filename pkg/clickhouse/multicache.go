package clickhouse

import (
	"sync"

	"github.com/CloudDetail/apo-receiver/pkg/tenancy"
)

type multiTenantCache struct {
	caches sync.Map
}

func (mc *multiTenantCache) GetCache(tenant tenancy.TenantInfo) *cache {
	var c *cache
	cachePtr, find := mc.caches.Load(tenant)
	if !find {
		c = newCache()
		mc.caches.Store(tenant, c)
	} else {
		c = cachePtr.(*cache)
	}
	return c
}
