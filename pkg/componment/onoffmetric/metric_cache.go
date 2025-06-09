package onoffmetric

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CloudDetail/apo-receiver/pkg/componment/threshold"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"

	slomodel "github.com/CloudDetail/apo-module/slo/api/v1/model"
)

var CacheInstance *MetricCache

type MetricCache struct {
	promClient         v1.API
	mutex              sync.RWMutex
	YesterdayMetricMap map[MetricKey]*MetricDatas
	LastHourMetricMap  map[MetricKey]*MetricDatas
	stopChan           chan bool
}

func NewMetricCache(promClient v1.API) *MetricCache {
	return &MetricCache{
		promClient:         promClient,
		stopChan:           make(chan bool),
		YesterdayMetricMap: make(map[MetricKey]*MetricDatas, 0),
		LastHourMetricMap:  make(map[MetricKey]*MetricDatas, 0),
	}
}

func (cache *MetricCache) Start() {
	cache.storeYesterdayMetrics()
	cache.storeLastHourMetrics()

	go cache.checkTask()
}

func (cache *MetricCache) Stop() {
	close(cache.stopChan)
}

func (cache *MetricCache) checkTask() {
	timer := time.NewTicker(1 * time.Hour)
	currentDay := time.Now().Day()
	for {
		select {
		case <-timer.C:
			newDay := time.Now().Day()
			if newDay != currentDay {
				currentDay = newDay
				dailyCount := cache.storeYesterdayMetrics()
				log.Printf("[Set Daily Metrics] Count: %d", dailyCount)
				log.Printf("[Clean Hour Metrics] Count: %d", len(cache.LastHourMetricMap))
				cache.cleanLastHourMetrics()
			}
			hourCount := cache.storeLastHourMetrics()
			log.Printf("[Update Hourly Metrics] Count: %d", hourCount)
		case <-cache.stopChan:
			timer.Stop()
			return
		}
	}
}

func (cache *MetricCache) storeYesterdayMetrics() int {
	sloType := slomodel.SLO_LATENCY_P90_TYPE

	now := time.Now()
	year, month, day := now.Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, time.Local)
	todayStartTSNano := today.UnixMilli()

	result := make(map[MetricKey]*MetricDatas, 0)
	for _, cpuType := range AllCPUTypes {
		yesterdayLatency := getYesterdayLatency(cache.promClient, sloType, cpuType, todayStartTSNano)
		addOnOffMetrics(result, sloType, cpuType, yesterdayLatency)
	}

	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.YesterdayMetricMap = result
	return len(result)
}

func (cache *MetricCache) cleanLastHourMetrics() {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.LastHourMetricMap = map[MetricKey]*MetricDatas{}
}

func (cache *MetricCache) storeLastHourMetrics() int {
	sloType := slomodel.SLO_LATENCY_P90_TYPE
	now := time.Now().UnixMilli()

	result := make(map[MetricKey]*MetricDatas, 0)
	for _, cpuType := range AllCPUTypes {
		lastHourLatency := GetLastOneHour(cache.promClient, sloType, cpuType, now)
		addOnOffMetrics(result, sloType, cpuType, lastHourLatency)
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	count := 0
	for key, value := range result {
		if _, exist := cache.YesterdayMetricMap[key]; exist {
			continue
		}
		if lastHour, exist := cache.LastHourMetricMap[key]; exist {
			if lastHour.Compare(value) > 0 {
				cache.LastHourMetricMap[key] = value
				count++
			}
		} else {
			cache.LastHourMetricMap[key] = value
			count++
		}
	}
	return count
}

func (cache *MetricCache) GetMetricValue(key MetricKey) (*MetricDatas, threshold.ThresholdRange) {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	if metric, exist := cache.YesterdayMetricMap[key]; exist {
		return metric, threshold.Yesterday
	}
	if metric, exist := cache.LastHourMetricMap[key]; exist {
		return metric, threshold.Last1h
	}

	key.ServiceName = ""
	if metric, exist := cache.YesterdayMetricMap[key]; exist {
		return metric, threshold.Yesterday
	}
	if metric, exist := cache.LastHourMetricMap[key]; exist {
		return metric, threshold.Last1h
	}
	return nil, threshold.UnknownRange
}

func addOnOffMetrics(metrics map[MetricKey]*MetricDatas, sloType slomodel.SLOType, cpuType CPUType, datas map[MetricKey]uint64) {
	if datas == nil {
		return
	}
	for key, value := range datas {
		metric, ok := metrics[key]
		if !ok {
			metric = NewMetricDatas()
			metrics[key] = metric
		}
		metric.updateValue(sloType, cpuType, value)
	}
}

type CPUType int

const (
	CPUTYPE_UNKNOWN CPUType = -1
	CPUType_ON      CPUType = 0
	CPUType_FILE    CPUType = 1
	CPUType_NET     CPUType = 2
	CPUType_FUTEX   CPUType = 3
	CPUType_IDLE    CPUType = 4
	CPUType_OTHER   CPUType = 5
	CPUType_EPOLL   CPUType = 6
	CPUTYPE_RUNQ    CPUType = 7
)

var (
	AllCPUTypes = []CPUType{
		CPUType_ON,
		CPUType_FILE,
		CPUType_NET,
		CPUType_FUTEX,
		CPUType_IDLE,
		CPUType_OTHER,
		CPUType_EPOLL,
		CPUTYPE_RUNQ,
	}
)

func GetCpuType(cpuType int) CPUType {
	if cpuType >= 0 && cpuType < len(AllCPUTypes) {
		return AllCPUTypes[cpuType]
	}
	return CPUTYPE_UNKNOWN
}

func (cpuType CPUType) String() string {
	switch cpuType {
	case CPUTYPE_UNKNOWN:
		return "unknown"
	case CPUType_ON:
		return "cpu"
	case CPUType_FILE:
		return "file"
	case CPUType_NET:
		return "net"
	case CPUType_FUTEX:
		return "futex"
	case CPUType_IDLE:
		return "idle"
	case CPUType_OTHER:
		return "other"
	case CPUType_EPOLL:
		return "epoll"
	case CPUTYPE_RUNQ:
		return "runq"
	}
	return "unknown"
}

type MetricDatas struct {
	P90Values [8]uint64
}

func NewMetricDatas() *MetricDatas {
	return &MetricDatas{}
}

func (metric *MetricDatas) updateValue(sloType slomodel.SLOType, cpuType CPUType, value uint64) {
	switch sloType {
	case slomodel.SLO_LATENCY_P90_TYPE:
		metric.P90Values[cpuType] = value
	default:
	}

}

func (metric *MetricDatas) GetValue(sloType slomodel.SLOType, cpuType int) uint64 {
	switch sloType {
	case slomodel.SLO_LATENCY_P90_TYPE:
		return metric.P90Values[cpuType]
	default:
		return 0
	}
}

func (metric *MetricDatas) getBaseMetric(sloType slomodel.SLOType) string {
	switch sloType {
	case slomodel.SLO_LATENCY_P90_TYPE:
		return getMetricStr(metric.P90Values)
	default:
		return ""
	}
}

func getMetricStr(metrics [8]uint64) string {
	var buffer bytes.Buffer
	for i, metric := range metrics {
		if i > 0 {
			buffer.WriteString(",")
		}
		buffer.WriteString(fmt.Sprintf("%d", metric))
	}
	return buffer.String()
}

func (metric *MetricDatas) Compare(other *MetricDatas) int {
	var compoareTime int64 = 0
	for _, value := range metric.P90Values {
		compoareTime += int64(value)
	}
	for _, value := range other.P90Values {
		compoareTime -= int64(value)
	}
	if compoareTime > 0 {
		return 1
	} else if compoareTime < 0 {
		return -1
	} else {
		return 0
	}
}

func getMetricValue(metric *MetricDatas, sloType slomodel.SLOType, cpuType int) uint64 {
	if metric == nil {
		return 0
	}
	return metric.GetValue(sloType, cpuType)
}

func GetMetricStr(metric *MetricDatas, sloType slomodel.SLOType) string {
	if metric == nil {
		return ""
	}
	return metric.getBaseMetric(sloType)
}

func CalcMutatedType(sloType slomodel.SLOType, key MetricKey, values string) (CPUType, string, string) {
	onoffValues := strings.Split(values, ",")
	metric, thresholdRange := CacheInstance.GetMetricValue(key)

	var mutatedType int = -1
	var mutatedValue uint64 = 0
	for i := 0; i < 7; i++ {
		metricValue, _ := strconv.ParseUint(onoffValues[i], 10, 64)
		if metricValue > 0 {
			baseValue := getMetricValue(metric, sloType, i)
			if metricValue > baseValue && (mutatedValue == 0 || mutatedValue < metricValue-baseValue) {
				mutatedValue = metricValue - baseValue
				mutatedType = i
			}
		}
	}

	runqOnOffValue := onoffValues[len(onoffValues)-1]
	metricValue, _ := strconv.ParseUint(runqOnOffValue, 10, 64)
	if metricValue > 0 {
		baseValue := getMetricValue(metric, sloType, 7)
		if metricValue > baseValue && (mutatedValue == 0 || mutatedValue < metricValue-baseValue) {
			// mutatedValue = metricValue - baseValue
			mutatedType = 7
		}
	}
	return GetCpuType(mutatedType), GetMetricStr(metric, sloType), thresholdRange.String()
}
