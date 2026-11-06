/* the one test file of this package allowed to exist without a matching source */
package http

import (
    "bufio"
    "context"
    "io"
    "net"
    nethttp "net/http"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
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
    onClose func()
}

func (instance *closeRecordingScope) OverrideProtectedInstance(serviceName string, value any) error {
    if true == instance.failOverride {
        return exception.NewError("forced override failure", nil, nil)
    }

    return instance.Scope.OverrideProtectedInstance(serviceName, value)
}

func (instance *closeRecordingScope) Close() error {
    instance.closed = true

    if nil != instance.onClose {
        instance.onClose()
    }

    return instance.Scope.Close()
}

type scopeRecordingContainer struct {
    containercontract.Container
    failOverride bool
    onClose      func()
    scope        *closeRecordingScope
}

func (instance *scopeRecordingContainer) NewScope() containercontract.Scope {
    instance.scope = &closeRecordingScope{
        Scope:        instance.Container.NewScope(),
        failOverride: instance.failOverride,
        onClose:      instance.onClose,
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

type capturedRecord struct {
    level   loggingcontract.Level
    message string
    context loggingcontract.Context
}

type recordsCaptureLogger struct {
    mutex   sync.Mutex
    records []capturedRecord
}

func (instance *recordsCaptureLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, capturedRecord{level: level, message: message, context: context})
}

func (instance *recordsCaptureLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *recordsCaptureLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *recordsCaptureLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *recordsCaptureLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *recordsCaptureLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *recordsCaptureLogger) failureRecords() []capturedRecord {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    selected := make([]capturedRecord, 0, len(instance.records))
    for _, record := range instance.records {
        if loggingcontract.LevelWarning == record.level || loggingcontract.LevelError == record.level || loggingcontract.LevelEmergency == record.level {
            selected = append(selected, record)
        }
    }

    return selected
}

var _ loggingcontract.Logger = (*recordsCaptureLogger)(nil)

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

type cyclicBodyReader struct{}

func (instance *cyclicBodyReader) Read(buffer []byte) (int, error) {
    return 0, newCyclicChainError()
}
