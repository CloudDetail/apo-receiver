package clickhouse

import (
	"log"
	"sync"

	"github.com/CloudDetail/apo-module/model/v1"
	"github.com/CloudDetail/apo-receiver/pkg/analyzer/report"
	profile_model "github.com/CloudDetail/apo-receiver/pkg/componment/profile/model"

	_ "github.com/ClickHouse/clickhouse-go/v2" // For register database driver.
)

type cache struct {
	mutex               sync.RWMutex
	cameraEventGroups   []string
	flameGraphs         []string
	jvmGcs              []string
	onoffMetrics        []string
	spanTraces          []*model.Trace
	cameraNodeReports   []*report.NodeReport
	cameraErrorReports  []*report.ErrorReport
	cameraReportMetrics []*profile_model.SlowReportCountMetric
	relations           []*report.Relation
	originxAgentEvents  []*model.AgentEvent
}

func newCache() *cache {
	return &cache{
		cameraEventGroups:   make([]string, 0),
		flameGraphs:         make([]string, 0),
		jvmGcs:              make([]string, 0),
		onoffMetrics:        make([]string, 0),
		spanTraces:          make([]*model.Trace, 0),
		cameraNodeReports:   make([]*report.NodeReport, 0),
		cameraErrorReports:  make([]*report.ErrorReport, 0),
		cameraReportMetrics: make([]*profile_model.SlowReportCountMetric, 0),
		relations:           make([]*report.Relation, 0),
		originxAgentEvents:  make([]*model.AgentEvent, 0),
	}
}

func (c *cache) batchStore(name string, datas []string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	switch name {
	case report.CameraEventGroup:
		c.cameraEventGroups = append(c.cameraEventGroups, datas...)
	case report.FlameGraph:
		c.flameGraphs = append(c.flameGraphs, datas...)
	case report.JvmGc:
		c.jvmGcs = append(c.jvmGcs, datas...)
	case report.OnOffMetricGroup:
		c.onoffMetrics = append(c.onoffMetrics, datas...)
	default:
		log.Printf("[x Unknown Data] %s, Skip.", name)
	}
}

func (c *cache) cacheSpanTrace(trace *model.Trace) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.spanTraces = append(c.spanTraces, trace)
}

func (c *cache) cacheNodeReport(nodeReport *report.NodeReport) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.cameraNodeReports = append(c.cameraNodeReports, nodeReport)
}

func (c *cache) cacheErrorReport(errorReport *report.ErrorReport) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.cameraErrorReports = append(c.cameraErrorReports, errorReport)
}

func (c *cache) cacheReportMetric(reportMetric *profile_model.SlowReportCountMetric) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.cameraReportMetrics = append(c.cameraReportMetrics, reportMetric)
}

func (c *cache) cacheRelations(relation *report.Relation) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.relations = append(c.relations, relation)
}

func (c *cache) cacheAgentEvent(agentEvent *model.AgentEvent) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.originxAgentEvents = append(c.originxAgentEvents, agentEvent)
}

func (c *cache) getToSendEventGroups() []string {
	size := len(c.cameraEventGroups)
	if size == 0 {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	toSends := c.cameraEventGroups[0:size]
	c.cameraEventGroups = c.cameraEventGroups[size:]
	return toSends
}

func (c *cache) getToSendFlameGraphs() []string {
	size := len(c.flameGraphs)
	if size == 0 {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	toSends := c.flameGraphs[0:size]
	c.flameGraphs = c.flameGraphs[size:]
	return toSends
}

func (c *cache) getToSendJvmGcs() []string {
	size := len(c.jvmGcs)
	if size == 0 {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	toSends := c.jvmGcs[0:size]
	c.jvmGcs = c.jvmGcs[size:]
	return toSends
}

func (c *cache) getToSendOnOffMetrics() []string {
	size := len(c.onoffMetrics)
	if size == 0 {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	toSends := c.onoffMetrics[0:size]
	c.onoffMetrics = c.onoffMetrics[size:]
	return toSends
}

func (c *cache) getToSendSpanTraces() []*model.Trace {
	size := len(c.spanTraces)
	if size == 0 {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	toSends := c.spanTraces[0:size]
	c.spanTraces = c.spanTraces[size:]
	return toSends
}

func (c *cache) getToSendNodeReports() []*report.NodeReport {
	size := len(c.cameraNodeReports)
	if size == 0 {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	toSends := c.cameraNodeReports[0:size]
	c.cameraNodeReports = c.cameraNodeReports[size:]
	return toSends
}

func (c *cache) getToSendErrorReports() []*report.ErrorReport {
	size := len(c.cameraErrorReports)
	if size == 0 {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	toSends := c.cameraErrorReports[0:size]
	c.cameraErrorReports = c.cameraErrorReports[size:]
	return toSends
}

func (c *cache) getToSendReportMetrics() []*profile_model.SlowReportCountMetric {
	size := len(c.cameraReportMetrics)
	if size == 0 {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	toSends := c.cameraReportMetrics[0:size]
	c.cameraReportMetrics = c.cameraReportMetrics[size:]
	return toSends
}

func (c *cache) getToSendRelations() []*report.Relation {
	size := len(c.relations)
	if size == 0 {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	toSends := c.relations[0:size]
	c.relations = c.relations[size:]
	return toSends
}

func (c *cache) getToSendAgentEvents() []*model.AgentEvent {
	size := len(c.originxAgentEvents)
	if size == 0 {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	toSends := c.originxAgentEvents[0:size]
	c.originxAgentEvents = c.originxAgentEvents[size:]
	return toSends
}
