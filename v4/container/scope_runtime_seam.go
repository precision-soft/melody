package container

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/internal/runtimeseam"
    "github.com/precision-soft/melody/v4/exception"
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

var _ runtimeseam.ChildScopeSpawner = (*scope)(nil)
