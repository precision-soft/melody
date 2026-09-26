package http

import (
    "context"
    "encoding/json"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/serializer"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
)

type xmlTestSerializer struct{}

func (instance *xmlTestSerializer) Serialize(value any) ([]byte, error) {
    return []byte("<xml-error/>"), nil
}

func (instance *xmlTestSerializer) Deserialize(payload []byte, target any) error {
    return nil
}

func (instance *xmlTestSerializer) ContentType() string {
    return "application/xml"
}

func newErrorRendererTestRuntime(
    logger loggingcontract.Logger,
    serializersByMime map[string]serializercontract.Serializer,
) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()

    if nil != serializersByMime {
        container.MustRegister[*serializer.SerializerManager](
            serviceContainer,
            serializer.ServiceSerializerManager,
            func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
                return serializer.NewSerializerManager(serializersByMime)
            },
        )
    }

    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger)

    return runtime.New(context.Background(), scope, serviceContainer)
}

func TestRenderErrorResponse_NegotiatesTheErrorBodyThroughTheSerializerManager(t *testing.T) {
    runtimeInstance := newErrorRendererTestRuntime(
        logging.NewNopLogger(),
        map[string]serializercontract.Serializer{
            "application/xml": &xmlTestSerializer{},
        },
    )

    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "application/xml")

    response := renderErrorResponse(runtimeInstance, request, nethttp.StatusServiceUnavailable, "unavailable", nil)

    if nethttp.StatusServiceUnavailable != response.StatusCode() {
        t.Fatalf("expected the error status to survive negotiation, got %d", response.StatusCode())
    }

    if "application/xml" != response.Headers().Get("Content-Type") {
        t.Fatalf("expected the negotiated content type, got %q", response.Headers().Get("Content-Type"))
    }

    if "<xml-error/>" != readResponseBody(t, response) {
        t.Fatalf("expected the negotiated serializer to render the body")
    }
}

func TestRenderErrorResponse_KeepsTheErrorStatusWhenTheAcceptHeaderRefusesEverything(t *testing.T) {
    runtimeInstance := newErrorRendererTestRuntime(
        logging.NewNopLogger(),
        map[string]serializercontract.Serializer{
            "application/xml": &xmlTestSerializer{},
        },
    )

    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "application/xml;q=0")

    response := renderErrorResponse(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized", nil)

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected the refusal status to be kept rather than masked as 406, got %d", response.StatusCode())
    }

    if false == strings.Contains(response.Headers().Get("Content-Type"), "application/json") {
        t.Fatalf("expected the default json body, got content type %q", response.Headers().Get("Content-Type"))
    }
}

func TestRenderErrorResponse_FallsBackToJsonQuietlyWithoutASerializerManager(t *testing.T) {
    captureLogger := &exceptionListenerCaptureLogger{}
    runtimeInstance := newErrorRendererTestRuntime(captureLogger, nil)

    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "application/xml")

    response := renderErrorResponse(runtimeInstance, request, nethttp.StatusInternalServerError, "internal server error", nil)

    if false == strings.Contains(response.Headers().Get("Content-Type"), "application/json") {
        t.Fatalf("expected the json fallback, got content type %q", response.Headers().Get("Content-Type"))
    }

    if 0 != captureLogger.errorCalls {
        t.Fatalf("expected the missing manager to be resolved quietly, got %d error records", captureLogger.errorCalls)
    }
}

func TestRenderErrorResponse_SurvivesANilRuntimeAndANilRequest(t *testing.T) {
    response := renderErrorResponse(nil, nil, nethttp.StatusInternalServerError, "internal server error", nil)

    if nethttp.StatusInternalServerError != response.StatusCode() {
        t.Fatalf("expected the status to be kept, got %d", response.StatusCode())
    }

    if false == strings.Contains(readResponseBody(t, response), "internal server error") {
        t.Fatalf("expected the message in the body")
    }
}

func TestRenderErrorResponse_HtmlPageCarriesStatusAndEscapedMessage(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "text/html")

    response := renderErrorResponse(nil, request, nethttp.StatusBadGateway, "<script>alert(1)</script>", nil)

    body := readResponseBody(t, response)

    if false == strings.Contains(body, "Status: 502") {
        t.Fatalf("expected the html page to name the status, got %q", body)
    }

    if true == strings.Contains(body, "<script>") {
        t.Fatalf("expected the message to be escaped, got %q", body)
    }

    if false == strings.Contains(response.Headers().Get("Content-Type"), "text/html") {
        t.Fatalf("expected the html content type, got %q", response.Headers().Get("Content-Type"))
    }
}

func TestRenderErrorResponse_BaseKeysWinACollisionWithTheExtras(t *testing.T) {
    response := renderErrorResponse(
        nil,
        nil,
        nethttp.StatusInternalServerError,
        "internal server error",
        map[string]any{
            "error":  "spoofed",
            "detail": "kept",
        },
    )

    payload := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(readResponseBody(t, response)), &payload); nil != unmarshalErr {
        t.Fatalf("unexpected unmarshal error: %v", unmarshalErr)
    }

    errorObject, isObject := payload["error"].(map[string]any)
    if false == isObject || "internal server error" != errorObject["message"] {
        t.Fatalf("expected the base error object to win the collision, got %v", payload["error"])
    }

    if "kept" != payload["detail"] {
        t.Fatalf("expected the extra key to be carried, got %v", payload["detail"])
    }
}

/* the renderer is consulted from inside the recovery defer: an application serializer that panics on the error payload must cost its representation, never the response — the contained panic degrades to the json fallback under the door's own "an error response always exists". */
func TestSerializeErrorPayloadSafely_ContainsAPanickingSerializer(t *testing.T) {
    _, serializeErr := serializeErrorPayloadSafely(&panickingSerializer{}, map[string]any{"error": "boom"})
    if nil == serializeErr {
        t.Fatal("expected the contained panic to answer as the error the caller degrades on")
    }

    if false == strings.Contains(serializeErr.Error(), "serialization panicked") {
        t.Fatalf("expected the error to name the contained panic, got %q", serializeErr.Error())
    }
}

type panickingSerializer struct{}

func (instance *panickingSerializer) Serialize(value any) ([]byte, error) {
    panic("serializer died on the error payload")
}

func (instance *panickingSerializer) Deserialize(payload []byte, target any) error {
    return nil
}

func (instance *panickingSerializer) ContentType() string {
    return "application/x-panic"
}
