package http

import (
    "bufio"
    "io"
    "net"
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func WriteToHttpResponseWriter(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    responseWriter nethttp.ResponseWriter,
    response httpcontract.Response,
) error {
    if nil == response {
        return nil
    }

    headers := response.Headers()
    if nil != headers {
        for key, values := range headers {
            for _, value := range values {
                responseWriter.Header().Add(key, value)
            }
        }
    }

    statusCode := response.StatusCode()
    if 0 == statusCode {
        statusCode = nethttp.StatusOK
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

/* @info headerCommitRecorder reports whether the response headers were already committed, so writeResponse can skip writing over a stream a handler committed itself. */
type headerCommitRecorder interface {
    HeadersWritten() bool
}

/* @info sessionPersistenceRecorder lets writeResponse persist the session at most once per request, so the panic-recovery path re-entering writeResponse does not save it a second time. */
type sessionPersistenceRecorder interface {
    SessionPersisted() bool
    MarkSessionPersisted()
}

/* @info recordingResponseWriter tracks whether the response headers were already committed, so the kernel can tell a handler that streamed its own response (for example Server-Sent Events) apart from one that returned no response and expects the default 204. */
type recordingResponseWriter struct {
    nethttp.ResponseWriter
    wroteHeader      bool
    sessionPersisted bool
}

func newRecordingResponseWriter(responseWriter nethttp.ResponseWriter) *recordingResponseWriter {
    return &recordingResponseWriter{
        ResponseWriter: responseWriter,
    }
}

func (instance *recordingResponseWriter) WriteHeader(statusCode int) {
    instance.wroteHeader = true
    instance.ResponseWriter.WriteHeader(statusCode)
}

func (instance *recordingResponseWriter) Write(data []byte) (int, error) {
    instance.wroteHeader = true

    return instance.ResponseWriter.Write(data)
}

/* @info Flush is forwarded so the wrapper keeps satisfying http.Flusher, which streaming handlers such as Server-Sent Events rely on; a flush commits the response, so it also records that the headers were written. */
func (instance *recordingResponseWriter) Flush() {
    flusher, isFlusher := instance.ResponseWriter.(nethttp.Flusher)
    if true == isFlusher {
        instance.wroteHeader = true
        flusher.Flush()
    }
}

func (instance *recordingResponseWriter) HeadersWritten() bool {
    return instance.wroteHeader
}

/* @info SessionPersisted reports whether the session for this request was already persisted by an earlier writeResponse call, so a second call (for example the panic-recovery path re-entering writeResponse after the first write committed the session but then failed) does not save the session a second time. */
func (instance *recordingResponseWriter) SessionPersisted() bool {
    return instance.sessionPersisted
}

func (instance *recordingResponseWriter) MarkSessionPersisted() {
    instance.sessionPersisted = true
}

/* @info Hijack is forwarded so the wrapper keeps satisfying http.Hijacker, which connection-upgrade handlers (for example WebSocket) rely on; only a successful hijack counts as committing the response, so a failed hijack still lets the kernel write a default response rather than leaving the client with nothing. Under HTTP/2 the underlying writer is not an http.Hijacker, so the assertion against the wrapper is optimistic: the capability probe succeeds but this call returns an error, which connection-upgrade handlers already handle the same way they would a missing capability. */
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

/* @info ReadFrom is forwarded so the wrapper keeps satisfying io.ReaderFrom, preserving the underlying writer's sendfile fast path for file responses. */
func (instance *recordingResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
    instance.wroteHeader = true

    return io.Copy(instance.ResponseWriter, reader)
}

/* @info Unwrap exposes the underlying writer so http.ResponseController can reach its flush/hijack/deadline support through the wrapper. http.Pusher is intentionally not forwarded: HTTP/2 server push is deprecated and disabled by mainstream browsers, so a handler probing the wrapper sees no push support rather than a capability that would have to fail in practice. */
func (instance *recordingResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

var _ headerCommitRecorder = (*recordingResponseWriter)(nil)
var _ sessionPersistenceRecorder = (*recordingResponseWriter)(nil)
