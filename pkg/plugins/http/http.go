package http

import (
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const (
	pluginTypeHTTPMetrics plugins.PluginType = "http_metrics"
)

type Factory struct {
	logger *zap.Logger
	prefix string
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger
}

func (f *Factory) NewHttpInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	return &filterInstance{
		logger: f.logger,
		ctx:    ctx,

		filter: f,
		prefix: f.prefix,
	}
}

func (f *Factory) Destroy() {}

type filterInstance struct {
	logger          *zap.Logger
	ctx             plugins.PluginContext
	filter          *Factory
	prefix          string
	requestStart    time.Time
	responseStart   time.Time
	method          string
	host            string
	statusCode      string
	rpcMethod       string // gRPC RPC method from :path header
	metricsRecorded bool   // prevent double-recording in Destroy()
}

func (h *filterInstance) RequestHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.requestStart = time.Now()

	// Extract HTTP method and host from headers
	if method, ok := headers.Get(":method"); ok {
		h.method = method.String()
	}
	if host, ok := headers.Get(":authority"); ok {
		h.host = host.String()
	}

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) RequestBody(body plugins.BodyBuffer, endStream bool) plugins.BodyStatus {
	return plugins.BodyStatusContinue
}

func (h *filterInstance) ResponseHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.responseStart = time.Now()

	// Extract status code from response headers
	if status, ok := headers.Get(":status"); ok {
		h.statusCode = status.String()
	} else {
		h.statusCode = "000"
	}

	h.recordRequestMetrics()

	if endStream {
		h.recordResponseMetrics()
		h.recordSizeMetrics()
	}

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) ResponseBody(body plugins.BodyBuffer, endStream bool) plugins.BodyStatus {
	if endStream {
		h.recordResponseMetrics()
		h.recordSizeMetrics()
	}
	return plugins.BodyStatusContinue
}

func (h *filterInstance) recordRequestMetrics() {
	protocol := h.ctx.Meta().Protocol()

	host := h.host
	if host == "" {
		host = h.ctx.Meta().Endpoint()
	}

	requestsTotal.WithLabelValues(h.method, host, h.statusCode, protocol).Inc()
	requestsDuration.WithLabelValues(h.method, host, h.statusCode, protocol).Observe(float64(h.responseStart.Sub(h.requestStart).Milliseconds()))
}

func (h *filterInstance) recordResponseMetrics() {
	protocol := h.ctx.Meta().Protocol()

	host := h.host
	if host == "" {
		host = h.ctx.Meta().Endpoint()
	}

	responsesTotal.WithLabelValues(h.method, host, h.statusCode, protocol).Inc()
	responsesDuration.WithLabelValues(h.method, host, h.statusCode, protocol).Observe(float64(time.Since(h.responseStart).Milliseconds()))

	// record the combined duration of the request and response
	duration.Observe(float64(time.Since(h.requestStart).Milliseconds()))
}

// recordGrpcMetrics records all gRPC metrics using the qtap_grpc_* metric family.
// This is called in Destroy() after gRPC trailers have been processed by session.HandleTrailers().
func (h *filterInstance) recordGrpcMetrics() {
	host := h.host
	if host == "" {
		host = h.ctx.Meta().Endpoint()
	}

	// For gRPC, we need to get the final status code from response headers after trailer processing
	// The session.HandleTrailers() updates the :status header with the mapped gRPC status
	if h.ctx != nil {
		// Try to get updated status from context if available
		// For now, use the statusCode we captured (which should be updated by session.HandleTrailers)
	}

	rpcMethod := h.rpcMethod
	if rpcMethod == "" {
		rpcMethod = "unknown"
	}

	// Record request metrics
	grpcRequestsTotal.WithLabelValues(h.method, host, h.statusCode, rpcMethod).Inc()
	if !h.responseStart.IsZero() {
		grpcRequestsDuration.WithLabelValues(h.method, host, h.statusCode, rpcMethod).Observe(float64(h.responseStart.Sub(h.requestStart).Milliseconds()))
	}
	grpcRequestsSize.WithLabelValues(h.method, host, h.statusCode, rpcMethod).Observe(float64(h.ctx.Meta().WriteBytes()))

	// Record response metrics
	grpcResponsesTotal.WithLabelValues(h.method, host, h.statusCode, rpcMethod).Inc()
	if !h.responseStart.IsZero() {
		grpcResponsesDuration.WithLabelValues(h.method, host, h.statusCode, rpcMethod).Observe(float64(time.Since(h.responseStart).Milliseconds()))
	}
	grpcResponsesSize.WithLabelValues(h.method, host, h.statusCode, rpcMethod).Observe(float64(h.ctx.Meta().ReadBytes()))

	// Record combined duration
	if !h.requestStart.IsZero() {
		grpcDuration.WithLabelValues(rpcMethod).Observe(float64(time.Since(h.requestStart).Milliseconds()))
	}
}

func (h *filterInstance) recordSizeMetrics() {
	protocol := h.ctx.Meta().Protocol()

	host := h.host
	if host == "" {
		host = h.ctx.Meta().Endpoint()
	}

	requestsSize.WithLabelValues(h.method, host, h.statusCode, protocol).Observe(float64(h.ctx.Meta().WriteBytes()))
	responsesSize.WithLabelValues(h.method, host, h.statusCode, protocol).Observe(float64(h.ctx.Meta().ReadBytes()))
}

func (h *filterInstance) Destroy() {
	// HTTP metrics are recorded in ResponseHeaders/ResponseBody. gRPC uses grpcMetricsInstance.
}

func (f *Factory) PluginType() plugins.PluginType {
	return pluginTypeHTTPMetrics
}
