package http

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrpcMetricsInstanceRecordsOnDestroy(t *testing.T) {
	factory := &Factory{}
	ctx := &mockPluginContext{
		protocol:   "http2",
		endpoint:   "grpc-json-server.poc.svc.cluster.local:9090",
		readBytes:  222,
		writeBytes: 299,
	}

	inst := factory.NewGrpcInstance(ctx, nil).(*grpcMetricsInstance)

	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "grpc-json-server.poc.svc.cluster.local:9090",
		":path":        "/echo.EchoService/Echo",
		"content-type": "application/grpc+json",
	})
	require.Equal(t, plugins.HeadersStatusContinue, inst.RequestHeaders(reqHeaders, false))

	resHeaders := newMockHeaders(map[string]string{
		":status":     "200",
		"Grpc-Status": "0",
	})
	require.Equal(t, plugins.HeadersStatusContinue, inst.ResponseHeaders(resHeaders, false))

	before := testutil.ToFloat64(grpcRequestsTotal.WithLabelValues(
		"POST", "grpc-json-server.poc.svc.cluster.local:9090", "200", "/echo.EchoService/Echo",
	))

	inst.Destroy()

	after := testutil.ToFloat64(grpcRequestsTotal.WithLabelValues(
		"POST", "grpc-json-server.poc.svc.cluster.local:9090", "200", "/echo.EchoService/Echo",
	))
	assert.Equal(t, before+1, after)

	mfs, err := metrics.ProductRegistry().Gather()
	require.NoError(t, err)
	var found bool
	for _, mf := range mfs {
		if mf.GetName() == "qtap_http_grpc_requests_total" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected qtap_http_grpc_requests_total to be registered")

	inst.Destroy()
}

func TestGrpcMetricsInstanceMapsErrorStatus(t *testing.T) {
	factory := &Factory{}
	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  100,
		writeBytes: 100,
	}

	inst := factory.NewGrpcInstance(ctx, nil).(*grpcMetricsInstance)
	inst.requestStart = time.Now().Add(-10 * time.Millisecond)

	reqHeaders := newMockHeaders(map[string]string{
		":method":    "POST",
		":authority": "10.42.0.3:9090",
		":path":      "/echo.EchoService/Echo",
	})
	require.Equal(t, plugins.HeadersStatusContinue, inst.RequestHeaders(reqHeaders, false))

	resHeaders := newMockHeaders(map[string]string{
		":status":     "200",
		"Grpc-Status": "5",
	})
	require.Equal(t, plugins.HeadersStatusContinue, inst.ResponseHeaders(resHeaders, false))

	before := testutil.ToFloat64(grpcRequestsTotal.WithLabelValues(
		"POST", "10.42.0.3:9090", "404", "/echo.EchoService/Echo",
	))

	inst.Destroy()

	after := testutil.ToFloat64(grpcRequestsTotal.WithLabelValues(
		"POST", "10.42.0.3:9090", "404", "/echo.EchoService/Echo",
	))
	assert.Equal(t, before+1, after)
}
