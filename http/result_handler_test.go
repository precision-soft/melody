package http

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/runtime"
    "github.com/precision-soft/melody/serializer"
    serializercontract "github.com/precision-soft/melody/serializer/contract"
)

func TestNormalizeResultToResponse_TypedNilResponseBecomesNilInterface(t *testing.T) {
    var typedNil *Response

    response, err := NormalizeResultToResponse(nil, nil, typedNil)
    if nil != err {
        t.Fatalf("NormalizeResultToResponse returned an error: %v", err)
    }

    if nil != response {
        t.Fatalf("a typed-nil *Response must normalize to a nil httpcontract.Response interface so the kernel takes the no-content path; a non-nil interface wrapping a nil *Response panics on Headers()")
    }
}

/* @info the accept header is list-typed, so its repeated lines are one header: reading only the first line dropped whatever the client sent on the second — here the refusal of json would be honoured while the acceptance of text/plain vanished, answering 406 to a client that named an available type */
func TestNormalizeResultToResponse_JoinsRepeatedAcceptLines(t *testing.T) {
    serviceContainer := container.NewContainer()

    registerErr := container.Register[*serializer.SerializerManager](
        serviceContainer,
        serializer.ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
            return serializer.NewSerializerManager(
                map[string]serializercontract.Serializer{
                    serializer.MimeApplicationJson: serializer.NewJsonSerializer(),
                    serializer.MimeTextPlain:       serializer.NewPlainTextSerializer(),
                },
            )
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/value", nil)
    httpRequest.Header.Add("Accept", "application/json;q=0")
    httpRequest.Header.Add("Accept", "text/plain")

    request := NewRequest(httpRequest, nil, runtimeInstance, NewRequestContext("test", time.Now()))

    response, err := NormalizeResultToResponse(runtimeInstance, request, map[string]any{"a": "b"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == response {
        t.Fatalf("expected a response")
    }

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the second accept line to be honoured with 200, got %d", response.StatusCode())
    }

    if false == strings.HasPrefix(response.Headers().Get("Content-Type"), serializer.MimeTextPlain) {
        t.Fatalf("expected the representation accepted on the second line, got %s", response.Headers().Get("Content-Type"))
    }
}
