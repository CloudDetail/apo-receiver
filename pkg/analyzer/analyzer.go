package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/CloudDetail/apo-receiver/pkg/analyzer/external"
	"github.com/CloudDetail/apo-receiver/pkg/analyzer/report"
	"github.com/CloudDetail/apo-receiver/pkg/componment/onoffmetric"
	"github.com/CloudDetail/apo-receiver/pkg/componment/profile"
	"github.com/CloudDetail/apo-receiver/pkg/config"
	"github.com/CloudDetail/apo-receiver/pkg/global"
	"github.com/CloudDetail/apo-receiver/pkg/metrics"
	metricModel "github.com/CloudDetail/apo-receiver/pkg/metrics/model"

	apmclient "github.com/CloudDetail/apo-module/apm/client/v1"
	apmmodel "github.com/CloudDetail/apo-module/apm/model/v1"
	"github.com/CloudDetail/apo-module/model/v1"
	slomodel "github.com/CloudDetail/apo-module/slo/api/v1/model"
)

var (
	ErrNoSampledTrace error = errors.New("no sampled service is found")
	TraceExpireTime         = time.Minute
	MetricExpireTime        = time.Minute
)

type ReportAnalyzer struct {
	signals         *profile.SingalsCache
	waitMap         sync.Map
	checkMissMap    sync.Map // <traceId, traceApmType>
	taskPool        *taskPool
	delayPeriod     int64
	retryTimes      int
	missTopTime     int64
	threadCount     int
	minuteTaskCount int
	taskIndex       int
	muatedRatio     int
	profileDuration int64
	mutateNodeMode  string
	topologyPeriod  uint64
	externalFactory *external.ExternalFactory
	taskChans       []chan *traceTask
	stopChan        chan bool
}

func NewReportAnalyzer(cfg *config.AnalyzerConfig, signals *profile.SingalsCache) *ReportAnalyzer {
	taskChans := make([]chan *traceTask, 0)
	for i := 0; i < cfg.ThreadCount; i++ {
		taskChans = append(taskChans, make(chan *traceTask))
	}

	topologyPeriod := cfg.TopologyPeriod
	if topologyPeriod == 0 {
		topologyPeriod = 60
	}
	return &ReportAnalyzer{
		signals:         signals,
		taskPool:        newTaskPool(cfg.RetryDuration),
		delayPeriod:     cfg.DelayDuration,
		retryTimes:      cfg.RetryTimes,
		missTopTime:     cfg.MissTopTime,
		threadCount:     cfg.ThreadCount,
		minuteTaskCount: 0,
		taskIndex:       0,
		muatedRatio:     cfg.RatioThreshold,
		profileDuration: int64(cfg.SegmentSize / 2),
		mutateNodeMode:  cfg.MuateNodeMode,
		topologyPeriod:  topologyPeriod * 1000000000,
		externalFactory: external.NewExternalFactory(cfg.HttpParser),
		taskChans:       taskChans,
		stopChan:        make(chan bool),
	}
}

func (analyzer *ReportAnalyzer) Start() {
	for i, taskChan := range analyzer.taskChans {
		// go routine Pool
		go analyzer.analyze(i, taskChan)
	}
	go analyzer.checkTask()
	go global.CACHE.SubscribeReportTraceId(analyzer)
}

func (analyzer *ReportAnalyzer) Stop() {
	close(analyzer.stopChan)
}

func (analyzer *ReportAnalyzer) StoreEvent(eventJson string) {
	agentEvent := &model.AgentEvent{}
	if err := json.Unmarshal([]byte(eventJson), agentEvent); err != nil {
		log.Printf("[x Parse Agent Event] Error: %s", err.Error())
		return
	}
	fillK8sMetadataInEvent(agentEvent)

	global.CLICK_HOUSE.StoreAgentEvent(agentEvent)
}

func (analyzer *ReportAnalyzer) CacheMetric(metricJson string) {
	onOffMetricGroup := &model.OnOffMetricGroup{}
	if err := json.Unmarshal([]byte(metricJson), onOffMetricGroup); err != nil {
		log.Printf("[x Parse OnOff Metric] Error: %s", err.Error())
		return
	}
	global.CACHE.StoreMetric(onOffMetricGroup, metricJson)
}

func (analyzer *ReportAnalyzer) CacheTrace(traceJson string) {
	trace := &model.Trace{Labels: &model.TraceLabels{ThresholdMultiple: 1.0}}
	if err := json.Unmarshal([]byte(traceJson), trace); err != nil {
		log.Printf("[x Parse Trace] Error: %s", err.Error())
		return
	}

	if fillK8sMetadataInSpanTrace(trace) {
		jsonValue := ""
		if !global.CACHE.IsLocal() {
			jsonBytes, _ := json.Marshal(trace)
			jsonValue = string(jsonBytes)
		}
		global.CACHE.StoreTrace(trace, jsonValue)
	} else {
		global.CACHE.StoreTrace(trace, traceJson)
	}

	traceLabel := trace.Labels
	if analyzer.missTopTime > 0 {
		if traceLabel.TopSpan {
			// When top is collected by one collector, mark the flag to -1.
			global.CACHE.RecordTraceTime(traceLabel.TraceId, -1)
		} else {
			timeNano := time.Now().UnixNano()
			analyzer.checkMissMap.Store(traceLabel.TraceId, &traceApmType{
				apmType:       traceLabel.ApmType,
				expireTime:    time.Now().Unix() + analyzer.missTopTime,
				checkNanoTime: timeNano,
			})
			flag := global.CACHE.GetTraceTime(traceLabel.TraceId)
			if flag >= 0 {
				// If the top is not collected, all collectors will raced for the trace.
				global.CACHE.RecordTraceTime(traceLabel.TraceId, timeNano)
			}
		}
	}

	if !traceLabel.TopSpan {
		return
	}

	// Wait delay_duration.
	analyzer.waitMap.Store(traceLabel.TraceId, time.Now().Unix()+analyzer.getWaitTime(traceLabel.ApmType))
}

func (analyzer *ReportAnalyzer) Consume(traceId string) {
	traces := getTracesFromCache(traceId)
	if analyzer.missTopTime <= 0 && traces.RootTrace == nil {
		log.Printf("[x Miss RootTrace] TraceId: %s", traceId)
		return
	}
	if len(traces.Traces) == 0 {
		log.Printf("[x Miss Trace] TraceId: %s", traceId)
		return
	}
	for _, trace := range traces.Traces {
		sendProfiledSpanTrace(trace)
	}
	if traces.HasSingleTrace() || traces.HasChangedSample() {
		// Do not build Relation.
		return
	}

	ignoreRetry := traces.Traces[0].Labels.ApmType == "cw"
	if traces.HasSlow {
		analyzer.taskPool.addTask(newSlowTraceTask(traces, ignoreRetry))
	}
	if traces.HasError {
		analyzer.taskPool.addTask(newErrorTraceTask(traces, ignoreRetry))
	}
	if !traces.HasSlow && !traces.HasError && traces.UnSentTraceCount > 0 {
		analyzer.taskPool.addTask(newNormalTraceTask(traces, ignoreRetry))
	}
}

func getTracesFromCache(traceId string) *model.Traces {
	traces := model.NewTraces(traceId)
	for _, trace := range global.CACHE.GetTraces(traceId) {
		traces.AddTrace(trace)
	}

	// Relate OnOffMetric
	metrics := global.CACHE.GetMetrics(traceId)
	for _, onOffMetricGroup := range metrics {
		if matchTrace := traces.FindTrace(onOffMetricGroup.SpanId); matchTrace != nil {
			matchTrace.SetOnOffMetrics(onOffMetricGroup.Metrics)
			key := onoffmetric.MetricKey{
				ServiceName: matchTrace.Labels.ServiceName,
				ContentKey:  matchTrace.Labels.Url,
			}
			mutatedType, baseOnOffMetrics, thresholdRange := onoffmetric.CalcMutatedType(slomodel.SLO_LATENCY_P90_TYPE, key, onOffMetricGroup.Metrics)
			matchTrace.BaseOnOffMetrics = baseOnOffMetrics
			matchTrace.BaseRange = thresholdRange
			matchTrace.MutatedType = mutatedType.String()
		}
	}
	traces.MetricCount = len(metrics)

	return traces
}

func mergeTraces(oldTraces *model.Traces, newTraces *model.Traces) {
	existTraces := make(map[string]*model.Trace)
	for _, oldTrace := range oldTraces.Traces {
		existTraces[oldTrace.Labels.ApmSpanId] = oldTrace
	}
	for _, newTrace := range newTraces.Traces {
		if _, exist := existTraces[newTrace.Labels.ApmSpanId]; !exist {
			sendProfiledSpanTrace(newTrace)
			oldTraces.AddTrace(newTrace)
		}
	}
}

func (analyzer *ReportAnalyzer) getWaitTime(apmType string) int64 {
	if apmType == "nbs3" || apmType == "cw" {
		// Trace will send every 60 seconds.
		// It's uncertain which second will be collected, we will wait it for 60 seconds.
		return 60
	}
	return analyzer.delayPeriod
}

func (analyzer *ReportAnalyzer) analyze(index int, taskChan chan *traceTask) {
	for {
		select {
		case task := <-taskChan:
			log.Printf("[Channel - %d] Analyze Trace %s, TraceNum: %d, RetryTime: %d", index+1, task.traces.TraceId, task.traces.GetTraceCount(), task.retryTimes)
			analyzer.processTask(task)
		case <-analyzer.stopChan:
			return
		}
	}
}

func (analyzer *ReportAnalyzer) processTask(task *traceTask) {
	if task.retryTimes > 0 {
		// Check whether there will be new Trace/OnOffMetric.
		traces := task.traces
		traceCount := global.CACHE.GetTraceSize(traces.TraceId)
		metricCount := global.CACHE.GetMetricSize(traces.TraceId)
		if traceCount > traces.GetTraceCount() || metricCount > traces.MetricCount {
			// Update New Traces.
			mergeTraces(task.traces, getTracesFromCache(traces.TraceId))
		}
	}
	retry, err := analyzer.buildReport(context.Background(), task.traces, task.reportType)
	if err != nil {
		if retry {
			if !task.ignoreRetry && task.retryTimes < analyzer.retryTimes {
				analyzer.taskPool.retryTask(task)
			} else {
				recordDropReport(task.traces, err, task.reportType)
			}
		} else {
			recordDropReport(task.traces, err, task.reportType)
		}
	}
}

func sendProfiledSpanTrace(trace *model.Trace) {
	if trace.Labels.IsProfiled || trace.Labels.IsSingleTrace() {
		if trace.Labels.IsSlow {
			if trace.MutatedType == "" {
				trace.MutatedType = "unknown"
			}
		}
		storeTrace(trace)
	}
}

func (analyzer *ReportAnalyzer) buildReport(ctx context.Context, traces *model.Traces, reportType report.ReportType) (retry bool, err error) {
	switch reportType {
	case report.ErrorReportType:
		return analyzer.buildErrorReports(ctx, traces)
	case report.SlowReportType:
		return analyzer.buildSlowReports(ctx, traces)
	case report.NormalReportType:
		if _, err := analyzer.buildRelations(ctx, traces); err != nil {
			return true, err
		}
		return false, nil
	default:
		return false, nil
	}
}

func (analyzer *ReportAnalyzer) buildErrorReports(ctx context.Context, traces *model.Traces) (retry bool, err error) {
	serviceNodes, err := analyzer.buildRelations(ctx, traces)
	if err != nil {
		return true, err
	}
	if traces.RootTrace != nil {
		return analyzer.buildSingleErrorReport(ctx, serviceNodes, traces)
	} else {
		return analyzer.buildMultiErrorReports(ctx, serviceNodes, traces)
	}
}

func (analyzer *ReportAnalyzer) buildSingleErrorReport(ctx context.Context, serviceNodes []*apmmodel.OtelServiceNode, traces *model.Traces) (retry bool, err error) {
	entryTrace := traces.RootTrace
	apmType := entryTrace.Labels.ApmType
	if serviceNodes == nil {
		serviceNodes, err = analyzer.queryServices(ctx, apmType, traces.TraceId, entryTrace.Labels)
		if err != nil {
			return true, err
		}
	}

	spanTraces := apmclient.NewNodeSpanTraces(apmType, serviceNodes, traces)
	for _, spanTrace := range spanTraces.Traces {
		if spanTrace.SampledTrace.Labels.ApmSpanId == entryTrace.Labels.ApmSpanId {
			return analyzer.generateErrorReport(ctx, apmType, traces, spanTrace)
		}
	}
	return true, fmt.Errorf("entry[%s-%s] is not collected by apo", entryTrace.Labels.ServiceName, entryTrace.Labels.Url)
}

func (analyzer *ReportAnalyzer) buildMultiErrorReports(ctx context.Context, serviceNodes []*apmmodel.OtelServiceNode, traces *model.Traces) (retry bool, err error) {
	queryTrace := traces.GetQueryTrace()
	apmType := queryTrace.Labels.ApmType
	if serviceNodes == nil {
		serviceNodes, err = analyzer.queryServices(ctx, apmType, traces.TraceId, queryTrace.Labels)
		if err != nil {
			return true, err
		}
	}

	spanTraces := apmclient.NewNodeSpanTraces(apmType, serviceNodes, traces)
	if len(spanTraces.Traces) == 0 {
		return true, fmt.Errorf("trace[%s] is not found in Apm System", traces.TraceId)
	}

	for _, spanTrace := range spanTraces.Traces {
		if _, err := analyzer.generateErrorReport(ctx, apmType, traces, spanTrace); err != nil {
			log.Print(err.Error())
		}
	}

	return false, nil
}

func (analyzer *ReportAnalyzer) generateErrorReport(ctx context.Context, apmType string, traces *model.Traces, spanTrace *apmclient.NodeSpanTrace) (retry bool, err error) {
	apmErrorTree := apmclient.ConvertErrorTree(spanTrace)
	// [Fix for Arms] Add all error nodes.
	if global.TRACE_CLIENT.NeedGetDetailSpan(ctx, apmType) {
		for spanId, errorNode := range apmErrorTree.NodeMap {
			if errorNode.IsError && errorNode.IsSampled {
				if node := spanTrace.GetServiceNode(spanId); node != nil {
					if err := global.TRACE_CLIENT.FillMutatedSpan(ctx, traces.RootTrace.Labels.ClusterID, apmType, traces.TraceId, node); err != nil {
						return true, err
					}
					errorNode.ErrorSpans = apmclient.GetErrorSpans(node)
				}
			}
		}
	}
	mutatedTrace, err := apmErrorTree.GetRootCauseErrorNode(traces.TraceId)
	if err != nil {
		return false, err
	}

	if !mutatedTrace.IsProfiled {
		return false, fmt.Errorf("error instance(%s) is not profiled", mutatedTrace.Id)
	}

	storeTraces(traces)
	log.Printf("[Write Error Report] Trace: %s", traces.TraceId)

	data := &report.ErrorReportData{
		EndTime: apmErrorTree.Root.StartTime + apmErrorTree.Root.TotalTime,
		ErrorReportData: model.ErrorReportData{
			EntryService:        apmErrorTree.Root.ServiceName,
			EntryInstance:       apmErrorTree.Root.Id,
			MutatedService:      mutatedTrace.ServiceName,
			MutatedUrl:          mutatedTrace.Url,
			MutatedInstance:     mutatedTrace.Id,
			MutatedSpan:         mutatedTrace.SpanId,
			MutatedPod:          mutatedTrace.Pod,
			MutatedPodNS:        mutatedTrace.PodNS,
			MutatedWorkloadName: mutatedTrace.Workload,
			MutatedWorkloadType: mutatedTrace.WorkloadType,
			ContentKey:          apmErrorTree.Root.Url,
			RelationTree:        apmErrorTree.Root,
			ThresholdType:       apmErrorTree.Root.ThresholdType,
			ThresholdRange:      apmErrorTree.Root.ThresholdRange,
			ThresholdValue:      apmErrorTree.Root.ThresholdValue,
			ThresholdMultiple:   apmErrorTree.Root.ThresholdMultiple,
		},
	}

	exception := mutatedTrace.GetRootCauseError()
	if exception != nil {
		data.Cause = exception.Type
		data.CauseMessage = exception.Message
	} else {
		data.Cause = "unknown"
		data.CauseMessage = ""
	}
	errorReport := report.NewErrorReport(apmErrorTree.Root.StartTime, traces.TraceId, apmErrorTree.Root.TotalTime, data)
	global.CLICK_HOUSE.StoreErrorReport(errorReport)

	return false, nil
}

func storeTraces(traces *model.Traces) {
	for _, trace := range traces.Traces {
		storeTrace(trace)
	}
}

func storeTrace(trace *model.Trace) {
	if !trace.IsSent {
		trace.MarkSent()
		global.CLICK_HOUSE.StoreTraceGroup(trace)
	}
}

func (analyzer *ReportAnalyzer) buildSlowReports(ctx context.Context, traces *model.Traces) (retry bool, err error) {
	serviceNodes, err := analyzer.buildRelations(ctx, traces)
	if err != nil {
		return true, err
	}

	if traces.RootTrace != nil {
		return analyzer.buildSingleSlowReport(ctx, serviceNodes, traces)
	} else {
		return analyzer.buildMultiSlowReports(ctx, serviceNodes, traces)
	}
}

func (analyzer *ReportAnalyzer) buildSingleSlowReport(ctx context.Context, serviceNodes []*apmmodel.OtelServiceNode, traces *model.Traces) (bool, error) {
	entryTrace := traces.RootTrace.Labels
	if uint64(entryTrace.ThresholdValue) >= entryTrace.Duration {
		return false, fmt.Errorf("entry service(%s) duration(%d) is less than threshold(%s(%s)=%f)",
			entryTrace.ServiceName, entryTrace.Duration, entryTrace.ThresholdType, entryTrace.ThresholdRange,
			entryTrace.ThresholdValue)
	}

	size := len(traces.Traces)
	var maxSampledTrace *model.TraceLabels = nil
	for i := 0; i < size; i++ {
		sampledTrace := traces.Traces[i]
		if sampledTrace.Labels.IsSampled && (maxSampledTrace == nil || maxSampledTrace.Duration < sampledTrace.Labels.Duration) {
			maxSampledTrace = sampledTrace.Labels
		}
	}
	if maxSampledTrace == nil {
		return false, ErrNoSampledTrace
	}
	// [FIX Arms] Drop sampled duration rate < 50% entry duration
	if maxSampledTrace.Duration*2 < traces.RootTrace.Labels.Duration {
		rate := uint64(maxSampledTrace.Duration * 100.0 / entryTrace.Duration)
		return false, fmt.Errorf("top Sampled service(%s) duration(%d) has not enough rate(%d) with service(%s) duration(%d)",
			maxSampledTrace.ServiceName, maxSampledTrace.Duration, rate, entryTrace.ServiceName, entryTrace.Duration)
	}

	apmType := entryTrace.ApmType
	var err error
	if serviceNodes == nil {
		serviceNodes, err = analyzer.queryServices(ctx, apmType, traces.TraceId, entryTrace)
		if err != nil {
			return true, err
		}
	}

	spanTraces := apmclient.NewNodeSpanTraces(apmType, serviceNodes, traces)
	for _, spanTrace := range spanTraces.Traces {
		if spanTrace.SampledTrace.Labels.ApmSpanId == entryTrace.ApmSpanId {
			return analyzer.generateSlowReport(ctx, apmType, traces, spanTrace)
		}
	}
	return true, fmt.Errorf("entry[%s-%s] is not collected by apo", entryTrace.ServiceName, entryTrace.Url)
}

func (analyzer *ReportAnalyzer) buildMultiSlowReports(ctx context.Context, serviceNodes []*apmmodel.OtelServiceNode, traces *model.Traces) (retry bool, err error) {
	queryTrace := traces.GetQueryTrace().Labels
	apmType := queryTrace.ApmType
	if serviceNodes == nil {
		serviceNodes, err = analyzer.queryServices(ctx, apmType, traces.TraceId, queryTrace)
		if err != nil {
			return true, err
		}
	}

	spanTraces := apmclient.NewNodeSpanTraces(apmType, serviceNodes, traces)
	if len(spanTraces.Traces) == 0 {
		return true, fmt.Errorf("trace[%s] is not found in Apm System", traces.TraceId)
	}

	for _, spanTrace := range spanTraces.Traces {
		entryTrace := spanTrace.SampledTrace.Labels
		if uint64(entryTrace.ThresholdValue) >= entryTrace.Duration {
			log.Printf("[x Ignore Entry] service(%s) duration(%d) is less than threshold(%s(%s)=%f)",
				entryTrace.ServiceName, entryTrace.Duration, entryTrace.ThresholdType, entryTrace.ThresholdRange,
				entryTrace.ThresholdValue)
		} else {
			if _, err := analyzer.generateSlowReport(ctx, entryTrace.ApmType, traces, spanTrace); err != nil {
				log.Print(err.Error())
			}
		}
	}
	return false, nil
}

func (analyzer *ReportAnalyzer) generateSlowReport(ctx context.Context, apmType string, traces *model.Traces, spanTrace *apmclient.NodeSpanTrace) (retry bool, err error) {
	apmTraceTree := apmclient.ConvertSlowTree(spanTrace)
	mutatedTrace, err := apmTraceTree.GetMutatedTraceNode(traces.TraceId, analyzer.muatedRatio, analyzer.mutateNodeMode)
	if err != nil {
		return false, err
	}

	// [FIX Arms] Add Spans for Clients and Excpetions
	if global.TRACE_CLIENT.NeedGetDetailSpan(ctx, apmType) {
		if err := global.TRACE_CLIENT.FillMutatedSpan(ctx, traces.RootTrace.Labels.ClusterID, apmType, traces.TraceId, spanTrace.GetServiceNode(mutatedTrace.SpanId)); err != nil {
			return true, err
		}
	}

	mutatedType := "unknown"
	if foundTrace := traces.FindTrace(mutatedTrace.SpanId); foundTrace != nil {
		entryTrace := apmTraceTree.Root
		foundTraceLabels := foundTrace.Labels
		needProfile := false
		if foundTraceLabels.IsSampled && !foundTraceLabels.IsSilent && !foundTraceLabels.IsProfiled {
			if ((time.Now().UnixNano()-int64(foundTraceLabels.StartTime))/1e9 + 2) < analyzer.profileDuration {
				foundTraceLabels.IsProfiled = true
				needProfile = true
			}
		}
		analyzer.signals.AddSignal(entryTrace.ServiceName, entryTrace.Url, foundTrace, needProfile)

		if !foundTraceLabels.IsSampled {
			return false, fmt.Errorf("instance(%s) is not sampled", foundTrace.GetInstanceId())
		}
		if !foundTraceLabels.IsProfiled {
			return false, fmt.Errorf("instance(%s) is not profiled", foundTrace.GetInstanceId())
		}

		mutatedType = foundTrace.MutatedType
		storeTraces(traces)
	} else {
		return false, fmt.Errorf("instance(%s) is not monited", mutatedTrace.Id)
	}

	log.Printf("[Write Slow Report] Trace: %s", traces.TraceId)
	data := &report.ReportData{
		EndTime: apmTraceTree.Root.StartTime + apmTraceTree.Root.TotalTime,
		CameraNodeReportData: model.CameraNodeReportData{
			EntryService:        apmTraceTree.Root.ServiceName,
			EntryInstance:       apmTraceTree.Root.Id,
			MutatedService:      mutatedTrace.ServiceName,
			MutatedUrl:          mutatedTrace.Url,
			MutatedInstance:     mutatedTrace.Id,
			MutatedSpan:         mutatedTrace.SpanId,
			MutatedPod:          mutatedTrace.Pod,
			MutatedPodNS:        mutatedTrace.PodNS,
			MutatedWorkloadName: mutatedTrace.Workload,
			MutatedWorkloadType: mutatedTrace.WorkloadType,

			Cause:             mutatedType,
			ContentKey:        apmTraceTree.Root.Url,
			ThresholdType:     apmTraceTree.Root.ThresholdType,
			ThresholdRange:    apmTraceTree.Root.ThresholdRange,
			ThresholdValue:    apmTraceTree.Root.ThresholdValue,
			ThresholdMultiple: apmTraceTree.Root.ThresholdMultiple,

			RelationTree:    apmTraceTree.Root,
			OTelClientCalls: spanTrace.GetClientCalls(mutatedTrace.SpanId),
		},
	}

	nodeReport := report.NewNodeReport(apmTraceTree.Root.StartTime, traces.TraceId, apmTraceTree.Root.TotalTime, data)
	global.CLICK_HOUSE.StoreNodeReport(nodeReport)
	return false, nil
}

func (analyzer *ReportAnalyzer) buildRelations(ctx context.Context, traces *model.Traces) ([]*apmmodel.OtelServiceNode, error) {
	entryTrace := traces.GetQueryTrace()
	if entryTrace == nil {
		return nil, nil
	}
	entryTraceLabels := entryTrace.Labels
	if traces.RootTrace != nil {
		key := analyzer.getRelationKey(entryTraceLabels.ServiceName, entryTraceLabels.Url, entryTraceLabels.StartTime, false)
		if global.CACHE.GetRelationTraceId(key) != "" {
			storeTraces(traces)
			return nil, nil
		}
	}

	serviceNodes, err := analyzer.queryServices(ctx, entryTraceLabels.ApmType, traces.TraceId, entryTraceLabels)
	if err != nil {
		return nil, err
	}

	if analyzer.missTopTime == 0 {
		foundRoot := false
		for _, serviceNode := range serviceNodes {
			for _, entrySpan := range serviceNode.EntrySpans {
				if entrySpan.PSpanId == "" {
					foundRoot = true
				}
			}
		}
		if !foundRoot {
			return serviceNodes, fmt.Errorf("no matched entry span is found in Apm System")
		}
	}

	topology := report.NewTopology(entryTraceLabels.ApmType, serviceNodes, traces.GetSpanIdTraceMap(), analyzer.externalFactory)
	for _, topologyNode := range topology.Nodes {
		key := analyzer.getRelationKey(topologyNode.ServiceName, topologyNode.Url, topologyNode.StartTime, topologyNode.TopNode)
		if global.CACHE.GetRelationTraceId(key) == "" {
			global.CACHE.StoreRelationTraceId(key, traces.TraceId)
			global.CLICK_HOUSE.StoreRelation(report.NewRelation(traces.TraceId, topologyNode))

			storeTraces(traces)
		}
	}
	return serviceNodes, nil
}

func (analyzer *ReportAnalyzer) getRelationKey(serviceName, url string, timestamp uint64, vnode bool) string {
	return fmt.Sprintf("%s-%s-%d-%t", serviceName, url, timestamp/analyzer.topologyPeriod, vnode)
}

func recordDropReport(traces *model.Traces, err error, reportType report.ReportType) {
	log.Printf("[x Build Report] TraceId: %s, Error: %s", traces.TraceId, err.Error())
	if reportType == report.ErrorReportType {
		dropReport := report.NewDropErrorReport(report.CameraErrorReport, traces.GetQueryTrace(), err.Error())
		global.CLICK_HOUSE.StoreErrorReport(dropReport)
	} else if reportType == report.SlowReportType {
		dropReport := report.NewDropReport(report.CameraNodeReport, traces.GetQueryTrace(), err.Error())
		global.CLICK_HOUSE.StoreNodeReport(dropReport)
	} else if reportType == report.NormalReportType {
		storeTraces(traces)
	}
}

func (analyzer *ReportAnalyzer) checkTask() {
	timer := time.NewTicker(1 * time.Second)
	currentMinute := time.Now().Minute()
	for {
		select {
		case <-timer.C:
			checkTime := time.Now().Unix()
			tasks := analyzer.taskPool.getToProcessTasks(checkTime)
			for _, task := range tasks {
				analyzer.taskChans[analyzer.taskIndex] <- task
				if analyzer.taskIndex == analyzer.threadCount-1 {
					analyzer.taskIndex = 0
				} else {
					analyzer.taskIndex += 1
				}
				analyzer.minuteTaskCount += 1
			}
			newMinute := time.Now().Minute()
			if newMinute != currentMinute {
				log.Printf("[Minute Execute Task] %d", analyzer.minuteTaskCount)
				currentMinute = newMinute
				analyzer.minuteTaskCount = 0
			}

			analyzer.waitMap.Range(func(k, v interface{}) bool {
				expireTime := v.(int64)
				if expireTime < checkTime {
					global.CACHE.NotifyReportTraceId(k.(string))
					analyzer.waitMap.Delete(k)
				}
				return true
			})

			analyzer.checkMissMap.Range(func(k, v interface{}) bool {
				traceValue := v.(*traceApmType)
				if traceValue.expireTime < checkTime {
					traceId := k.(string)
					if global.CACHE.GetTraceTime(traceId) == traceValue.checkNanoTime {
						analyzer.waitMap.Store(traceId, checkTime+analyzer.getWaitTime(traceValue.apmType))
					}
					analyzer.checkMissMap.Delete(k)
				}
				return true
			})
		case <-analyzer.stopChan:
			timer.Stop()
			return
		}
	}
}

func (analyzer *ReportAnalyzer) queryServices(ctx context.Context, apmType string, traceId string, rootTrace *model.TraceLabels) ([]*apmmodel.OtelServiceNode, error) {
	serviceNodes, err := global.TRACE_CLIENT.QueryServices(ctx, apmType, rootTrace.ClusterID, traceId, rootTrace)
	// Record Metric
	metrics.UpdateMetric(metricModel.MetricAdapterApmTraceCount, []string{
		rootTrace.NodeName,
		rootTrace.NodeIp,
		strconv.FormatUint(uint64(rootTrace.Pid), 10),
		rootTrace.ContainerId,
		strconv.FormatBool(err == nil),
	}, float64(1))

	return serviceNodes, err
}

type traceApmType struct {
	apmType       string
	expireTime    int64
	checkNanoTime int64
}

type taskPool struct {
	taskLock    sync.RWMutex
	todoTasks   []*traceTask
	retryTasks  []*traceTask
	checkPeriod int64
}

func newTaskPool(checkPeriod int64) *taskPool {
	retryPeriod := checkPeriod
	if retryPeriod == 0 {
		retryPeriod = 5
	}
	return &taskPool{
		todoTasks:   make([]*traceTask, 0),
		retryTasks:  make([]*traceTask, 0),
		checkPeriod: retryPeriod,
	}
}

func (pool *taskPool) addTask(task *traceTask) {
	pool.taskLock.Lock()
	defer pool.taskLock.Unlock()

	log.Printf("[Add %s Task] %s, TraceNum: %d", task.reportType.String(), task.traces.TraceId, task.traces.GetTraceCount())
	pool.todoTasks = append(pool.todoTasks, task)
}

func (pool *taskPool) retryTask(task *traceTask) {
	pool.taskLock.Lock()
	defer pool.taskLock.Unlock()

	task.retryTimes += 1
	task.checkTime = time.Now().Unix() + pool.checkPeriod
	pool.retryTasks = append(pool.retryTasks, task)
}

func (pool *taskPool) getToProcessTasks(checkTime int64) []*traceTask {
	pool.taskLock.Lock()
	defer pool.taskLock.Unlock()

	var tasks []*traceTask = make([]*traceTask, 0)
	size := len(pool.todoTasks)
	if size > 0 {
		tasks = append(tasks, pool.todoTasks...)
		pool.todoTasks = pool.todoTasks[0:0]
	}

	if len(pool.retryTasks) > 0 {
		index := 0
		for i, task := range pool.retryTasks {
			if task.checkTime <= checkTime {
				index = i + 1
			} else {
				break
			}
		}
		if index > 0 {
			size += index
			tasks = append(tasks, pool.retryTasks[0:index]...)
			pool.retryTasks = pool.retryTasks[index:]
		}
	}
	if size+len(pool.retryTasks) > 0 {
		log.Printf("[Process %d Task] Left %d Tasks", size, len(pool.retryTasks))
	}

	return tasks
}

type traceTask struct {
	traces      *model.Traces
	reportType  report.ReportType
	retryTimes  int
	checkTime   int64
	ignoreRetry bool
}

func newSlowTraceTask(traces *model.Traces, ignoreRetry bool) *traceTask {
	return &traceTask{
		traces:      traces,
		reportType:  report.SlowReportType,
		retryTimes:  0,
		ignoreRetry: ignoreRetry,
	}
}

func newErrorTraceTask(traces *model.Traces, ignoreRetry bool) *traceTask {
	return &traceTask{
		traces:      traces,
		reportType:  report.ErrorReportType,
		retryTimes:  0,
		ignoreRetry: ignoreRetry,
	}
}

func newNormalTraceTask(traces *model.Traces, ignoreRetry bool) *traceTask {
	return &traceTask{
		traces:      traces,
		reportType:  report.NormalReportType,
		retryTimes:  0,
		ignoreRetry: ignoreRetry,
	}
}
