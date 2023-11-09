package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
)

type ReverseProxyNode struct {
	URL   *url.URL
	Proxy *httputil.ReverseProxy

	Calls    uint64
	Calls2XX uint64
	Calls4XX uint64
	Calls5XX uint64
}

func (node *ReverseProxyNode) ModifyResponse(r *http.Response) {
	atomic.AddUint64(&node.Calls, 1)
	if r.StatusCode >= 200 && r.StatusCode < 300 {
		atomic.AddUint64(&node.Calls2XX, 1)
	} else if r.StatusCode >= 400 && r.StatusCode < 500 {
		atomic.AddUint64(&node.Calls4XX, 1)
	} else if r.StatusCode >= 500 && r.StatusCode < 600 {
		atomic.AddUint64(&node.Calls5XX, 1)
	}
}
