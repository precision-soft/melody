package http

import (
    "context"
    "encoding/json"
    "errors"
    nethttp "net/http"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/internal/plaintext"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/serializer"
    serializercontract "github.com/precision-soft/melody/v4/serializer/contract"
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
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    if nil != serializersByMime {
        container.MustRegister[*serializer.SerializerManager](
            serviceContainer,
            serializer.ServiceSerializerManager,
            func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
                return serializer.NewSerializerManager(serializersByMime)
            },
            container.RegisterOptions{},
        )
    }

    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

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
            "error":   "spoofed",
            "status":  "spoofed",
            "message": "spoofed",
            "detail":  "kept",
        },
    )

    payload := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(readResponseBody(t, response)), &payload); nil != unmarshalErr {
        t.Fatalf("unexpected unmarshal error: %v", unmarshalErr)
    }

    errorObject, isMap := payload["error"].(map[string]any)
    if false == isMap || "internal server error" != errorObject["message"] {
        t.Fatalf("expected the base error object to win the collision, got %v", payload["error"])
    }

    if float64(nethttp.StatusInternalServerError) != payload["status"] {
        t.Fatalf("expected the base status to win the collision, got %v", payload["status"])
    }

    if "spoofed" != payload["message"] {
        t.Fatalf("expected a top-level message extra to stay beside the error object, got %v", payload["message"])
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
            "text/plain":       serializer.NewPlainTextSerializer(serializer.PlainTextSerializerConfig{}),
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
    if 3 != len(lines) || "400 bad request" != lines[0] || "request id: test" != lines[1] || false == strings.HasPrefix(lines[2], "time: ") {
        t.Fatalf("expected the status line, the request id and the time, got %q", body)
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
            "validationErrors": []error{errors.New("name: is required"), errors.New("price: must be positive")},
            "context":          map[string]any{"limit": 3, "field": "name", "route": ""},
            "cause":            "decoder refused the body",
        },
    )

    expected := []string{
        "422 invalid payload",
        "request id: test",
        "cause: decoder refused the body",
        "context:",
        "  field: name",
        "  limit: 3",
        "  route:",
        "validation errors:",
        "  - name: is required",
        "  - price: must be positive",
    }

    lines := plainTextErrorLinesWithoutTime(t, readResponseBody(t, response), 2)
    if strings.Join(expected, "\n") != strings.Join(lines, "\n") {
        t.Fatalf("expected\n%s\ngot\n%s", strings.Join(expected, "\n"), strings.Join(lines, "\n"))
    }
}

func TestRenderErrorResponse_ListsTheClientFacingIdentityOfATextPlainErrorByKey(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "text/plain")

    response := renderErrorResponse(
        newErrorRendererTextAndJsonRuntime(),
        request,
        nethttp.StatusConflict,
        "order is locked",
        map[string]any{
            "code":       "order.locked",
            "key":        "error.order.locked",
            "parameters": map[string]any{"id": 7},
        },
    )

    expected := []string{
        "409 order is locked",
        "request id: test",
        "code: order.locked",
        "key: error.order.locked",
        "parameters:",
        "  id: 7",
    }

    lines := plainTextErrorLinesWithoutTime(t, readResponseBody(t, response), 2)
    if strings.Join(expected, "\n") != strings.Join(lines, "\n") {
        t.Fatalf("expected\n%s\ngot\n%s", strings.Join(expected, "\n"), strings.Join(lines, "\n"))
    }
}

func TestRenderErrorResponse_KeepsTheJsonBodyForAJsonClientBesideThePlainTextSerializer(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "application/json")

    response := renderErrorResponse(newErrorRendererTextAndJsonRuntime(), request, nethttp.StatusBadRequest, "bad request", nil)

    if false == strings.HasPrefix(response.Headers().Get("Content-Type"), "application/json") {
        t.Fatalf("expected the json content type, got %q", response.Headers().Get("Content-Type"))
    }

    payload := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(readResponseBody(t, response)), &payload); nil != unmarshalErr {
        t.Fatalf("expected a json body, got %v", unmarshalErr)
    }

    errorObject, isMap := payload["error"].(map[string]any)
    if false == isMap || "bad request" != errorObject["message"] || float64(nethttp.StatusBadRequest) != payload["status"] || "test" != payload["requestId"] || "" == payload["time"] {
        t.Fatalf("expected the json envelope, got %v", payload)
    }
}

func TestRenderErrorResponse_MovesTheErrorObjectExtrasIntoTheErrorObject(t *testing.T) {
    response := renderErrorResponse(
        nil,
        nil,
        nethttp.StatusConflict,
        "order is locked",
        map[string]any{
            "code":             "order.locked",
            "key":              "error.order.locked",
            "parameters":       map[string]any{"id": 7},
            "context":          map[string]any{"orderId": "7"},
            "cause":            "lock held",
            "validationErrors": []any{"kept"},
        },
    )

    payload := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(readResponseBody(t, response)), &payload); nil != unmarshalErr {
        t.Fatalf("unexpected unmarshal error: %v", unmarshalErr)
    }

    errorObject, isMap := payload["error"].(map[string]any)
    if false == isMap {
        t.Fatalf("expected an error object, got %v", payload)
    }

    for _, key := range []string{"message", "code", "key", "parameters", "context", "cause"} {
        if _, exists := errorObject[key]; false == exists {
            t.Fatalf("expected %s inside the error object, got %v", key, errorObject)
        }

        if _, exists := payload[key]; true == exists {
            t.Fatalf("expected %s only inside the error object, got %v", key, payload)
        }
    }

    if _, exists := payload["validationErrors"]; false == exists {
        t.Fatalf("expected validationErrors at the top level, got %v", payload)
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

type errorRendererPanickingStringer struct{}

func (instance errorRendererPanickingStringer) String() string {
    panic("stringer exploded")
}

func TestRenderErrorResponse_ASelfReferencingMapIsCutAtTheDepthBound(t *testing.T) {
    selfReferencing := map[string]any{"name": "loop"}
    selfReferencing["self"] = selfReferencing

    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "text/plain")

    rendered := make(chan string, 1)
    go func() {
        response := renderErrorResponse(newErrorRendererTextAndJsonRuntime(), request, nethttp.StatusBadRequest, "bad request", map[string]any{"context": selfReferencing})
        rendered <- readResponseBody(t, response)
    }()

    select {
    case body := <-rendered:
        if false == strings.Contains(body, "self: ...") || 40 < strings.Count(body, "\n") {
            t.Fatalf("expected the walk cut at the depth bound, got %q", body)
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("expected the self-referencing map rendered within the bound")
    }
}

type errorRendererCycleHolder struct {
    Name  string
    Cycle map[string]any
}

func TestRenderErrorResponse_ACycleThroughASliceAStructOrAnIntegerKeyedMapIsCutAtTheDepthBound(t *testing.T) {
    throughSlice := map[string]any{"name": "loop"}
    throughSlice["self"] = []any{throughSlice}

    throughStruct := map[string]any{"name": "loop"}
    throughStruct["self"] = errorRendererCycleHolder{Name: "holder", Cycle: throughStruct}

    throughIntegerKeys := map[int]any{}
    throughIntegerKeys[1] = throughIntegerKeys

    payloads := map[string]any{
        "slice":       throughSlice,
        "struct":      throughStruct,
        "integerKeys": throughIntegerKeys,
    }

    for name, payload := range payloads {
        request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "text/plain")

        rendered := make(chan string, 1)
        go func() {
            response := renderErrorResponse(newErrorRendererTextAndJsonRuntime(), request, nethttp.StatusBadRequest, "bad request", map[string]any{"context": payload})
            rendered <- readResponseBody(t, response)
        }()

        select {
        case body := <-rendered:
            if false == strings.Contains(body, "...") || 40 < strings.Count(body, "\n") {
                t.Fatalf("%s: expected the walk cut at the depth bound, got %q", name, body)
            }
        case <-time.After(5 * time.Second):
            t.Fatalf("%s: expected the cycle rendered within the bound", name)
        }
    }
}

func TestRenderErrorResponse_AValueSpelledManyTimesOverStaysWithinTheBodyBudget(t *testing.T) {
    selfReferences := make([]any, 48)
    for index := range selfReferences {
        selfReferences[index] = selfReferences
    }

    longText := strings.Repeat("a", 1<<20)
    longTexts := make([]any, 1000)
    for index := range longTexts {
        longTexts[index] = longText
    }

    payloads := map[string]any{
        "selfReferences": selfReferences,
        "longTexts":      longTexts,
    }

    for name, payload := range payloads {
        request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "text/plain")

        rendered := make(chan string, 1)
        go func() {
            response := renderErrorResponse(newErrorRendererTextAndJsonRuntime(), request, nethttp.StatusBadRequest, "bad request", map[string]any{"context": payload})
            rendered <- readResponseBody(t, response)
        }()

        select {
        case body := <-rendered:
            if plaintext.DefaultMaxBytes+8*plaintext.DefaultMaxMembers+64*1024 < len(body) || false == strings.HasSuffix(body, "...(truncated)\n") {
                t.Fatalf("%s: expected the body cut at its budget, got %d bytes ending %q", name, len(body), body[max(0, len(body)-40):])
            }
        case <-time.After(10 * time.Second):
            t.Fatalf("%s: expected the value rendered within the budget", name)
        }
    }
}

func TestRenderErrorResponse_WritesAStructByItsJsonNamesAndAListItemBeneathItsDash(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "text/plain")

    payload := map[string]any{
        "items":  []any{map[string]any{"field": "email", "rule": "required"}, 7},
        "holder": errorRendererCycleHolder{Name: "holder"},
    }

    body := readResponseBody(t, renderErrorResponse(newErrorRendererTextAndJsonRuntime(), request, nethttp.StatusBadRequest, "bad request", payload))

    for _, expected := range []string{"\nholder:\n  name: holder\n  cycle:\n", "\nitems:\n  -\n    field: email\n    rule: required\n  - 7\n"} {
        if false == strings.Contains(body, expected) {
            t.Fatalf("expected %q in the body, got %q", expected, body)
        }
    }
    if true == strings.Contains(body, "map[") || true == strings.Contains(body, "{") || true == strings.Contains(body, "...(truncated)") {
        t.Fatalf("expected no Go dump and a body within its budget not marked truncated, got %q", body)
    }
}

type errorRendererHiddenFields struct {
    Shown    string `json:"shownField"`
    Password string `json:"-"`
    secret   string
}

func TestPlainTextErrorResponse_WritesNoFieldAJsonClientWouldNotRead(t *testing.T) {
    response := plainTextErrorResponse(nethttp.StatusBadRequest, "bad request", map[string]any{"context": errorRendererHiddenFields{Shown: "yes", Password: "hunter2", secret: "s3cr3t"}}, "text/plain; charset=utf-8")

    body := readResponseBody(t, response)
    if "400 bad request\ncontext:\n  shown field: yes\n" != body {
        t.Fatalf("expected the json-named field alone, its key spelled as words, got %q", body)
    }
}

func TestPlainTextErrorResponse_KeepsTheMessageOnTheStatusLine(t *testing.T) {
    body := readResponseBody(t, plainTextErrorResponse(nethttp.StatusNotFound, "no route for /a\nrequest id: forged", map[string]any{"requestId": "req-1"}, "text/plain; charset=utf-8"))

    if "404 no route for /a\\nrequest id: forged\nrequest id: req-1\n" != body {
        t.Fatalf("expected the message on the status line, its control characters escaped, got %q", body)
    }
}

func TestPlainTextErrorResponse_KeepsTheBodyOfMapsAndListsByteForByte(t *testing.T) {
    for name, testCase := range map[string]struct {
        payload  map[string]any
        expected string
    }{
        "envelope": {
            payload: map[string]any{
                "status":    "error",
                "requestId": "req-1",
                "time":      "2026-11-11T00:00:00Z",
                "error":     map[string]any{"message": "bad request", "code": "E1", "detailsList": []string{"a", "b"}},
            },
            expected: "400 bad request\nrequest id: req-1\ntime: 2026-11-11T00:00:00Z\ncode: E1\ndetails list:\n  - a\n  - b\n",
        },
        "nested": {
            payload: map[string]any{
                "requestId": "req-2",
                "context":   map[string]any{"limit": 3, "fieldName": "name", "route": "", "inner": map[string]any{"deepKey": map[string]any{"x": 1}}},
                "cause":     "decoder refused the body",
            },
            expected: "400 bad request\nrequest id: req-2\ncause: decoder refused the body\ncontext:\n  field name: name\n  inner:\n    deep key:\n      x: 1\n  limit: 3\n  route:\n",
        },
        "lists": {
            payload: map[string]any{
                "validationErrors": []error{errors.New("name: is required"), errors.New("price: must be positive")},
                "strings":          []string{"one", "two"},
                "empty":            []string{},
                "bytes":            []byte("raw"),
            },
            expected: "400 bad request\nbytes: [114 97 119]\nempty:\nstrings:\n  - one\n  - two\nvalidation errors:\n  - name: is required\n  - price: must be positive\n",
        },
    } {
        body := readResponseBody(t, plainTextErrorResponse(nethttp.StatusBadRequest, "bad request", testCase.payload, "text/plain; charset=utf-8"))
        if testCase.expected != body {
            t.Fatalf("%s: expected %q, got %q", name, testCase.expected, body)
        }
    }
}

func TestRenderErrorResponse_ALongListIsCutAtTheLineBound(t *testing.T) {
    items := make([]string, 5000)
    for index := range items {
        items[index] = "item"
    }

    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "text/plain")

    body := readResponseBody(t, renderErrorResponse(newErrorRendererTextAndJsonRuntime(), request, nethttp.StatusBadRequest, "bad request", map[string]any{"items": items}))

    if 1100 < strings.Count(body, "\n") || false == strings.HasSuffix(body, "...(truncated)\n") {
        t.Fatalf("expected the body cut at the line bound, got %d lines", strings.Count(body, "\n"))
    }
}

func TestRenderErrorResponse_APanickingStringerFallsBackToTheJsonEnvelope(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/fail", "text/plain")

    response := renderErrorResponse(newErrorRendererTextAndJsonRuntime(), request, nethttp.StatusBadRequest, "bad request", map[string]any{"cause": errorRendererPanickingStringer{}})

    if nethttp.StatusBadRequest != response.StatusCode() || false == strings.HasPrefix(response.Headers().Get("Content-Type"), "application/json") {
        t.Fatalf("expected the json envelope under the error status, got %d %q", response.StatusCode(), response.Headers().Get("Content-Type"))
    }
}
