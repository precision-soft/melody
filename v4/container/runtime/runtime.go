package runtime

import (
    "context"
    "sync"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/internal/runtimeseam"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* NewRequest builds the runtime of one http request and binds KindRequest to its scope; a scope bound to another kind panics. */
func NewRequest(ctx context.Context, scope containercontract.Scope) containercontract.Runtime {
    return newBoundRuntime(ctx, scope, containercontract.KindRequest)
}

/* NewRun builds the runtime of one cli run, message delivery or cron tick and binds KindRun to its scope. */
func NewRun(ctx context.Context, scope containercontract.Scope) containercontract.Runtime {
    return newBoundRuntime(ctx, scope, containercontract.KindRun)
}

/* NewChildRun opens a new scope of the container behind the parent's scope and builds a run runtime over it: the runtime of one message delivery or one cron tick. The caller closes the child through child.Scope().Close(). A parent whose scope is closed, or whose scope is not the container's own, answers an error. */
func NewChildRun(parent containercontract.Runtime, ctx context.Context) (containercontract.Runtime, error) {
    if true == internal.IsNilInterface(parent) {
        exception.Panic(
            exception.NewError("parent runtime may not be nil on child run", nil, nil),
        )
    }

    if true == internal.IsNilInterface(ctx) {
        exception.Panic(
            exception.NewError("context may not be nil on runtime", nil, nil),
        )
    }

    if containercontract.KindBoot == parent.Kind() {
        return nil, exception.NewError(
            "the boot runtime opens no child run",
            exceptioncontract.Context{"argument": "parent"},
            containercontract.ErrRuntimeKindRefused,
        )
    }

    spawner, isSpawner := parent.Scope().(runtimeseam.ChildScopeSpawner)
    if false == isSpawner {
        return nil, exception.NewError(
            "the parent runtime's scope cannot open a child scope",
            exceptioncontract.Context{"argument": "parent"},
            nil,
        )
    }

    childScope, spawnErr := spawner.SpawnChildScope(runtimeseam.Token{})
    if nil != spawnErr {
        return nil, spawnErr
    }

    return NewRun(ctx, childScope), nil
}

/* NewBoot opens the runtime of the application's boot over a boot scope of the container: finish closes the scope, then ends the context with ErrBootFinished as its cause, and answers the close's error; a second finish answers nil. */
func NewBoot(ctx context.Context, serviceContainer containercontract.Container) (containercontract.Runtime, func() error) {
    if true == internal.IsNilInterface(ctx) {
        exception.Panic(
            exception.NewError("context may not be nil on runtime", nil, nil),
        )
    }

    if true == internal.IsNilInterface(serviceContainer) {
        exception.Panic(
            exception.NewError("container may not be nil on boot runtime", nil, nil),
        )
    }

    bootContext, cancel := context.WithCancelCause(ctx)
    bootScope := serviceContainer.NewBootScope()
    runtimeInstance := newRuntime(bootContext, bootScope, containercontract.KindBoot)

    var finishOnce sync.Once
    finish := func() error {
        var closeErr error
        finishOnce.Do(func() {
            closeErr = bootScope.CloseWithContext(context.Background())
            cancel(containercontract.ErrBootFinished)
        })

        return closeErr
    }

    return runtimeInstance, finish
}

/* WithContext answers the same runtime over another context: the kind and the scope are kept. */
func WithContext(runtimeInstance containercontract.Runtime, ctx context.Context) containercontract.Runtime {
    if true == internal.IsNilInterface(runtimeInstance) {
        exception.Panic(
            exception.NewError("runtime may not be nil on with context", nil, nil),
        )
    }

    return newRuntime(ctx, runtimeInstance.Scope(), runtimeInstance.Kind())
}

/* a scope of another implementation is not bound; the runtime still answers its own kind */
func newBoundRuntime(ctx context.Context, scope containercontract.Scope, kind containercontract.Kind) containercontract.Runtime {
    runtimeInstance := newRuntime(ctx, scope, kind)

    if binder, isBinder := scope.(runtimeseam.KindBinder); true == isBinder {
        if bindErr := binder.BindRuntimeKind(runtimeseam.Token{}, kind); nil != bindErr {
            exception.Panic(
                exception.NewError("scope cannot serve the runtime", exceptioncontract.Context{"argument": "scope"}, bindErr),
            )
        }
    }

    return runtimeInstance
}

func newRuntime(ctx context.Context, scope containercontract.Scope, kind containercontract.Kind) containercontract.Runtime {
    if true == internal.IsNilInterface(ctx) {
        exception.Panic(
            exception.NewError("context may not be nil on runtime", nil, nil),
        )
    }

    if true == internal.IsNilInterface(scope) {
        exception.Panic(
            exception.NewError("scope may not be nil on runtime", nil, nil),
        )
    }

    return &runtime{
        ctx:   ctx,
        scope: scope,
        kind:  kind,
    }
}

type runtime struct {
    ctx   context.Context
    scope containercontract.Scope
    kind  containercontract.Kind
}

func (instance *runtime) Context() context.Context {
    return instance.ctx
}

func (instance *runtime) Scope() containercontract.Scope {
    return instance.scope
}

func (instance *runtime) Kind() containercontract.Kind {
    return instance.kind
}

var _ containercontract.Runtime = (*runtime)(nil)
