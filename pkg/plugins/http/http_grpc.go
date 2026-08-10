package http

import (
	"strconv"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"github.com/qpoint-io/qtap/pkg/services"
)

// grpcMetricsInstance records Prometheus metrics for gRPC RPCs using the
// qtap_http_grpc_* metric family. Metrics are emitted in Destroy() after
// grpc-status trailers are merged into the response header map.
type grpcMetricsInstance struct {
	filterInstance
	reqheaders plugins.Headers
	resheaders plugins.Headers
}

func (f *Factory) NewGrpcInstance(ctx plugins.PluginContext, _ *services.ServiceRegistry) plugins.GrpcPluginInstance {
	return &grpcMetricsInstance{
		filterInstance: filterInstance{
			logger: f.logger,
			ctx:    ctx,
			filter: f,
			prefix: f.prefix,
		},
	}
}

func (h *grpcMetricsInstance) RequestHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.reqheaders = headers
	return h.filterInstance.RequestHeaders(headers, endStream)
}

func (h *grpcMetricsInstance) ResponseHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.resheaders = headers
	h.responseStart = time.Now()

	if status, ok := headers.Get(":status"); ok {
		h.statusCode = status.String()
	} else {
		h.statusCode = "000"
	}

	return plugins.HeadersStatusContinue
}

func (h *grpcMetricsInstance) ResponseBody(_ plugins.BodyBuffer, _ bool) plugins.BodyStatus {
	return plugins.BodyStatusContinue
}

func (h *grpcMetricsInstance) Destroy() {
	if h.metricsRecorded {
		return
	}
	h.metricsRecorded = true

	h.applyGRPCStatusCode()

	if h.responseStart.IsZero() {
		h.responseStart = time.Now()
	}
	if h.requestStart.IsZero() {
		h.requestStart = h.responseStart
	}

	h.rpcMethod = h.grpcRPCMethod()
	h.recordGrpcMetrics()
}

func (h *grpcMetricsInstance) grpcRPCMethod() string {
	if h.reqheaders == nil {
		return "unknown"
	}

	service, method := tools.NewHeaderMap(h.reqheaders).GRPCServiceMethod()
	if service != "" && method != "" {
		return "/" + service + "/" + method
	}
	if method != "" {
		return method
	}
	if path, ok := h.reqheaders.Get(":path"); ok {
		return path.String()
	}
	return "unknown"
}

func (h *grpcMetricsInstance) applyGRPCStatusCode() {
	if h.resheaders == nil {
		return
	}

	if grpcStatus, ok := h.resheaders.Get("Grpc-Status"); ok && grpcStatus.String() != "" {
		h.statusCode = strconv.Itoa(grpcStatusToHTTP(grpcStatus.String()))
		return
	}

	if h.statusCode == "" {
		h.statusCode = "200"
	}
}

// grpcStatusToHTTP maps a gRPC status code string to an HTTP status code for metrics labels.
func grpcStatusToHTTP(grpcStatus string) int {
	switch grpcStatus {
	case "0":
		return 200
	case "1":
		return 499
	case "2":
		return 500
	case "3":
		return 400
	case "4":
		return 504
	case "5":
		return 404
	case "6":
		return 409
	case "7":
		return 403
	case "8":
		return 429
	case "9":
		return 400
	case "10":
		return 409
	case "11":
		return 400
	case "12":
		return 501
	case "13":
		return 500
	case "14":
		return 503
	case "15":
		return 500
	case "16":
		return 401
	default:
		return 500
	}
}
