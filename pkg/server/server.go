package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	ext_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/day0ops/ext-proc-routing-decision/pkg/processor"
	"github.com/day0ops/ext-proc-routing-decision/test/mock"
)

const (
	defaultGrpcNetwork          = "tcp"
	defaultGrpcAddress          = ":8081"
	defaultHTTPBindAddr         = ":8080"
	defaultMaxConcurrentStreams = 1000
	defaultShutdownWait         = 5 * time.Second
)

type Server struct {
	grpcServer  *grpc.Server
	grpcNetwork string
	grpcAddress string
	mockBackend mockHttpBackend
	ctx         context.Context
	log         *zap.Logger
}

type mockHttpBackend struct {
	enabled     bool
	bindAddress string
	mux         *http.ServeMux
	httpsrv     *http.Server
}

type HealthServer struct {
	Log *zap.Logger
}

type Option func(*Server)

func New(ctx context.Context, log *zap.Logger, opts ...Option) *Server {
	srv := &Server{
		ctx: ctx,
		log: log,
	}

	for _, opt := range opts {
		opt(srv)
	}

	if srv.grpcNetwork == "" {
		srv.grpcNetwork = defaultGrpcNetwork
	}
	if srv.grpcAddress == "" {
		srv.grpcAddress = defaultGrpcAddress
	}
	if srv.grpcServer == nil {
		sopts := []grpc.ServerOption{grpc.MaxConcurrentStreams(defaultMaxConcurrentStreams)}
		srv.grpcServer = grpc.NewServer(sopts...)
	}

	if srv.mockBackend.enabled {
		if srv.mockBackend.mux == nil {
			srv.mockBackend.mux = http.NewServeMux()
		}
		if srv.mockBackend.bindAddress == "" {
			srv.mockBackend.bindAddress = defaultHTTPBindAddr
		}

		srv.mockBackend.mux.HandleFunc("/headers", mock.RequestHeaders)
		srv.mockBackend.mux.HandleFunc("/response-headers", mock.ResponseHeaders)
		srv.mockBackend.httpsrv = &http.Server{
			Addr: srv.mockBackend.bindAddress,
		}
		srv.mockBackend.httpsrv.Handler = srv.mockBackend.mux

	}
	return srv
}

func (s *Server) Serve() error {
	if s.ctx == nil {
		s.ctx = context.TODO()
	}

	errCh := make(chan error, 1)
	if s.mockBackend.enabled {
		go func() {
			s.log.Info("starting mock http server", zap.String("address", s.mockBackend.bindAddress))
			errCh <- s.mockBackend.httpsrv.ListenAndServe()
		}()
	}

	go func() {
		if s.grpcNetwork == "unix" {
			os.RemoveAll(s.grpcAddress) // nolint:errcheck
		}
		listener, err := net.Listen(s.grpcNetwork, s.grpcAddress)
		if err != nil {
			errCh <- fmt.Errorf("cannot listen: %w", err)
			return
		}
		extProcProcessor := processor.New(s.log)
		ext_proc_v3.RegisterExternalProcessorServer(s.grpcServer, extProcProcessor)
		grpc_health_v1.RegisterHealthServer(s.grpcServer, &processor.HealthServer{Log: s.log})
		s.log.Info("starting ext proc grpc server", zap.String("address", s.grpcAddress))
		errCh <- s.grpcServer.Serve(listener)
	}()

	select {
	case <-s.ctx.Done():
		return s.Stop()
	case err := <-errCh:
		return err
	}
}

func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if s.grpcServer != nil {
		s.log.Info("stopping grpc server")
		s.grpcServer.GracefulStop()
	}
	if s.grpcNetwork == "unix" {
		os.RemoveAll(s.grpcAddress) // nolint:errcheck
	}
	if s.mockBackend.httpsrv != nil {
		s.log.Info("stopping http server")
		if err := s.mockBackend.httpsrv.Shutdown(ctx); err != nil {
			return fmt.Errorf("http server shutdown error: %w", err)
		}
	}
	time.Sleep(defaultShutdownWait)
	return nil
}

func IsReady(s *Server) bool {
	if s.mockBackend.enabled {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/headers", s.mockBackend.bindAddress), nil)
		if err != nil {
			return false
		}
		httpClient := http.Client{
			Timeout: 5 * time.Second,
		}
		res, err := httpClient.Do(req)
		if err != nil {
			return false
		}
		if res.StatusCode != http.StatusOK {
			return false
		}
	}
	return true
}

func WaitReady(s *Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	tck := time.NewTicker(500 * time.Millisecond)
	defer tck.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tck.C:
			if IsReady(s) {
				return nil
			}
		}
	}
}

func WithGrpcServer(server *grpc.Server, network string, address string) Option {
	return func(s *Server) {
		s.grpcServer = server
		s.grpcNetwork = network
		s.grpcAddress = fmt.Sprintf(":%s", address)
	}
}

func WithMockBackend() Option {
	return func(s *Server) {
		s.mockBackend.enabled = true
	}
}
