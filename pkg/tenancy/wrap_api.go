package tenancy

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/prometheus/client_golang/api"
)

type TenantClient struct {
	api.Client
}

var epPrefix = []string{
	"/api/v1/query",
	"/api/v1/series",
	"/api/v1/label",
	"/api/v1/status",
	"/api/v1/export",
}

func (c *TenantClient) URL(ep string, args map[string]string) *url.URL {
	for _, prefix := range epPrefix {
		if strings.HasPrefix(ep, prefix) {
			ep = "/select/:accountID/prometheus" + ep // account will be saved util Do is called
		}
	}
	return c.Client.URL(ep, args)
}

func (c *TenantClient) Do(ctx context.Context, req *http.Request) (*http.Response, []byte, error) {
	accountID := GetAccountID(ctx)
	if len(accountID) > 0 {
		req.URL.Path = strings.Replace(req.URL.Path, ":accountID", accountID, 1)
	} else {
		// remove tenant select
		req.URL.Path = strings.Replace(req.URL.Path, "/select/:accountID/prometheus", "", 1)
	}
	return c.Client.Do(ctx, req)
}
