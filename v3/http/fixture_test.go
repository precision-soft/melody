/* The shared test material of this package: the doubles every test file of it reaches for, the helpers that build them, and — where a contract spans every source rather than any one of them — the test that asserts it. It carries no mirror of its own on purpose: it is the ONE test file of a package allowed to exist without a matching source, which is what keeps every other one honest. A test provable from a single source belongs in that source's own mirror, not here. */
package http

import (
    "bufio"
    "context"
    "io"
    "net"
    nethttp "net/http"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/session"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
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
    /* onClose runs at the moment the scope-close defer runs. That defer is registered first, so it runs last: whatever a test reads here has already been decided by every defer above it, which is how the ordering between the early recovery guard and the scope close is asserted rather than assumed. */
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

var _ loggingcontract.Logger = (*exceptionListenerCaptureLogger)(nil)

func newExceptionListenerTestRuntimeWithLogger(logger loggingcontract.Logger) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger)

    return runtime.New(context.Background(), scope, serviceContainer)
}

/* errorContextRecordingLogger captures every Error call with its context, so a test can assert not just that something was logged but what the record carries. Two recovery boundaries of the package file such a record — the kernel's error-handler containment and the json handler's responder containment — so the double belongs to the package rather than to either mirror. */
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

/* the shared containers of the package: a nop logger, a configuration built from the given environment values, the session manager, and the exception listener an application registers at boot. Most of the package's tests build on them, so they live here rather than in the test file that happened to write them first. */
func newHttpTestContainer() containercontract.Container {
    return newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage())
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
        session.NewManager(storage, 30*time.Minute),
        environmentValues,
    )
}

func newHttpTestContainerWithSessionManagerAndEnvironmentValues(
    sessionManager sessioncontract.Manager,
    environmentValues map[string]string,
) containercontract.Container {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
    )

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            values := map[string]string{
                config.EnvKey: config.EnvDevelopment,
            }
            for key, value := range environmentValues {
                values[key] = value
            }

            environment, err := config.NewEnvironment(
                &testEnvironmentSource{
                    values: values,
                },
            )
            if nil != err {
                return nil, err
            }

            return config.NewConfiguration(environment, "/tmp/melody")
        },
    )

    serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return sessionManager, nil
        },
    )

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return event.NewEventDispatcher(clock.NewSystemClock()), nil
        },
    )

    return serviceContainer
}

func routeRegistryTestHandler() httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return TextResponse(200, "ok"), nil
    }
}
