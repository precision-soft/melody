package runtime

import (
    "context"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func New(
    ctx context.Context,
    scope containercontract.Scope,
    container containercontract.Container,
) runtimecontract.Runtime {
    /* the guards read through the interface: a typed nil passes a plain nil comparison and every resolution through the runtime then panics on the request path instead of failing here, at the construction the panic can name */
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

    if true == internal.IsNilInterface(container) {
        exception.Panic(
            exception.NewError("container may not be nil on runtime", nil, nil),
        )
    }

    return &runtime{
        ctx:       ctx,
        scope:     scope,
        container: container,
    }
}

type runtime struct {
    ctx       context.Context
    scope     containercontract.Scope
    container containercontract.Container
}

func (instance *runtime) Context() context.Context {
    return instance.ctx
}

func (instance *runtime) Scope() containercontract.Scope {
    return instance.scope
}

func (instance *runtime) Container() containercontract.Container {
    return instance.container
}

var _ runtimecontract.Runtime = (*runtime)(nil)
