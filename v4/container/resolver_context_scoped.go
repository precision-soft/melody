package container

import (
    "context"
    "reflect"
    "runtime"
    "strings"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* containerNameNodeKeyPrefix is the one spelling of a container name node's key, so a declared edge and a created node always meet. */
const containerNameNodeKeyPrefix = "service:"

/* containerNameNodeKey is the key of a container service in the resolution stack, the creation order and the teardown graph. */
func containerNameNodeKey(serviceName string) string {
    return containerNameNodeKeyPrefix + serviceName
}

/* containerTypeNodeKeyPrefix is the one spelling of a container type node's key. */
const containerTypeNodeKeyPrefix = "type:"

/* containerTypeNodeKey is the key of a container service under its type, canonicalised first since T and *T are one type. */
func containerTypeNodeKey(targetType reflect.Type) string {
    return containerTypeNodeKeyPrefix + typeIdentityKey(canonicalServiceType(targetType))
}

/* scoped node keys carry their own prefix so the two levels never share a key, in the cycle detection or in the teardown graph */
func scopedNameNodeKey(serviceName string) string {
    return "scope:service:" + serviceName
}

func scopedTypeNodeKey(typeKey string) string {
    return "scope:type:" + typeKey
}

/* scopedProviderCreateFunc runs a scoped provider and names it for the failure reports. */
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

/* scopedServiceByName builds or hands back a service this scope owns: the provider, the creation guard and the store are the scope's, so two scopes never meet while goroutines of one request share an instance. It runs under the container mutex, container before scope being the only lock order, with the scope visible. */
func (instance *resolverContext) scopedServiceByName(
    ctx context.Context,
    scopeInstance *scope,
    serviceName string,
    provider providerAny,
    canonicalType reflect.Type,
) (any, error) {
    return instance.containerInstance.serviceWithCreationGuardLocked(
        guardedCreation{
            waitContext:  ctx,
            requestedKey: instance.rootRequestedKey,
            creatingKey:  scopedNameNodeKey(serviceName),
            ownerNodeKey: scopedNameNodeKey(serviceName),
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
            suspendsScope:    false,
            scopeDoneChannel: scopeInstance.doneChannel,
        },
        instance,
    )
}

/* scopedServiceByType is scopedServiceByName for a scoped registration reached only by type. */
func (instance *resolverContext) scopedServiceByType(
    ctx context.Context,
    scopeInstance *scope,
    typeKey string,
    canonicalType reflect.Type,
    provider providerAny,
) (any, error) {
    return instance.containerInstance.serviceWithCreationGuardLocked(
        guardedCreation{
            waitContext:  ctx,
            requestedKey: instance.rootRequestedKey,
            creatingKey:  scopedTypeNodeKey(typeKey),
            ownerNodeKey: scopedTypeNodeKey(typeKey),
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
            suspendsScope:    false,
            scopeDoneChannel: scopeInstance.doneChannel,
        },
        instance,
    )
}

/* refuseScopedDependencyRingLocked refuses the scoped edge from dependentKey to dependencyKey when the dependency already reaches the dependent in the scope's graph, as refuseDependencyRingLocked does for the container; a scoped type standing for one name is keyed by that name, so no fold is needed. The container mutex is held. */
func refuseScopedDependencyRingLocked(scopeInstance *scope, dependentKey string, dependencyKey string) error {
    if "" == dependentKey || "" == dependencyKey || dependentKey == dependencyKey || true == hasDependencyEdge(scopeInstance.dependencyGraph, dependentKey, dependencyKey) {
        return nil
    }

    if false == dependencyReaches(scopeInstance.dependencyGraph, dependencyKey, dependentKey) {
        return nil
    }

    return exception.NewError(
        "circular service dependency detected",
        exceptioncontract.Context{
            "dependent":  dependentKey,
            "dependency": dependencyKey,
        },
        ErrCircularDependency,
    )
}

func isScopedNodeKey(nodeKey string) bool {
    return strings.HasPrefix(nodeKey, "scope:")
}
