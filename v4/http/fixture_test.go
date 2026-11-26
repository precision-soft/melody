package http

import (
    "bufio"
    "context"
    "errors"
    "io"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bag"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

func newTestRuntime() containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger(), container.OverrideOptions{})

    return runtime.NewRequest(context.Background(), scope)
}

func newDebugTestRuntime() containercontract.Runtime {
    serviceContainer := newHttpTestContainer()
    scope := serviceContainer.NewScope()

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger(), container.OverrideOptions{})

    return runtime.NewRequest(context.Background(), scope)
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

func (instance *closeRecordingScope) OverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) error {
    if true == instance.failOverride {
        return exception.NewError("forced override failure", nil, nil)
    }

    return instance.Scope.OverrideProtectedInstance(serviceName, value, options)
}

func (instance *closeRecordingScope) CloseWithContext(closeContext context.Context) error {
    instance.closed = true

    if nil != instance.onClose {
        instance.onClose()
    }

    return instance.Scope.CloseWithContext(closeContext)
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

func (instance *countingSessionStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    instance.saveCount = instance.saveCount + 1

    return instance.Storage.Save(runtimeInstance, sessionId, data, ttl)
}

type exceptionListenerCaptureLogger struct {
    errorCalls       int
    warningCalls     int
    lastMessage      string
    lastContext      map[string]any
    lastErrorContext map[string]any
}

func (instance *exceptionListenerCaptureLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    if loggingcontract.LevelError == level {
        instance.errorCalls++
        instance.lastErrorContext = context
    }
    if loggingcontract.LevelWarning == level {
        instance.warningCalls++
    }
    instance.lastMessage = message
    instance.lastContext = context
}

func (instance *exceptionListenerCaptureLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *exceptionListenerCaptureLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *exceptionListenerCaptureLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *exceptionListenerCaptureLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *exceptionListenerCaptureLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *exceptionListenerCaptureLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *exceptionListenerCaptureLogger) CauseChainDepth() int {
    return 8
}

var _ loggingcontract.Logger = (*exceptionListenerCaptureLogger)(nil)

func newExceptionListenerTestRuntimeWithLogger(logger loggingcontract.Logger) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    return runtime.NewRequest(context.Background(), scope)
}

type errorContextRecordingLogger struct {
    mutex         sync.Mutex
    errorMessages []string
    errorContexts []loggingcontract.Context
}

func (instance *errorContextRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *errorContextRecordingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *errorContextRecordingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *errorContextRecordingLogger) Warning(message string, context loggingcontract.Context) {
}

func (instance *errorContextRecordingLogger) Error(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.errorMessages = append(instance.errorMessages, message)
    instance.errorContexts = append(instance.errorContexts, context)
}

func (instance *errorContextRecordingLogger) Emergency(message string, context loggingcontract.Context) {
}

func (instance *errorContextRecordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *errorContextRecordingLogger) CauseChainDepth() int {
    return 8
}

func (instance *errorContextRecordingLogger) errorContextFor(message string) (loggingcontract.Context, bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for index, loggedMessage := range instance.errorMessages {
        if message == loggedMessage {
            return instance.errorContexts[index], true
        }
    }

    return nil, false
}

var _ loggingcontract.Logger = (*errorContextRecordingLogger)(nil)

func newHttpTestContainer() containercontract.Container {
    return newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage(session.InMemoryStorageConfig{}))
}

func newHttpTestContainerInEnvironment(environment string) containercontract.Container {
    return newHttpTestContainerWithSessionStorageAndEnvironmentValues(
        session.NewInMemoryStorage(session.InMemoryStorageConfig{}),
        map[string]string{config.EnvironmentNameKey: environment},
    )
}

func newHttpTestContainerWithSessionStorage(storage sessioncontract.Storage) containercontract.Container {
    return newHttpTestContainerWithSessionStorageAndEnvironmentValues(storage, nil)
}

func newHttpTestContainerWithSessionManager(
    sessionManager sessioncontract.Manager,
) containercontract.Container {
    return newHttpTestContainerWithSessionManagerAndEnvironmentValues(sessionManager, nil)
}

func newHttpTestContainerWithSessionStorageAndEnvironmentValues(
    storage sessioncontract.Storage,
    environmentValues map[string]string,
) containercontract.Container {
    return newHttpTestContainerWithSessionManagerAndEnvironmentValues(
        session.NewManager(storage, session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)}),
        environmentValues,
    )
}

func newHttpTestContainerWithSessionManagerAndEnvironmentValues(
    sessionManager sessioncontract.Manager,
    environmentValues map[string]string,
) containercontract.Container {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            values := map[string]string{
                config.EnvironmentNameKey: config.EnvironmentDevelopment,
            }
            for key, value := range environmentValues {
                values[key] = value
            }

            environment, err := config.NewEnvironment(context.Background(), 
                &testEnvironmentSource{
                    values: values,
                },
            )
            if nil != err {
                return nil, err
            }

            return config.NewConfiguration(environment, "/tmp/melody", config.ConfigurationConfig{})
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return sessionManager, nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()}), nil
        },
        container.RegisterOptions{},
    )

    return serviceContainer
}

func routeRegistryTestHandler() httpcontract.Handler {
    return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return TextResponse(200, "ok"), nil
    }
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

func (instance *recordsCaptureLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *recordsCaptureLogger) CauseChainDepth() int {
    return 8
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

func requireRefusalNamingField(t *testing.T, refusalErr error, expectedMessage string, field string) {
    t.Helper()

    var refusal *exception.Error
    if false == errors.As(refusalErr, &refusal) {
        t.Fatalf("expected the refusal %q, got %v", expectedMessage, refusalErr)
    }

    if expectedMessage != refusal.Message() {
        t.Fatalf("expected the message %q, got %q", expectedMessage, refusal.Message())
    }

    if field != refusal.Context()["field"] {
        t.Fatalf("expected the refusal to name %q, got %v", field, refusal.Context()["field"])
    }
}

func assertPanicsNamingField(t *testing.T, callback func(), expectedMessage string, field string) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a panic with the refusal %q", expectedMessage)
        }

        requireRefusalNamingField(t, recoveredErr, expectedMessage, field)
    }()

    callback()
}

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

func mustSession(sessionInstance sessioncontract.Session, sessionErr error) sessioncontract.Session {
    if nil != sessionErr {
        panic(sessionErr)
    }

    return sessionInstance
}

type contextBlockingSessionStorage struct {
    inner       *session.InMemoryStorage
    blockLoad   atomic.Bool
    blockSave   atomic.Bool
    blockDelete atomic.Bool
    entered     chan string
    saves       atomic.Int32
    deletes     atomic.Int32
}

func newContextBlockingSessionStorage() *contextBlockingSessionStorage {
    return &contextBlockingSessionStorage{
        inner:   session.NewInMemoryStorage(session.InMemoryStorageConfig{}),
        entered: make(chan string, 16),
    }
}

func (instance *contextBlockingSessionStorage) block(ctx context.Context, door string) error {
    instance.entered <- door
    <-ctx.Done()

    return ctx.Err()
}

func (instance *contextBlockingSessionStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    ctx := runtimeInstance.Context()
    if true == instance.blockLoad.Load() {
        return nil, false, instance.block(ctx, "Load")
    }

    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *contextBlockingSessionStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    ctx := runtimeInstance.Context()
    instance.saves.Add(1)

    if true == instance.blockSave.Load() {
        return instance.block(ctx, "Save")
    }

    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *contextBlockingSessionStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    ctx := runtimeInstance.Context()
    instance.deletes.Add(1)

    if true == instance.blockDelete.Load() {
        return instance.block(ctx, "Delete")
    }

    return instance.inner.Delete(runtimeInstance, sessionId)
}

func (instance *contextBlockingSessionStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(closeContext)
}

var _ sessioncontract.Storage = (*contextBlockingSessionStorage)(nil)

func awaitStorageDoor(t *testing.T, entered <-chan string, door string) {
    t.Helper()

    select {
    case reached := <-entered:
        if door != reached {
            t.Fatalf("expected the storage entered at %s, got %s", door, reached)
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("timed out waiting for the storage's %s", door)
    }
}

func awaitClosed(t *testing.T, done <-chan struct{}, what string) {
    t.Helper()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatalf("timed out waiting for %s", what)
    }
}

func seedStoredSession(t *testing.T, manager sessioncontract.Manager) (string, sessioncontract.Session) {
    t.Helper()

    seeded, newErr := manager.NewSession(sessionRuntime())
    if nil != newErr {
        t.Fatalf("unexpected new session error: %v", newErr)
    }

    seeded.Set("userId", "u-1")
    if saveErr := manager.SaveSession(sessionRuntime(), seeded); nil != saveErr {
        t.Fatalf("unexpected seed error: %v", saveErr)
    }

    copyLoaded, loadErr := manager.Session(sessionRuntime(), seeded.Id())
    if nil != loadErr || nil == copyLoaded {
        t.Fatalf("expected the seeded session to load, got %v", loadErr)
    }

    return seeded.Id(), copyLoaded
}

func sessionRuntime() containercontract.Runtime {
    return sessionRuntimeUnder(context.Background())
}

func sessionRuntimeUnder(ctx context.Context) containercontract.Runtime {
    return runtime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}

/* formTestRequestConfig carries the cap a request built without a runtime reads its form under */
func formTestRequestConfig() RequestConfig {
    return RequestConfig{MaxBodyBytes: bound.Of(config.DefaultHttpMaxRequestBodyBytes)}
}

/* countingBody counts the reads a body answers, so a pin tells a body never touched from one read */
type countingBody struct {
    reader io.Reader
    reads  atomic.Int64
}

func newCountingBody(content string) *countingBody {
    return &countingBody{reader: strings.NewReader(content)}
}

func (instance *countingBody) Read(buffer []byte) (int, error) {
    instance.reads.Add(1)

    return instance.reader.Read(buffer)
}

func (instance *countingBody) Close() error {
    return nil
}

/* newFormRoutedKernel serves POST /form under the given options and POST /other under none, each handler answering the form read's error or the length of the field it read */
func newFormRoutedKernel(t *testing.T, applicationMaxBodyBytes int, options RouteOptions, environment string) (nethttp.Handler, *recordsCaptureLogger) {
    t.Helper()

    formHandler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        postBag, formErr := request.Post()
        if nil != formErr {
            return nil, formErr
        }

        value, _, _ := bag.StringAt(postBag, "value", 0)

        return TextResponse(nethttp.StatusOK, strconv.Itoa(len(value))), nil
    }

    router := NewRouter()
    options.Methods = []string{nethttp.MethodPost}
    router.Handle("/form", formHandler, options)
    router.Handle("/other", formHandler, RouteOptions{Methods: []string{nethttp.MethodPost}})

    capture := &recordsCaptureLogger{}
    serviceContainer := newHttpTestContainerWithSessionStorageAndEnvironmentValues(
        session.NewInMemoryStorage(session.InMemoryStorageConfig{}),
        map[string]string{
            config.HttpMaxRequestBodyBytesKey: strconv.Itoa(applicationMaxBodyBytes),
            config.EnvironmentNameKey:         environment,
        },
    )
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    return NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer), capture
}

func serveUrlEncodedForm(handler nethttp.Handler, path string, valueBytes int) *httptest.ResponseRecorder {
    request := httptest.NewRequest(nethttp.MethodPost, path, strings.NewReader("value="+strings.Repeat("a", valueBytes)))
    request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    return recorder
}
