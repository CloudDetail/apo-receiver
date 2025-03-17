package model

import (
	"fmt"
	"io"
)

type MetricType string

var (
	MetricDbDuration = &MetricDef{
		Name: "kindling_db_duration_nanoseconds",
		Help: "A histogram of the db duration",
		Type: MetricHistogram,
		Keys: []string{
			"svc_name", "content_key", "node_name", "node_ip", "pid", "containerId",
			"name", "db_system", "db_name", "db_url", "is_error", "source",
		},
	}

	MetricExternalDuration = &MetricDef{
		Name: "kindling_external_duration_nanoseconds",
		Help: "A histogram of the external duration",
		Type: MetricHistogram,
		Keys: []string{
			"svc_name", "content_key", "node_name", "node_ip", "pid", "containerId",
			"name", "system", "address", "is_error", "source",
		},
	}

	MetricMqDuration = &MetricDef{
		Name: "kindling_mq_duration_nanoseconds",
		Help: "A histogram of the mq duration",
		Type: MetricHistogram,
		Keys: []string{
			"svc_name", "content_key", "node_name", "node_ip", "pid", "containerId",
			"name", "system", "role", "address", "is_error", "source",
		},
	}

	MetricAdapterApmTraceCount = &MetricDef{
		Name: "originx_sr_adapter_apmtrace_count",
		Help: "A counter of the apmtrace connectivity",
		Type: MetricCounter,
		Keys: []string{
			"node_name", "node_ip", "pid", "container_id", "is_hit",
		},
	}
)

const (
	MetricHistogram MetricType = "histogram"
	MetricCounter   MetricType = "counter"
	MetricGauge     MetricType = "gauge"
)

type MetricDef struct {
	Name string
	Help string
	Type MetricType
	Keys []string
}

func (def *MetricDef) WriteHead(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s.\n", def.Name, def.Help)
	fmt.Fprintf(w, "# TYPE %s %s\n", def.Name, string(def.Type))
}
