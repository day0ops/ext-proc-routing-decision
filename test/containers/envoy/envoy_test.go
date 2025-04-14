package envoy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/day0ops/ext-proc-routing-decision/test/containers/envoy"
)

func TestRunContainer(t *testing.T) {
	container := envoy.NewTestContainer()
	err := container.Run(context.Background(), false, "quay.io/solo-io/envoy-gloo:1.34.0-patch0")
	defer testcontainers.CleanupContainer(t, container)

	require.NoError(t, err)
	require.Contains(t, container.URL.String(), "http://localhost:")
}
