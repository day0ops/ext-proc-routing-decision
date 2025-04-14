package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/day0ops/ext-proc-routing-decision/pkg/config"

	core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	ext_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type RoutingDecision struct {
	Decision string `json:"decision"`
}

type ProcessingServer struct {
	log *zap.Logger
}

type HealthServer struct {
	Log *zap.Logger
}

func New(log *zap.Logger) *ProcessingServer {
	ps := &ProcessingServer{log: log}
	return ps
}

func (s *HealthServer) Check(ctx context.Context, in *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	s.Log.Debug("received health check request", zap.String("service", in.String()))
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (s *HealthServer) Watch(in *grpc_health_v1.HealthCheckRequest, srv grpc_health_v1.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "watch is not implemented")
}

func (s *ProcessingServer) Process(srv ext_proc_v3.ExternalProcessor_ProcessServer) error {
	ctx := srv.Context()
	for {
		select {
		case <-ctx.Done():
			s.log.Debug("processing server context done")
			return ctx.Err()
		default:
		}

		req, err := srv.Recv()
		if err == io.EOF {
			// envoy has closed the stream. Don't return anything and close this stream entirely
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "cannot receive stream request: %v", err)
		}

		// build response based on request type
		resp := &ext_proc_v3.ProcessingResponse{}
		switch v := req.Request.(type) {
		case *ext_proc_v3.ProcessingRequest_RequestHeaders:
			s.log.Debug("got RequestHeaders")
			h := req.Request.(*ext_proc_v3.ProcessingRequest_RequestHeaders)
			headersResp, err := s.generateRoutingDecision(h.RequestHeaders)
			if err != nil {
				return err
			}
			resp = &ext_proc_v3.ProcessingResponse{
				Response: &ext_proc_v3.ProcessingResponse_RequestHeaders{
					RequestHeaders: headersResp,
				},
			}

		case *ext_proc_v3.ProcessingRequest_RequestBody:
			s.log.Debug("got RequestBody (not currently implemented)")

		case *ext_proc_v3.ProcessingRequest_RequestTrailers:
			s.log.Debug("got RequestTrailers (not currently implemented)")

		case *ext_proc_v3.ProcessingRequest_ResponseHeaders:
			s.log.Debug("got ResponseHeaders (not currently implemented)")

		case *ext_proc_v3.ProcessingRequest_ResponseBody:
			s.log.Debug("got ResponseBody (not currently implemented)")

		case *ext_proc_v3.ProcessingRequest_ResponseTrailers:
			s.log.Debug("got ResponseTrailers (not currently handled)")

		default:
			s.log.Error("unknown Request type", zap.Any("v", v))
		}

		s.log.Info("sending ProcessingResponse")
		if err := srv.Send(resp); err != nil {
			s.log.Error("send error", zap.Error(err))
			return err
		}

	}
}

// look at the preferred svc header value so we can take a short-circuiting routing decision from the list of headers
func (s *ProcessingServer) getPreferredSvcFromHeaders(in *ext_proc_v3.HttpHeaders) string {
	for _, n := range in.Headers.Headers {
		if strings.ToLower(n.Key) == config.PreferredSvcHeader {
			return string(n.RawValue)
		}
	}
	return ""
}

func (s *ProcessingServer) generateRoutingDecision(in *ext_proc_v3.HttpHeaders) (*ext_proc_v3.HeadersResponse, error) {
	header := s.getPreferredSvcFromHeaders(in)

	if header == "" {
		// let's call the outbound service for any routing decisions
		decision, err := s.fetchRoutingDecision()
		if err != nil {
			s.log.Error("failed to fetch routing decision", zap.Error(err))
			return &ext_proc_v3.HeadersResponse{}, err
		} else if decision == "" {
			// let's just fall through
			s.log.Error("no decision is present")
			return &ext_proc_v3.HeadersResponse{}, nil
		}
		header = decision
	}

	// build the response
	resp := &ext_proc_v3.HeadersResponse{
		Response: &ext_proc_v3.CommonResponse{},
	}

	resp.Response.Status = ext_proc_v3.CommonResponse_CONTINUE

	resp.Response.HeaderMutation = &ext_proc_v3.HeaderMutation{
		SetHeaders: []*core_v3.HeaderValueOption{
			{
				Header: &core_v3.HeaderValue{
					Key:      config.RoutingDecisionHeader,
					RawValue: []byte(header),
				},
				AppendAction: core_v3.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
			},
		},
		RemoveHeaders: []string{
			config.PreferredSvcHeader,
		},
	}

	// clear the route cache
	resp.Response.ClearRouteCache = true

	return resp, nil
}

func (s *ProcessingServer) doExternalServiceCall(url string, rc chan *http.Response) error {
	s.log.Debug("calling the external service", zap.String("url", url))

	resp, err := http.Get(url)

	if err == nil {
		rc <- resp
	}

	return err
}

func (s *ProcessingServer) fetchRoutingDecision() (string, error) {
	if config.RoutingDecisionServer == "" {
		err := fmt.Errorf("routing decision server has not been configured")
		s.log.Error("unable to get the routing decision from external service", zap.Error(err))
		return "", err
	}

	start := time.Now()

	rChan := make(chan *http.Response, 1)
	errGrp, _ := errgroup.WithContext(context.Background())
	errGrp.Go(func() error { return s.doExternalServiceCall(config.RoutingDecisionServer, rChan) })
	err := errGrp.Wait()
	if err != nil {
		s.log.Sugar().Errorf("unable to get the routing decision from external service %s: %v", config.RoutingDecisionServer, zap.Error(err))
	}
	resp := <-rChan
	defer resp.Body.Close()

	end := time.Now()
	duration := end.Sub(start)
	s.log.Debug("fetching took", zap.Duration("duration", duration))

	var decisionResp RoutingDecision
	err = json.NewDecoder(resp.Body).Decode(&decisionResp)
	if err != nil {
		s.log.Error("error decoding response from external service", zap.Error(err))
	}

	return decisionResp.Decision, err
}
