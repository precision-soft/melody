package http

import (
    "bufio"
    "context"
    "io"
    "net"
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/logging"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

func newTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger())

    return runtime.New(context.Background(), scope, serviceContainer)
}

type writeHeaderCountingResponseWriter struct {
    nethttp.ResponseWriter
    writeHeaderCount int
}

func (instance *writeHeaderCountingResponseWriter) WriteHeader(statusCode int) {
    instance.writeHeaderCount = instance.writeHeaderCount + 1
    instance.ResponseWriter.WriteHeader(statusCode)
}

func (instance *writeHeaderCountingResponseWriter) Flush() {
    flusher, isFlusher := instance.ResponseWriter.(nethttp.Flusher)
    if true == isFlusher {
        flusher.Flush()
    }
}

type hijackableResponseWriter struct {
    nethttp.ResponseWriter
    hijacked bool
}

func (instance *hijackableResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    instance.hijacked = true

    serverConnection, clientConnection := net.Pipe()
    _ = clientConnection.Close()

    return serverConnection, bufio.NewReadWriter(bufio.NewReader(serverConnection), bufio.NewWriter(serverConnection)), nil
}

type failingHijackResponseWriter struct {
    nethttp.ResponseWriter
    writeHeaderCount int
}

func (instance *failingHijackResponseWriter) WriteHeader(statusCode int) {
    instance.writeHeaderCount = instance.writeHeaderCount + 1
    instance.ResponseWriter.WriteHeader(statusCode)
}

func (instance *failingHijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    return nil, nil, nethttp.ErrHijacked
}

type nonFlushingResponseWriter struct {
    header     nethttp.Header
    statusCode int
}

func (instance *nonFlushingResponseWriter) Header() nethttp.Header {
    if nil == instance.header {
        instance.header = make(nethttp.Header)
    }

    return instance.header
}

func (instance *nonFlushingResponseWriter) Write(data []byte) (int, error) {
    return len(data), nil
}

func (instance *nonFlushingResponseWriter) WriteHeader(statusCode int) {
    instance.statusCode = statusCode
}

type closeRecordingReadCloser struct {
    closeCount int
}

func (instance *closeRecordingReadCloser) Read(buffer []byte) (int, error) {
    return 0, io.EOF
}

func (instance *closeRecordingReadCloser) Close() error {
    instance.closeCount = instance.closeCount + 1

    return nil
}

type closeRecordingScope struct {
    containercontract.Scope
    failOverride bool
    closed       bool
}

func (instance *closeRecordingScope) OverrideProtectedInstance(serviceName string, value any) error {
    if true == instance.failOverride {
        return exception.NewError("forced override failure", nil, nil)
    }

    return instance.Scope.OverrideProtectedInstance(serviceName, value)
}

func (instance *closeRecordingScope) Close() error {
    instance.closed = true

    return instance.Scope.Close()
}

type scopeRecordingContainer struct {
    containercontract.Container
    failOverride bool
    scope        *closeRecordingScope
}

func (instance *scopeRecordingContainer) NewScope() containercontract.Scope {
    instance.scope = &closeRecordingScope{
        Scope:        instance.Container.NewScope(),
        failOverride: instance.failOverride,
    }

    return instance.scope
}

type writeFailingResponseWriter struct {
    header     nethttp.Header
    statusCode int
}

func (instance *writeFailingResponseWriter) Header() nethttp.Header {
    if nil == instance.header {
        instance.header = make(nethttp.Header)
    }

    return instance.header
}

func (instance *writeFailingResponseWriter) Write(data []byte) (int, error) {
    return 0, exception.NewError("forced write failure", nil, nil)
}

func (instance *writeFailingResponseWriter) WriteHeader(statusCode int) {
    instance.statusCode = statusCode
}

type countingSessionStorage struct {
    sessioncontract.Storage
    saveCount int
}

func (instance *countingSessionStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    instance.saveCount = instance.saveCount + 1

    return instance.Storage.Save(sessionId, data, ttl)
}
