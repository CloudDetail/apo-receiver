package analyzer

import (
	"github.com/CloudDetail/metadata/model/cache"
	"github.com/CloudDetail/apo-module/model/v1"
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
