package analyzer

import (
	"github.com/CloudDetail/apo-module/model/v1"
	"github.com/CloudDetail/apo-receiver/pkg/analyzer/appinfo"
	"github.com/CloudDetail/metadata/model/cache"
)

func fillK8sMetadataInSpanTrace(trace *model.Trace) bool {
	if trace.Labels.ContainerId == "" || len(trace.PodName) > 0 {
		return false
	}
	if pod, find := cache.Querier.GetPodByContainerId("", trace.Labels.ContainerId); find {
		trace.PodName = pod.Name
		trace.Namespace = pod.NS()
		owners := pod.GetOwnerReferences(true)
		if len(owners) > 0 {
			trace.WorkloadName = owners[0].Name
			trace.WorkloadKind = owners[0].Kind
		}
		return true
	}
	return false
}

func fillK8sMetadataInEvent(event *model.AgentEvent) {
	if containerId, ok := event.Labels["container_id"]; ok && len(containerId) > 0 {
		if pod, find := cache.Querier.GetPodByContainerId("", containerId); find {
			event.Labels["pod_name"] = pod.Name
			event.Labels["namespace"] = pod.NS()
			owners := pod.GetOwnerReferences(true)
			if len(owners) > 0 {
				event.Labels["workload_name"] = owners[0].Name
				event.Labels["workload_kind"] = owners[0].Kind
			}
		}
	}
}

func fillK8sMetadataInApp(appInfo *appinfo.AppInfo) {
	if appInfo.ContainerId != "" {
		if pod, find := cache.Querier.GetPodByContainerId("", appInfo.ContainerId); find {
			appInfo.Labels["pod_name"] = pod.Name
			appInfo.Labels["namespace"] = pod.NS()
			owners := pod.GetOwnerReferences(true)
			if len(owners) > 0 {
				appInfo.Labels["workload_name"] = owners[0].Name
				appInfo.Labels["workload_kind"] = owners[0].Kind
			}
		}
	}
}
