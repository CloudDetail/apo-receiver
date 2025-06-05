package threshold

import (
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/robfig/cron/v3"

	sloapi "github.com/CloudDetail/apo-module/slo/api/v1"
	slomodel "github.com/CloudDetail/apo-module/slo/api/v1/model"
	slochecker "github.com/CloudDetail/apo-module/slo/sdk/v1/checker"
	grpc_model "github.com/CloudDetail/apo-receiver/pkg/model"
)

var CacheInstance *ThresholdCache
var defaultLatencyMultiple = 1.1

type ThresholdCache struct {
	// not used now
	promClient     v1.API
	sloConfigCache sloapi.ConfigManager
	// ContentKey -> SlowThresholdData
	// TODO use sync.Map
	// The threshold here is the product of percentile and its multiple
	SlowThresholdMap map[string]*grpc_model.SlowThresholdData

	cronTask *cron.Cron
}

func NewThresholdCache(promClient v1.API, sloConfigCache sloapi.ConfigManager) *ThresholdCache {
	return &ThresholdCache{
		promClient:       promClient,
		sloConfigCache:   sloConfigCache,
		SlowThresholdMap: make(map[string]*grpc_model.SlowThresholdData),
		cronTask:         cron.New(cron.WithSeconds()),
	}
}

func (t *ThresholdCache) Start() {
	t.storeAllSlowThreshold(true)
	t.cronTask.AddFunc("0 0/5 * * * *", func() {
		t.storeAllSlowThreshold(false)
	})
	t.cronTask.Start()
}

func (t *ThresholdCache) GetSlowThreshold(contentKey string) *grpc_model.SlowThresholdData {
	return t.SlowThresholdMap[contentKey]
}

func (t *ThresholdCache) UpdateThresholdConfig(contentKey string, slowThreshold *grpc_model.SlowThresholdData) {
	t.SlowThresholdMap[contentKey] = slowThreshold
}

func (t *ThresholdCache) storeAllSlowThreshold(isInit bool) {
	now := time.Now()
	year, month, day := now.Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, time.Local)

	var resultMap = map[string]*grpc_model.SlowThresholdData{}

	if isInit {
		resultMap = t.getYesterdaySLO(resultMap, today)
	} else if now.Hour() == 0 && now.Minute() < 5 {
		resultMap = t.getYesterdaySLO(resultMap, today)
		t.SlowThresholdMap = resultMap
		return
	}

	var from time.Time
	if now.Minute() > 5 {
		from = now.Add(-time.Minute * time.Duration(now.Minute()))
	} else {
		from = now.Add(-time.Hour)
	}

	entries, err := slochecker.DefaultChecker.ListContentKeyTemp(
		"", from.UnixMilli(), now.UnixMilli())

	if err != nil {
		return
	}

	// copy map
	for key, threshold := range t.SlowThresholdMap {
		resultMap[key] = threshold
	}

	for _, entry := range entries {
		if _, find := resultMap[entry.EntryURI]; find {
			continue
		}
		sloConfig := t.sloConfigCache.GetSLOConfigOrDefaultInLastHour(
			slomodel.SLOEntryKey{
				EntryURI: entry.EntryURI,
			})
		slowThreshold := GetSlowThresholdFromSLOs(entry.EntryURI, sloConfig)
		resultMap[slowThreshold.Url] = slowThreshold
	}

	t.SlowThresholdMap = resultMap
}

func (t *ThresholdCache) getYesterdaySLO(resultMap map[string]*grpc_model.SlowThresholdData, today time.Time) map[string]*grpc_model.SlowThresholdData {
	yesterday := today.Add(-24 * time.Hour)
	entries, err := slochecker.DefaultChecker.ListContentKeyTemp("", yesterday.UnixMilli(), today.UnixMilli())
	if err == nil {
		for _, entry := range entries {
			sloConfig := t.sloConfigCache.GetSLOConfigOrDefault(
				slomodel.SLOEntryKey{
					EntryURI: entry.EntryURI,
				})
			slowThreshold := GetSlowThresholdFromSLOs(entry.EntryURI, sloConfig)
			resultMap[entry.EntryURI] = slowThreshold
		}
	}

	return resultMap
}

const (
	dayDuration  = "24h"
	hourDuration = "1h"
)

func (t *ThresholdCache) getConfigSloThresholds() map[string]*grpc_model.SlowThresholdData {
	sloThresholdMap := make(map[string]*grpc_model.SlowThresholdData)
	cache := t.sloConfigCache.ListTarget()
	cache.Range(func(key, value interface{}) bool {
		target := value.(*slomodel.SLOTarget)
		uri := target.InfoRef.KeyRef.EntryURI
		slowThreshold := GetSlowThresholdFromSLOs(uri, target.SLOConfigs)
		sloThresholdMap[uri] = slowThreshold
		return true
	})
	return sloThresholdMap
}

func GetSlowThresholdFromSLOs(uri string, configs []slomodel.SLOConfig) *grpc_model.SlowThresholdData {
	slowThreshold := &grpc_model.SlowThresholdData{
		Url:         uri,
		ContainerId: "",
		Value:       1e20,
		Type:        "",
		Range:       "",
		ServiceName: "",
	}
	for _, config := range configs {
		if config.Type == slomodel.SLO_SUCCESS_RATE_TYPE {
			continue
		}
		// Get the lowest threshold
		if config.Type == slomodel.SLO_LATENCY_P90_TYPE ||
			config.Type == slomodel.SLO_LATENCY_P95_TYPE ||
			config.Type == slomodel.SLO_LATENCY_P99_TYPE {
			if slowThreshold.Value > config.ExpectedValue {
				slowThreshold.Value = config.ExpectedValue
				slowThreshold.Range = string(config.Source)
				slowThreshold.Type = string(config.Type)
				slowThreshold.Multiple = config.Multiple
			}
		}
	}
	// If there is no latency configuration, use the default value 500ms
	if slowThreshold.Value >= 1e20 {
		slowThreshold.Value = 500e6 // 500ms
		slowThreshold.Range = string(slomodel.DefaultExpectSource)
		slowThreshold.Type = string(slomodel.SLO_LATENCY_P90_TYPE)
		slowThreshold.Multiple = 1.0
	} else {
		slowThreshold.Value = slowThreshold.Value * 1e6
	}
	return slowThreshold
}

// TODO add accountID support
// func (t *ThresholdCache) getYesterdayPercentile() map[string]*grpc_model.SlowThresholdData {
// 	now := time.Now()
// 	year, month, day := now.Date()
// 	today := time.Date(year, month, day, 0, 0, 0, 0, time.Local)
// 	result, err := queryMetric(t.promClient, today, 0.9, dayDuration)
// 	if err != nil {
// 		log.Printf("Failed to get yesterday percentile: %v", err)
// 		return nil
// 	}
// 	return result
// }

// func (t *ThresholdCache) getLastHourPercentile() map[string]*grpc_model.SlowThresholdData {
// 	result, err := queryMetric(t.promClient, time.Now(), 0.9, hourDuration)
// 	if err != nil {
// 		log.Printf("Failed to get yesterday percentile: %v", err)
// 		return nil
// 	}
// 	return result
// }

// func getContentKeyPercentileQuery(p9xValue float64, duration string) string {
// 	return fmt.Sprintf("histogram_quantile(%f, sum by (content_key, %s) (rate(kindling_span_trace_duration_nanoseconds_bucket{}[%s])))",
// 		p9xValue,
// 		global.PROM_RANGE,
// 		duration,
// 	)
// }

const LabelContentKey = "content_key"

// TODO add accountID support
// func queryMetric(client v1.API, endTime time.Time, percentile float64, duration string) (map[string]*grpc_model.SlowThresholdData, error) {
// 	query := getContentKeyPercentileQuery(percentile, duration)

// 	result, warnings, err := client.Query(context.Background(), query, endTime)
// 	if err != nil {
// 		return nil, err
// 	}
// 	if len(warnings) > 0 {
// 		log.Printf("Request Prometheus Warning: %s", warnings)
// 	}
// 	thresholdType := ToThresholdType(percentile)
// 	thresholdRange := ToThresholdRange(duration)
// 	resultMap := make(map[string]*grpc_model.SlowThresholdData)
// 	if vector, ok := result.(prometheus_model.Vector); ok {
// 		for _, sample := range vector {
// 			contentKey := string(sample.Metric[LabelContentKey])
// 			if float64(sample.Value) > 0 {
// 				resultMap[contentKey] = &grpc_model.SlowThresholdData{
// 					Url:         contentKey,
// 					ContainerId: "",
// 					// Note the value is the product of the percentile and the default multiple 1.1
// 					Value:       float64(sample.Value) * defaultLatencyMultiple,
// 					Type:        string(thresholdType),
// 					Range:       string(thresholdRange),
// 					Multiple:    defaultLatencyMultiple,
// 					ServiceName: "",
// 				}
// 			}
// 		}
// 	}
// 	return resultMap, nil
// }
