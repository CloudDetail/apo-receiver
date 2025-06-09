package onoffmetric

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/CloudDetail/apo-receiver/pkg/global"
	"github.com/CloudDetail/apo-receiver/pkg/tenancy"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prometheus_model "github.com/prometheus/common/model"

	slomodel "github.com/CloudDetail/apo-module/slo/api/v1/model"
)

const (
	dayDuration      = "24h"
	hourDuration     = "1h"
	LabelContentKey  = "content_key"
	LabelServiceName = "svc_name"

	LabelVMAccountID = "vm_account_id"
)

func getYesterdayLatency(client v1.API, sloType slomodel.SLOType, cpuType CPUType, todayZeroMillis int64) map[MetricKey]uint64 {
	value, err := queryMetrics(tenancy.SystemCtx(), client, todayZeroMillis, sloType, cpuType, dayDuration)
	if err == nil && len(value) > 0 {
		return value
	}
	return map[MetricKey]uint64{}
}

func GetLastOneHour(client v1.API, sloType slomodel.SLOType, cpuType CPUType, nowMillis int64) map[MetricKey]uint64 {
	value, err := queryMetrics(tenancy.SystemCtx(), client, nowMillis, sloType, cpuType, hourDuration)
	if err == nil && len(value) > 0 {
		return value
	}
	return map[MetricKey]uint64{}
}

func getLatencyPercentilePQL(sloType slomodel.SLOType, cpuType CPUType, duration string) string {
	return fmt.Sprintf("histogram_quantile(%f, sum by (content_key, svc_name, vm_account_id ,%s) (rate(kindling_profiling_%s_duration_nanoseconds_bucket{}[%s])))",
		slomodel.GetLatencyPercentileByType(sloType),
		global.PROM_RANGE,
		cpuType.String(),
		duration,
	)
}

func queryMetrics(ctx context.Context, client v1.API, endTimeMillis int64, sloType slomodel.SLOType, cpuType CPUType, duration string) (map[MetricKey]uint64, error) {
	query := getLatencyPercentilePQL(sloType, cpuType, duration)

	result, warnings, err := client.Query(ctx, query, time.UnixMilli(endTimeMillis))
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		log.Printf("Request Prometheus Warning: %s", warnings)
	}
	resultMap := make(map[MetricKey]uint64)
	if vector, ok := result.(prometheus_model.Vector); ok {
		for _, sample := range vector {
			if float64(sample.Value) > 0 {
				contentKey := string(sample.Metric[LabelContentKey])
				servcieName := string(sample.Metric[LabelServiceName])
				accountID := string(sample.Metric[LabelVMAccountID])
				key := MetricKey{
					ServiceName: servcieName,
					ContentKey:  contentKey,
					AccountID:   accountID,
				}
				resultMap[key] = uint64(sample.Value)
			}
		}
	}
	return resultMap, nil
}

type MetricKey struct {
	ServiceName string
	ContentKey  string
	AccountID   string
}
