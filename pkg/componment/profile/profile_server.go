package profile

import (
	"context"
	"encoding/json"
	"time"

	"github.com/CloudDetail/apo-receiver/pkg/global"
	"github.com/CloudDetail/apo-receiver/pkg/model"
)

type ProfileServer struct {
	model.UnimplementedProfileServiceServer
	normalTraceIdCache *traceIdCache
	slowTraceIdCache   *traceIdCache
	errorTraceIdCache  *traceIdCache
	SignalsCache       *SignalsCache
	openWindowSample   bool
	windowSampleNum    uint32
}

func NewProfileServer(cacheTime int, openWindowSample bool, windowSampleNum int) *ProfileServer {
	return &ProfileServer{
		normalTraceIdCache: NewTraceIdCache("Normal", cacheTime),
		slowTraceIdCache:   NewTraceIdCache("Slow", cacheTime),
		errorTraceIdCache:  NewTraceIdCache("Error", cacheTime),
		SignalsCache:       newSignalsCache(),
		openWindowSample:   openWindowSample,
		windowSampleNum:    uint32(windowSampleNum),
	}
}

func (server *ProfileServer) Start() {
	go server.cleanExpireTraceIds()
	go global.CACHE.SubscribeTraceIds(server.normalTraceIdCache, server.slowTraceIdCache, server.errorTraceIdCache)
	go server.SignalsCache.CollectMetrics()
}

func (server *ProfileServer) QueryProfiles(ctx context.Context, request *model.ProfileQuery) (*model.ProfileResult, error) {
	normalIgnoreTraceIds := make(map[string]bool, 0)
	for _, normalTraceId := range request.NormalTraceIds {
		normalIgnoreTraceIds[normalTraceId] = true
	}

	slowIgnoreTraceIds := make(map[string]bool, 0)
	for _, slowTraceId := range request.SlowTraceIds {
		slowIgnoreTraceIds[slowTraceId] = true
	}

	errorIgnoreTraceIds := make(map[string]bool, 0)
	for _, errorTraceId := range request.ErrorTraceIds {
		errorIgnoreTraceIds[errorTraceId] = true
	}
	global.CACHE.NotifySampledTraceIds(request.NormalTraceIds, request.SlowTraceIds, request.ErrorTraceIds)

	endIndex := global.CACHE.GetTraceIndex()
	var (
		closePidUrls   []string
		recoverPidUrls []string
	)

	if server.openWindowSample {
		closePidUrls, recoverPidUrls = server.SignalsCache.GetSignalsMap(ctx).QuerySilentSwitches(request.NodeIp)
	}
	signals := convertToSignals(global.CACHE.GetAndCleanSignals(request.NodeIp))
	return &model.ProfileResult{
		QueryTime:      endIndex,
		SampleCount:    server.windowSampleNum,
		NormalTraceIds: server.normalTraceIdCache.getTraceIds(normalIgnoreTraceIds, request.QueryTime, endIndex),
		SlowTraceIds:   server.slowTraceIdCache.getTraceIds(slowIgnoreTraceIds, request.QueryTime, endIndex),
		ErrorTraceIds:  server.errorTraceIdCache.getTraceIds(errorIgnoreTraceIds, request.QueryTime, endIndex),
		ClosePidUrls:   closePidUrls,
		RecoverPidUrls: recoverPidUrls,
		Signals:        signals,
	}, nil
}

func (server *ProfileServer) cleanExpireTraceIds() {
	timer := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-timer.C:
			now := time.Now().UnixNano()
			server.normalTraceIdCache.cleanExpireTraceId(now)
			server.slowTraceIdCache.cleanExpireTraceId(now)
			server.errorTraceIdCache.cleanExpireTraceId(now)
		}
	}
}

func convertToSignals(datas []string) []*model.ProfileSignal {
	var signals []*model.ProfileSignal
	if len(datas) > 0 {
		signals = make([]*model.ProfileSignal, 0)
		for _, data := range datas {
			signal := &model.ProfileSignal{}
			if err := json.Unmarshal([]byte(data), signal); err == nil {
				signals = append(signals, signal)
			}
		}
	}
	return signals
}
