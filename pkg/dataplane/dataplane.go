package dataplane

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type DataplaneClient struct {
	Address string
	client  *http.Client
}

const (
	TraceEndpoint  = "/datasource/queryTraces"
	MetricEndpoint = "/datasource/queryMetrics"
	LogEndpoint    = "/datasource/queryLogs"
)

func NewClient(address string) *DataplaneClient {
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}

	return &DataplaneClient{
		Address: address,
		client:  &http.Client{},
	}
}

func (c *DataplaneClient) post(endpoint string, req any) (*http.Response, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, c.Address+endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var bodyStr = ""
		if body, err := io.ReadAll(resp.Body); err == nil {
			bodyStr = string(body)
		}
		resp.Body.Close()
		return nil, fmt.Errorf("[%d] at %s ,resp body: %s", resp.StatusCode, endpoint, bodyStr)
	}
	return resp, nil
}
