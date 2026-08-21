package http

import (
    "bufio"
    "io"
    "net"
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func WriteToHttpResponseWriter(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    responseWriter nethttp.ResponseWriter,
    response httpcontract.Response,
) error {
    /* the interface is read through, not compared: this is a public door, and a nil pointer of a response type boxed into the contract passes a plain comparison and dereferences on Headers() below */
    if true == internal.IsNilInterface(response) {
        return nil
    }

    statusCode := response.StatusCode()
    if 0 == statusCode {
        statusCode = nethttp.StatusOK
    }

    /* a status outside net/http's [100, 999] is refused by name before anything is written, the headers included: this is a public door, and handing the code to WriteHeader panics deep inside the response path, turning a caller's arithmetic mistake into a connection reset instead of the error return this signature promises. The whole validation runs ahead of the first mutation because a caller that handles the returned error and writes its own response would otherwise send it with the refused response's Set-Cookie on top — the internal write path refuses the same range before it renders anything. */
    if 100 > statusCode || 999 < statusCode {
        return exception.NewError(
            "response status code is out of range",
            map[string]any{
                "statusCode": statusCode,
            },
            nil,
        )
    }

    headers := response.Headers()
    if nil != headers {
        /* a key the response names is owned by the response: the writer's values for it are
           replaced rather than appended to, so a header both sides set — the request id the kernel
           puts on the raw writer, and any header a kernel.response listener sets on the response —
           reaches the client once instead of twice. Keys the response does not name keep whatever
           the writer already carries. */
        for key, values := range headers {
            responseWriter.Header().Del(key)
            for _, value := range values {
                responseWriter.Header().Add(key, value)
            }
        }
    }

    responseWriter.WriteHeader(statusCode)

    bodyReader := response.BodyReader()
    if nil == bodyReader {
        return nil
    }

    if closer, ok := bodyReader.(io.Closer); true == ok {
        defer func(closer io.Closer) {
            err := closer.Close()
            if nil != err {
                logger := logging.LoggerFromRuntime(runtimeInstance)
                if nil != logger {
                    logger.Error(
                        "failed to close response body reader",
                        exception.LogContext(err),
                    )
                }
            }
        }(closer)
    }

    if nil != request && nil != request.HttpRequest() && nethttp.MethodHead == request.HttpRequest().Method {
        return nil
    }

    _, err := io.Copy(responseWriter, bodyReader)
    if nil != err {
        return err
    }

    return nil
}

/* headerCommitRecorder reports whether the response headers were already committed, so writeResponse can skip writing over a stream a handler committed itself. */
type headerCommitRecorder interface {
    HeadersWritten() bool
}

/* sessionPersistenceRecorder lets writeResponse persist the session at most once per request, so the panic-recovery path re-entering writeResponse does not save it a second time. */
type sessionPersistenceRecorder interface {
    SessionPersisted() bool
    MarkSessionPersisted()
}

/* committedStatusRecorder reports the status code actually committed on the connection, so the terminate event and the access log can name the status the client received when a handler streamed its own response instead of the synthetic response the kernel substituted for it. Zero means no status was committed through the recorder — nothing was written, or the connection was hijacked and left http entirely. */
type committedStatusRecorder interface {
    CommittedStatusCode() int
}

/* recordingResponseWriter tracks whether the response headers were already committed, so the kernel can tell a handler that streamed its own response (for example a hand-rolled streaming or download handler) apart from one that returned no response and expects the default 204. */
type recordingResponseWriter struct {
    nethttp.ResponseWriter
    wroteHeader      bool
    statusCode       int
    sessionPersisted bool
}

func newRecordingResponseWriter(responseWriter nethttp.ResponseWriter) *recordingResponseWriter {
    return &recordingResponseWriter{
        ResponseWriter: responseWriter,
    }
}

/* WriteHeader raises the commit flag only after the delegate returns: the delegate panics on a status code outside [100, 999] before anything reaches the connection, and a flag raised first recorded a commit that never happened — the recovery then read the response as a committed stream, skipped writing its 500, and the client received an implicit empty 200 for a handler bug. */
func (instance *recordingResponseWriter) WriteHeader(statusCode int) {
    instance.ResponseWriter.WriteHeader(statusCode)
    instance.wroteHeader = true
    instance.statusCode = statusCode
}

/* Write raises the flag after the delegate returns, error or not: a write the client disconnected under has still committed the implicit header. */
func (instance *recordingResponseWriter) Write(data []byte) (int, error) {
    written, writeErr := instance.ResponseWriter.Write(data)
    instance.recordImplicitCommit()

    return written, writeErr
}

/* Flush is forwarded so the wrapper keeps satisfying http.Flusher, which streaming handlers rely on; a flush commits the response, so it also records that the headers were written — after the delegate returns, the convention every commit recording in this type follows. */
func (instance *recordingResponseWriter) Flush() {
    flusher, isFlusher := instance.ResponseWriter.(nethttp.Flusher)
    if true == isFlusher {
        flusher.Flush()
        instance.recordImplicitCommit()
    }
}

/* recordImplicitCommit records a commit that reached the connection without an explicit WriteHeader: net/http writes an implicit 200 ahead of the first byte, so the recorded status is 200 unless an earlier explicit call already named one. */
func (instance *recordingResponseWriter) recordImplicitCommit() {
    instance.wroteHeader = true
    if 0 == instance.statusCode {
        instance.statusCode = nethttp.StatusOK
    }
}

func (instance *recordingResponseWriter) HeadersWritten() bool {
    return instance.wroteHeader
}

/* CommittedStatusCode answers the status the connection actually carries: the explicitly written one, or the implicit 200 recorded with the first byte. Zero means nothing was committed through this recorder — a hijacked connection included, whose bytes leave http entirely. */
func (instance *recordingResponseWriter) CommittedStatusCode() int {
    return instance.statusCode
}

/* SessionPersisted reports whether the session for this request was already persisted by an earlier writeResponse call, so a second call (for example the panic-recovery path re-entering writeResponse after the first write committed the session but then failed) does not save the session a second time. */
func (instance *recordingResponseWriter) SessionPersisted() bool {
    return instance.sessionPersisted
}

func (instance *recordingResponseWriter) MarkSessionPersisted() {
    instance.sessionPersisted = true
}

/* Hijack is forwarded so the wrapper keeps satisfying http.Hijacker, which connection-upgrade handlers (for example WebSocket) rely on; only a successful hijack counts as committing the response, so a failed hijack still lets the kernel write a default response rather than leaving the client with nothing. Under HTTP/2 the underlying writer is not an http.Hijacker, so the assertion against the wrapper is optimistic: the capability probe succeeds but this call returns an error, which connection-upgrade handlers already handle the same way they would a missing capability. */
func (instance *recordingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    hijacker, isHijacker := instance.ResponseWriter.(nethttp.Hijacker)
    if false == isHijacker {
        return nil, nil, exception.NewError("response writer does not support hijacking", nil, nil)
    }

    connection, readWriter, hijackErr := hijacker.Hijack()
    if nil == hijackErr {
        instance.wroteHeader = true
    }

    return connection, readWriter, hijackErr
}

/* ReadFrom is forwarded so the wrapper keeps satisfying io.ReaderFrom, preserving the underlying writer's sendfile fast path for file responses. The commit is recorded only after the copy and only when a byte actually reached the delegate: a source that fails before the first byte has committed nothing, and a flag raised ahead of the copy classified exactly that failure as a committed stream, so the recovery skipped its 500 and the client received an implicit empty 200. The recording rides a defer so a source that panics mid-copy unwinds through it; the copy's own count is then still zero, and the recovery's rewrite over the partially committed stream is absorbed by the delegate's superfluous-WriteHeader guard. */
func (instance *recordingResponseWriter) ReadFrom(reader io.Reader) (written int64, copyErr error) {
    defer func() {
        if 0 < written {
            instance.recordImplicitCommit()
        }
    }()

    written, copyErr = io.Copy(instance.ResponseWriter, reader)

    return written, copyErr
}

/* Unwrap exposes the underlying writer so http.ResponseController can reach its flush/hijack/deadline support through the wrapper. http.Pusher is intentionally not forwarded: HTTP/2 server push is deprecated and disabled by mainstream browsers, so a handler probing the wrapper sees no push support rather than a capability that would have to fail in practice. */
func (instance *recordingResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

var _ headerCommitRecorder = (*recordingResponseWriter)(nil)
var _ sessionPersistenceRecorder = (*recordingResponseWriter)(nil)
