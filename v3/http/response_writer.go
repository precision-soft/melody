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

    if true == internal.IsNilInterface(response) {
        return nil
    }

    statusCode := response.StatusCode()
    if 0 == statusCode {
        statusCode = nethttp.StatusOK
    }

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

type headerCommitRecorder interface {
    HeadersWritten() bool
}

type sessionPersistenceRecorder interface {
    SessionPersisted() bool
    MarkSessionPersisted()
}

type committedStatusRecorder interface {
    CommittedStatusCode() int
}

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

/* Flush forwards through http.ResponseController, including delegate Unwrap chains, and records header commitment after the delegate returns. */
func (instance *recordingResponseWriter) Flush() {
    flushErr := nethttp.NewResponseController(instance.ResponseWriter).Flush()
    if nil == flushErr {
        instance.recordImplicitCommit()
    }
}

func (instance *recordingResponseWriter) recordImplicitCommit() {
    instance.wroteHeader = true
    if 0 == instance.statusCode {
        instance.statusCode = nethttp.StatusOK
    }
}

func (instance *recordingResponseWriter) HeadersWritten() bool {
    return instance.wroteHeader
}

/* CommittedStatusCode returns the explicit status or the implicit 200 recorded on the first byte. Zero means no commitment through this recorder, including a hijacked connection. */
func (instance *recordingResponseWriter) CommittedStatusCode() int {
    return instance.statusCode
}

/* SessionPersisted reports a completed session persistence step so a reentrant response-write path does not save it twice. */
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

/* ReadFrom delegates copying to preserve io.ReaderFrom fast paths and records commitment only when the copy reports written bytes. A panic inside the copy can prevent its byte count from returning; the underlying writer still controls its actual committed response. */
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
var _ committedStatusRecorder = (*recordingResponseWriter)(nil)
