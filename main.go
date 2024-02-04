package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/spf13/pflag"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"
)

var (
	host = pflag.String("server.host", "0.0.0.0", "http host to listen on")
	port = pflag.Int("server.port", 8080, "http port to listen on")

	nodes = pflag.StringArray("reverse.nodes", []string{
		"https://eth.llamarpc.com",
		"https://ethereum.publicnode.com",
		"https://rpc.flashbots.net/fast",
		"https://rpc.flashbots.net",
		"https://1rpc.io/eth",
		"https://rpc.ankr.com/eth",
		"https://rpc.eth.gateway.fm",
	}, "ethereum nodes to reverse proxy to")

	nodeHealthProxy = pflag.Bool("node-health-proxy", false, "enable health check proxy, if enabled, the `reverse.nodes` MUST only one node, it will proxy `eth_syncing`")

	printMetricsInterval = pflag.Duration("metrics.interval", time.Minute*5, "print metrics interval, set to 0 to disable")
	debug                = pflag.Bool("debug", false, "debug mode")
)

var (
	logger Logger

	ReverseProxyNodes []*ReverseProxyNode
	nodesIndex        = &atomic.Uint64{}
)

func main() {
	pflag.Parse()

	// setup logger
	logger = NewConsoleLogger(*debug)

	// setup reverse proxy nodes
	if len(*nodes) == 0 {
		logger.Fatalf("No nodes specified")
	}

	if *nodeHealthProxy && len(*nodes) > 1 {
		logger.Fatalf("node health proxy can only have one node")
	}

	ReverseProxyNodes = make([]*ReverseProxyNode, len(*nodes))
	for i, node := range *nodes {
		nodeURL, err := url.Parse(node)
		if err != nil {
			logger.Fatalf("Can't parse node url: %s", node)
		}
		ReverseProxyNodes[i] = buildNode(nodeURL)
	}

	// start http server

	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		node := nextNode()
		node.Proxy.ServeHTTP(writer, request)
	})
	http.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		if *nodeHealthProxy {
			node := ReverseProxyNodes[0]
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, "POST", node.URL.String(), strings.NewReader(`
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "eth_syncing"
}
`))
			if err != nil {
				writer.WriteHeader(http.StatusInternalServerError)
				logger.Errorf("create health check request error: %s", err)
				return
			}
			req.Header.Set("Content-Type", "application/json;charset=utf8")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				writer.WriteHeader(http.StatusServiceUnavailable)
				logger.Errorf("health check request error: %s", err)
				return
			}
			defer resp.Body.Close()

			b, err := io.ReadAll(resp.Body)
			if err != nil {
				writer.WriteHeader(http.StatusInternalServerError)
				logger.Errorf("read health check response error: %s", err)
				return
			}
			jsonResp := make(map[string]any)
			decoder := json.NewDecoder(bytes.NewReader(b))
			decoder.UseNumber()
			if err := decoder.Decode(&jsonResp); err != nil {
				writer.WriteHeader(http.StatusInternalServerError)
				logger.Errorf("decode health check response error: %s", err)
				return
			}
			result := jsonResp["result"]
			if boolResult, ok := result.(bool); ok {
				if boolResult {
					writer.WriteHeader(http.StatusInternalServerError)
					logger.Errorf("unexpected health check result: %v", result)
				} else {
					writer.WriteHeader(http.StatusOK)
				}
			} else {
				writer.Header().Set("Content-Type", "application/json;charset=utf8")
				writer.WriteHeader(http.StatusBadGateway)
				_, _ = writer.Write(b)
				logger.Infof("health check result: %v", result)
			}
		} else {
			writer.WriteHeader(http.StatusOK)
		}
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() {
		err := http.ListenAndServe(fmt.Sprintf("%s:%d", *host, *port), nil)
		if err != nil {
			logger.Fatalf("http server error: %s", err)
		}
	}()

	// start print metrics
	go runPrintNodeMetrics(ctx)

	logger.Infof("proxy server started at http://%s:%d", *host, *port)
	<-ctx.Done()
	logger.Debugf("proxy server stopped")
	cancel()
}

func buildNode(target *url.URL) *ReverseProxyNode {
	proxy := httputil.NewSingleHostReverseProxy(target)

	originDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originDirector(req)

		// remove trailing slash
		if strings.HasSuffix(req.URL.Path, "/") {
			req.URL.Path = req.URL.Path[:len(req.URL.Path)-1]
		}

		// disable set x-forwarded-for
		req.Header["X-Forwarded-For"] = nil
		req.Host = target.Host
	}

	node := &ReverseProxyNode{
		URL:   target,
		Proxy: proxy,
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		return node.ModifyResponse(response)
	}

	return node
}

func nextNode() *ReverseProxyNode {
	nextIndex := nodesIndex.Add(1)
	nextIndex = nextIndex % uint64(len(ReverseProxyNodes))
	logger.Debugf("round robin: next %d", nextIndex)
	return ReverseProxyNodes[nextIndex]
}

func runPrintNodeMetrics(ctx context.Context) {
	interval := *printMetricsInterval
	if interval == 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logger.Infof("==============================metrics start==============================")
			for _, node := range ReverseProxyNodes {
				logger.Infof("node %s: calls %d, 2xx %d, 4xx %d, 5xx %d",
					node.URL,
					atomic.LoadUint64(&node.Calls), atomic.LoadUint64(&node.Calls2XX),
					atomic.LoadUint64(&node.Calls4XX), atomic.LoadUint64(&node.Calls5XX),
				)
			}
		case <-ctx.Done():
			return
		}
	}
}
