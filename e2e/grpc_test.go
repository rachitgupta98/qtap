//go:build e2e

package e2e

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	e2epkg "github.com/qpoint-io/qtap/pkg/e2e"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func TestGRPCLanguages(t *testing.T) {
	configMut := func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS
	}

	suite, err := e2epkg.NewGRPCTestSuite("gRPC Echo").
		WithConfig(configMut).
		WithOS("alpine").
		WithLanguage(e2epkg.Go, "1.25.1").
		WithLanguage(e2epkg.Java, "21").
		WithLanguage(e2epkg.Python, "3.12.0").
		WithLanguage(e2epkg.NodeJS, "22.16.0").
		WithLanguage(e2epkg.Ruby, "3.4.5").
		WithLanguage(e2epkg.PHP, "8.3").
		WithMessage(`{"message":"hello"}`).
		WithPlaintextOnly().
		WithValidation(func(t *testing.T, ctx e2epkg.ValidationContext) error {
			tc := ctx.GRPCTestCase

			// Wait for gRPC request to appear in the event store.
			// The babel images use the proto service babel.v1.EchoService
			// with method UnaryEcho on the wire.
			var grpcReq *eventstore.GrpcRequest
			require.Eventually(t, func() bool {
				events := e2ectx.EventStore.GetByCtxID(ctx.TestContext.ID)
				for _, r := range events.GrpcRequests {
					if r.GrpcService == "babel.v1.EchoService" && r.GrpcMethod == "UnaryEcho" {
						grpcReq = r
						return true
					}
				}
				return false
			}, 10*time.Second, 100*time.Millisecond,
				"no gRPC babel.v1.EchoService/UnaryEcho request for %s:%s", tc.Language, tc.Version)

			// Verify gRPC metadata
			assert.Equal(t, "babel.v1.EchoService", grpcReq.GrpcService)
			assert.Equal(t, "UnaryEcho", grpcReq.GrpcMethod)
			assert.Equal(t, "0", grpcReq.GrpcStatus)
			assert.Equal(t, "OK", grpcReq.GrpcStatusName)
			assert.Equal(t, http.StatusOK, grpcReq.Status)

			// Verify qtap observed content (bytes flowing through the wire)
			assert.Equal(t, "/babel.v1.EchoService/UnaryEcho", grpcReq.URLPath)
			assert.Equal(t, "application/grpc", grpcReq.ContentType)
			assert.Greater(t, grpcReq.WrBytes, int64(0), "expected client to send request bytes")
			assert.Greater(t, grpcReq.RdBytes, int64(0), "expected client to receive response bytes")
			assert.Greater(t, grpcReq.Duration, int64(0), "expected positive request duration")

			return nil
		}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, suite)

	runner := &e2epkg.GRPCTestSuiteRunner{
		Suite:  suite,
		Logger: e2ectx.L,
	}
	runner.Run(t, e2ectx)
}

func TestGRPCLocal(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	// Start in-process gRPC server with the standard health check service
	lis, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	port := lis.Addr().(*net.TCPAddr).Port

	grpcServer := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, health.NewServer())
	reflection.Register(grpcServer) // needed so grpcurl can resolve the service
	go grpcServer.Serve(lis)        //nolint:errcheck
	defer grpcServer.Stop()

	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS
	}, func(t *testing.T) {
		target := fmt.Sprintf("%s:%d", ctx.MachineIP().String(), port)
		ctxID := e2epkg.NewID()

		// Run grpcurl container as gRPC client
		container, err := testcontainers.Run(
			ctx,
			"fullstorydev/grpcurl:latest",
			testcontainers.WithCmd("-plaintext", target, "grpc.health.v1.Health/Check"),
			testcontainers.WithEnv(map[string]string{
				"QPOINT_TAGS": "ctxid:" + ctxID,
			}),
			testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(10*time.Second)),
		)
		t.Cleanup(func() { testcontainers.TerminateContainer(container) })
		require.NoError(t, err)

		// grpcurl probes gRPC reflection before the real call, so multiple
		// connections to port will appear. Wait for one with L7Protocol_GRPC.
		var grpcConn *eventstore.Connection
		require.Eventually(t, func() bool {
			for _, c := range e2ectx.EventStore.GetByCtxID(ctxID).Connections {
				if c.L7Protocol != eventstore.L7Protocol_GRPC {
					continue
				}
				if dst, ok := c.Destination.(*eventstore.ConnectionEndpointRemote); ok {
					if int(dst.Address.Port) == port {
						grpcConn = c
						return true
					}
				}
			}
			return false
		}, 10*time.Second, 100*time.Millisecond, "no gRPC connection to server port %d", port)
		assert.Equal(t, eventstore.L7Protocol_GRPC, grpcConn.L7Protocol)

		// The Health/Check request may arrive after the reflection connection
		// closes, so poll until it appears in GrpcRequests.
		var grpcReq *eventstore.GrpcRequest
		require.Eventually(t, func() bool {
			for _, r := range e2ectx.EventStore.GetByCtxID(ctxID).GrpcRequests {
				if r.URLPath == "/grpc.health.v1.Health/Check" {
					grpcReq = r
					return true
				}
			}
			return false
		}, 10*time.Second, 100*time.Millisecond, "no gRPC Health/Check request in GrpcRequests")

		assert.Equal(t, "/grpc.health.v1.Health/Check", grpcReq.URLPath)
		assert.Equal(t, http.StatusOK, grpcReq.Status)
		assert.Equal(t, "grpc.health.v1.Health", grpcReq.GrpcService)
		assert.Equal(t, "Check", grpcReq.GrpcMethod)
		assert.Equal(t, "0", grpcReq.GrpcStatus)
		assert.Equal(t, "OK", grpcReq.GrpcStatusName)
	})
}

func TestGRPCPrometheusMetrics(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	// Start in-process gRPC server with the standard health check service
	lis, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	port := lis.Addr().(*net.TCPAddr).Port

	grpcServer := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, health.NewServer())
	reflection.Register(grpcServer)
	go grpcServer.Serve(lis) //nolint:errcheck
	defer grpcServer.Stop()

	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS

		// Enable http_metrics plugin
		c.Stacks = append(c.Stacks, config.Stack{
			Name: "test_metrics",
			Plugins: []config.Plugin{
				{
					Type: "http_metrics",
				},
			},
		})
	}, func(t *testing.T) {
		target := fmt.Sprintf("%s:%d", ctx.MachineIP().String(), port)
		ctxID := e2epkg.NewID()

		// Run grpcurl container as gRPC client
		container, err := testcontainers.Run(
			ctx,
			"fullstorydev/grpcurl:latest",
			testcontainers.WithCmd("-plaintext", target, "grpc.health.v1.Health/Check"),
			testcontainers.WithEnv(map[string]string{
				"QPOINT_TAGS": "ctxid:" + ctxID,
			}),
			testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(10*time.Second)),
		)
		t.Cleanup(func() { testcontainers.TerminateContainer(container) })
		require.NoError(t, err)

		// Wait for the gRPC request to complete
		var grpcReq *eventstore.GrpcRequest
		require.Eventually(t, func() bool {
			for _, r := range e2ectx.EventStore.GetByCtxID(ctxID).GrpcRequests {
				if r.URLPath == "/grpc.health.v1.Health/Check" {
					grpcReq = r
					return true
				}
			}
			return false
		}, 10*time.Second, 100*time.Millisecond, "no gRPC Health/Check request in GrpcRequests")

		assert.Equal(t, "/grpc.health.v1.Health/Check", grpcReq.URLPath)
		assert.Equal(t, http.StatusOK, grpcReq.Status)
		assert.Equal(t, "0", grpcReq.GrpcStatus)

		// Now verify that Prometheus metrics are exposed
		// Query the metrics endpoint
		time.Sleep(1 * time.Second) // Give metrics time to be recorded

		metricsURL := fmt.Sprintf("http://%s:9091/metrics", ctx.MachineIP().String())
		resp, err := http.Get(metricsURL)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		// Read the metrics response
		buf := make([]byte, 100*1024) // 100KB buffer
		n, _ := resp.Body.Read(buf)
		metricsOutput := string(buf[:n])

		// Verify gRPC-specific metrics are present
		assert.Contains(t, metricsOutput, "qtap_grpc_requests_total",
			"Expected qtap_grpc_requests_total metric to be present")
		assert.Contains(t, metricsOutput, "qtap_grpc_responses_total",
			"Expected qtap_grpc_responses_total metric to be present")
		assert.Contains(t, metricsOutput, "qtap_grpc_duration_ms",
			"Expected qtap_grpc_duration_ms metric to be present")

		// Verify rpc_method label is present with the correct value
		assert.Contains(t, metricsOutput, `rpc_method="/grpc.health.v1.Health/Check"`,
			"Expected rpc_method label with correct RPC path")

		// Verify status_code label shows 200 (mapped from grpc-status=0)
		assert.Contains(t, metricsOutput, `status_code="200"`,
			"Expected status_code=200 for successful gRPC call")

		// Verify HTTP metrics still exist (for backwards compatibility)
		assert.Contains(t, metricsOutput, "qtap_http_requests_total",
			"Expected qtap_http_requests_total metric to still be present for HTTP/1 and HTTP/2")

		t.Logf("✅ gRPC Prometheus metrics verified successfully")
	})
}

