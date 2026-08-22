package container

import (
    "reflect"
    "runtime"
    "strings"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* the scoped node keys carry their own prefix so the two levels never share one. The resolution stack detects a cycle by node key, and a scoped service that resolves the container service it replaces must be reported as repeating the scoped node rather than the container one; the container's teardown walks its own dependency graph by the same keys and must never find a node standing for a value it does not hold. */
func scopedNameNodeKey(serviceName string) string {
    return "scope:service:" + serviceName
}

func scopedTypeNodeKey(typeKey string) string {
    return "scope:type:" + typeKey
}

/* scopedProviderCreateFunc runs a scoped provider and names it for the failure reports, exactly as the container path names its own. */
func scopedProviderCreateFunc(provider providerAny) createWithGuardCreateFunc {
    return func(resolver containercontract.Resolver) (any, error, *providerDebugInfo) {
        providerTypeString := reflect.TypeOf(provider).String()

        providerFunctionString := ""
        providerPointer := reflect.ValueOf(provider).Pointer()
        if 0 != providerPointer {
            providerFunction := runtime.FuncForPC(providerPointer)
            if nil != providerFunction {
                providerFunctionString = providerFunction.Name()
            }
        }

        createdValue, createErr := provider(resolver)

        return createdValue, createErr, &providerDebugInfo{
            providerTypeString:     providerTypeString,
            providerFunctionString: providerFunctionString,
        }
    }
}

/* scopedServiceByName builds — or hands back — a service this scope owns. Everything about it is the scope's: the provider comes from the scope, the creation guard that serialises concurrent resolutions is the scope's own map, and so is the map the finished value lands in. Two scopes resolving the same name therefore never meet — they contend on nothing, wait on nothing of each other's, and each ends up with the instance that belongs to its own request — while two goroutines of the SAME request still share one instance, because they share one guard.

It runs under the container mutex like every other creation: that is the lock the guard holds and releases around the provider call, and container-then-scope is the only order the two locks are ever taken in. The scope is deliberately left visible for the duration, because a scoped service is the request and may read both levels. */
func (instance *resolverContext) scopedServiceByName(
    scopeInstance *scope,
    serviceName string,
    provider providerAny,
    canonicalType reflect.Type,
) (any, error) {
    return instance.containerInstance.serviceWithCreationGuardLocked(
        guardedCreation{
            requestedKey: instance.rootRequestedKey,
            creatingKey:  scopedNameNodeKey(serviceName),
            getCreatingState: func() (*creationState, bool) {
                state, exists := scopeInstance.creatingByName[serviceName]

                return state, exists
            },
            setCreatingState: func(state *creationState) {
                scopeInstance.creatingByName[serviceName] = state
            },
            clearCreatingState: func() {
                delete(scopeInstance.creatingByName, serviceName)
            },
            lookup: func() (any, bool) {
                value, exists, lookupErr := scopeInstance.lookupInstanceByName(serviceName)
                if nil != lookupErr {
                    return nil, false
                }

                return value, exists
            },
            create: scopedProviderCreateFunc(provider),
            store: instanceStore{
                keep: func(value any) (any, bool, error) {
                    return scopeInstance.storeCreatedInstance(serviceName, canonicalType, value)
                },
            },
            suspendsScope: false,
        },
        instance,
    )
}

/* scopedServiceByType is scopedServiceByName's counterpart for a scoped registration reached only by its type, with no name to file it under. */
func (instance *resolverContext) scopedServiceByType(
    scopeInstance *scope,
    typeKey string,
    canonicalType reflect.Type,
    provider providerAny,
) (any, error) {
    return instance.containerInstance.serviceWithCreationGuardLocked(
        guardedCreation{
            requestedKey: instance.rootRequestedKey,
            creatingKey:  scopedTypeNodeKey(typeKey),
            getCreatingState: func() (*creationState, bool) {
                state, exists := scopeInstance.creatingByType[typeKey]

                return state, exists
            },
            setCreatingState: func(state *creationState) {
                scopeInstance.creatingByType[typeKey] = state
            },
            clearCreatingState: func() {
                delete(scopeInstance.creatingByType, typeKey)
            },
            lookup: func() (any, bool) {
                value, exists, lookupErr := scopeInstance.lookupInstanceByType(canonicalType)
                if nil != lookupErr {
                    return nil, false
                }

                return value, exists
            },
            create: scopedProviderCreateFunc(provider),
            store: instanceStore{
                keep: func(value any) (any, bool, error) {
                    return scopeInstance.storeCreatedInstance("", canonicalType, value)
                },
            },
            suspendsScope: false,
        },
        instance,
    )
}

/* registerScopedDependencyLocked records that one service the scope built depends on another, so the scope's teardown can close dependents before their dependencies. Only edges between two scoped nodes are kept: a container singleton reached from a scoped service is not the scope's to close, and the reverse edge cannot occur at all, since a container provider runs with the scope suspended. */
func registerScopedDependencyLocked(scopeInstance *scope, dependentKey string, dependencyKey string) {
    if "" == dependentKey || "" == dependencyKey {
        return
    }

    dependencies, exists := scopeInstance.dependencyGraph[dependentKey]
    if false == exists {
        dependencies = make(map[string]struct{})
        scopeInstance.dependencyGraph[dependentKey] = dependencies
    }

    dependencies[dependencyKey] = struct{}{}
}

func isScopedNodeKey(nodeKey string) bool {
    return strings.HasPrefix(nodeKey, "scope:")
}
