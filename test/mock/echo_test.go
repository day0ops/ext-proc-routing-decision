package mock_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/day0ops/ext-proc-routing-decision/test/mock"
)

func TestRequestHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set("X-Test-Header", "test-value")
	rr := httptest.NewRecorder()

	mock.RequestHeaders(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, "expected HTTP status OK")

	var resp mock.RequestHeaderResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp), "failed to decode response body")

	expectedHeaders := map[string]string{
		"X-Test-Header": "test-value",
		"Host":          "example.com",
		"Method":        http.MethodGet,
	}
	for key, expectedValue := range expectedHeaders {
		require.Equal(t, expectedValue, resp.Headers[key], "mismatch for header %s", key)
	}
}

func TestResponseHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com?status=201&X-Test-Response=test-value", nil)
	rr := httptest.NewRecorder()

	mock.ResponseHeaders(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code, "expected HTTP status Created")
	require.Equal(t, "test-value", rr.Header().Get("X-Test-Response"), "mismatch for header X-Test-Response")

	var resp mock.ResponseHeaderResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp), "failed to decode response body")
	require.Equal(t, "test-value", resp["X-Test-Response"], "mismatch for X-Test-Response in body")
}

func TestResponseHeadersInvalidStatus(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com?status=invalid", nil)
	rr := httptest.NewRecorder()

	mock.ResponseHeaders(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code, "expected HTTP status Bad Request")

	var resp mock.ErrorResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp), "failed to decode response body")
	require.NotEmpty(t, resp.Error, "error message should not be empty")
}
