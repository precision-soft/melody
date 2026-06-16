package httpclient

import (
    "net/http"
    "testing"
)

func TestResponseHelpers_StatusClassificationAndString(t *testing.T) {
    response := NewResponse(201, "201 Created", http.Header{}, []byte("ok"), nil)

    if false == response.IsSuccess() {
        t.Fatalf("expected success")
    }
    if true == response.IsClientError() {
        t.Fatalf("expected not client error")
    }
    if true == response.IsServerError() {
        t.Fatalf("expected not server error")
    }
    if "ok" != response.String() {
        t.Fatalf("unexpected string")
    }

    response = NewResponse(404, "404 Not Found", http.Header{}, []byte("no"), nil)
    if true == response.IsSuccess() {
        t.Fatalf("expected not success")
    }
    if false == response.IsClientError() {
        t.Fatalf("expected client error")
    }

    response = NewResponse(500, "500 Internal Server Error", http.Header{}, []byte("error"), nil)
    if false == response.IsServerError() {
        t.Fatalf("expected server error")
    }
}
