package container

import (
    "reflect"
    "runtime"
    "sort"
    "strings"

    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

type providerDebugInfo struct {
    providerTypeString     string
    providerFunctionString string
}

func newResolverContext(containerInstance *container) *resolverContext {
    return &resolverContext{
        containerInstance: containerInstance,
        scopeInstance:     nil,
        contextId:         containerInstance.resolverContextIdCounter.Add(1),
        rootRequestedKey:  "",
        stack:             make([]string, 0, 8),
    }
}

func newScopeResolverContext(containerInstance *container, scopeInstance *scope) *resolverContext {
    return &resolverContext{
        containerInstance: containerInstance,
        scopeInstance:     scopeInstance,
        contextId:         containerInstance.resolverContextIdCounter.Add(1),
        rootRequestedKey:  "",
        stack:             make([]string, 0, 8),
    }
}

type resolverContext struct {
    containerInstance *container
    scopeInstance     *scope
    contextId         uint64
    rootRequestedKey  string
    stack             []string
    /* consumedScopeEntries names what this resolution has read out of the request scope while building the service currently in creation. A value assembled from one of those entries holds the request they belong to, so it may not be kept as a process-lifetime singleton; the names travel into the refusal when a store would still put it in the root container. The set is put aside and restored around every creation, so a sibling that read nothing from the scope stays a singleton while the taint still reaches whoever depends on the tainted service. */
    consumedScopeEntries []string
}

func (instance *resolverContext) markScopeEntryConsumed(entryKey string) {
    for _, existing := range instance.consumedScopeEntries {
        if existing == entryKey {
            return
        }
    }

    instance.consumedScopeEntries = append(instance.consumedScopeEntries, entryKey)
}

/* scopeInstanceStore names the two places a service resolved through this context may be kept. The scope target is absent when no scope drove the resolution, which is what makes a scope-tainted value landing in the root container refusable instead of merely unlikely. */
func (instance *resolverContext) scopeInstanceStore(
    serviceName string,
    canonicalType reflect.Type,
    storeInRoot func(value any),
) instanceStore {
    if nil == instance.scopeInstance {
        return instanceStore{
            inRoot:  storeInRoot,
            inScope: nil,
        }
    }

    scopeInstance := instance.scopeInstance

    return instanceStore{
        inRoot: storeInRoot,
        inScope: func(value any) error {
            return scopeInstance.storeCreatedInstance(serviceName, canonicalType, value)
        },
    }
}

func (instance *resolverContext) Get(serviceName string) (any, error) {
    if "" == serviceName {
        return nil, exception.NewError("service name is required in get", nil, nil)
    }

    if "" == instance.rootRequestedKey {
        instance.rootRequestedKey = serviceName
    }

    requestedKey := instance.rootRequestedKey
    nodeKey := "service:" + serviceName

    parentKey := ""
    if 0 < len(instance.stack) {
        parentKey = instance.stack[len(instance.stack)-1]
    }

    pushKeyErr := instance.pushKey(nodeKey)
    if nil != pushKeyErr {
        return nil, pushKeyErr
    }
    defer instance.popKey()

    if nil != instance.scopeInstance {
        value, exists, lookupInstanceByNameErr := instance.scopeInstance.lookupInstanceByName(serviceName)
        if nil != lookupInstanceByNameErr {
            return nil, lookupInstanceByNameErr
        }

        if true == exists {
            instance.markScopeEntryConsumed(nodeKey)

            return value, nil
        }
    }

    instance.containerInstance.mutex.Lock()
    defer instance.containerInstance.mutex.Unlock()

    if "" != parentKey {
        instance.containerInstance.registerDependencyLocked(
            parentKey,
            nodeKey,
        )
    }

    /* @important snapshot the provider under the container mutex before serviceWithCreationGuardLocked releases it; the create closure runs unlocked, so reading the providers map there would race concurrent Register writes. */
    provider, providerExists := instance.containerInstance.providers[serviceName]

    return instance.containerInstance.serviceWithCreationGuardLocked(
        requestedKey,
        serviceName,
        func() (*creationState, bool) {
            state, exists := instance.containerInstance.creatingByName[serviceName]
            return state, exists
        },
        func(state *creationState) {
            instance.containerInstance.creatingByName[serviceName] = state
        },
        func() {
            delete(instance.containerInstance.creatingByName, serviceName)
        },
        instance.lookupByName(serviceName, nodeKey),
        func(resolver containercontract.Resolver) (any, error, *providerDebugInfo) {
            if false == providerExists {
                return nil, exception.NewError(
                    "service is not registered",
                    exceptioncontract.Context{
                        "serviceName": serviceName,
                    },
                    nil,
                ), nil
            }

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
        },
        instance.scopeInstanceStore(
            serviceName,
            nil,
            func(value any) {
                instance.containerInstance.instances[serviceName] = value
            },
        ),
        instance,
    )
}

/* lookupByName is the creation guard's lookup for a named service: an instance this request scope already holds — an installed override, or one the scope itself built — comes before the process-wide one, and reading it is recorded, because whatever is being assembled around it is scope-bound too. A scope that closed underneath the resolution reports nothing here; the store is where that failure is raised. */
func (instance *resolverContext) lookupByName(serviceName string, nodeKey string) createWithGuardLookupFunc {
    return func() (any, bool) {
        if nil != instance.scopeInstance {
            scopeValue, scopeExists, lookupErr := instance.scopeInstance.lookupInstanceByName(serviceName)
            if nil == lookupErr && true == scopeExists {
                instance.markScopeEntryConsumed(nodeKey)

                return scopeValue, true
            }
        }

        value, exists := instance.containerInstance.instances[serviceName]

        return value, exists
    }
}

/* lookupByType is lookupByName's counterpart for a type-keyed resolution. */
func (instance *resolverContext) lookupByType(canonicalTargetType reflect.Type, nodeKey string) createWithGuardLookupFunc {
    return func() (any, bool) {
        if nil != instance.scopeInstance {
            scopeValue, scopeExists, lookupErr := instance.scopeInstance.lookupInstanceByType(canonicalTargetType)
            if nil == lookupErr && true == scopeExists {
                instance.markScopeEntryConsumed(nodeKey)

                return scopeValue, true
            }
        }

        value, exists := instance.containerInstance.typeInstances[canonicalTargetType]

        return value, exists
    }
}

func (instance *resolverContext) MustGet(serviceName string) any {
    value, getErr := instance.Get(serviceName)
    if nil != getErr {
        exception.Panic(
            exception.NewError(
                "failed to get service instance",
                exceptioncontract.Context{
                    "serviceName": serviceName,
                },
                getErr,
            ),
        )
    }

    return value
}

func (instance *resolverContext) GetByType(targetType reflect.Type) (any, error) {
    if nil == targetType {
        return nil, exception.NewError(
            "service type is required in get by type",
            nil,
            nil,
        )
    }

    canonicalTargetType := canonicalServiceType(targetType)
    if nil == canonicalTargetType {
        return nil, exception.NewError(
            "canonical type is nil in get by type",
            nil,
            nil,
        )
    }

    if "" == instance.rootRequestedKey {
        instance.rootRequestedKey = "type:" + typeIdentityKey(canonicalTargetType)
    }

    requestedKey := instance.rootRequestedKey
    typeKey := typeIdentityKey(canonicalTargetType)
    nodeKey := "type:" + typeKey

    parentKey := ""
    if 0 < len(instance.stack) {
        parentKey = instance.stack[len(instance.stack)-1]
    }

    pushKeyErr := instance.pushKey(nodeKey)
    if nil != pushKeyErr {
        return nil, pushKeyErr
    }
    defer instance.popKey()

    if nil != instance.scopeInstance {
        value, exists, lookupInstanceByTypeErr := instance.scopeInstance.lookupInstanceByType(canonicalTargetType)
        if nil != lookupInstanceByTypeErr {
            return nil, lookupInstanceByTypeErr
        }

        if true == exists {
            instance.markScopeEntryConsumed(nodeKey)

            return value, nil
        }
    }

    instance.containerInstance.mutex.Lock()
    defer instance.containerInstance.mutex.Unlock()

    if "" != parentKey {
        instance.containerInstance.registerDependencyLocked(
            parentKey,
            nodeKey,
        )
    }

    registeredServiceNames, exists := instance.containerInstance.typeRegistrationNamesByType[canonicalTargetType]
    if true == exists && 0 < len(registeredServiceNames) {
        if 1 < len(registeredServiceNames) {
            completeConflicts := make([]string, 0, len(registeredServiceNames))
            completeConflicts = append(completeConflicts, registeredServiceNames...)
            sort.Strings(completeConflicts)

            return nil, exception.NewError(
                "service type has multiple registrations",
                exceptioncontract.Context{
                    "type":      canonicalTargetType.String(),
                    "conflicts": completeConflicts,
                },
                nil,
            )
        }

        serviceName := registeredServiceNames[0]

        value, valueExists := instance.containerInstance.instances[serviceName]
        if true == valueExists {
            instance.containerInstance.typeInstances[canonicalTargetType] = value
            return value, nil
        }

        /* @important snapshot the provider under the container mutex before serviceWithCreationGuardLocked releases it; the create closure runs unlocked, so reading the providers map there would race concurrent Register writes. */
        provider, providerExists := instance.containerInstance.providers[serviceName]

        return instance.containerInstance.serviceWithCreationGuardLocked(
            requestedKey,
            serviceName,
            func() (*creationState, bool) {
                state, exists := instance.containerInstance.creatingByName[serviceName]
                return state, exists
            },
            func(state *creationState) {
                instance.containerInstance.creatingByName[serviceName] = state
            },
            func() {
                delete(instance.containerInstance.creatingByName, serviceName)
            },
            instance.lookupByName(serviceName, "service:"+serviceName),
            func(resolver containercontract.Resolver) (any, error, *providerDebugInfo) {
                if false == providerExists {
                    return nil, exception.NewError(
                        "service is not registered",
                        exceptioncontract.Context{
                            "serviceName": serviceName,
                        },
                        nil,
                    ), nil
                }

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
            },
            instance.scopeInstanceStore(
                serviceName,
                canonicalTargetType,
                func(resolvedValue any) {
                    instance.containerInstance.instances[serviceName] = resolvedValue
                    instance.containerInstance.typeInstances[canonicalTargetType] = resolvedValue
                },
            ),
            instance,
        )
    }

    /* @important snapshot the provider under the container mutex before serviceWithCreationGuardLocked releases it; the create closure runs unlocked, so reading the typeProviders map there would race concurrent Register writes. */
    provider, providerExists := instance.containerInstance.typeProviders[canonicalTargetType]

    return instance.containerInstance.serviceWithCreationGuardLocked(
        requestedKey,
        typeKey,
        func() (*creationState, bool) {
            state, exists := instance.containerInstance.creatingByType[typeKey]
            return state, exists
        },
        func(state *creationState) {
            instance.containerInstance.creatingByType[typeKey] = state
        },
        func() {
            delete(instance.containerInstance.creatingByType, typeKey)
        },
        instance.lookupByType(canonicalTargetType, nodeKey),
        func(resolver containercontract.Resolver) (any, error, *providerDebugInfo) {
            if false == providerExists {
                return nil, exception.NewError(
                    "service type is not registered",
                    exceptioncontract.Context{
                        "type": canonicalTargetType.String(),
                    },
                    nil,
                ), nil
            }

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
        },
        instance.scopeInstanceStore(
            "",
            canonicalTargetType,
            func(value any) {
                instance.containerInstance.typeInstances[canonicalTargetType] = value
            },
        ),
        instance,
    )
}

func (instance *resolverContext) MustGetByType(targetType reflect.Type) any {
    value, getByTypeErr := instance.GetByType(targetType)
    if nil != getByTypeErr {
        typeString := ""
        if nil != targetType {
            typeString = targetType.String()
        }

        exception.Panic(
            exception.NewError(
                "failed to get service instance by type",
                exceptioncontract.Context{
                    "type": typeString,
                },
                getByTypeErr,
            ),
        )
    }

    return value
}

func (instance *resolverContext) Has(serviceName string) bool {
    if nil != instance.scopeInstance {
        return instance.scopeInstance.Has(serviceName)
    }

    return instance.containerInstance.Has(serviceName)
}

func (instance *resolverContext) HasType(targetType reflect.Type) bool {
    if nil != instance.scopeInstance {
        return instance.scopeInstance.HasType(targetType)
    }

    return instance.containerInstance.HasType(targetType)
}

func (instance *resolverContext) pushKey(creatingKey string) error {
    if "" == creatingKey {
        return exception.NewError(
            "creating key is empty",
            nil,
            nil,
        )
    }

    for _, key := range instance.stack {
        if key == creatingKey {
            return exception.NewError(
                "circular service dependency detected",
                exceptioncontract.Context{
                    "creatingKey": creatingKey,
                    "stack":       instance.stackStringWithRepeat(creatingKey),
                },
                nil,
            )
        }
    }

    instance.stack = append(instance.stack, creatingKey)

    return nil
}

func (instance *resolverContext) popKey() {
    if 0 == len(instance.stack) {
        return
    }

    instance.stack = instance.stack[:len(instance.stack)-1]
}

func (instance *resolverContext) stackStringWithRepeat(repeatedKey string) string {
    parts := make([]string, 0, len(instance.stack)+1)
    parts = append(parts, instance.stack...)
    parts = append(parts, repeatedKey)

    return strings.Join(parts, " -> ")
}

var _ containercontract.Resolver = (*resolverContext)(nil)
