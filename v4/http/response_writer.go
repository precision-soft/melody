package http

import (
    "bufio"
    "io"
    "net"
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func WriteToHttpResponseWriter(
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
    responseWriter nethttp.ResponseWriter,
    response httpcontract.Response,
) error {
    /* read through, not compared, so a typed-nil response is refused */
    if true == internal.IsNilInterface(response) {
        return nil
    }

    statusCode := response.StatusCode()
    if 0 == statusCode {
        statusCode = nethttp.StatusOK
    }

    /* refused before any header is written: outside [200, 999] panics, and an informational status is not final */
    if false == isFinalStatusCode(statusCode) {
        var logger loggingcontract.Logger
        if false == internal.IsNilInterface(runtimeInstance) {
            logger = logging.LoggerFromRuntime(runtimeInstance)
        }
        closeDiscardedResponseBody(response, logger)

        return exception.NewError(
            "response status code is not a final status",
            map[string]any{
                "statusCode": statusCode,
                "admitted":   "200-999",
            },
            nil,
        )
    }

    headers := response.Headers()
    if nil != headers {
        /* a named key replaces the writer's values; Set-Cookie is appended, each line being a cookie */
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
                        exception.LogContext(err, exception.LogContextConfig{}),
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

/* zero means nothing was committed through the recorder, a hijack included */
type committedStatusRecorder interface {
    CommittedStatusCode() int
}

/* recordingResponseWriter carries no lock: it is used on the serving goroutine only. */
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

/* WriteHeader records the commit after the delegate returns; an informational status other than 101 commits nothing. */
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

/* Write records the commit even on error: a failed write still committed the implicit header. */
func (instance *recordingResponseWriter) Write(data []byte) (int, error) {
    written, writeErr := instance.ResponseWriter.Write(data)
    instance.recordImplicitCommit()

    return written, writeErr
}

/* Flush goes through a ResponseController, so it reaches the connection past any Unwrap wrapper. */
func (instance *recordingResponseWriter) Flush() {
    /* a failed flush still committed the header; only ErrNotSupported flushed nothing */
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

/* CommittedStatusCode is the status on the connection; zero means none was committed through this recorder. */
func (instance *recordingResponseWriter) CommittedStatusCode() int {
    return instance.statusCode
}

func (instance *recordingResponseWriter) SessionPersisted() bool {
    return instance.sessionPersisted
}

func (instance *recordingResponseWriter) MarkSessionPersisted() {
    instance.sessionPersisted = true
}

/* Hijack counts as a commit only when it succeeds; under HTTP/2 it fails. */
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

/* ReadFrom keeps the sendfile path, recording the commit in a defer only when a byte reached the delegate. */
func (instance *recordingResponseWriter) ReadFrom(reader io.Reader) (written int64, copyErr error) {
    defer func() {
        if 0 < written {
            instance.recordImplicitCommit()
        }
    }()

    written, copyErr = io.Copy(instance.ResponseWriter, reader)

    return written, copyErr
}

/* Unwrap exposes the delegate to http.ResponseController; http.Pusher is not forwarded. */
func (instance *recordingResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

var _ headerCommitRecorder = (*recordingResponseWriter)(nil)
var _ sessionPersistenceRecorder = (*recordingResponseWriter)(nil)
var _ committedStatusRecorder = (*recordingResponseWriter)(nil)

/* 101 is not final here: an upgrade writes it on the writer and hijacks */
func isFinalStatusCode(statusCode int) bool {
    return nethttp.StatusOK <= statusCode && 999 >= statusCode
}
