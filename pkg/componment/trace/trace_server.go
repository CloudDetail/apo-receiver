package trace

import (
	"context"
	"encoding/json"
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/CloudDetail/apo-module/model/v1"
	"github.com/CloudDetail/apo-receiver/pkg/analyzer"
	"github.com/CloudDetail/apo-receiver/pkg/analyzer/report"
	"github.com/CloudDetail/apo-receiver/pkg/global"
	grpc_model "github.com/CloudDetail/apo-receiver/pkg/model"
)

var (
	ReceiveMessageTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "originx_receiver_received_message_total",
			Help: "The total number of receiving message from agents",
		},
		[]string{"type"},
	)
)

func init() {
	prometheus.MustRegister(ReceiveMessageTotal)
}

type TraceServer struct {
	grpc_model.UnimplementedTraceServiceServer
	analyzer *analyzer.ReportAnalyzer
}

func NewTraceServer(analyzer *analyzer.ReportAnalyzer) *TraceServer {
	return &TraceServer{
		analyzer: analyzer,
	}
}

func (server *TraceServer) StoreDataGroups(ctx context.Context, dataGroups *grpc_model.DataGroups) (*emptypb.Empty, error) {
	if dataGroups.Name == report.OnOffMetricGroup {
		for _, data := range dataGroups.Datas {
			server.analyzer.CacheMetric(data)
		}
		// OnOffMetric
		global.CLICK_HOUSE.BatchStore(ctx, dataGroups.Name, dataGroups.Datas)
	} else if dataGroups.Name == report.SpanTraceGroup {
		for _, data := range dataGroups.Datas {
			server.analyzer.CacheTrace(ctx, data)
		}
	} else if dataGroups.Name == report.DesignatedProfilingSignal {
		// Same with the structure of SpanTraceGroup but lacked trace labels,
		// also saved as SpanTraceGroup. DesignatedProfilingSignal is used for TraceProfiling.
		for _, data := range dataGroups.Datas {
			signal := &model.Trace{Labels: &model.TraceLabels{ThresholdMultiple: 1.0}}
			if err := json.Unmarshal([]byte(data), signal); err != nil {
				log.Printf("[x Parse Profile Signal] Error: %s", err.Error())
				continue
			}
			global.CLICK_HOUSE.StoreTraceGroup(ctx, signal)
		}
	} else if dataGroups.Name == report.OriginxAgentEvent {
		for _, data := range dataGroups.Datas {
			server.analyzer.StoreEvent(ctx, data)
		}
	} else {
		// Profile、Log
		global.CLICK_HOUSE.BatchStore(ctx, dataGroups.Name, dataGroups.Datas)
	}
	if len(dataGroups.Datas) > 0 {
		ReceiveMessageTotal.WithLabelValues(dataGroups.Name).Inc()
	}
	return &emptypb.Empty{}, nil
}

func (server *TraceServer) Start() {
	server.analyzer.Start()
}
