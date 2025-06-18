package tables

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/CloudDetail/apo-module/model/v1"
)

const (
	insertSpanTraceSQL = `INSERT INTO span_trace (
		timestamp,
		data_version,
		pid,
		tid,
		report_type,
		threshold_type,
		threshold_range,
		threshold_value,
		threshold_multiple,
		trace_id,
		apm_span_id,
		flags,
		labels,
		metrics,
		start_time,
		duration,
		end_time,
		offset_ts
	) VALUES (
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?,
		?
	)`
)

var cpuTypes = []string{
	"cpu",
	"file",
	"net",
	"futex",
	"idle",
	"other",
	"epoll",
	"runq",
}

func WriteSpanTraces(ctx context.Context, conn *sql.DB, toSends []*model.Trace) error {
	if len(toSends) == 0 {
		return nil
	}

	err := doWithTx(ctx, conn, func(tx *sql.Tx) error {
		statement, err := tx.PrepareContext(ctx, insertSpanTraceSQL)
		if err != nil {
			return fmt.Errorf("PrepareContext:%w", err)
		}
		defer func() {
			_ = statement.Close()
		}()
		for _, trace := range toSends {
			traceLabel := trace.Labels
			flags := map[string]bool{
				"top_span":    traceLabel.TopSpan,
				"is_silent":   traceLabel.IsSilent,
				"is_sampled":  traceLabel.IsSampled,
				"is_slow":     traceLabel.IsSlow,
				"is_server":   traceLabel.IsServer,
				"is_error":    traceLabel.IsError,
				"is_profiled": traceLabel.IsProfiled,
			}

			metrics := calcMutatedTypes(trace.OnOffMetrics, trace.BaseOnOffMetrics)
			labels := map[string]string{
				"instance_id":        trace.GetInstanceId(),
				"protocol":           traceLabel.Protocol,
				"service_name":       traceLabel.ServiceName,
				"content_key":        traceLabel.Url,
				"http_url":           traceLabel.HttpUrl,
				"apm_type":           traceLabel.ApmType,
				"attributes":         traceLabel.Attributes,
				"container_id":       traceLabel.ContainerId,
				"container_name":     traceLabel.ContainerName,
				"workload_name":      trace.WorkloadName,
				"workload_kind":      trace.WorkloadKind,
				"pod_ip":             trace.PodIp,
				"pod_name":           trace.PodName,
				"namespace":          trace.Namespace,
				"node_name":          traceLabel.NodeName,
				"node_ip":            traceLabel.NodeIp,
				"cluster_id":         traceLabel.ClusterID,
				"onoff_metrics":      trace.OnOffMetrics,
				"base_onoff_metrics": trace.BaseOnOffMetrics,
				"base_range":         trace.BaseRange,
				"data_source":        trace.Source,
				"mutated_type":       trace.MutatedType,
			}
			_, err = statement.ExecContext(ctx,
				asTime(int64(trace.Timestamp)), // NanoTime
				trace.Version,
				traceLabel.Pid,
				traceLabel.Tid,
				traceLabel.ReportType,
				string(traceLabel.ThresholdType),
				string(traceLabel.ThresholdRange),
				traceLabel.ThresholdValue,
				traceLabel.ThresholdMultiple,
				traceLabel.TraceId,
				traceLabel.ApmSpanId,
				flags,
				labels,
				metrics,
				traceLabel.StartTime,
				traceLabel.Duration,
				traceLabel.EndTime,
				traceLabel.OffsetTs,
			)
			if err != nil {
				return fmt.Errorf("ExecContext:%w", err)
			}
		}
		return nil
	})
	return err
}

func QueryTraces(ctx context.Context, conn *sql.DB, traceId string) (*model.Traces, error) {
	rows, err := conn.Query("SELECT * FROM span_trace WHERE trace_id=?", traceId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	traces := model.NewTraces(traceId)
	for rows.Next() {
		spanTrace := &SpanTrace{}
		if err = rows.Scan(
			&spanTrace.Timestamp,
			&spanTrace.DataVersion,
			&spanTrace.Pid,
			&spanTrace.Tid,
			&spanTrace.ReportType,
			&spanTrace.ThresholdType,
			&spanTrace.ThresholdRange,
			&spanTrace.ThresholdValue,
			&spanTrace.ThresholdMultiple,
			&spanTrace.TraceId,
			&spanTrace.ApmSpanId,
			&spanTrace.Flags,
			&spanTrace.Labels,
			&spanTrace.StartTime,
			&spanTrace.Duration,
			&spanTrace.EndTime,
			&spanTrace.OffsetTs,
			&spanTrace.Metrics); err != nil {
			return nil, err
		}

		label := &model.TraceLabels{
			Pid:               spanTrace.Pid,
			Tid:               spanTrace.Tid,
			TopSpan:           spanTrace.Flags["top_span"],
			Protocol:          spanTrace.Labels["protocol"],
			ServiceName:       spanTrace.Labels["service_name"],
			Url:               spanTrace.Labels["content_key"],
			HttpUrl:           spanTrace.Labels["http_url"],
			IsSilent:          spanTrace.Flags["is_silent"],
			IsSampled:         spanTrace.Flags["is_sampled"],
			IsSlow:            spanTrace.Flags["is_slow"],
			IsServer:          spanTrace.Flags["is_server"],
			IsError:           spanTrace.Flags["is_error"],
			IsProfiled:        spanTrace.Flags["is_profiled"],
			ReportType:        spanTrace.ReportType,
			ThresholdType:     model.ThresholdType(spanTrace.ThresholdType),
			ThresholdValue:    spanTrace.ThresholdValue,
			ThresholdRange:    model.ThresholdRange(spanTrace.ThresholdRange),
			ThresholdMultiple: spanTrace.ThresholdMultiple,
			TraceId:           spanTrace.TraceId,
			ApmType:           spanTrace.Labels["apm_type"],
			ApmSpanId:         spanTrace.ApmSpanId,
			Attributes:        spanTrace.Labels["attributes"],
			ContainerId:       spanTrace.Labels["container_id"],
			ContainerName:     spanTrace.Labels["container_name"],
			StartTime:         spanTrace.StartTime,
			Duration:          spanTrace.Duration,
			EndTime:           spanTrace.EndTime,
			NodeName:          spanTrace.Labels["node_name"],
			NodeIp:            spanTrace.Labels["node_ip"],
			OffsetTs:          spanTrace.OffsetTs,
		}
		trace := &model.Trace{
			Timestamp:        spanTrace.StartTime,
			Version:          spanTrace.DataVersion,
			Source:           spanTrace.Labels["data_source"],
			Labels:           label,
			WorkloadName:     spanTrace.Labels["workload_name"],
			WorkloadKind:     spanTrace.Labels["workload_kind"],
			PodIp:            spanTrace.Labels["pod_ip"],
			PodName:          spanTrace.Labels["pod_name"],
			Namespace:        spanTrace.Labels["namespace"],
			OnOffMetrics:     spanTrace.Labels["onoff_metrics"],
			BaseOnOffMetrics: spanTrace.Labels["base_onoff_metrics"],
			BaseRange:        spanTrace.Labels["base_range"],
			MutatedType:      spanTrace.Labels["mutated_type"],
		}
		traces.AddTrace(trace)
	}
	return traces, nil
}

func calcMutatedTypes(onoffMetrics string, baseOnOffMetrics string) map[string]uint64 {
	mutatedValues := make(map[string]uint64, 0)
	if onoffMetrics == "" {
		return mutatedValues
	}
	onoffValues := strings.Split(onoffMetrics, ",")
	baseOnOffValues := strings.Split(baseOnOffMetrics, ",")

	if baseOnOffMetrics == "" {
		baseOnOffValues = make([]string, len(onoffValues))
	}

	for i := 0; i < 9; i++ {
		if i == 7 {
			// Ignore [7] - FutexNet
			continue
		}
		index := i
		if i == 8 {
			index = 7
		}
		metricValue, _ := strconv.ParseUint(onoffValues[i], 10, 64)
		baseOnOffValue, _ := strconv.ParseUint(baseOnOffValues[index], 10, 64)
		var diffValue uint64 = 0
		if metricValue > baseOnOffValue {
			diffValue = metricValue - baseOnOffValue
		}
		mutatedValues[cpuTypes[index]] = diffValue
	}

	return mutatedValues
}

type SpanTrace struct {
	Timestamp         time.Time         `db:"timestamp"`
	DataVersion       string            `db:"data_version"`
	Pid               uint32            `db:"pid"`
	Tid               uint32            `db:"tid"`
	ReportType        uint32            `db:"report_type"`
	ThresholdType     string            `db:"threshold_type"`
	ThresholdRange    string            `db:"threshold_range"`
	ThresholdValue    float64           `db:"threshold_value"`
	ThresholdMultiple float64           `db:"threshold_multiple"`
	TraceId           string            `db:"trace_id"`
	ApmSpanId         string            `db:"apm_span_id"`
	Flags             map[string]bool   `db:"flags"`
	Labels            map[string]string `db:"labels"`
	StartTime         uint64            `db:"start_time"`
	Duration          uint64            `db:"duration"`
	EndTime           uint64            `db:"end_time"`
	OffsetTs          int64             `db:"offset_ts"`
	Metrics           map[string]uint64 `db:"metrics"`
}
