package runtime

import (
    "context"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/internal/runtimeseam"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* NewRequest builds the runtime of one http request. */
func NewRequest(ctx context.Context, scope containercontract.Scope) containercontract.Runtime {
    return newRuntime(ctx, scope, containercontract.KindRequest)
}

/* NewRun builds the runtime of one cli run, of one message delivery or of one cron tick. */
func NewRun(ctx context.Context, scope containercontract.Scope) containercontract.Runtime {
    return newRuntime(ctx, scope, containercontract.KindRun)
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

/* WithContext answers the same runtime over another context: the kind and the scope are kept. */
func WithContext(runtimeInstance containercontract.Runtime, ctx context.Context) containercontract.Runtime {
    if true == internal.IsNilInterface(runtimeInstance) {
        exception.Panic(
            exception.NewError("runtime may not be nil on with context", nil, nil),
        )
    }

    return newRuntime(ctx, runtimeInstance.Scope(), runtimeInstance.Kind())
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
