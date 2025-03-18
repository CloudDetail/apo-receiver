package metrics

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	pb "github.com/CloudDetail/apo-receiver/internal/prometheus"
	"github.com/CloudDetail/apo-receiver/pkg/metrics/model"
	"github.com/CloudDetail/apo-receiver/pkg/metrics/pm"
	"github.com/CloudDetail/apo-receiver/pkg/metrics/vm"
)

type Metric interface {
	Update(value float64)
	MarshalTo(name string, labelKey string, w io.Writer) error
}

type PromMetric interface {
	ExportTimeSeries(ts int64, series *[]*pb.TimeSeries) error
}

type StorageType int

const (
	StorageProm StorageType = 0
	StorageVm   StorageType = 1
)

var (
	registeredMetrics = make(map[*model.MetricDef]*lruMetrics) // <name, lruMetrics>
	registeredLock    sync.Mutex
	metricConfig      *MetricConfig
)

func init() {
	metricConfig = newMetricConfig("vm", 10000, []time.Duration{})
}

func UpdateMetricConfig(promType string, cacheSize int, durationBuckets []time.Duration) {
	metricConfig = newMetricConfig(promType, cacheSize, durationBuckets)
}

type MetricConfig struct {
	storageType StorageType
	cacheSize   int
	buckets     []float64
}

func newMetricConfig(promType string, cacheSize int, durationBuckets []time.Duration) *MetricConfig {
	storageType := StorageProm
	if promType == "vm" {
		storageType = StorageVm
	}
	buckets := make([]float64, 0)
	for _, durationBucket := range durationBuckets {
		buckets = append(buckets, float64(durationBucket.Nanoseconds()))
	}
	return &MetricConfig{
		storageType: storageType,
		cacheSize:   cacheSize,
		buckets:     buckets,
	}
}

func UpdateMetric(metricDef *model.MetricDef, labelValues []string, value float64) error {
	existMetrics, found := registeredMetrics[metricDef]
	if !found {
		labelsToTags, err := model.NewCache[string, struct{}](metricConfig.cacheSize)
		if err != nil {
			return err
		}
		existMetrics = &lruMetrics{
			datas:        make(map[string]Metric),
			keyBuilder:   bytes.NewBuffer(make([]byte, 0, 1024)),
			labelsToTags: labelsToTags,
		}
		registeredLock.Lock()
		registeredMetrics[metricDef] = existMetrics
		registeredLock.Unlock()
	}

	existMetrics.lock.Lock()
	defer existMetrics.lock.Unlock()
	key, err := buildLabelKey(existMetrics.keyBuilder, metricDef.Name, metricDef.Keys, labelValues)
	if err != nil {
		return err
	}
	if _, exist := existMetrics.labelsToTags.Get(key); !exist {
		existMetrics.labelsToTags.Add(key, struct{}{})
	}
	metric, ok := existMetrics.datas[key]
	if !ok {
		if metricDef.Type == model.MetricHistogram {
			if metricConfig.storageType == StorageVm {
				metric = vm.NewVmHistogram(metricDef.Name, metricDef.Keys, labelValues)
			} else {
				metric = pm.NewPromHistogram(metricDef, labelValues, metricConfig.buckets)
			}
		} else if metricDef.Type == model.MetricCounter {
			metric = pm.NewPromCounter(metricDef, labelValues)
		} else if metricDef.Type == model.MetricGauge {
			metric = pm.NewPromGauge(metricDef, labelValues)
		} else {
			return fmt.Errorf("unknown MetricType: %s", metricDef.Type)
		}
		existMetrics.datas[key] = metric
	}
	metric.Update(value)
	return nil
}

func GetMetrics(w io.Writer) {
	for metricDef, lru := range registeredMetrics {
		lru.lock.Lock()
		if len(lru.datas) > 0 {
			metricDef.WriteHead(w)
			for labelKey, metric := range lru.datas {
				if err := metric.MarshalTo(metricDef.Name, labelKey, w); err != nil {
					log.Printf("[x Collect Metric] Name: %s, Key: %s, Error: %s", metricDef.Name, labelKey, err.Error())
				}
			}
			lru.labelsToTags.RemoveEvictedItems()

			for key := range lru.datas {
				if !lru.labelsToTags.Contains(key) {
					log.Printf("[Clear Expire HistogramKey] Name: %s, Key: %s", metricDef.Name, key)
					delete(lru.datas, key)
				}
			}
		}
		lru.lock.Unlock()
	}
}

func BuildPromWriteRequest() *pb.WriteRequest {
	ts := time.Now().UnixMilli()
	timeSeries := make([]*pb.TimeSeries, 0)
	for metricDef, lru := range registeredMetrics {
		lru.lock.Lock()
		if len(lru.datas) > 0 {
			for labelKey, metric := range lru.datas {
				if promMetric, ok := metric.(PromMetric); ok {
					if err := promMetric.ExportTimeSeries(ts, &timeSeries); err != nil {
						log.Printf("[x Build PromMetrics] Name: %s, Key: %s, Error: %s", metricDef.Name, labelKey, err.Error())
					}
				}
			}
			lru.labelsToTags.RemoveEvictedItems()

			for key := range lru.datas {
				if !lru.labelsToTags.Contains(key) {
					log.Printf("[Clear Expire HistogramKey] Name: %s, Key: %s", metricDef.Name, key)
					delete(lru.datas, key)
				}
			}
		}
		lru.lock.Unlock()
	}
	if len(timeSeries) == 0 {
		return nil
	}
	return &pb.WriteRequest{
		Timeseries: timeSeries,
	}
}

func buildLabelKey(keyBuilder *bytes.Buffer, name string, keys []string, values []string) (string, error) {
	if len(values) != len(keys) {
		return "", fmt.Errorf("%s expect have %d value count, but got %d value count", name, len(keys), len(values))
	}

	keyBuilder.Reset()
	for i, key := range keys {
		if i > 0 {
			keyBuilder.WriteByte(',')
		}
		keyBuilder.WriteString(key)
		keyBuilder.WriteByte('=')
		keyBuilder.WriteByte('"')
		keyBuilder.WriteString(values[i])
		keyBuilder.WriteByte('"')
		i++
	}
	return keyBuilder.String(), nil
}

type lruMetrics struct {
	lock         sync.Mutex
	keyBuilder   *bytes.Buffer
	datas        map[string]Metric // <labels, Metric>
	labelsToTags *model.Cache[string, struct{}]
}
