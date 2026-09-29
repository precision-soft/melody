package http

import (
    "context"
    "encoding/json"
    "errors"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/serializer"
    serializercontract "github.com/precision-soft/melody/serializer/contract"
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
        serviceContainer.MustRegister(
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

    if "internal server error" != payload["error"] {
        t.Fatalf("expected the base error key to win the collision, got %v", payload["error"])
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

func newErrorRendererTextAndJsonRuntime() runtimecontract.Runtime {
    return newErrorRendererTestRuntime(
        logging.NewNopLogger(),
        map[string]serializercontract.Serializer{
            "application/json": serializer.NewJsonSerializer(),
            "text/plain":       serializer.NewPlainTextSerializer(),
        },
    )
}

func plainTextErrorLinesWithoutTime(t *testing.T, body string, timeIndex int) []string {
    t.Helper()

    if false == strings.HasSuffix(body, "\n") {
        t.Fatalf("expected the body to end with a newline, got %q", body)
    }

    lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
    kept := make([]string, 0, len(lines))
    timeLines := 0
    for index, line := range lines {
        if true == strings.HasPrefix(line, "time: ") {
            timeLines++

            if timeIndex != index {
                t.Fatalf("expected the time on line %d, got it on line %d of %q", timeIndex, index, body)
            }

            continue
        }

        kept = append(kept, line)
    }

    if 1 != timeLines {
        t.Fatalf("expected exactly one time line, got %d in %q", timeLines, body)
    }

    return kept
}

func TestRenderErrorResponse_AnswersATextPlainClientWithReadableLines(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "text/plain")

    response := renderErrorResponse(newErrorRendererTextAndJsonRuntime(), request, nethttp.StatusBadRequest, "bad request", nil)

    if nethttp.StatusBadRequest != response.StatusCode() {
        t.Fatalf("expected the error status, got %d", response.StatusCode())
    }

    if "text/plain; charset=utf-8" != response.Headers().Get("Content-Type") {
        t.Fatalf("expected the plain text content type, got %q", response.Headers().Get("Content-Type"))
    }

    body := readResponseBody(t, response)
    if true == strings.Contains(body, "map[") {
        t.Fatalf("expected no Go map dump, got %q", body)
    }

    lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
    if 2 != len(lines) || "400 bad request" != lines[0] || false == strings.HasPrefix(lines[1], "time: ") {
        t.Fatalf("expected the status line and the time, got %q", body)
    }
}

func TestRenderErrorResponse_ListsTheDebugEntriesOfATextPlainErrorByKey(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "text/plain")

    response := renderErrorResponse(
        newErrorRendererTextAndJsonRuntime(),
        request,
        nethttp.StatusUnprocessableEntity,
        "invalid payload",
        map[string]any{
            "errors":  []error{errors.New("name: is required"), errors.New("price: must be positive")},
            "context": map[string]any{"limit": 3, "field": "name", "route": ""},
            "cause":   "decoder refused the body",
        },
    )

    expected := []string{
        "422 invalid payload",
        "cause: decoder refused the body",
        "context:",
        "  field: name",
        "  limit: 3",
        "  route:",
        "errors:",
        "  - name: is required",
        "  - price: must be positive",
    }

    lines := plainTextErrorLinesWithoutTime(t, readResponseBody(t, response), 1)
    if strings.Join(expected, "\n") != strings.Join(lines, "\n") {
        t.Fatalf("expected\n%s\ngot\n%s", strings.Join(expected, "\n"), strings.Join(lines, "\n"))
    }
}

func TestRenderErrorResponse_KeepsTheJsonEnvelopeForAJsonClientBesideThePlainTextSerializer(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "application/json")

    response := renderErrorResponse(newErrorRendererTextAndJsonRuntime(), request, nethttp.StatusBadRequest, "bad request", nil)

    if false == strings.HasPrefix(response.Headers().Get("Content-Type"), "application/json") {
        t.Fatalf("expected the json content type, got %q", response.Headers().Get("Content-Type"))
    }

    payload := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(readResponseBody(t, response)), &payload); nil != unmarshalErr {
        t.Fatalf("expected a json body, got %v", unmarshalErr)
    }

    if "bad request" != payload["error"] {
        t.Fatalf("expected the json envelope, got %v", payload)
    }
}

func TestIsPlainTextMediaType_ReadsTheMediaTypeWhateverItsParameters(t *testing.T) {
    cases := map[string]bool{
        "text/plain":                  true,
        "text/plain; charset=utf-8":   true,
        "TEXT/PLAIN; charset=latin-1": true,
        "text/plainish":               false,
        "application/json":            false,
        "":                            false,
    }

    for contentType, expected := range cases {
        if expected != isPlainTextMediaType(contentType) {
            t.Fatalf("expected %v for %q", expected, contentType)
        }
    }
}

func TestPlainTextLabel_SpellsACamelCasedKeyAsWords(t *testing.T) {
    cases := map[string]string{
        "requestId":        "request id",
        "validationErrors": "validation errors",
        "cause":            "cause",
    }

    for key, expected := range cases {
        if expected != plainTextLabel(key) {
            t.Fatalf("expected %q for %q, got %q", expected, key, plainTextLabel(key))
        }
    }
}
