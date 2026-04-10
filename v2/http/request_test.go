package http

import (
    "io"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestNewRequest_ParseFormError_PostBagIsEmpty(t *testing.T) {
    httpRequest := httptest.NewRequest("POST", "/test", nil)
    httpRequest.Header.Set("Content-Type", "multipart/form-data; boundary=invalid")
    httpRequest.Body = io.NopCloser(strings.NewReader("not valid multipart"))

    request := NewRequest(httpRequest, nil, nil, nil)

    postBag := request.Post()
    if nil == postBag {
        t.Fatalf("expected non-nil post bag")
    }

    if true == postBag.Has("anything") {
        t.Fatalf("expected empty post bag when form parsing fails")
    }
}

func TestNewRequest_ParseFormError_NilRuntime_NoPanic(t *testing.T) {
    httpRequest := httptest.NewRequest("POST", "/test", nil)
    httpRequest.Header.Set("Content-Type", "multipart/form-data; boundary=invalid")
    httpRequest.Body = io.NopCloser(strings.NewReader("not valid multipart"))

    request := NewRequest(httpRequest, nil, nil, nil)

    if nil == request {
        t.Fatalf("expected non-nil request even when form parsing fails with nil runtime")
    }
}
