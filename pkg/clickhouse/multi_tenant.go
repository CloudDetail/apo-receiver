package clickhouse

import (
	"log"
	"strings"
	"sync"

	"github.com/CloudDetail/apo-receiver/pkg/config"
	"github.com/CloudDetail/apo-receiver/pkg/tenancy"
)

type multiTenantCache struct {
	caches sync.Map
}

func (mc *multiTenantCache) GetCache(
	tenant tenancy.TenantInfo,
	cfg *config.ClickHouseConfig,
	tableTTLs map[string]uint,
	tableHash map[string]string,
) *cache {
	var c *cache
	cachePtr, find := mc.caches.Load(tenant)
	if !find {
		db := tenantDB(tenant.TenantID, cfg)
		init := NewClickHouseInit(
			cfg.Endpoint,
			db,
			cfg.Replication,
			cfg.Cluster,
			cfg.Username,
			cfg.Password,
			true,
			cfg.TTLDays, tableTTLs, tableHash)
		if err := init.Start(); err != nil {
			// TODO error log
			log.Panic("init tenant failed")
		}

		c = newCache()
		mc.caches.Store(tenant, c)
	} else {
		c = cachePtr.(*cache)
	}
	return c
}

const DEFAULT_TENANT_DATABASE_PATTERN = `apo_tenant_{TENANT_ID}`

func tenantDB(tenantID string, cfg *config.ClickHouseConfig) string {
	if db, exist := cfg.TenantDBMap[tenantID]; exist {
		return db
	}

	if len(cfg.TenantDBPattern) > 0 {
		return strings.ReplaceAll(cfg.TenantDBPattern, "{TENANT_ID}", tenantID)
	}

	return strings.ReplaceAll(DEFAULT_TENANT_DATABASE_PATTERN, "{TENANT_ID}", tenantID)
}
