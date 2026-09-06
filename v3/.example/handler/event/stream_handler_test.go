package event

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* recordingResponseWriter answers the two questions the kernel asks a writer before it decides whether a
   handler's response is still writable: were the headers committed, and with which status. The kernel's own
   recording writer is unexported, so the test carries the same shape rather than a bare httptest recorder,
   whose Code field reads 200 whether the handler committed that status or wrote nothing at all. */
type recordingResponseWriter struct {
    header      nethttp.Header
    wroteHeader bool
    status      int
    flushed     bool
}

func (instance *recordingResponseWriter) Header() nethttp.Header {
    if nil == instance.header {
        instance.header = nethttp.Header{}
    }

    return instance.header
}

func (instance *recordingResponseWriter) Write(payload []byte) (int, error) {
    if false == instance.wroteHeader {
        instance.WriteHeader(nethttp.StatusOK)
    }

    return len(payload), nil
}

func (instance *recordingResponseWriter) WriteHeader(statusCode int) {
    if true == instance.wroteHeader {
        return
    }

    instance.wroteHeader = true
    instance.status = statusCode
}

func (instance *recordingResponseWriter) Flush() {
    instance.flushed = true
}

func (instance *recordingResponseWriter) HeadersWritten() bool {
    return instance.wroteHeader
}

func (instance *recordingResponseWriter) CommittedStatusCode() int {
    return instance.status
}

func streamRequest(t *testing.T, target string, cancelled bool) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, target, nil)

    if true == cancelled {
        requestContext, cancel := context.WithCancel(context.Background())
        cancel()

        httpRequest = httpRequest.WithContext(requestContext)
    }

    request := melodyhttp.NewRequest(
        httpRequest,
        nil,
        runtimeInstance,
        melodyhttp.NewRequestContext("stream-test", time.Now()),
    )

    return request, runtimeInstance
}

/* the refusal has to be decided while the response is still writable. NewServerSentEventWriter commits it —
   event-stream headers, 200, flush — and the kernel discards whatever a handler returns once the headers are
   committed, so a gate placed after it answers a successful empty stream that a browser reconnects to
   forever, and the access log records the 200. The assertion is therefore on the writer being UNTOUCHED,
   not on the 403 alone: the status was already right while the defect was live. */
func TestStreamHandler_RefusesAPrivilegedTopicWithoutCommittingTheResponse(t *testing.T) {
    request, runtimeInstance := streamRequest(t, "/events/stream/", false)
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(melodyhttp.NewServerSentEventHub())(runtimeInstance, writer, request)
    if nil != handlerErr {
        t.Fatalf("expected the handler to answer the refusal itself, got %v", handlerErr)
    }

    if nil == response {
        t.Fatalf("expected a refusal response for a caller without the topic's role")
    }

    if nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected the caller to be refused with 403, got %d", response.StatusCode())
    }

    if true == writer.HeadersWritten() {
        t.Fatalf(
            "expected the refusal to leave the response uncommitted, but the handler had already written status %d with content type %q",
            writer.CommittedStatusCode(),
            writer.Header().Get("Content-Type"),
        )
    }

    if true == writer.flushed {
        t.Fatalf("expected nothing to be flushed to a refused caller")
    }
}

/* the sister case, which is what keeps the assertion above from passing over a handler that refuses
   everything: a topic this application does not publish onto itself is readable by any authenticated
   caller, and there the stream IS opened — headers committed, 200, flushed. */
func TestStreamHandler_OpensTheStreamForATopicThatNeedsNoRole(t *testing.T) {
    request, runtimeInstance := streamRequest(t, "/events/stream/?topic=visitor", true)
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(melodyhttp.NewServerSentEventHub())(runtimeInstance, writer, request)
    if nil != handlerErr {
        t.Fatalf("expected the stream to end on the cancelled request context, got %v", handlerErr)
    }

    if nil != response {
        t.Fatalf("expected a streamed request to answer no response, got status %d", response.StatusCode())
    }

    if false == writer.HeadersWritten() {
        t.Fatalf("expected the stream to commit the response for an allowed topic")
    }

    if nethttp.StatusOK != writer.CommittedStatusCode() {
        t.Fatalf("expected the opened stream to commit 200, got %d", writer.CommittedStatusCode())
    }

    if "text/event-stream" != writer.Header().Get("Content-Type") {
        t.Fatalf("expected the opened stream to carry the event-stream content type, got %q", writer.Header().Get("Content-Type"))
    }
}
