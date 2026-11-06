package http

import (
    "bufio"
    "io"
    "net"
    nethttp "net/http"

    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
    "github.com/precision-soft/melody/logging"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func WriteToHttpResponseWriter(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    responseWriter nethttp.ResponseWriter,
    response httpcontract.Response,
) error {
    /* read through, not compared: a typed nil response passes a plain comparison and dereferences on Headers() below */
    if true == internal.IsNilInterface(response) {
        return nil
    }

    statusCode := response.StatusCode()
    if 0 == statusCode {
        statusCode = nethttp.StatusOK
    }

    /* validated ahead of the first mutation: WriteHeader panics on it, and a refused response's Set-Cookie must not go out */
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
        /* a key the response names replaces the writer's values, except Set-Cookie, where each line is a separate cookie */
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

/* WriteHeader raises the commit flag after the delegate returns: the delegate panics on a bad status ahead of any commit. */
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
    /* a flush that reached a flusher committed the header even on a failed write; only ErrNotSupported means nothing flushed */
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

/* Hijack counts only a success as a commit, so after a failed hijack the kernel can still write a default response. */
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

/* ReadFrom records the commit after the copy and only when a byte reached the delegate, leaving the recovery its 500. */
func (instance *recordingResponseWriter) ReadFrom(reader io.Reader) (written int64, copyErr error) {
    defer func() {
        if 0 < written {
            instance.recordImplicitCommit()
        }
    }()

    written, copyErr = io.Copy(instance.ResponseWriter, reader)

    return written, copyErr
}

/* Unwrap exposes the writer to http.ResponseController; http.Pusher is intentionally not forwarded. */
func (instance *recordingResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

var _ headerCommitRecorder = (*recordingResponseWriter)(nil)
var _ sessionPersistenceRecorder = (*recordingResponseWriter)(nil)
