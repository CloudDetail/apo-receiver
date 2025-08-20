package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/CloudDetail/apo-module/model/v1"

	"github.com/CloudDetail/apo-receiver/pkg/analyzer/appinfo"
	"github.com/CloudDetail/apo-receiver/pkg/analyzer/report"
	"github.com/CloudDetail/apo-receiver/pkg/clickhouse/tables"
	profile_model "github.com/CloudDetail/apo-receiver/pkg/componment/profile/model"
	"github.com/CloudDetail/apo-receiver/pkg/config"
	grpc_model "github.com/CloudDetail/apo-receiver/pkg/model"
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
}

func NewClickHouseClient(ctx context.Context, cfg *config.ClickHouseConfig, generateClientMetric bool, clientMetricWithUrl bool) (*ClickHouseClient, error) {
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
	}
	return client, nil
}

func (client *ClickHouseClient) BatchStore(table string, datas []string) {
	client.cache.batchStore(table, datas)
}

func (client *ClickHouseClient) StoreTraceGroup(trace *model.Trace) {
	client.cache.cacheSpanTrace(trace)
}

func (client *ClickHouseClient) StoreNodeReport(nodeReport *report.NodeReport) {
	client.cache.cacheNodeReport(nodeReport)
}

func (client *ClickHouseClient) StoreErrorReport(errorReport *report.ErrorReport) {
	client.cache.cacheErrorReport(errorReport)
}

func (client *ClickHouseClient) StoreReportMetric(reportMetric *profile_model.SlowReportCountMetric) {
	client.cache.cacheReportMetric(reportMetric)
}

func (client *ClickHouseClient) StoreRelation(relation *report.Relation) {
	client.cache.cacheRelations(relation)
}

func (client *ClickHouseClient) StoreAgentEvent(agentEvent *model.AgentEvent) {
	client.cache.cacheAgentEvent(agentEvent)
}

func (client *ClickHouseClient) StoreAppInfo(appInfo *appinfo.AppInfo) {
	client.cache.cacheAppInfo(appInfo)
}

func (client *ClickHouseClient) QueryActiveApps(ctx context.Context, nodeIp string, nodeName string, deadApps []*grpc_model.QueryActiveApp) ([]*grpc_model.QueryActiveApp, error) {
	return tables.QueryActiveApps(ctx, client.Conn, nodeIp, nodeName, deadApps)
}

func (client *ClickHouseClient) UpdateAppHeartTimes(nodeIp string, nodeName string, activeApps []*grpc_model.QueryActiveApp) error {
	return tables.UpdateAppHeartTimes(client.Conn, nodeIp, nodeName, activeApps)
}

func (client *ClickHouseClient) UpdateAppDeadFlags(nodeIp string, nodeName string, deadApps []*grpc_model.QueryActiveApp) error {
	return tables.UpdateAppDeadFlags(client.Conn, nodeIp, nodeName, deadApps)
}

func (client *ClickHouseClient) QueryTraces(ctx context.Context, traceId string) (*model.Traces, error) {
	return tables.QueryTraces(ctx, client.Conn, traceId)
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
			if err := tables.WriteProfilingEvents(ctx, client.Conn, client.cache.getToSendEventGroups()); err != nil {
				log.Printf("[x Add ProfilingEvent] %s", err.Error())
			}
			if err := tables.WriteFlameGraph(ctx, client.Conn, client.cache.getToSendFlameGraphs()); err != nil {
				log.Printf("[x Add FlameGraph] %s", err.Error())
			}
			if err := tables.WriteJvmGcs(ctx, client.Conn, client.cache.getToSendJvmGcs()); err != nil {
				log.Printf("[x Add JvmGc] %s", err.Error())
			}
			if err := tables.WriteSpanTraces(ctx, client.Conn, client.cache.getToSendSpanTraces()); err != nil {
				log.Printf("[x Add SpanTrace] %s", err.Error())
			}
			if err := tables.WriteAppInfos(ctx, client.Conn, client.cache.getToSendAppInfos()); err != nil {
				log.Printf("[x Add AppInfo] %s", err.Error())
			}
			if err := tables.WriteSlowReports(ctx, client.Conn, client.cache.getToSendNodeReports()); err != nil {
				log.Printf("[x Add SlowReport] %s", err.Error())
			}
			errorReports := client.cache.getToSendErrorReports()
			if err := tables.WriteErrorReports(ctx, client.Conn, errorReports); err != nil {
				log.Printf("[x Add ErrorReport] %s", err.Error())
			}
			if err := tables.WriteErrorPropagations(ctx, client.Conn, errorReports); err != nil {
				log.Printf("[x Add ErrorPropagation] %s", err.Error())
			}
			if err := tables.WriteReportMetrics(ctx, client.Conn, client.cache.getToSendReportMetrics()); err != nil {
				log.Printf("[x Add ReportMetric] %s", err.Error())
			}
			if err := tables.WriteOnOffMetrics(ctx, client.Conn, client.cache.getToSendOnOffMetrics()); err != nil {
				log.Printf("[x Add OnOffMetric] %s", err.Error())
			}
			relations := client.cache.getToSendRelations()
			if err := tables.WriteServiceRelationships(ctx, client.Conn, relations); err != nil {
				log.Printf("[x Add ServiceRelationship] %s", err.Error())
			}
			agentEvents := client.cache.getToSendAgentEvents()
			if err := tables.WriteAgentEvents(ctx, client.Conn, agentEvents); err != nil {
				log.Printf("[x Add AgentEvent] %s", err.Error())
			}
			if client.exportServiceClient {
				if err := tables.WriteServiceClients(ctx, client.Conn, relations); err != nil {
					log.Printf("[x Add ServiceClient] %s", err.Error())
				}
			}
			if client.generateClientMetric {
				tables.WriteClientMetric(relations, client.clientMetricWithUrl)
			}
		case <-client.stopChan:
			timer.Stop()
			return
		}
	}
}

func (client *ClickHouseClient) Stop() {
	close(client.stopChan)
}
