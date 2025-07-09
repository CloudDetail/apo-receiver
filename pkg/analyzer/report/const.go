package report

const (
	FlameGraph        = "flame_graph"
	JvmGc             = "jvm_gc"
	OnOffMetricGroup  = "onoff_metric_group"
	SpanTraceGroup    = "span_trace_group"
	CameraEventGroup  = "camera_event_group"
	CameraNodeReport  = "camera_node_report"
	CameraErrorReport = "camera_error_report"

	DesignatedProfilingSignal = "designated_profiling_signal"

	OriginxAgentEvent = "originx_agent_event"
	OriginxAppInfo    = "originx_app_info"
)

type ReportType int

const (
	SlowReportType ReportType = iota
	ErrorReportType
	NormalReportType
)

func (reportType ReportType) String() string {
	switch reportType {
	case SlowReportType:
		return "Slow"
	case ErrorReportType:
		return "Error"
	case NormalReportType:
		return "Normal"
	default:
		return "Unknown"
	}
}
