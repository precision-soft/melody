package lock

import (
    "context"
    "fmt"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* recordingLogger captures every record with its level and message, so a test can assert both that a record was written and what it said. */
type recordingLogger struct {
    mutex   sync.Mutex
    records []recordedLogEntry
}

type recordedLogEntry struct {
    level   string
    message string
    context loggingcontract.Context
}

func (instance *recordingLogger) record(level string, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, recordedLogEntry{level: level, message: message, context: context})
}

func (instance *recordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.record("log", message, context)
}

func (instance *recordingLogger) Debug(message string, context loggingcontract.Context) {
    instance.record("debug", message, context)
}

func (instance *recordingLogger) Info(message string, context loggingcontract.Context) {
    instance.record("info", message, context)
}

func (instance *recordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.record("warning", message, context)
}

func (instance *recordingLogger) Error(message string, context loggingcontract.Context) {
    instance.record("error", message, context)
}

func (instance *recordingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.record("emergency", message, context)
}

func (instance *recordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *recordingLogger) CauseChainDepth() int {
    return 8
}

func (instance *recordingLogger) hasMessageContaining(fragment string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, entry := range instance.records {
        if true == strings.Contains(entry.message, fragment) {
            return true
        }
    }

    return false
}

/* runtimeWithRecordingLogger builds a runtime whose scope carries the recording logger under the logger service, mirroring how a framework-assembled scope resolves logging.LoggerFromRuntime. */
func runtimeWithRecordingLogger(ctx context.Context) (containercontract.Runtime, *recordingLogger) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()

    logger := &recordingLogger{}
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    return runtime.NewRun(ctx, scope), logger
}

/* sessionStyleLocker stands in for the MySQL and PostgreSQL lockers, which hold until their connection drops and ignore the ttl, unlimited included: it hands the in-memory store an absent ttl whatever it is given. */
type sessionStyleLocker struct {
    inner lockcontract.Locker
}

func (instance *sessionStyleLocker) CreateLock(name string, ttl bound.Duration) (lockcontract.Lock, error) {
    return instance.inner.CreateLock(name, bound.Duration{})
}

/* ttlRecordingLocker records the ttl every CreateLock is handed before it reaches the inner locker, so a test can pin what a door resolved its configuration to. */
type ttlRecordingLocker struct {
    inner lockcontract.Locker
    mutex sync.Mutex
    ttls  []bound.Duration
}

func (instance *ttlRecordingLocker) CreateLock(name string, ttl bound.Duration) (lockcontract.Lock, error) {
    instance.mutex.Lock()
    instance.ttls = append(instance.ttls, ttl)
    instance.mutex.Unlock()

    return instance.inner.CreateLock(name, ttl)
}

func (instance *ttlRecordingLocker) firstTtl(t *testing.T) bound.Duration {
    t.Helper()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if 0 == len(instance.ttls) {
        t.Fatal("expected a lock to be created")
    }

    return instance.ttls[0]
}

/* mustCreateLock answers the lock a test's CreateLock made, a refusal failing the test where it happened */
func mustCreateLock(lock lockcontract.Lock, createErr error) lockcontract.Lock {
    if nil != createErr {
        panic(createErr)
    }

    return lock
}

/* noLockLocker answers neither a lock nor an error, the slip a locker of the application's own can make */
type noLockLocker struct{}

func (instance *noLockLocker) CreateLock(name string, ttl bound.Duration) (lockcontract.Lock, error) {
    return nil, nil
}

/* namesNoLock reports whether a refusal's cause chain carries the no-lock refusal */
func namesNoLock(err error) bool {
    return nil != err && true == strings.Contains(fmt.Sprint(exception.LogContext(err, exception.LogContextConfig{})), "the locker answered no lock")
}

type namespaceEnvironmentSource struct {
    namespace string
}

func (instance *namespaceEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return map[string]string{config.NamespaceKey: instance.namespace}, nil
}

/* runtimeWithNamespace answers a runtime whose configuration service reads MELODY_NAMESPACE as the namespace given. */
func runtimeWithNamespace(t *testing.T, namespace string) containercontract.Runtime {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            environment, environmentErr := config.NewEnvironment(context.Background(), &namespaceEnvironmentSource{namespace: namespace})
            if nil != environmentErr {
                return nil, environmentErr
            }

            return config.NewConfiguration(environment, t.TempDir(), config.ConfigurationConfig{})
        },
        container.RegisterOptions{},
    )

    return runtime.NewRun(context.Background(), serviceContainer.NewScope())
}
