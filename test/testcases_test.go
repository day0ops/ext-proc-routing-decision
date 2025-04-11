package test_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"

	"github.com/day0ops/ext-proc-routing-decision/pkg/server"
	extproctest "github.com/day0ops/ext-proc-routing-decision/test"
	"github.com/day0ops/ext-proc-routing-decision/test/containers/envoy"
)

const enableDebug = false

func TestIntegration(t *testing.T) {
	suite.Run(t, &IntegrationTestSuite{})
}

type IntegrationTestSuite struct {
	suite.Suite
	container *envoy.TestContainer
	url       string
	ctx       context.Context
}

func (suite *IntegrationTestSuite) SetupSuite() {
	suite.ctx = context.Background()
	suite.container = envoy.NewTestContainer()
	if err := suite.container.Run(suite.ctx, enableDebug, "quay.io/solo-io/envoy-gloo:1.34.0-patch0"); err != nil {
		log.Fatal(err)
	}
	suite.url = suite.container.URL.String()
}

func (suite *IntegrationTestSuite) TearDownSuite() {
	if err := suite.container.Terminate(suite.ctx); err != nil {
		log.Fatalf("error terminating envoy container: %s", err)
	}
}

func (suite *IntegrationTestSuite) TestIntegrationTest() {
	t := suite.T()
	var logger *zap.Logger
	if enableDebug {
		logger = zap.Must(zap.NewDevelopment())
	} else {
		logger = zap.NewNop()
	}
	srv := server.New(context.Background(), logger, server.WithMockBackend())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve()
	}()
	err := server.WaitReady(srv, 10*time.Second)
	require.NoError(t, err)

	templateData := struct {
		HeaderName  string
		HeaderValue string
	}{
		HeaderName:  "x-custom-header",
		HeaderValue: "value-1",
	}
	testcases := extproctest.LoadTemplate(t, "testdata/httptest.yaml", templateData)
	require.NotEmpty(t, testcases)
	testcases.Run(t, extproctest.WithURL(suite.url))
	require.NoError(t, srv.Stop())
}
