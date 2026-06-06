package opentelemetry_test

import (
    "testing"

    opentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
)

func TestNewMetricsMiddlewareWithPrometheus(t *testing.T) {
    middleware, handler, middlewareErr := opentelemetry.NewMetricsMiddlewareWithPrometheus("example")
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
