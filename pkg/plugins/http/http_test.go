package http

import (
	"context"
	"io"
<<<<<<< Updated upstream
	"strings"
=======
>>>>>>> Stashed changes
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/connmeta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPluginContext implements plugins.PluginContext for testing
type mockPluginContext struct {
	protocol   string
	endpoint   string
	readBytes  int64
	writeBytes int64
}

func (m *mockPluginContext) GetRequestBodyBuffer() plugins.BodyBuffer {
	return nil
}

func (m *mockPluginContext) GetResponseBodyBuffer() plugins.BodyBuffer {
	return nil
}

func (m *mockPluginContext) Context() context.Context {
	return context.Background()
}

func (m *mockPluginContext) Meta() plugins.Meta {
	return &mockMeta{
		protocol:   m.protocol,
		endpoint:   m.endpoint,
		readBytes:  m.readBytes,
		writeBytes: m.writeBytes,
	}
}

// mockMeta implements plugins.Meta for testing
type mockMeta struct {
	connmeta.Service
	protocol   string
	endpoint   string
	readBytes  int64
	writeBytes int64
	requestID  string
}

func (m *mockMeta) Protocol() string {
	return m.protocol
}

func (m *mockMeta) Endpoint() string {
	return m.endpoint
}

func (m *mockMeta) ReadBytes() int64 {
	return m.readBytes
}

func (m *mockMeta) WriteBytes() int64 {
	return m.writeBytes
}

func (m *mockMeta) SetReadBytes(bytes int64) {
	m.readBytes = bytes
}

func (m *mockMeta) SetWriteBytes(bytes int64) {
	m.writeBytes = bytes
}

func (m *mockMeta) RequestID() string {
	return m.requestID
}

// mockHeaders implements plugins.Headers for testing
type mockHeaders struct {
	headers map[string]string
}

func newMockHeaders(headers map[string]string) *mockHeaders {
	return &mockHeaders{headers: headers}
}

func (m *mockHeaders) Get(key string) (plugins.HeaderValue, bool) {
	val, ok := m.headers[key]
	if !ok {
		return nil, false
	}
	return &mockHeaderValue{value: val}, true
}

func (m *mockHeaders) Values(key string, iter func(value plugins.HeaderValue)) {
	if val, ok := m.headers[key]; ok {
		iter(&mockHeaderValue{value: val})
	}
}

func (m *mockHeaders) Set(key, value string) {
	m.headers[key] = value
}

func (m *mockHeaders) Remove(key string) {
	delete(m.headers, key)
}

func (m *mockHeaders) All() map[string]string {
	return m.headers
}

// mockHeaderValue implements plugins.HeaderValue for testing
type mockHeaderValue struct {
	value string
}

func (m *mockHeaderValue) String() string {
	return m.value
}

func (m *mockHeaderValue) Bytes() []byte {
	return []byte(m.value)
}

func (m *mockHeaderValue) Equal(str string) bool {
	return m.value == str
}

// mockBodyBuffer implements plugins.BodyBuffer for testing
type mockBodyBuffer struct{}

func (m *mockBodyBuffer) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, nil
}

func (m *mockBodyBuffer) Length() int {
	return 0
}

func (m *mockBodyBuffer) Slices(iter func(view []byte)) {}

func (m *mockBodyBuffer) Copy() []byte {
	return nil
}

func (m *mockBodyBuffer) NewReader() io.Reader {
<<<<<<< Updated upstream
	return strings.NewReader("")
=======
	return nil
>>>>>>> Stashed changes
}

func TestHTTP1Metrics(t *testing.T) {
	// Create a factory
	factory := &Factory{}

	// Create plugin context for HTTP/1
	ctx := &mockPluginContext{
		protocol:   "http1",
		endpoint:   "example.com:80",
		readBytes:  1024,
		writeBytes: 512,
	}

	// Create plugin instance
	instance := factory.NewHttpInstance(ctx, nil)
	require.NotNil(t, instance)
	
	httpInstance, ok := instance.(*filterInstance)
	require.True(t, ok)

	// Simulate request
	reqHeaders := newMockHeaders(map[string]string{
		":method":    "GET",
		":authority": "example.com",
		":path":      "/api/users",
	})
	status := httpInstance.RequestHeaders(reqHeaders, false)
	assert.Equal(t, plugins.HeadersStatusContinue, status)
	assert.Equal(t, "GET", httpInstance.method)
	assert.Equal(t, "example.com", httpInstance.host)
	assert.Empty(t, httpInstance.rpcMethod, "HTTP/1 should not have rpc_method")

	// Simulate a small delay
	time.Sleep(10 * time.Millisecond)

	// Simulate response
	resHeaders := newMockHeaders(map[string]string{
		":status": "200",
	})
	status = httpInstance.ResponseHeaders(resHeaders, false)
	assert.Equal(t, plugins.HeadersStatusContinue, status)
	assert.Equal(t, "200", httpInstance.statusCode)

	// Simulate response body
	bodyStatus := httpInstance.ResponseBody(&mockBodyBuffer{}, true)
	assert.Equal(t, plugins.BodyStatusContinue, bodyStatus)
}

func TestGRPCMetrics_Success(t *testing.T) {
	factory := &Factory{}
	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  2048,
		writeBytes: 1024,
	}

	inst := factory.NewGrpcInstance(ctx, nil).(*grpcMetricsInstance)

	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		":path":        "/echo.EchoService/Echo",
		"content-type": "application/grpc+json",
	})
	status := inst.RequestHeaders(reqHeaders, false)
	assert.Equal(t, plugins.HeadersStatusContinue, status)
	assert.Equal(t, "POST", inst.method)
	assert.Equal(t, "10.42.0.3:9090", inst.host)

	resHeaders := newMockHeaders(map[string]string{
		":status":     "200",
		"Grpc-Status": "0",
	})
	status = inst.ResponseHeaders(resHeaders, false)
	assert.Equal(t, plugins.HeadersStatusContinue, status)
	assert.Equal(t, "200", inst.statusCode)

	inst.Destroy()
	assert.True(t, inst.metricsRecorded)
	assert.Equal(t, "/echo.EchoService/Echo", inst.rpcMethod)
}

func TestGRPCMetrics_NotFound(t *testing.T) {
	factory := &Factory{}
	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  512,
		writeBytes: 256,
	}

	inst := factory.NewGrpcInstance(ctx, nil).(*grpcMetricsInstance)

	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		":path":        "/echo.EchoService/NonExistent",
		"content-type": "application/grpc",
	})
	inst.RequestHeaders(reqHeaders, false)

	resHeaders := newMockHeaders(map[string]string{
		":status":     "404",
		"Grpc-Status": "5",
	})
	inst.ResponseHeaders(resHeaders, false)

	inst.Destroy()
	assert.Equal(t, "404", inst.statusCode)
	assert.True(t, inst.metricsRecorded)
}

func TestGRPCMetrics_Cancelled(t *testing.T) {
	factory := &Factory{}
	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  100,
		writeBytes: 50,
	}

	inst := factory.NewGrpcInstance(ctx, nil).(*grpcMetricsInstance)

	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		":path":        "/echo.EchoService/SlowMethod",
		"content-type": "application/grpc",
	})
	inst.RequestHeaders(reqHeaders, false)

	resHeaders := newMockHeaders(map[string]string{
		":status":     "499",
		"Grpc-Status": "1",
	})
	inst.ResponseHeaders(resHeaders, false)

	inst.Destroy()
	assert.Equal(t, "499", inst.statusCode)
	assert.True(t, inst.metricsRecorded)
}

func TestGRPCMetrics_TrailersOnly(t *testing.T) {
	factory := &Factory{}
	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  200,
		writeBytes: 150,
	}

	inst := factory.NewGrpcInstance(ctx, nil).(*grpcMetricsInstance)

	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		":path":        "/echo.EchoService/QuickError",
		"content-type": "application/grpc",
	})
	inst.RequestHeaders(reqHeaders, false)

	resHeaders := newMockHeaders(map[string]string{
		":status":     "400",
		"Grpc-Status": "3",
	})
	inst.ResponseHeaders(resHeaders, true)

	inst.Destroy()
	assert.Equal(t, "400", inst.statusCode)
	assert.True(t, inst.metricsRecorded)
}

func TestHTTP2Metrics(t *testing.T) {
	// Test plain HTTP/2 (not gRPC) - should use HTTP metrics, not gRPC metrics
	factory := &Factory{}

	ctx := &mockPluginContext{
		protocol:   "http2",
		endpoint:   "example.com:443",
		readBytes:  1500,
		writeBytes: 800,
	}

	instance := factory.NewHttpInstance(ctx, nil)
	require.NotNil(t, instance)
	
	httpInstance, ok := instance.(*filterInstance)
	require.True(t, ok)

	// Simulate HTTP/2 request
	reqHeaders := newMockHeaders(map[string]string{
		":method":    "GET",
		":authority": "example.com",
		":path":      "/api/data",
		":scheme":    "https",
	})
	status := httpInstance.RequestHeaders(reqHeaders, false)
	assert.Equal(t, plugins.HeadersStatusContinue, status)
	assert.Equal(t, "GET", httpInstance.method)
	assert.Empty(t, httpInstance.rpcMethod)

	// Simulate response
	resHeaders := newMockHeaders(map[string]string{
		":status": "200",
	})
	httpInstance.ResponseHeaders(resHeaders, false)
	assert.Equal(t, "200", httpInstance.statusCode)
}

func TestGRPCMetrics_MissingRPCMethod(t *testing.T) {
	factory := &Factory{}
	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  100,
		writeBytes: 100,
	}

	inst := factory.NewGrpcInstance(ctx, nil).(*grpcMetricsInstance)

	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		"content-type": "application/grpc",
	})
	inst.RequestHeaders(reqHeaders, false)

	resHeaders := newMockHeaders(map[string]string{
		":status":     "200",
		"Grpc-Status": "0",
	})
	inst.ResponseHeaders(resHeaders, false)

	inst.Destroy()
	assert.Equal(t, "unknown", inst.rpcMethod)
	assert.True(t, inst.metricsRecorded)
}

func TestMetricsRecordedOnlyOnce(t *testing.T) {
	factory := &Factory{}
	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  100,
		writeBytes: 100,
	}

	inst := factory.NewGrpcInstance(ctx, nil).(*grpcMetricsInstance)

	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		":path":        "/test.Service/Method",
		"content-type": "application/grpc",
	})
	inst.RequestHeaders(reqHeaders, false)

	resHeaders := newMockHeaders(map[string]string{
		":status":     "200",
		"Grpc-Status": "0",
	})
	inst.ResponseHeaders(resHeaders, false)

	inst.Destroy()
	inst.Destroy()
	inst.Destroy()
	assert.True(t, inst.metricsRecorded)
}
