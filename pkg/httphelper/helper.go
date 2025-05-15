package httphelper

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"time"
)

func CreateHttpClient(proxy bool, proxyAddress string) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 10,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	if proxy {
		proxyURL, err := url.Parse(proxyAddress)
		if err != nil {
			log.Printf("warning: no proxy will be used since cannot parse the proxy address %v", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	return &http.Client{
		Transport: transport,
	}
}
