package pm

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"

	pb "github.com/CloudDetail/apo-receiver/internal/prometheus"
	"github.com/golang/snappy"
	"google.golang.org/protobuf/proto"
)

type PromRemoteWriter struct {
	remoteWriteURL       string
	buildMetricRequestFn func() *pb.WriteRequest
}

func NewPromRemoteWriter(writeURL string, buildMetricRequestFn func() *pb.WriteRequest) (*PromRemoteWriter, error) {
	wu, err := url.Parse(writeURL)
	if err != nil {
		return nil, fmt.Errorf("cannot parse writeURL=%q: %w", writeURL, err)
	}
	if wu.Scheme != "http" && wu.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme in writeURL=%q; expecting 'http' or 'https'", writeURL)
	}
	if wu.Host == "" {
		return nil, fmt.Errorf("missing host in writeURL=%q", writeURL)
	}
	return &PromRemoteWriter{
		remoteWriteURL:       writeURL,
		buildMetricRequestFn: buildMetricRequestFn,
	}, nil
}

func (pm *PromRemoteWriter) SendMetrics(ctx context.Context, _ string) error {
	request := pm.buildMetricRequestFn()
	if request == nil {
		return nil
	}
	marshal, err := proto.Marshal(request)
	if err != nil {
		return fmt.Errorf("unable to marshal metrics into Protobuf: %v", err)
	}

	encoded := snappy.Encode(nil, marshal)
	body := bytes.NewReader(encoded)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, pm.remoteWriteURL, body)
	if err != nil {
		return fmt.Errorf("unable to create HTTP request: %v", err)
	}

	httpReq.Header.Add("Content-Encoding", "snappy")
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")

	if _, err := http.DefaultClient.Do(httpReq); err != nil {
		return fmt.Errorf("unable to send metrics to Prometheus remote write destination: %v", err)
	}
	return nil
}
