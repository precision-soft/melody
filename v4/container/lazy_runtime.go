package container

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* runtimeBoundSource is a resolver that starts a resolution under a caller's runtime: the caller's context always, the caller's kind where the source reads the container */
type runtimeBoundSource interface {
    getUnderRuntime(runtimeInstance containercontract.Runtime, serviceName string) (any, error)
    getByTypeUnderRuntime(runtimeInstance containercontract.Runtime, targetType reflect.Type) (any, error)
}

/* a foreign resolver is asked under the runtime's context only */
func fromResolverUnderRuntime[T any](runtimeInstance containercontract.Runtime, resolver containercontract.Resolver, serviceName string) (T, error) {
    boundSource, isBound := resolver.(runtimeBoundSource)
    if false == isBound {
        return FromResolver[T](runtimeInstance.Context(), resolver, serviceName)
    }

    value, getErr := boundSource.getUnderRuntime(runtimeInstance, serviceName)

    return resolvedAs[T](serviceName, value, getErr)
}

func fromResolverByTypeUnderRuntime[T any](runtimeInstance containercontract.Runtime, resolver containercontract.Resolver) (T, error) {
    boundSource, isBound := resolver.(runtimeBoundSource)
    if false == isBound {
        return FromResolverByType[T](runtimeInstance.Context(), resolver)
    }

    value, getByTypeErr := boundSource.getByTypeUnderRuntime(runtimeInstance, canonicalServiceType(reflect.TypeOf((*T)(nil)).Elem()))

    return resolvedByTypeAs[T](value, getByTypeErr)
}

func (instance *container) getUnderRuntime(runtimeInstance containercontract.Runtime, serviceName string) (any, error) {
    resolver := newResolverContext(instance)
    resolver.kind = runtimeInstance.Kind()

    return resolver.Get(runtimeInstance.Context(), serviceName)
}

func (instance *container) getByTypeUnderRuntime(runtimeInstance containercontract.Runtime, targetType reflect.Type) (any, error) {
    resolver := newResolverContext(instance)
    resolver.kind = runtimeInstance.Kind()

    return resolver.GetByType(runtimeInstance.Context(), targetType)
}

/* a scope's creations keep its own kind; only the runtime's context and scope are read */
func (instance *scope) getUnderRuntime(runtimeInstance containercontract.Runtime, serviceName string) (any, error) {
    if scopeErr := refuseRuntimeOfAnotherScope(runtimeInstance, instance, serviceName); nil != scopeErr {
        return nil, scopeErr
    }

    return instance.Get(runtimeInstance.Context(), serviceName)
}

func (instance *scope) getByTypeUnderRuntime(runtimeInstance containercontract.Runtime, targetType reflect.Type) (any, error) {
    if scopeErr := refuseRuntimeOfAnotherScope(runtimeInstance, instance, targetType.String()); nil != scopeErr {
        return nil, scopeErr
    }

    return instance.GetByType(runtimeInstance.Context(), targetType)
}

/* a provider still running resolves on its live chain under its creation's kind; a returned one starts a late chain, which takes the caller's kind where the view reads the container */
func (instance *resolverContext) getUnderRuntime(runtimeInstance containercontract.Runtime, serviceName string) (any, error) {
    if true == instance.scopeVisible() {
        if scopeErr := refuseRuntimeOfAnotherScope(runtimeInstance, instance.scopeInstance, serviceName); nil != scopeErr {
            return nil, scopeErr
        }
    }

    lateResolver, release := instance.lateResolution()
    if nil == lateResolver {
        return instance.Get(runtimeInstance.Context(), serviceName)
    }
    defer release()

    if false == lateResolver.scopeVisible() {
        lateResolver.kind = runtimeInstance.Kind()
    }

    return lateResolver.Get(runtimeInstance.Context(), serviceName)
}

func (instance *resolverContext) getByTypeUnderRuntime(runtimeInstance containercontract.Runtime, targetType reflect.Type) (any, error) {
    if true == instance.scopeVisible() {
        if scopeErr := refuseRuntimeOfAnotherScope(runtimeInstance, instance.scopeInstance, targetType.String()); nil != scopeErr {
            return nil, scopeErr
        }
    }

    lateResolver, release := instance.lateResolution()
    if nil == lateResolver {
        return instance.GetByType(runtimeInstance.Context(), targetType)
    }
    defer release()

    if false == lateResolver.scopeVisible() {
        lateResolver.kind = runtimeInstance.Kind()
    }

    return lateResolver.GetByType(runtimeInstance.Context(), targetType)
}

func (instance *suspendedScope) getUnderRuntime(runtimeInstance containercontract.Runtime, serviceName string) (any, error) {
    if boundSource, isBound := instance.resolver.(runtimeBoundSource); true == isBound {
        return boundSource.getUnderRuntime(runtimeInstance, serviceName)
    }

    return instance.resolver.Get(runtimeInstance.Context(), serviceName)
}

func (instance *suspendedScope) getByTypeUnderRuntime(runtimeInstance containercontract.Runtime, targetType reflect.Type) (any, error) {
    if boundSource, isBound := instance.resolver.(runtimeBoundSource); true == isBound {
        return boundSource.getByTypeUnderRuntime(runtimeInstance, targetType)
    }

    return instance.resolver.GetByType(runtimeInstance.Context(), targetType)
}

/* scopeBehind answers the container scope a runtime's scope resolves in, unwrapping the provider and suspended scopes; nil for one reading the container, and false for a scope of another implementation */
func scopeBehind(scopeInstance containercontract.Scope) (*scope, bool) {
    switch typedScope := scopeInstance.(type) {
    case *scope:
        return typedScope, true
    case *providerScope:
        return typedScope.scope, true
    case *suspendedScope:
        if view, isView := typedScope.resolver.(*resolverContext); true == isView {
            return view.scopeInstance, true
        }

        return nil, true
    }

    return nil, false
}

/* a scoped handle resolves in its own scope, so a runtime of another scope is a request's handle used by another request */
func refuseRuntimeOfAnotherScope(runtimeInstance containercontract.Runtime, handleScope *scope, lookup string) error {
    runtimeScope, known := scopeBehind(runtimeInstance.Scope())
    if true == known && handleScope == runtimeScope {
        return nil
    }

    runtimeScopeKind := "foreign"
    if true == known && nil == runtimeScope {
        runtimeScopeKind = "container"
    }
    if nil != runtimeScope {
        runtimeScopeKind = runtimeScope.boundRuntimeKind().String()
    }

    return exception.NewError(
        "the runtime belongs to another scope than the lazy handle",
        exceptioncontract.Context{
            "lookup":           lookup,
            "handleScopeKind":  handleScope.boundRuntimeKind().String(),
            "runtimeScopeKind": runtimeScopeKind,
            "runtimeKind":      runtimeInstance.Kind().String(),
        },
        nil,
    )
}

var _ runtimeBoundSource = (*container)(nil)

var _ runtimeBoundSource = (*scope)(nil)

var _ runtimeBoundSource = (*resolverContext)(nil)

var _ runtimeBoundSource = (*suspendedScope)(nil)

var _ runtimeBoundSource = (*providerScope)(nil)
