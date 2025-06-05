package vm

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type VmPusher struct {
	pushURL            *url.URL
	method             string
	pushURLRedacted    string
	headers            http.Header
	disableCompression bool

	client           *http.Client
	collectMetricsFn func(accountID string, w io.Writer)
}

func NewVmPusher(pushURL string, collectMetricsFn func(accountID string, w io.Writer)) (*VmPusher, error) {
	// validate pushURL
	pu, err := url.Parse(pushURL)
	if err != nil {
		return nil, fmt.Errorf("cannot parse pushURL=%q: %w", pushURL, err)
	}
	if pu.Scheme != "http" && pu.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme in pushURL=%q; expecting 'http' or 'https'", pushURL)
	}
	if pu.Host == "" {
		return nil, fmt.Errorf("missing host in pushURL=%q", pushURL)
	}

	method := http.MethodGet
	// validate Headers
	headers := make(http.Header)
	pushURLRedacted := pu.Redacted()
	client := &http.Client{}
	return &VmPusher{
		pushURL:            pu,
		method:             method,
		pushURLRedacted:    pushURLRedacted,
		headers:            headers,
		disableCompression: false,
		client:             client,
		collectMetricsFn:   collectMetricsFn,
	}, nil
}

const (
	// prometheusRemoteWrite
	// https://<vminsert-addr>/insert/{TENANT_ID}/prometheus/api/v1/import/prometheus
	TenantIDPlaceholder = "{TENANT_ID}"
)

func (pc *VmPusher) SendMetrics(ctx context.Context, accountID string) error {
	bb := getBytesBuffer()
	defer putBytesBuffer(bb)

	pc.collectMetricsFn(accountID, bb)

	if !pc.disableCompression {
		bbTmp := getBytesBuffer()
		bbTmp.B = append(bbTmp.B[:0], bb.B...)
		bb.B = bb.B[:0]
		zw := getGzipWriter(bb)
		if _, err := zw.Write(bbTmp.B); err != nil {
			return fmt.Errorf("cannot write %d bytes to gzip writer: %s", len(bbTmp.B), err)
		}
		if err := zw.Close(); err != nil {
			return fmt.Errorf("cannot flush metrics to gzip writer: %s", err)
		}
		putGzipWriter(zw)
		putBytesBuffer(bbTmp)
	}

	// Prepare the request to sent to pc.pushURL
	reqBody := bytes.NewReader(bb.B)

	var url = pc.pushURL.String()
	if len(accountID) > 0 {
		url = strings.ReplaceAll(url, TenantIDPlaceholder, accountID)
	}

	req, err := http.NewRequestWithContext(ctx, pc.method, url, reqBody)
	if err != nil {
		return fmt.Errorf("metrics.push: cannot initialize request for metrics push to %q: %w", pc.pushURLRedacted, err)
	}

	req.Header.Set("Content-Type", "text/plain")
	// Set the needed headers, and `Content-Type` allowed be overwrited.
	for name, values := range pc.headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	if !pc.disableCompression {
		req.Header.Set("Content-Encoding", "gzip")
	}

	// Perform the request
	resp, err := pc.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("cannot push metrics to %q: %s", pc.pushURLRedacted, err)
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return fmt.Errorf("unexpected status code in response from %q: %d; expecting 2xx; response body: %q", pc.pushURLRedacted, resp.StatusCode, body)
	}
	_ = resp.Body.Close()
	return nil
}

func getBytesBuffer() *bytesBuffer {
	v := bytesBufferPool.Get()
	if v == nil {
		return &bytesBuffer{}
	}
	return v.(*bytesBuffer)
}

func putBytesBuffer(bb *bytesBuffer) {
	bb.B = bb.B[:0]
	bytesBufferPool.Put(bb)
}

var bytesBufferPool sync.Pool

type bytesBuffer struct {
	B []byte
}

func (bb *bytesBuffer) Write(p []byte) (int, error) {
	bb.B = append(bb.B, p...)
	return len(p), nil
}

func getGzipWriter(w io.Writer) *gzip.Writer {
	v := gzipWriterPool.Get()
	if v == nil {
		return gzip.NewWriter(w)
	}
	zw := v.(*gzip.Writer)
	zw.Reset(w)
	return zw
}

func putGzipWriter(zw *gzip.Writer) {
	zw.Reset(io.Discard)
	gzipWriterPool.Put(zw)
}

var gzipWriterPool sync.Pool
