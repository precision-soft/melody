package middleware

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/precision-soft/melody/v2/http"
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	"github.com/precision-soft/melody/v2/internal/testhelper"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func TestCorsMiddleware_PreflightOptions(t *testing.T) {
	middleware := DefaultCorsMiddleware()

	next := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
		t.Fatalf("next should not be called for OPTIONS preflight")
		return nil, nil
	}

	handler := middleware(next)

	req := httptest.NewRequest(nethttp.MethodOptions, "/x", nil)
	req.Header.Set("Origin", "https://example.com")

	rec := httptest.NewRecorder()

	response, err := handler(nil, rec, testhelper.NewHttpTestRequestFromHttpRequest(req))
	if nil != err {
		t.Fatalf("unexpected error")
	}
	if nil == response {
		t.Fatalf("expected response")
	}

	if nethttp.StatusNoContent != response.StatusCode() {
		t.Fatalf("unexpected status")
	}
	if "" == response.Headers().Get("Access-Control-Allow-Origin") {
		t.Fatalf("expected allow-origin header")
	}
}

func TestCorsMiddleware_NonPreflightAddsHeaders(t *testing.T) {
	middleware := DefaultCorsMiddleware()

	next := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
		return http.EmptyResponse(200), nil
	}

	handler := middleware(next)

	req := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
	req.Header.Set("Origin", "https://example.com")

	rec := httptest.NewRecorder()

	response, err := handler(nil, rec, testhelper.NewHttpTestRequestFromHttpRequest(req))
	if nil != err {
		t.Fatalf("unexpected error")
	}
	if nil == response {
		t.Fatalf("expected response")
	}

	if "" == response.Headers().Get("Access-Control-Allow-Origin") {
		t.Fatalf("expected allow-origin header")
	}
}
