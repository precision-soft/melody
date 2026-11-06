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

    /* refused before anything is written: WriteHeader would panic and a later response must not inherit this Set-Cookie */
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
        /* Set-Cookie is appended, since each line is a separate cookie; other named keys replace the writer's values */
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

/* WriteHeader raises the commit flag only after the delegate returns, since the delegate panics on an invalid status */
func (instance *recordingResponseWriter) WriteHeader(statusCode int) {
    instance.ResponseWriter.WriteHeader(statusCode)
    if true == instance.wroteHeader {
        return
    }

    if nethttp.StatusOK > statusCode && nethttp.StatusSwitchingProtocols != statusCode {
        return
    }

    instance.wroteHeader = true
    instance.statusCode = statusCode
}

/* Write raises the flag after the delegate returns, error or not: a failed write has still committed the implicit header. */
func (instance *recordingResponseWriter) Write(data []byte) (int, error) {
    written, writeErr := instance.ResponseWriter.Write(data)
    instance.recordImplicitCommit()

    return written, writeErr
}

func (instance *recordingResponseWriter) Flush() {
    /* a flush that reached a flusher committed the header even when its write failed; only ErrNotSupported flushed nothing */
    flushErr := nethttp.NewResponseController(instance.ResponseWriter).Flush()
    if false == exception.ChainIs(flushErr, nethttp.ErrNotSupported) {
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

func (instance *recordingResponseWriter) CommittedStatusCode() int {
    return instance.statusCode
}

func (instance *recordingResponseWriter) SessionPersisted() bool {
    return instance.sessionPersisted
}

func (instance *recordingResponseWriter) MarkSessionPersisted() {
    instance.sessionPersisted = true
}

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

func (instance *recordingResponseWriter) ReadFrom(reader io.Reader) (written int64, copyErr error) {
    defer func() {
        if 0 < written {
            instance.recordImplicitCommit()
        }
    }()

    written, copyErr = io.Copy(instance.ResponseWriter, reader)

    return written, copyErr
}

/* Unwrap exposes the writer to http.ResponseController; http.Pusher is deliberately not forwarded */
func (instance *recordingResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

var _ headerCommitRecorder = (*recordingResponseWriter)(nil)
var _ sessionPersistenceRecorder = (*recordingResponseWriter)(nil)
var _ committedStatusRecorder = (*recordingResponseWriter)(nil)
