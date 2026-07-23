package container

import (
    "reflect"
    "runtime"
    "sort"
    "strings"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
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
        stackTypes:        make([]reflect.Type, 0, 8),
    }
}

func newScopeResolverContext(containerInstance *container, scopeInstance *scope) *resolverContext {
    return &resolverContext{
        containerInstance: containerInstance,
        scopeInstance:     scopeInstance,
        contextId:         containerInstance.resolverContextIdCounter.Add(1),
        rootRequestedKey:  "",
        stack:             make([]string, 0, 8),
        stackTypes:        make([]reflect.Type, 0, 8),
    }
}

type resolverContext struct {
    containerInstance *container
    scopeInstance     *scope
    contextId         uint64
    rootRequestedKey  string
    stack             []string
    /* stackTypes runs parallel to stack: the canonical type of a type node, nil for a name node. The collection exclusion compares type identity through it, because two distinct types from same-named packages share a String() and a string comparison would exclude a service that is not on the path at all. */
    stackTypes []reflect.Type
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

    pushKeyErr := instance.pushKey(nodeKey, nil)
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
        func() (any, bool) {
            value, exists := instance.containerInstance.instances[serviceName]
            return value, exists
        },
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
        func(value any) {
            instance.containerInstance.instances[serviceName] = value
        },
        instance,
    )
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
        instance.rootRequestedKey = "type:" + canonicalTargetType.String()
    }

    requestedKey := instance.rootRequestedKey
    typeKey := canonicalTargetType.String()
    nodeKey := "type:" + typeKey

    parentKey := ""
    if 0 < len(instance.stack) {
        parentKey = instance.stack[len(instance.stack)-1]
    }

    pushKeyErr := instance.pushKey(nodeKey, canonicalTargetType)
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
            func() (any, bool) {
                resolvedValue, exists := instance.containerInstance.instances[serviceName]
                return resolvedValue, exists
            },
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
            func(resolvedValue any) {
                instance.containerInstance.instances[serviceName] = resolvedValue
                instance.containerInstance.typeInstances[canonicalTargetType] = resolvedValue
            },
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
        func() (any, bool) {
            value, exists := instance.containerInstance.typeInstances[canonicalTargetType]
            return value, exists
        },
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
        func(value any) {
            instance.containerInstance.typeInstances[canonicalTargetType] = value
        },
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

/* TypesImplementing delegates to the root container, so a provider that collects its collaborators through AllImplementing can do it with the resolver it was handed instead of needing the container itself. */
func (instance *resolverContext) TypesImplementing(interfaceType reflect.Type) []reflect.Type {
    return instance.containerInstance.TypesImplementing(interfaceType)
}

func (instance *resolverContext) ReferencesImplementing(interfaceType reflect.Type) []containercontract.ServiceReference {
    return instance.containerInstance.ReferencesImplementing(interfaceType)
}

/* isResolvingReference reports whether the reference is the service this context is creating right now — the innermost node of the resolution stack. Only that service is excluded from a collection: it is the composite dispatcher collecting the handlers it belongs to. A reference deeper on the path is not excluded, so collecting it runs into the creation guard and fails loudly as the circular dependency it is — excluding it instead would freeze a collection whose content depends on which service happened to boot first. On a type node the exclusion is narrowed to the name this context actually holds in creation, so a sibling name of the same type — registered while the creation ran — stays collectable; the type comparison itself is reflect.Type identity, never the type's String(), which two types from same-named packages share. */
func (instance *resolverContext) isResolvingReference(reference containercontract.ServiceReference) bool {
    if 0 == len(instance.stack) {
        return false
    }

    topIndex := len(instance.stack) - 1

    if "service:"+reference.ServiceName == instance.stack[topIndex] {
        return true
    }

    topType := instance.stackTypes[topIndex]
    if nil == topType || topType != reference.ServiceType {
        return false
    }

    instance.containerInstance.mutex.RLock()
    state, isCreating := instance.containerInstance.creatingByName[reference.ServiceName]
    instance.containerInstance.mutex.RUnlock()

    if true == isCreating {
        return state.ownerContextId == instance.contextId
    }

    /* an idle sibling name of the collector's type: the collector's own reference is always pinned by the creation entry above, and a purely type-keyed creation never yields a listed reference, so nothing here is the collector */
    return false
}

func (instance *resolverContext) pushKey(creatingKey string, creatingType reflect.Type) error {
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
    instance.stackTypes = append(instance.stackTypes, creatingType)

    return nil
}

func (instance *resolverContext) popKey() {
    if 0 == len(instance.stack) {
        return
    }

    instance.stack = instance.stack[:len(instance.stack)-1]
    instance.stackTypes = instance.stackTypes[:len(instance.stackTypes)-1]
}

func (instance *resolverContext) stackStringWithRepeat(repeatedKey string) string {
    parts := make([]string, 0, len(instance.stack)+1)
    parts = append(parts, instance.stack...)
    parts = append(parts, repeatedKey)

    return strings.Join(parts, " -> ")
}

var _ containercontract.Resolver = (*resolverContext)(nil)
var _ containercontract.TypeLister = (*resolverContext)(nil)
