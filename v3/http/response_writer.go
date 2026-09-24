package http

import (
    "bufio"
    "errors"
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
    /* the interface is read through, not compared, so a typed-nil response is refused rather than dereferenced */
    if true == internal.IsNilInterface(response) {
        return nil
    }

    statusCode := response.StatusCode()
    if 0 == statusCode {
        statusCode = nethttp.StatusOK
    }

    /* a status outside net/http's [100, 999] is refused before anything is written, the headers included, since WriteHeader would panic and a caller writing its own response after the error must not inherit this one's Set-Cookie */
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
        /* a key the response names replaces the writer's values for it, so a header both sides set reaches the client once; keys it does not name keep the writer's values. Set-Cookie is appended, since each line is a separate cookie. */
        for key, values := range headers {
            if "Set-Cookie" != nethttp.CanonicalHeaderKey(key) {
                responseWriter.Header().Del(key)
            }

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

    if false == internal.IsNilInterface(request) && nil != request.HttpRequest() && nethttp.MethodHead == request.HttpRequest().Method {
        return nil
    }

    _, err := io.Copy(responseWriter, bodyReader)
    if nil != err {
        return err
    }

    return nil
}

/* headerCommitRecorder reports whether the response headers were already committed. */
type headerCommitRecorder interface {
    HeadersWritten() bool
}

/* sessionPersistenceRecorder lets writeResponse persist the session at most once per request. */
type sessionPersistenceRecorder interface {
    SessionPersisted() bool
    MarkSessionPersisted()
}

/* committedStatusRecorder reports the status committed on the connection; zero means none was committed through the recorder, a hijacked connection included. */
type committedStatusRecorder interface {
    CommittedStatusCode() int
}

/* recordingResponseWriter records what the delegate committed, so the kernel can tell a response it still owns from one already on the wire. Its fields are written and read on the serving goroutine only and carry no lock; the server-sent-event writer serializes its own frames. */
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

/* WriteHeader raises the commit flag only after the delegate returns, since the delegate panics on an invalid status before anything is written. */
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

/* Flush flushes through a ResponseController, so it reaches the connection through any wrapper that implements Unwrap, and records the commit after it returns. */
func (instance *recordingResponseWriter) Flush() {
    /* a flush that reached a flusher committed the header even when its write failed; only ErrNotSupported means nothing was flushed */
    flushErr := nethttp.NewResponseController(instance.ResponseWriter).Flush()
    if false == errors.Is(flushErr, nethttp.ErrNotSupported) {
        instance.recordImplicitCommit()
    }
}

/* recordImplicitCommit records a commit that reached the connection without an explicit WriteHeader, as the implicit 200 unless an explicit status was named first. */
func (instance *recordingResponseWriter) recordImplicitCommit() {
    instance.wroteHeader = true
    if 0 == instance.statusCode {
        instance.statusCode = nethttp.StatusOK
    }
}

func (instance *recordingResponseWriter) HeadersWritten() bool {
    return instance.wroteHeader
}

/* CommittedStatusCode answers the status the connection carries: the explicit one, or the implicit 200 recorded with the first byte. Zero means nothing was committed through this recorder, a hijacked connection included. */
func (instance *recordingResponseWriter) CommittedStatusCode() int {
    return instance.statusCode
}

/* SessionPersisted reports whether an earlier writeResponse call already persisted the session for this request. */
func (instance *recordingResponseWriter) SessionPersisted() bool {
    return instance.sessionPersisted
}

func (instance *recordingResponseWriter) MarkSessionPersisted() {
    instance.sessionPersisted = true
}

/* Hijack is forwarded for connection upgrades; only a successful hijack counts as a commit. Under HTTP/2 the call returns an error. */
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

/* ReadFrom is forwarded to keep the sendfile fast path. The commit is recorded after the copy, only when a byte reached the delegate, in a defer so a source that panics mid-copy unwinds through it. */
func (instance *recordingResponseWriter) ReadFrom(reader io.Reader) (written int64, copyErr error) {
    defer func() {
        if 0 < written {
            instance.recordImplicitCommit()
        }
    }()

    written, copyErr = io.Copy(instance.ResponseWriter, reader)

    return written, copyErr
}

/* Unwrap exposes the underlying writer to http.ResponseController. http.Pusher is deliberately not forwarded. */
func (instance *recordingResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

var _ headerCommitRecorder = (*recordingResponseWriter)(nil)
var _ sessionPersistenceRecorder = (*recordingResponseWriter)(nil)
var _ committedStatusRecorder = (*recordingResponseWriter)(nil)
