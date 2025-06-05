package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/CloudDetail/apo-module/model/v1"

	"github.com/CloudDetail/apo-receiver/pkg/analyzer/report"
	"github.com/CloudDetail/apo-receiver/pkg/clickhouse/tables"
	profile_model "github.com/CloudDetail/apo-receiver/pkg/componment/profile/model"
	"github.com/CloudDetail/apo-receiver/pkg/config"
	"github.com/CloudDetail/apo-receiver/pkg/tenancy"
)

var (
	errConfigNoEndpoint      = errors.New("endpoint must be specified")
	errConfigInvalidEndpoint = errors.New("endpoint must be url format")
)

type ClickHouseClient struct {
	Conn                 *sql.DB
	cache                *cache
	flushPeriod          uint
	stopChan             chan bool
	exportServiceClient  bool
	generateClientMetric bool
	clientMetricWithUrl  bool

	multiTenantEnabled bool
	multiTenantCache   *multiTenantCache

	cfg *config.ClickHouseConfig

	tableTTLs map[string]uint
	tableHash map[string]string
}

func NewClickHouseClient(ctx context.Context, cfg *config.ClickHouseConfig, generateClientMetric bool, clientMetricWithUrl bool, multiTenantEnabled bool) (*ClickHouseClient, error) {
	if cfg.Endpoint == "" {
		return nil, errConfigNoEndpoint
	}

	tableTTLs := make(map[string]uint)
	tableHash := make(map[string]string)
	for _, ttl := range cfg.TTLConfig {
		for _, tableName := range ttl.Tables {
			tableTTLs[tableName] = ttl.TTL
		}
	}
	for _, hash := range cfg.HashConfig {
		for _, tableName := range hash.Tables {
			tableHash[tableName] = hash.Hash
		}
	}

	init := NewClickHouseInit(cfg.Endpoint, cfg.Database, cfg.Replication, cfg.Cluster,
		cfg.Username, cfg.Password, true, cfg.TTLDays, tableTTLs, tableHash)
	if err := init.Start(); err != nil {
		return nil, err
	}

	client := &ClickHouseClient{
		Conn:                 init.GetConn(),
		cache:                newCache(),
		flushPeriod:          cfg.FlushSeconds,
		stopChan:             make(chan bool),
		exportServiceClient:  cfg.ExportServiceClient,
		generateClientMetric: generateClientMetric,
		clientMetricWithUrl:  clientMetricWithUrl,

		multiTenantEnabled: multiTenantEnabled,
		multiTenantCache:   &multiTenantCache{},
		cfg:                cfg,
		tableTTLs:          tableTTLs,
		tableHash:          tableHash,
	}
	return client, nil
}

func (client *ClickHouseClient) BatchStore(ctx context.Context, table string, datas []string) {
	client.GetCache(ctx).batchStore(table, datas)
}

func (client *ClickHouseClient) StoreTraceGroup(ctx context.Context, trace *model.Trace) {
	client.GetCache(ctx).cacheSpanTrace(trace)
}

func (client *ClickHouseClient) StoreNodeReport(ctx context.Context, nodeReport *report.NodeReport) {
	client.GetCache(ctx).cacheNodeReport(nodeReport)
}

func (client *ClickHouseClient) StoreErrorReport(ctx context.Context, errorReport *report.ErrorReport) {
	client.GetCache(ctx).cacheErrorReport(errorReport)
}

func (client *ClickHouseClient) StoreReportMetric(ctx context.Context, reportMetric *profile_model.SlowReportCountMetric) {
	client.GetCache(ctx).cacheReportMetric(reportMetric)
}

func (client *ClickHouseClient) StoreRelation(ctx context.Context, relation *report.Relation) {
	client.GetCache(ctx).cacheRelations(relation)
}

func (client *ClickHouseClient) StoreAgentEvent(ctx context.Context, agentEvent *model.AgentEvent) {
	client.GetCache(ctx).cacheAgentEvent(agentEvent)
}

func (client *ClickHouseClient) QueryTraces(ctx context.Context, traceId string) (*model.Traces, error) {
	database := tenantDB(tenancy.GetTenant(ctx).TenantID, client.cfg)
	return tables.QueryTraces(ctx, database, client.Conn, traceId)
}

func (client *ClickHouseClient) GetCache(ctx context.Context) *cache {
	if !client.multiTenantEnabled {
		return client.cache
	}
	tenant := tenancy.GetTenant(ctx)
	return client.multiTenantCache.GetCache(tenant, client.cfg, client.tableTTLs, client.tableHash)
}

func (client *ClickHouseClient) Start() {
	go client.batchSendToServer()
}

func (client *ClickHouseClient) batchSendToServer() {
	ctx := context.Background()
	waitSecond := client.flushPeriod
	if waitSecond == 0 {
		waitSecond = 5
	}
	timer := time.NewTicker(time.Duration(waitSecond) * time.Second)
	for {
		select {
		case <-timer.C:
			if !client.multiTenantEnabled {
				client.flushCache(ctx, client.cfg.Database, "", client.cache)
				continue
			}
			client.multiTenantCache.caches.Range(func(k, v interface{}) bool {
				cache := v.(*cache)
				tenant := k.(tenancy.TenantInfo)
				database := tenantDB(tenant.TenantID, client.cfg)
				client.flushCache(ctx, database, tenant.AccountID, cache)
				return true
			})
		case <-client.stopChan:
			timer.Stop()
			return
		}
	}
}

func (client *ClickHouseClient) flushCache(ctx context.Context, database string, accountID string, cache *cache) {
	if err := tables.WriteProfilingEvents(ctx, database, client.Conn, cache.getToSendEventGroups()); err != nil {
		log.Printf("[x Add ProfilingEvent] %s", err.Error())
	}
	if err := tables.WriteFlameGraph(ctx, database, client.Conn, cache.getToSendFlameGraphs()); err != nil {
		log.Printf("[x Add FlameGraph] %s", err.Error())
	}
	if err := tables.WriteJvmGcs(ctx, database, client.Conn, cache.getToSendJvmGcs()); err != nil {
		log.Printf("[x Add JvmGc] %s", err.Error())
	}
	if err := tables.WriteSpanTraces(ctx, database, client.Conn, cache.getToSendSpanTraces()); err != nil {
		log.Printf("[x Add SpanTrace] %s", err.Error())
	}
	if err := tables.WriteSlowReports(ctx, database, client.Conn, cache.getToSendNodeReports()); err != nil {
		log.Printf("[x Add SlowReport] %s", err.Error())
	}
	errorReports := cache.getToSendErrorReports()
	if err := tables.WriteErrorReports(ctx, database, client.Conn, errorReports); err != nil {
		log.Printf("[x Add ErrorReport] %s", err.Error())
	}
	if err := tables.WriteErrorPropagations(ctx, database, client.Conn, errorReports); err != nil {
		log.Printf("[x Add ErrorPropagation] %s", err.Error())
	}
	if err := tables.WriteReportMetrics(ctx, database, client.Conn, cache.getToSendReportMetrics()); err != nil {
		log.Printf("[x Add ReportMetric] %s", err.Error())
	}
	if err := tables.WriteOnOffMetrics(ctx, database, client.Conn, cache.getToSendOnOffMetrics()); err != nil {
		log.Printf("[x Add OnOffMetric] %s", err.Error())
	}
	relations := cache.getToSendRelations()
	if err := tables.WriteServiceRelationships(ctx, database, client.Conn, relations); err != nil {
		log.Printf("[x Add ServiceRelationship] %s", err.Error())
	}
	agentEvents := cache.getToSendAgentEvents()
	if err := tables.WriteAgentEvents(ctx, database, client.Conn, agentEvents); err != nil {
		log.Printf("[x Add AgentEvent] %s", err.Error())
	}
	if client.exportServiceClient {
		if err := tables.WriteServiceClients(ctx, database, client.Conn, relations); err != nil {
			log.Printf("[x Add ServiceClient] %s", err.Error())
		}
	}
	if client.generateClientMetric {
		tables.WriteClientMetric(relations, client.clientMetricWithUrl, accountID)
	}
}

func (client *ClickHouseClient) Stop() {
	close(client.stopChan)
}
