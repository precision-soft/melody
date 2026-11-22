package security

import (
    nethttp "net/http"
    "testing"
)

func TestJsonEntryPoint_ZeroConfigChallengesWithBearer(t *testing.T) {
    entryPoint := NewJsonEntryPoint(JsonEntryPointConfig{})

    response, startErr := entryPoint.Start(testRuntime(), bearerRequest(""))
    if nil != startErr {
        t.Fatalf("unexpected start error: %v", startErr)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected a 401 status, got %d", response.StatusCode())
    }

    if "Bearer" != DefaultJsonEntryPointChallenge || DefaultJsonEntryPointChallenge != response.Headers().Get("WWW-Authenticate") {
        t.Fatalf("expected a WWW-Authenticate: Bearer header, got %q", response.Headers().Get("WWW-Authenticate"))
    }
}

func TestJsonEntryPoint_WritesTheConfiguredChallenge(t *testing.T) {
    entryPoint := NewJsonEntryPoint(JsonEntryPointConfig{Challenge: `Bearer realm="api"`})

    response, startErr := entryPoint.Start(testRuntime(), bearerRequest(""))
    if nil != startErr {
        t.Fatalf("unexpected start error: %v", startErr)
    }

    if `Bearer realm="api"` != response.Headers().Get("WWW-Authenticate") {
        t.Fatalf("expected the configured challenge, got %q", response.Headers().Get("WWW-Authenticate"))
    }
}

func TestJsonEntryPoint_RefusesAChallengeHoldingALineBreak(t *testing.T) {
    for _, challenge := range []string{"Bearer\r\nSet-Cookie: forged=1", "Bearer\nrealm", "Bearer\x00", "Bearer\u2028"} {
        expectPanicNamingField(t, "JsonEntryPointConfig.Challenge", func() {
            NewJsonEntryPoint(JsonEntryPointConfig{Challenge: challenge})
        })
    }
}
