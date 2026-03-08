package http

import (
    "net/http/httptest"
    "testing"
)

func TestTextResponse_WritesBodyAndStatus(t *testing.T) {
    response := TextResponse(201, "created")

    rec := httptest.NewRecorder()

    err := WriteToHttpResponseWriter(nil, nil, rec, response)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 201 != rec.Code {
        t.Fatalf("unexpected status")
    }

    if "created" != rec.Body.String() {
        t.Fatalf("unexpected body")
    }
}

func TestJsonResponse_WritesJson(t *testing.T) {
    response, err := JsonResponse(
        200,
        map[string]any{
            "a": "b",
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    rec := httptest.NewRecorder()

    err = WriteToHttpResponseWriter(nil, nil, rec, response)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 200 != rec.Code {
        t.Fatalf("unexpected status")
    }

    if "" == rec.Body.String() {
        t.Fatalf("expected body")
    }

    contentType := rec.Header().Get("Content-Type")
    if "" == contentType {
        t.Fatalf("expected content-type header")
    }
}
