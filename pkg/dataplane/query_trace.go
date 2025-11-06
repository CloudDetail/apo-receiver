package dataplane

import (
	"context"
	"fmt"

	"encoding/json"

	apmmodel "github.com/CloudDetail/apo-module/apm/model/v1"
	"github.com/CloudDetail/apo-module/model/v1"
)

func (c *DataplaneClient) QueryTrace(ctx context.Context, req *QueryTraceSpansRequest) (*QueryTracesResponse, error) {
	resp, err := c.post(TraceEndpoint, req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var dr DataplaneTraceResponse
	err = json.NewDecoder(resp.Body).Decode(&dr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse completion response, err: %w", err)
	}

	if dr.Success {
		// Pick up the first trace
		for _, traceResp := range dr.Data {
			if len(traceResp.Error) > 0 {
				continue
			}
			if len(traceResp.Data) > 0 {
				return traceResp, nil
			}
		}
	}
	return nil, fmt.Errorf("trace not found")
}

type DataplaneTraceResponse struct {
	Success bool                   `json:"success"`
	Data    []*QueryTracesResponse `json:"data"`
}

// QueryTraceSpansRequest 定义查询Spans参数
type QueryTraceSpansRequest struct {
	ProviderId int              `json:"providerId"`      // 接入数据源ID
	StartTime  int64            `json:"startTime"`       // 开始时间
	EndTime    int64            `json:"endTime"`         // 结束时间
	Filter     QueryTraceFilter `json:"filter"`          // 过滤条件
	Limit      int64            `json:"limit,omitempty"` // 返回记录数量
}

// QueryTraceFilter 定义过滤条件
type QueryTraceFilter struct {
	Service     string `json:"service,omitempty"`     // 服务名
	Operation   string `json:"operation,omitempty"`   // 操作名
	Error       bool   `json:"error,omitempty"`       // 是否Error
	MinDuration uint64 `json:"minDuration,omitempty"` // 最小耗时
	MaxDuration uint64 `json:"maxDuration,omitempty"` // 最大耗时

	TraceId string `json:"traceId,omitempty"` // traceID
}

type QueryTracesResponse struct {
	ProviderId int          `json:"providerId"` // 接入数据源ID
	ClusterId  string       `json:"clusterId"`  // 接入集群ID
	DataSource string       `json:"dataSource"` // 接入数据源名称
	Unit       string       `json:"unit"`       // Span返回单位
	Data       []*OtelTrace `json:"data"`
	HasData    bool         `json:"hasData"`
	Error      string       `json:"error,omitempty"`
}

type OtelTrace struct {
	TraceId string      `json:"traceId"`
	Spans   []*OtelSpan `json:"spans"`
}

type OtelSpan struct {
	StartTime   uint64             `json:"startTime"` // us
	Duration    uint64             `json:"duration"`  // us
	ServiceName string             `json:"serviceName"`
	Name        string             `json:"name"`
	SpanId      string             `json:"spanId,omitempty"`
	TraceId     string             `json:"-"`
	PSpanId     string             `json:"parentSpanId,omitempty"`
	Kind        OtelSpanKind       `json:"spanKind"` // unspecified|internal|server|client|consumer|providerw
	IsError     bool               `json:"isError"`
	Attributes  map[string]string  `json:"attributes"`
	Exceptions  []*model.Exception `json:"exceptions,omitempty"`
}

type OtelSpanKind string

const (
	SpanKindUnspecified = "unspecified"
	SpanKindInternal    = "internal"
	SpanKindServer      = "server"
	SpanKindClient      = "client"
	SpanKindProducer    = "producer"
	SpanKindConsumer    = "consumer"
)

func (kind OtelSpanKind) IsExit() bool {
	return kind == SpanKindClient || kind == SpanKindProducer
}

func PickClientCall(resp *QueryTracesResponse, spanId string) []*model.ApmClientCall {
	if len(resp.Data) <= 0 {
		return nil
	}

	var result []*model.ApmClientCall
	for _, trace := range resp.Data {
		spans := trace.Spans
		var childSpans = make(map[string][]*OtelSpan, 0) // spanId -> childSpan
		var serviceSpan *OtelSpan

		for _, span := range spans {
			children, find := childSpans[span.PSpanId]
			if find {
				childSpans[span.PSpanId] = append(children, span)
			} else {
				childSpans[span.PSpanId] = []*OtelSpan{span}
			}

			if span.SpanId == spanId {
				serviceSpan = span
			}
		}

		if serviceSpan == nil {
			continue
		}
		clientCalls := rSearchClientCall(childSpans, serviceSpan)
		if len(clientCalls) > 0 {
			result = append(result, clientCalls...)
		}
	}
	return result
}

func rSearchClientCall(childSpanMap map[string][]*OtelSpan, pSpan *OtelSpan) []*model.ApmClientCall {
	childSpans, find := childSpanMap[pSpan.SpanId]
	if !find {
		return nil
	}

	var clientCalls []*model.ApmClientCall

	for _, span := range childSpans {
		if pSpan.Kind.IsExit() && !span.Kind.IsExit() {
			clientCalls = append(clientCalls, newApmClientCall(pSpan, span))
			continue
		}
		if span.ServiceName != pSpan.ServiceName {
			continue
		}
		rs := rSearchClientCall(childSpanMap, span)
		if len(rs) > 0 {
			clientCalls = append(clientCalls, rs...)
		}
	}

	return clientCalls
}

func newApmClientCall(clientSpan *OtelSpan, serverEntrySpan *OtelSpan) *model.ApmClientCall {
	if serverEntrySpan == nil {
		return &model.ApmClientCall{
			ClientStartTime:  clientSpan.StartTime,
			ClientEndTime:    clientSpan.StartTime + clientSpan.Duration,
			ClientName:       clientSpan.Name,
			ClientSpanId:     clientSpan.SpanId,
			ClientAttributes: clientSpan.Attributes,
			// ClientOriginalSpanId: clientSpan.OriginalSpanId(),
			ServerDuration: 0,
		}
	}
	return &model.ApmClientCall{
		ClientStartTime:  clientSpan.StartTime,
		ClientEndTime:    clientSpan.StartTime + clientSpan.Duration,
		ClientName:       clientSpan.Name,
		ClientSpanId:     clientSpan.SpanId,
		ClientAttributes: clientSpan.Attributes,
		// ClientOriginalSpanId: clientSpan.OriginalSpanId(),
		ServerDuration: serverEntrySpan.Duration,
		ServerName:     serverEntrySpan.ServiceName,
	}
}

func GetServiceNode(resp *QueryTracesResponse) ([]*apmmodel.OtelServiceNode, error) {
	trace, err := ConvertToOtelTrace(resp)
	if err != nil {
		return nil, err
	}

	return trace.GetServiceNodes(), nil
}

func ConvertToOtelTrace(resp *QueryTracesResponse) (*apmmodel.OTelTrace, error) {
	data := resp.Data

	var result []*apmmodel.OTelTrace
	for _, trace := range data {
		otelTrace := apmmodel.NewOTelTrace(resp.DataSource)
		traceTree := apmmodel.NewOtelTree()

		for _, span := range trace.Spans {
			otelSpan := &apmmodel.OtelSpan{
				StartTime:   span.StartTime,
				Duration:    span.Duration,
				ServiceName: span.ServiceName,
				Name:        span.Name,
				SpanId:      span.SpanId,
				PSpanId:     span.PSpanId,
				NextSpanId:  "",
				Kind:        transSpanKind(span.Kind),
				Code:        apmmodel.StatusCodeUnset,
				NotSampled:  false,
				Attributes:  span.Attributes,
				Exceptions:  span.Exceptions,
			}
			err := traceTree.AddSpan(otelSpan)
			if err != nil {
				return nil, err
			}
		}

		err := traceTree.BuildRelation4Spans(otelTrace)
		if err != nil {
			return nil, err
		}
		result = append(result, otelTrace)
	}
	return result[0], nil
}

func transSpanKind(kind OtelSpanKind) apmmodel.OtelSpanKind {
	switch kind {
	case SpanKindUnspecified:
		return apmmodel.SpanKindUnspecified
	case SpanKindClient:
		return apmmodel.SpanKindClient
	case SpanKindServer:
		return apmmodel.SpanKindServer
	case SpanKindInternal:
		return apmmodel.SpanKindInternal
	case SpanKindProducer:
		return apmmodel.SpanKindProducer
	case SpanKindConsumer:
		return apmmodel.SpanKindConsumer
	default:
		return apmmodel.SpanKindUnspecified
	}
}
