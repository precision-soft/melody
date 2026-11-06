package security

import (
    nethttp "net/http"
    "testing"
)

func TestJsonEntryPoint_SetsWwwAuthenticateHeader(t *testing.T) {
    entryPoint := NewJsonEntryPoint()

    response, startErr := entryPoint.Start(testRuntime(), bearerRequest(""))
    if nil != startErr {
        t.Fatalf("unexpected start error: %v", startErr)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected a 401 status, got %d", response.StatusCode())
    }

    if "Bearer" != response.Headers().Get("WWW-Authenticate") {
        t.Fatalf("expected a WWW-Authenticate: Bearer header, got %q", response.Headers().Get("WWW-Authenticate"))
    }
}
