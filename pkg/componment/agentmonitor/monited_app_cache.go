package agentmonitor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/CloudDetail/apo-receiver/pkg/global"
	"github.com/CloudDetail/apo-receiver/pkg/model"
)

var CacheInstance *MonitedAppCache

type MonitedAppCache struct {
	monitedApps sync.Map   // <string, []*model.QueryMonitedAppData>
	stopChan    chan bool
}

func NewMonitedAppCache() *MonitedAppCache {
	return &MonitedAppCache{}
}

func (cache *MonitedAppCache) Start() {
	go cache.loopGetRelatedApps()
}

func (cache *MonitedAppCache) Stop() {
	close(cache.stopChan)
}

func (cache *MonitedAppCache) loopGetRelatedApps() {
	timer := time.NewTicker(1 * time.Minute)
	for {
		select {
		case <-timer.C:
			cache.getAndStoreRelatedApps()
		case <-cache.stopChan:
			timer.Stop()
			return
		}
	}
}

func (cache *MonitedAppCache) getAndStoreRelatedApps() {
	apps, err := global.CLICK_HOUSE.QueryRelatedAppInfos(context.Background())
	if err != nil {
		log.Printf("[x query related apps] error: %v", err)
		return
	}
	for key, value := range apps {
		cache.monitedApps.Store(key, value)
	}
}

func (cache *MonitedAppCache) GetMonitedApps(nodeIp string, nodeName string, clusterId string) []*model.QueryMonitedAppData {
	key := fmt.Sprintf("%s-%s-%s", nodeIp, nodeName, clusterId)
	if appsInterface, ok := cache.monitedApps.Load(key); ok {
		apps := appsInterface.([]*model.QueryMonitedAppData)
		return apps
	}
	return []*model.QueryMonitedAppData{}
}
