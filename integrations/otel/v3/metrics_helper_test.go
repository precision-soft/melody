package otel_test

import (
    "testing"

    otel "github.com/precision-soft/melody/integrations/otel/v3"
)

func TestNewMetricsMiddlewareWithPrometheus(t *testing.T) {
    middleware, handler, middlewareErr := otel.NewMetricsMiddlewareWithPrometheus("example")
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
