package container

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/internal/runtimeseam"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* SpawnChildScope opens a new scope of this scope's container; a closed scope answers ErrScopeClosed, as its resolutions do. */
func (instance *scope) SpawnChildScope(token runtimeseam.Token) (containercontract.Scope, error) {
    containerInstance := instance.container.Load()
    if nil == containerInstance {
        return nil, exception.NewError(
            "scope is closed",
            nil,
            ErrScopeClosed,
        )
    }

    return containerInstance.NewScope(), nil
}

/* the first bind wins, so one scope serves one phase */
func (instance *scope) BindRuntimeKind(token runtimeseam.Token, kind containercontract.Kind) error {
    if true == instance.runtimeKind.CompareAndSwap(uint32(containercontract.KindUnspecified), uint32(kind)) {
        return nil
    }

    boundKind := containercontract.Kind(instance.runtimeKind.Load())
    if kind == boundKind {
        return nil
    }

    return exception.NewError(
        "the scope already serves a runtime of another kind",
        exceptioncontract.Context{
            "boundKind": boundKind.String(),
            "kind":      kind.String(),
        },
        nil,
    )
}

func (instance *scope) boundRuntimeKind() containercontract.Kind {
    kind := containercontract.Kind(instance.runtimeKind.Load())
    if containercontract.KindUnspecified == kind {
        return containercontract.KindDetached
    }

    return kind
}

var _ runtimeseam.ChildScopeSpawner = (*scope)(nil)

var _ runtimeseam.KindBinder = (*scope)(nil)
