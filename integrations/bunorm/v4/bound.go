package bunorm

import (
    "github.com/uptrace/bun"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

/* Bound is a manager registry bound to one runtime: every door opens under it. It is a value for one request, one run or one delivery, never kept in a service, a goroutine that outlives the runtime or a field: a service holds the registry and binds it at the top of each method. A call that needs another deadline binds again over runtime.WithContext. MigrationDatabase takes its hold through the view, and the hold is released through Registry().CloseMigrationDatabase, which takes no runtime. */
type Bound struct {
    runtimeInstance containercontract.Runtime
    registry        *ManagerRegistry
}

/* For binds the registry to the runtime; it allocates nothing and never panics, a nil registry being refused by the first door called and a nil runtime by the registry's own door. */
func For(runtimeInstance containercontract.Runtime, registry *ManagerRegistry) Bound {
    return Bound{
        runtimeInstance: runtimeInstance,
        registry:        registry,
    }
}

/* FromRuntime resolves the registry the application registered under serviceName through the runtime's scope and binds it; the module registers none, and a missing one panics as runtime.MustFromRuntime does. */
func FromRuntime(runtimeInstance containercontract.Runtime, serviceName string) Bound {
    return For(runtimeInstance, runtime.MustFromRuntime[*ManagerRegistry](runtimeInstance, serviceName))
}

/* Runtime answers the runtime the view is bound to, for a callee that binds something else to it. */
func (instance Bound) Runtime() containercontract.Runtime {
    return instance.runtimeInstance
}

/* Registry answers the unbound registry, for its doors that take no runtime and for a callee that binds it to another runtime. */
func (instance Bound) Registry() *ManagerRegistry {
    return instance.registry
}

func (instance Bound) Manager(name string) (*Manager, error) {
    if nilErr := instance.refuseNilRegistry(); nil != nilErr {
        return nil, nilErr
    }

    return instance.registry.Manager(instance.runtimeInstance, name)
}

func (instance Bound) MustManager(name string) *Manager {
    instance.panicOnNilRegistry()

    return instance.registry.MustManager(instance.runtimeInstance, name)
}

func (instance Bound) Database(name string) (*bun.DB, error) {
    if nilErr := instance.refuseNilRegistry(); nil != nilErr {
        return nil, nilErr
    }

    return instance.registry.Database(instance.runtimeInstance, name)
}

func (instance Bound) MustDatabase(name string) *bun.DB {
    instance.panicOnNilRegistry()

    return instance.registry.MustDatabase(instance.runtimeInstance, name)
}

func (instance Bound) DefaultManager() (*Manager, error) {
    if nilErr := instance.refuseNilRegistry(); nil != nilErr {
        return nil, nilErr
    }

    return instance.registry.DefaultManager(instance.runtimeInstance)
}

func (instance Bound) MustDefaultManager() *Manager {
    instance.panicOnNilRegistry()

    return instance.registry.MustDefaultManager(instance.runtimeInstance)
}

func (instance Bound) DefaultDatabase() (*bun.DB, error) {
    if nilErr := instance.refuseNilRegistry(); nil != nilErr {
        return nil, nilErr
    }

    return instance.registry.DefaultDatabase(instance.runtimeInstance)
}

func (instance Bound) MustDefaultDatabase() *bun.DB {
    instance.panicOnNilRegistry()

    return instance.registry.MustDefaultDatabase(instance.runtimeInstance)
}

func (instance Bound) MigrationDatabase(name string) (*bun.DB, error) {
    if nilErr := instance.refuseNilRegistry(); nil != nilErr {
        return nil, nilErr
    }

    return instance.registry.MigrationDatabase(instance.runtimeInstance, name)
}

func (instance Bound) refuseNilRegistry() error {
    if nil == instance.registry {
        return exception.NewError("manager registry is nil", nil, nil)
    }

    return nil
}

/* the Must doors panic with the view's refusal rather than with a nil dereference */
func (instance Bound) panicOnNilRegistry() {
    if nilErr := instance.refuseNilRegistry(); nil != nilErr {
        exception.Panic(exception.FromError(nilErr))
    }
}
