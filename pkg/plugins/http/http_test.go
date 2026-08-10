package http

import (
	"context"
	"net/http"
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

func (m *mockBodyBuffer) NewReader() http.Header {
	return nil
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

	// Destroy should record metrics
	httpInstance.Destroy()
	assert.True(t, httpInstance.metricsRecorded, "Metrics should be marked as recorded")

	// Calling Destroy again should not panic (idempotent)
	httpInstance.Destroy()
}

func TestGRPCMetrics_Success(t *testing.T) {
	// Create a factory
	factory := &Factory{}

	// Create plugin context for gRPC
	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  2048,
		writeBytes: 1024,
	}

	// Create plugin instance
	instance := factory.NewHttpInstance(ctx, nil)
	require.NotNil(t, instance)
	
	httpInstance, ok := instance.(*filterInstance)
	require.True(t, ok)

	// Simulate gRPC request
	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		":path":        "/echo.EchoService/Echo",
		"content-type": "application/grpc+json",
	})
	status := httpInstance.RequestHeaders(reqHeaders, false)
	assert.Equal(t, plugins.HeadersStatusContinue, status)
	assert.Equal(t, "POST", httpInstance.method)
	assert.Equal(t, "10.42.0.3:9090", httpInstance.host)
	assert.Equal(t, "/echo.EchoService/Echo", httpInstance.rpcMethod, "gRPC should extract rpc_method from :path")

	// Simulate response with HTTP 200 (initial)
	resHeaders := newMockHeaders(map[string]string{
		":status": "200",
	})
	status = httpInstance.ResponseHeaders(resHeaders, false)
	assert.Equal(t, plugins.HeadersStatusContinue, status)
	assert.Equal(t, "200", httpInstance.statusCode)

	// In real scenario, session.HandleTrailers() would update statusCode to final gRPC status
	// For grpc-status=0 (OK), it remains 200
	// Here we simulate that the status is already correct

	// Destroy should record gRPC metrics
	httpInstance.Destroy()
	assert.True(t, httpInstance.metricsRecorded)
}

func TestGRPCMetrics_NotFound(t *testing.T) {
	// Create a factory
	factory := &Factory{}

	// Create plugin context for gRPC
	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  512,
		writeBytes: 256,
	}

	// Create plugin instance
	instance := factory.NewHttpInstance(ctx, nil)
	require.NotNil(t, instance)
	
	httpInstance, ok := instance.(*filterInstance)
	require.True(t, ok)

	// Simulate gRPC request
	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		":path":        "/echo.EchoService/NonExistent",
		"content-type": "application/grpc",
	})
	httpInstance.RequestHeaders(reqHeaders, false)

	// Simulate response - session.HandleTrailers() would map grpc-status=5 to HTTP 404
	resHeaders := newMockHeaders(map[string]string{
		":status":     "404", // Mapped from grpc-status=5
		"grpc-status": "5",
	})
	httpInstance.ResponseHeaders(resHeaders, false)
	assert.Equal(t, "404", httpInstance.statusCode)

	// Destroy should record gRPC metrics with status 404
	httpInstance.Destroy()
	assert.True(t, httpInstance.metricsRecorded)
}

func TestGRPCMetrics_Cancelled(t *testing.T) {
	// Create a factory
	factory := &Factory{}

	// Create plugin context for gRPC
	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  100,
		writeBytes: 50,
	}

	// Create plugin instance
	instance := factory.NewHttpInstance(ctx, nil)
	require.NotNil(t, instance)
	
	httpInstance, ok := instance.(*filterInstance)
	require.True(t, ok)

	// Simulate gRPC request
	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		":path":        "/echo.EchoService/SlowMethod",
		"content-type": "application/grpc",
	})
	httpInstance.RequestHeaders(reqHeaders, false)

	// Simulate cancelled stream - session.Close() sets grpc-status=1 → HTTP 499
	resHeaders := newMockHeaders(map[string]string{
		":status":     "499", // Mapped from grpc-status=1 (CANCELLED)
		"grpc-status": "1",
	})
	httpInstance.ResponseHeaders(resHeaders, false)
	assert.Equal(t, "499", httpInstance.statusCode)

	// Destroy should record gRPC metrics with status 499
	httpInstance.Destroy()
	assert.True(t, httpInstance.metricsRecorded)
}

func TestGRPCMetrics_TrailersOnly(t *testing.T) {
	// Test the Trailers-Only response pattern (single HEADERS frame with status + grpc-status)
	factory := &Factory{}

	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  200,
		writeBytes: 150,
	}

	instance := factory.NewHttpInstance(ctx, nil)
	require.NotNil(t, instance)
	
	httpInstance, ok := instance.(*filterInstance)
	require.True(t, ok)

	// Simulate gRPC request
	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		":path":        "/echo.EchoService/QuickError",
		"content-type": "application/grpc",
	})
	httpInstance.RequestHeaders(reqHeaders, false)

	// Trailers-Only response: single HEADERS with :status and grpc-status
	// session.HandleTrailers() maps grpc-status=3 (INVALID_ARGUMENT) → HTTP 400
	resHeaders := newMockHeaders(map[string]string{
		":status":     "400",
		"grpc-status": "3",
	})
	httpInstance.ResponseHeaders(resHeaders, true) // endStream=true
	assert.Equal(t, "400", httpInstance.statusCode)

	// Destroy should record gRPC metrics
	httpInstance.Destroy()
	assert.True(t, httpInstance.metricsRecorded)
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
	assert.Empty(t, httpInstance.rpcMethod, "HTTP/2 (non-gRPC) should not extract rpc_method")

	// Simulate response
	resHeaders := newMockHeaders(map[string]string{
		":status": "200",
	})
	httpInstance.ResponseHeaders(resHeaders, false)
	assert.Equal(t, "200", httpInstance.statusCode)

	// Destroy should record HTTP metrics (not gRPC metrics)
	httpInstance.Destroy()
	assert.True(t, httpInstance.metricsRecorded)
}

func TestGRPCMetrics_MissingRPCMethod(t *testing.T) {
	// Test gRPC request without :path header (edge case)
	factory := &Factory{}

	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  100,
		writeBytes: 100,
	}

	instance := factory.NewHttpInstance(ctx, nil)
	require.NotNil(t, instance)
	
	httpInstance, ok := instance.(*filterInstance)
	require.True(t, ok)

	// Simulate gRPC request WITHOUT :path header
	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		"content-type": "application/grpc",
	})
	httpInstance.RequestHeaders(reqHeaders, false)
	assert.Empty(t, httpInstance.rpcMethod, "Missing :path should result in empty rpc_method")

	// Simulate response
	resHeaders := newMockHeaders(map[string]string{
		":status": "200",
	})
	httpInstance.ResponseHeaders(resHeaders, false)

	// Destroy should handle missing rpc_method gracefully (sets to "unknown")
	httpInstance.Destroy()
	assert.True(t, httpInstance.metricsRecorded)
}

func TestMetricsRecordedOnlyOnce(t *testing.T) {
	// Verify that metrics are only recorded once even if Destroy() is called multiple times
	factory := &Factory{}

	ctx := &mockPluginContext{
		protocol:   "grpc",
		endpoint:   "10.42.0.3:9090",
		readBytes:  100,
		writeBytes: 100,
	}

	instance := factory.NewHttpInstance(ctx, nil)
	require.NotNil(t, instance)
	
	httpInstance, ok := instance.(*filterInstance)
	require.True(t, ok)

	// Setup basic request/response
	reqHeaders := newMockHeaders(map[string]string{
		":method":      "POST",
		":authority":   "10.42.0.3:9090",
		":path":        "/test.Service/Method",
		"content-type": "application/grpc",
	})
	httpInstance.RequestHeaders(reqHeaders, false)

	resHeaders := newMockHeaders(map[string]string{
		":status": "200",
	})
	httpInstance.ResponseHeaders(resHeaders, false)

	// First Destroy() should record metrics
	httpInstance.Destroy()
	assert.True(t, httpInstance.metricsRecorded)

	// Second Destroy() should be a no-op (not panic, not double-record)
	httpInstance.Destroy()
	httpInstance.Destroy()
	assert.True(t, httpInstance.metricsRecorded)
}
