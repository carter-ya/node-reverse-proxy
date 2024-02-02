package main

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"github.com/antchfx/jsonquery"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
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

func (node *ReverseProxyNode) ModifyResponse(r *http.Response) error {
	atomic.AddUint64(&node.Calls, 1)
	statusCode := r.StatusCode
	defer func() {
		if statusCode >= 200 && statusCode < 300 {
			atomic.AddUint64(&node.Calls2XX, 1)
		} else if statusCode >= 400 && statusCode < 500 {
			atomic.AddUint64(&node.Calls4XX, 1)
		} else if statusCode >= 500 && statusCode < 600 {
			atomic.AddUint64(&node.Calls5XX, 1)
		}
	}()

	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	ub, err := tryDecompressResponse(r, b)
	if err == nil {
		doc, err := jsonquery.Parse(bytes.NewReader(ub))
		if err == nil {
			errorNode := jsonquery.FindOne(doc, "//error")
			if errorNode != nil {
				logger.Warnf("detect error from node: %s, content: %s", node.URL, errorNode.Value())
				statusCode = 429 // if the response is invalid, force to return 429
			}
		} else {
			logger.Warnf("parse response from node %s error: %s", node.URL, err)
		}
	}

	body := io.NopCloser(bytes.NewReader(b))
	r.Body = body
	r.ContentLength = int64(len(b))
	r.Header.Set("Content-Length", strconv.Itoa(len(b)))
	return nil
}

func tryDecompressResponse(r *http.Response, b []byte) ([]byte, error) {
	if r.Header.Get("Content-Encoding") == "gzip" {
		ub, err := gzip.NewReader(io.NopCloser(bytes.NewReader(b)))
		if err != nil {
			return b, nil
		}
		return io.ReadAll(ub)
	} else if r.Header.Get("Content-Encoding") == "deflate" {
		ub := flate.NewReader(io.NopCloser(bytes.NewReader(b)))
		return io.ReadAll(ub)
	}
	return b, nil
}
