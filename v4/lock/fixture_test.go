package lock

import (
    "context"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
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
func runtimeWithRecordingLogger(ctx context.Context) (runtimecontract.Runtime, *recordingLogger) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()

    logger := &recordingLogger{}
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    return runtime.New(ctx, scope, serviceContainer), logger
}

/* sessionStyleLocker stands in for the MySQL and PostgreSQL lockers, which hold until their connection drops and ignore the ttl, unlimited included: it hands the in-memory store an absent ttl whatever it is given. */
type sessionStyleLocker struct {
    inner lockcontract.Locker
}

func (instance *sessionStyleLocker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
    return instance.inner.CreateLock(name, bound.Duration{})
}

/* ttlRecordingLocker records the ttl every CreateLock is handed before it reaches the inner locker, so a test can pin what a door resolved its configuration to. */
type ttlRecordingLocker struct {
    inner lockcontract.Locker
    mutex sync.Mutex
    ttls  []bound.Duration
}

func (instance *ttlRecordingLocker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
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
