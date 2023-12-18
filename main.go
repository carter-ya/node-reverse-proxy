package main

import (
	"context"
	"fmt"
	"github.com/spf13/pflag"
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
		writer.WriteHeader(http.StatusOK)
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
		node.ModifyResponse(response)
		return nil
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
