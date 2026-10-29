package opentelemetry

import (
    "testing"
)

func TestNewMetricsMiddlewareWithPrometheus(t *testing.T) {
    middleware, handler, middlewareErr := NewMetricsMiddlewareWithPrometheus("example")
    if nil != middlewareErr {
        t.Fatalf("unexpected error: %v", middlewareErr)
    }

    if nil == middleware {
        t.Fatal("expected a non-nil metrics middleware")
    }

    if nil == handler {
        t.Fatal("expected a non-nil metrics handler")
    }
}
