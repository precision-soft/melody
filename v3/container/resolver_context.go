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
    /* scopeSuspended is set while a provider registered on the CONTAINER builds its service, and it is what keeps the two apart. The container is request-agnostic: a service it owns is one instance for the whole process, so its construction may read only what the container holds. A request scope layers over the container for the code that runs inside a request, not underneath the container's own wiring, and a factory that reached through it would assemble a process-lifetime singleton out of one request's values.

       Suspension is a refusal, not a substitution. A container provider that asks for something only a scope carries — the request context — is told the service does not exist, which is a wiring mistake reported where it is made; a provider that asks for the logger gets the container's agnostic one, because that is the logger a process-lifetime service should hold. Only the service actually being requested is looked up through the scope, which is the layering a caller means by resolving through a scope at all. */
    scopeSuspended bool
}

/* scopeVisible reports whether this resolution may read the request scope. */
func (instance *resolverContext) scopeVisible() bool {
    return nil != instance.scopeInstance && false == instance.scopeSuspended
}

/* containerNameStore keeps a finished service under its name in the container's own maps, and under the canonical type as well when the resolution was type-keyed. It runs under the container mutex. An override that was installed while the provider ran already occupies the name — it answers before anything is built — so the built value is handed back to the guard as the loser, and the name is marked container-built otherwise, which is what tells a later override that the value it evicts is the container's to close. */
func containerNameStore(
    containerInstance *container,
    serviceName string,
    canonicalTargetType reflect.Type,
) instanceStore {
    return instanceStore{
        keep: func(value any) (any, bool, error) {
            if existingValue, exists := containerInstance.instances[serviceName]; true == exists {
                return existingValue, true, nil
            }

            containerInstance.instances[serviceName] = value
            containerInstance.builtServiceNames[serviceName] = struct{}{}
            containerInstance.recordCreationOrderLocked("service:" + serviceName)

            if nil != canonicalTargetType {
                containerInstance.typeInstances[canonicalTargetType] = value
                containerInstance.recordCreationOrderLocked("type:" + typeIdentityKey(canonicalTargetType))
            }

            return value, false, nil
        },
    }
}

/* containerTypeStore is containerNameStore's counterpart for a type-keyed registration with no name to file under. */
func containerTypeStore(
    containerInstance *container,
    canonicalTargetType reflect.Type,
) instanceStore {
    return instanceStore{
        keep: func(value any) (any, bool, error) {
            if existingValue, exists := containerInstance.typeInstances[canonicalTargetType]; true == exists {
                return existingValue, true, nil
            }

            containerInstance.typeInstances[canonicalTargetType] = value
            containerInstance.recordCreationOrderLocked("type:" + typeIdentityKey(canonicalTargetType))

            return value, false, nil
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

    /* whether the name belongs to this scope decides the node key, and the key has to be settled before it is pushed: the resolution stack is what tells the scope's own dependency graph which of its services depends on which, and a scoped node wearing the container's key would be indistinguishable from a container one. */
    scopedProvider := providerAny(nil)
    scopedProviderExists := false
    if true == instance.scopeVisible() {
        scopedProvider, scopedProviderExists = instance.scopeInstance.scopedProviderByName(serviceName)
    }

    nodeKey := "service:" + serviceName
    if true == scopedProviderExists {
        nodeKey = scopedNameNodeKey(serviceName)
    }

    parentKey := ""
    if 0 < len(instance.stack) {
        parentKey = instance.stack[len(instance.stack)-1]
    }

    pushKeyErr := instance.pushKey(nodeKey, nil)
    if nil != pushKeyErr {
        return nil, pushKeyErr
    }
    defer instance.popKey()

    if true == instance.scopeVisible() {
        value, exists, lookupInstanceByNameErr := instance.scopeInstance.lookupInstanceByName(serviceName)
        if nil != lookupInstanceByNameErr {
            return nil, lookupInstanceByNameErr
        }

        /* an installed override answers before anything is built, which is what keeps overriding a mechanism of its own rather than a competitor of registration */
        if true == exists {
            return value, nil
        }

        if true == scopedProviderExists {
            scopeInstance := instance.scopeInstance

            instance.containerInstance.mutex.Lock()
            defer instance.containerInstance.mutex.Unlock()

            if "" != parentKey && true == isScopedNodeKey(parentKey) {
                registerScopedDependencyLocked(scopeInstance, parentKey, nodeKey)
            }

            return instance.scopedServiceByName(scopeInstance, serviceName, scopedProvider, nil)
        }
    }

    instance.containerInstance.mutex.Lock()
    defer instance.containerInstance.mutex.Unlock()

    if "" != parentKey && false == isScopedNodeKey(nodeKey) {
        instance.containerInstance.registerDependencyLocked(
            parentKey,
            nodeKey,
        )
    }

    /* @important snapshot the provider under the container mutex before serviceWithCreationGuardLocked releases it; the create closure runs unlocked, so reading the providers map there would race concurrent Register writes. */
    provider, providerExists := instance.containerInstance.providers[serviceName]

    return instance.containerInstance.serviceWithCreationGuardLocked(
        guardedCreation{
            requestedKey: requestedKey,
            creatingKey:  serviceName,
            getCreatingState: func() (*creationState, bool) {
                state, exists := instance.containerInstance.creatingByName[serviceName]
                return state, exists
            },
            setCreatingState: func(state *creationState) {
                instance.containerInstance.creatingByName[serviceName] = state
            },
            clearCreatingState: func() {
                delete(instance.containerInstance.creatingByName, serviceName)
            },
            lookup: instance.lookupByName(serviceName),
            create: func(resolver containercontract.Resolver) (any, error, *providerDebugInfo) {
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
            store: containerNameStore(instance.containerInstance, serviceName, nil),
            suspendsScope: true,
        },
        instance,
    )
}

/* lookupByName is the creation guard's lookup for a named service: an instance this request scope already holds — an installed override, or one the scope itself built — comes before the process-wide one. A scope that closed underneath the resolution reports nothing here; the store is where that failure is raised. */
func (instance *resolverContext) lookupByName(serviceName string) createWithGuardLookupFunc {
    return func() (any, bool) {
        if true == instance.scopeVisible() {
            scopeValue, scopeExists, lookupErr := instance.scopeInstance.lookupInstanceByName(serviceName)
            if nil == lookupErr && true == scopeExists {
                return scopeValue, true
            }
        }

        value, exists := instance.containerInstance.instances[serviceName]

        return value, exists
    }
}

/* lookupByType is lookupByName's counterpart for a type-keyed resolution. */
func (instance *resolverContext) lookupByType(canonicalTargetType reflect.Type) createWithGuardLookupFunc {
    return func() (any, bool) {
        if true == instance.scopeVisible() {
            scopeValue, scopeExists, lookupErr := instance.scopeInstance.lookupInstanceByType(canonicalTargetType)
            if nil == lookupErr && true == scopeExists {
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
        melodyErr, isMelodyErr := getErr.(*exception.Error)
        if true == isMelodyErr && nil != melodyErr {
            melodyErr.SetContextValue("serviceName", serviceName)

            exception.Panic(melodyErr)
        }

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

    /* the scoped registrations are looked up before the node key is settled, for the reason Get settles its own key early: the key is what the scope's dependency graph is built from. */
    scopedTypeServiceNames := []string(nil)
    scopedTypeNamesExist := false
    scopedTypeProvider := providerAny(nil)
    scopedTypeProviderExists := false
    if true == instance.scopeVisible() {
        scopedTypeServiceNames, scopedTypeNamesExist = instance.scopeInstance.scopedTypeRegistrationNames(canonicalTargetType)
        if false == scopedTypeNamesExist {
            scopedTypeProvider, scopedTypeProviderExists = instance.scopeInstance.scopedProviderByType(canonicalTargetType)
        }
    }

    nodeKey := "type:" + typeKey
    if true == scopedTypeNamesExist || true == scopedTypeProviderExists {
        nodeKey = scopedTypeNodeKey(typeKey)
    }

    parentKey := ""
    if 0 < len(instance.stack) {
        parentKey = instance.stack[len(instance.stack)-1]
    }

    pushKeyErr := instance.pushKey(nodeKey, canonicalTargetType)
    if nil != pushKeyErr {
        return nil, pushKeyErr
    }
    defer instance.popKey()

    if true == instance.scopeVisible() {
        value, exists, lookupInstanceByTypeErr := instance.scopeInstance.lookupInstanceByType(canonicalTargetType)
        if nil != lookupInstanceByTypeErr {
            return nil, lookupInstanceByTypeErr
        }

        if true == exists {
            return value, nil
        }

        if true == scopedTypeNamesExist {
            if 1 < len(scopedTypeServiceNames) {
                completeConflicts := make([]string, 0, len(scopedTypeServiceNames))
                completeConflicts = append(completeConflicts, scopedTypeServiceNames...)
                sort.Strings(completeConflicts)

                return nil, exception.NewError(
                    "scoped service type has multiple registrations",
                    exceptioncontract.Context{
                        "type":      canonicalTargetType.String(),
                        "conflicts": completeConflicts,
                    },
                    nil,
                )
            }

            scopeInstance := instance.scopeInstance
            serviceName := scopedTypeServiceNames[0]

            scopedProvider, scopedProviderExists := scopeInstance.scopedProviderByName(serviceName)
            if false == scopedProviderExists {
                return nil, exception.NewError(
                    "scoped service type names a service that is not registered",
                    exceptioncontract.Context{
                        "type":        canonicalTargetType.String(),
                        "serviceName": serviceName,
                    },
                    nil,
                )
            }

            instance.containerInstance.mutex.Lock()
            defer instance.containerInstance.mutex.Unlock()

            if "" != parentKey && true == isScopedNodeKey(parentKey) {
                registerScopedDependencyLocked(scopeInstance, parentKey, scopedNameNodeKey(serviceName))
            }

            /* the type resolves through the name it is registered under, so a scoped service reached by name and by type is one instance rather than two */
            return instance.scopedServiceByName(scopeInstance, serviceName, scopedProvider, canonicalTargetType)
        }

        if true == scopedTypeProviderExists {
            scopeInstance := instance.scopeInstance

            instance.containerInstance.mutex.Lock()
            defer instance.containerInstance.mutex.Unlock()

            if "" != parentKey && true == isScopedNodeKey(parentKey) {
                registerScopedDependencyLocked(scopeInstance, parentKey, nodeKey)
            }

            return instance.scopedServiceByType(scopeInstance, typeKey, canonicalTargetType, scopedTypeProvider)
        }
    }

    instance.containerInstance.mutex.Lock()
    defer instance.containerInstance.mutex.Unlock()

    if "" != parentKey && false == isScopedNodeKey(nodeKey) {
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
            guardedCreation{
                requestedKey: requestedKey,
                creatingKey:  serviceName,
                getCreatingState: func() (*creationState, bool) {
                    state, exists := instance.containerInstance.creatingByName[serviceName]
                    return state, exists
                },
                setCreatingState: func(state *creationState) {
                    instance.containerInstance.creatingByName[serviceName] = state
                },
                clearCreatingState: func() {
                    delete(instance.containerInstance.creatingByName, serviceName)
                },
                lookup: instance.lookupByName(serviceName),
                create: func(resolver containercontract.Resolver) (any, error, *providerDebugInfo) {
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
                store: containerNameStore(instance.containerInstance, serviceName, canonicalTargetType),
                suspendsScope: true,
            },
            instance,
        )
    }

    /* @important snapshot the provider under the container mutex before serviceWithCreationGuardLocked releases it; the create closure runs unlocked, so reading the typeProviders map there would race concurrent Register writes. */
    provider, providerExists := instance.containerInstance.typeProviders[canonicalTargetType]

    return instance.containerInstance.serviceWithCreationGuardLocked(
        guardedCreation{
            requestedKey: requestedKey,
            creatingKey:  typeKey,
            getCreatingState: func() (*creationState, bool) {
                state, exists := instance.containerInstance.creatingByType[typeKey]
                return state, exists
            },
            setCreatingState: func(state *creationState) {
                instance.containerInstance.creatingByType[typeKey] = state
            },
            clearCreatingState: func() {
                delete(instance.containerInstance.creatingByType, typeKey)
            },
            lookup: instance.lookupByType(canonicalTargetType),
            create: func(resolver containercontract.Resolver) (any, error, *providerDebugInfo) {
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
            store: containerTypeStore(instance.containerInstance, canonicalTargetType),
            suspendsScope: true,
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

        melodyErr, isMelodyErr := getByTypeErr.(*exception.Error)
        if true == isMelodyErr && nil != melodyErr {
            melodyErr.SetContextValue("type", typeString)

            exception.Panic(melodyErr)
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

/* TypesImplementing lets a provider collect its collaborators through AllImplementing with the resolver it was handed instead of needing the container itself. A resolution that can see its scope collects what the scope can reach — its scoped registrations included — while a container provider, whose scope is suspended for the duration, collects only what the container holds. The two answers differ on purpose: a process singleton must not gather members that live for one request. */
func (instance *resolverContext) TypesImplementing(interfaceType reflect.Type) []reflect.Type {
    if true == instance.scopeVisible() {
        return instance.scopeInstance.TypesImplementing(interfaceType)
    }

    return instance.containerInstance.TypesImplementing(interfaceType)
}

func (instance *resolverContext) ReferencesImplementing(interfaceType reflect.Type) []containercontract.ServiceReference {
    if true == instance.scopeVisible() {
        return instance.scopeInstance.ReferencesImplementing(interfaceType)
    }

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
