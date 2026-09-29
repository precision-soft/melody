package presenter

import (
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodyserializer "github.com/precision-soft/melody/v3/serializer"
    melodyserializercontract "github.com/precision-soft/melody/v3/serializer/contract"
)

func textNegotiatingRequest(t *testing.T) (melodyruntimecontract.Runtime, melodyhttpcontract.Request) {
    t.Helper()

    runtimeInstance := runtimeForEnvironment(t, melodyconfig.EnvProduction)

    melodycontainer.MustRegister(
        runtimeInstance.Container().(melodycontainercontract.Registrar),
        melodyserializer.ServiceSerializerManager,
        func(resolver melodycontainercontract.Resolver) (*melodyserializer.SerializerManager, error) {
            return melodyserializer.NewSerializerManager(
                map[string]melodyserializercontract.Serializer{
                    melodyserializer.MimeApplicationJson: melodyserializer.NewJsonSerializer(),
                    melodyserializer.MimeTextPlain:       melodyserializer.NewPlainTextSerializer(),
                },
            )
        },
    )

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/missing", nil)
    httpRequest.Header.Set("Accept", "text/plain")

    return runtimeInstance, melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("refusal-test", time.Now()))
}

func TestApiErrorAnswersATextPlainClientWithReadableLines(t *testing.T) {
    runtimeInstance, request := textNegotiatingRequest(t)

    response := ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found")

    if "text/plain; charset=utf-8" != response.Headers().Get("Content-Type") {
        t.Fatalf("expected the plain text content type, got %q", response.Headers().Get("Content-Type"))
    }

    body := responseBodyOf(t, response)
    lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")

    if "404 not found" != lines[0] || "request id: refusal-test" != lines[1] || false == strings.HasPrefix(lines[2], "time: ") {
        t.Fatalf("expected the status line, the request id and the time first, got %q", body)
    }

    for _, expected := range []string{"method: GET", "path: /missing", "params:"} {
        found := false
        for _, line := range lines[3:] {
            if expected == line {
                found = true
            }
        }

        if false == found {
            t.Fatalf("expected the line %q, got %q", expected, body)
        }
    }

    if true == strings.Contains(body, "{false") || true == strings.Contains(body, "map[") || true == strings.Contains(body, "statusCode") {
        t.Fatalf("expected no bare struct, map or repeated status, got %q", body)
    }
}

func TestPlainTextRefusalListsNestedEntriesAndTheTrace(t *testing.T) {
    response := plainTextRefusal(
        nethttp.StatusInternalServerError,
        apiResponse{
            Errors: []string{"could not verify the code", "try again"},
            Context: map[string]any{
                "statusCode":   500,
                "requestId":    "abc",
                "time":         "2026-09-29T09:00:00Z",
                "routePattern": "/twofactor/verify/",
                "routeName":    "",
                "params":       map[string]string{"id": "7", "code": "x"},
            },
            Trace: []map[string]any{
                {"message": "store refused", "type": "*errors.errorString"},
            },
        },
        "text/plain; charset=utf-8",
    )

    expected := strings.Join([]string{
        "500 could not verify the code; try again",
        "request id: abc",
        "time: 2026-09-29T09:00:00Z",
        "params:",
        "  code: x",
        "  id: 7",
        "route name:",
        "route pattern: /twofactor/verify/",
        "trace:",
        "  - store refused (*errors.errorString)",
    }, "\n") + "\n"

    if body := responseBodyOf(t, response); expected != body {
        t.Fatalf("expected\n%s\ngot\n%s", expected, body)
    }
}

func TestApiSuccessKeepsTheSerializerRenderingForATextPlainClient(t *testing.T) {
    runtimeInstance, request := textNegotiatingRequest(t)

    body := responseBodyOf(t, ApiSuccess(runtimeInstance, request, nethttp.StatusOK, "ready"))

    if true == strings.HasPrefix(body, "200 ") {
        t.Fatalf("expected the success envelope left to the serializer, got %q", body)
    }
}

/* a refusal that tells the client how to go on keeps it for a text/plain client, beneath the status line, as the json client reads it in the envelope's payload */
func TestApiErrorWithPayloadWritesThePayloadBeneathTheStatusLine(t *testing.T) {
    runtimeInstance, request := textNegotiatingRequest(t)

    response := ApiErrorWithPayload(runtimeInstance, request, nethttp.StatusUnauthorized, map[string]any{"factor": "totp"}, "second factor required")

    body := responseBodyOf(t, response)
    lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")

    if nethttp.StatusUnauthorized != response.StatusCode() || "401 second factor required" != lines[0] || "factor: totp" != lines[1] || "request id: refusal-test" != lines[2] {
        t.Fatalf("expected the status line, the payload and then the request id, got %d %q", response.StatusCode(), body)
    }
}
