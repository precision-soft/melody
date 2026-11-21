package outbox

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

func expectErrorNamingField(t *testing.T, err error, field string) {
    t.Helper()

    if nil == err {
        t.Fatalf("expected a refusal naming %s, got none", field)
    }

    if field != exception.LogContext(err, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the refusal to name %s, got %v: %v", field, exception.LogContext(err, exception.LogContextConfig{}), err)
    }
}

func expectPanicNamingField(t *testing.T, field string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a refusal naming %s, got no error panic", field)
        }

        expectErrorNamingField(t, recoveredErr, field)
    }()

    callback()
}

/* mustCreateLock answers the lock a test's CreateLock made, a refusal failing the test where it happened */
func mustCreateLock(lock lockcontract.Lock, createErr error) lockcontract.Lock {
    if nil != createErr {
        panic(createErr)
    }

    return lock
}

/* outboxTestRuntime answers a run's runtime over ctx and an empty container's scope, the runtime a repository door takes */
func outboxTestRuntime(ctx context.Context) containercontract.Runtime {
    return runtime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}
