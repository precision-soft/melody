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
        stack:             newResolutionStack(),
    }
}

func newScopeResolverContext(containerInstance *container, scopeInstance *scope) *resolverContext {
    return &resolverContext{
        containerInstance: containerInstance,
        scopeInstance:     scopeInstance,
        contextId:         containerInstance.resolverContextIdCounter.Add(1),
        rootRequestedKey:  "",
        stack:             newResolutionStack(),
    }
}

/* resolutionStack is the live chain of node keys one resolution is building, shared by every view of it so the cycle detection reads one chain. types runs parallel to keys, the canonical type of a type node and nil for a name node, compared by identity. */
type resolutionStack struct {
    keys  []string
    types []reflect.Type
}

func newResolutionStack() *resolutionStack {
    return &resolutionStack{
        keys:  make([]string, 0, 8),
        types: make([]reflect.Type, 0, 8),
    }
}

type resolverContext struct {
    containerInstance *container
    scopeInstance     *scope
    contextId         uint64
    rootRequestedKey  string
    stack             *resolutionStack
    /* ownerKey is the node whose provider received this view, so a resolution made after that provider returned is recorded as its dependency; a view over the container itself has no owner */
    ownerKey string
    /* scopeSuspended is set while a container-owned provider builds its service, which may read only what the container holds. Suspension is a refusal: a scope-only service is reported as not existing, and the logger is the container's */
    scopeSuspended bool
}

/* childOwnedBy is the view handed to one node's provider: the same container, scope, resolution id and live stack, with the owning node written on it. The suspension rides on the view, so the caller above keeps seeing the scope. */
func (instance *resolverContext) childOwnedBy(nodeKey string, scopeSuspended bool) *resolverContext {
    return &resolverContext{
        containerInstance: instance.containerInstance,
        scopeInstance:     instance.scopeInstance,
        contextId:         instance.contextId,
        rootRequestedKey:  instance.rootRequestedKey,
        stack:             instance.stack,
        ownerKey:          nodeKey,
        scopeSuspended:    scopeSuspended,
    }
}

/* parentNodeKey answers the node a resolution starting here depends on: the node being built, or the owner once its provider returned. */
func (instance *resolverContext) parentNodeKey() string {
    if 0 < len(instance.stack.keys) {
        return instance.stack.keys[len(instance.stack.keys)-1]
    }

    return instance.ownerKey
}

func (instance *resolverContext) scopeVisible() bool {
    return nil != instance.scopeInstance && false == instance.scopeSuspended
}

/* Closed reports whether what this resolution reads has stopped answering resolutions, so a LazyService over a provider's resolver turns terminal with it. A resolution reading the request scope ends with the request; one reading the container, including a container-owned provider's, ends once the teardown has finished. */
func (instance *resolverContext) Closed() bool {
    if true == instance.scopeVisible() {
        return instance.scopeInstance.Closed()
    }

    return instance.containerInstance.resolutionsRefused()
}

/* isScopeClosed lets a collection through this resolver refuse as it refuses through a closed scope, under the same predicate as Closed. */
func (instance *resolverContext) isScopeClosed() bool {
    return true == instance.scopeVisible() && true == instance.scopeInstance.isScopeClosed()
}

/* containerNameStore keeps a finished service under its name, and under the canonical type for a type-keyed resolution, under the container mutex. An override installed while the provider ran wins and the built value is handed back as the loser; otherwise the name is marked container-built. */
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
            containerInstance.recordCreationOrderLocked(containerNameNodeKey(serviceName))
            containerInstance.recordHeldIdentitiesLocked(containerNameNodeKey(serviceName), value)

            if nil != canonicalTargetType {
                containerInstance.typeInstances[canonicalTargetType] = value
                containerInstance.recordCreationOrderLocked(containerTypeNodeKey(canonicalTargetType))
                containerInstance.recordHeldIdentitiesLocked(containerTypeNodeKey(canonicalTargetType), value)
            }

            return value, false, nil
        },
    }
}

/* containerTypeStore is containerNameStore for a type-keyed registration with no name. */
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
            containerInstance.recordCreationOrderLocked(containerTypeNodeKey(canonicalTargetType))
            containerInstance.recordHeldIdentitiesLocked(containerTypeNodeKey(canonicalTargetType), value)

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

    /* the node key is settled before the push, since a scoped node must not wear the container's key */
    scopedProvider := providerAny(nil)
    scopedProviderExists := false
    if true == instance.scopeVisible() {
        scopedProvider, scopedProviderExists = instance.scopeInstance.scopedProviderByName(serviceName)
    }

    nodeKey := containerNameNodeKey(serviceName)
    if true == scopedProviderExists {
        nodeKey = scopedNameNodeKey(serviceName)
    }

    parentKey := instance.parentNodeKey()

    /* a resolution with nothing to write — no scope, no node to record an edge for, the instance already built — takes the read lock; anything that records an edge falls through to the exclusive path */
    if false == instance.scopeVisible() && "" == parentKey {
        instance.containerInstance.mutex.RLock()
        memoizedValue, memoized := instance.containerInstance.instances[serviceName]
        teardownFinished := instance.containerInstance.teardownFinished
        instance.containerInstance.mutex.RUnlock()

        /* the fast path answers out of the map, so it refuses a finished teardown itself */
        if true == teardownFinished {
            return nil, newContainerClosedError(serviceName)
        }

        if true == memoized {
            return memoizedValue, nil
        }
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

        /* an installed override answers before anything is built */
        if true == exists {
            /* the edge is recorded even when the scope already holds the service */
            if "" != parentKey && true == isScopedNodeKey(parentKey) && true == isScopedNodeKey(nodeKey) {
                instance.containerInstance.mutex.Lock()
                registerScopedDependencyLocked(instance.scopeInstance, parentKey, nodeKey)
                instance.containerInstance.mutex.Unlock()
            }

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

    /* a scoped parent writes no edge into the container's graph, which never reads it and is never pruned */
    if "" != parentKey && false == isScopedNodeKey(parentKey) && false == isScopedNodeKey(nodeKey) {
        instance.containerInstance.registerDependencyLocked(
            parentKey,
            nodeKey,
        )
    }

    /* the provider is read under the mutex, since the create closure runs unlocked */
    provider, providerExists := instance.containerInstance.providers[serviceName]

    return instance.containerInstance.serviceWithCreationGuardLocked(
        guardedCreation{
            requestedKey: requestedKey,
            creatingKey:  serviceName,
            ownerNodeKey: nodeKey,
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
            store:         containerNameStore(instance.containerInstance, serviceName, nil),
            suspendsScope: true,
        },
        instance,
    )
}

/* lookupByName finds a named service for the creation guard: this scope's instance first, then the process-wide one. A closed scope reports nothing; the store raises that failure. */
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

/* lookupByType is lookupByName for a type-keyed resolution. */
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

/* MustGet panics with the failure FromResolver would return: a melody error whole, the service name in its context, and a foreign error wrapped naming the service. */
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
        instance.rootRequestedKey = containerTypeNodeKey(canonicalTargetType)
    }

    requestedKey := instance.rootRequestedKey
    typeKey := typeIdentityKey(canonicalTargetType)

    /* the scoped registrations are looked up before the node key is settled */
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

    nodeKey := containerTypeNodeKeyPrefix + typeKey
    if true == scopedTypeNamesExist || true == scopedTypeProviderExists {
        nodeKey = scopedTypeNodeKey(typeKey)
    }

    parentKey := instance.parentNodeKey()

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
            /* an already-held scoped instance is depended on, as in Get */
            if "" != parentKey && true == isScopedNodeKey(parentKey) && true == isScopedNodeKey(nodeKey) {
                instance.containerInstance.mutex.Lock()
                registerScopedDependencyLocked(instance.scopeInstance, parentKey, nodeKey)
                instance.containerInstance.mutex.Unlock()
            }

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

            /* the type resolves through its registered name, so name and type reach one instance */
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

    /* a scope-keyed dependent writes no edge into the container's graph, as in Get */
    if "" != parentKey && false == isScopedNodeKey(parentKey) && false == isScopedNodeKey(nodeKey) {
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

        /* the provider is read under the mutex, since the create closure runs unlocked */
        provider, providerExists := instance.containerInstance.providers[serviceName]

        return instance.containerInstance.serviceWithCreationGuardLocked(
            guardedCreation{
                requestedKey: requestedKey,
                creatingKey:  serviceName,
                ownerNodeKey: nodeKey,
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
                store:         containerNameStore(instance.containerInstance, serviceName, canonicalTargetType),
                suspendsScope: true,
            },
            instance,
        )
    }

    /* the provider is read under the mutex, since the create closure runs unlocked */
    provider, providerExists := instance.containerInstance.typeProviders[canonicalTargetType]

    return instance.containerInstance.serviceWithCreationGuardLocked(
        guardedCreation{
            requestedKey: requestedKey,
            creatingKey:  typeKey,
            ownerNodeKey: nodeKey,
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
            store:         containerTypeStore(instance.containerInstance, canonicalTargetType),
            suspendsScope: true,
        },
        instance,
    )
}

/* MustGetByType panics with the failure FromResolverByType would return: a melody error whole, the type in its context, and a foreign error wrapped naming the type. */
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

/* Has answers under the same suspension Get enforces. */
func (instance *resolverContext) Has(serviceName string) bool {
    if true == instance.scopeVisible() {
        return instance.scopeInstance.Has(serviceName)
    }

    return instance.containerInstance.Has(serviceName)
}

func (instance *resolverContext) HasType(targetType reflect.Type) bool {
    if true == instance.scopeVisible() {
        return instance.scopeInstance.HasType(targetType)
    }

    return instance.containerInstance.HasType(targetType)
}

func (instance *resolverContext) pushKey(creatingKey string, creatingType reflect.Type) error {
    if "" == creatingKey {
        return exception.NewError(
            "creating key is empty",
            nil,
            nil,
        )
    }

    for _, key := range instance.stack.keys {
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

    instance.stack.keys = append(instance.stack.keys, creatingKey)
    instance.stack.types = append(instance.stack.types, creatingType)

    return nil
}

func (instance *resolverContext) popKey() {
    if 0 == len(instance.stack.keys) {
        return
    }

    instance.stack.keys = instance.stack.keys[:len(instance.stack.keys)-1]
    instance.stack.types = instance.stack.types[:len(instance.stack.types)-1]
}

func (instance *resolverContext) stackStringWithRepeat(repeatedKey string) string {
    parts := make([]string, 0, len(instance.stack.keys)+1)
    parts = append(parts, instance.stack.keys...)
    parts = append(parts, repeatedKey)

    return strings.Join(parts, " -> ")
}

/* TypesImplementing lets a provider collect through the resolver it receives. A resolution seeing its scope collects what the scope reaches; a container provider, its scope suspended, collects only the container's services. */
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

/* isResolvingReference reports whether the reference is the service this context is creating now, the innermost node; only that one is excluded from a collection, so a deeper one fails as the circular dependency it is. On a type node the exclusion narrows to the name held in creation, by reflect.Type identity. */
func (instance *resolverContext) isResolvingReference(reference containercontract.ServiceReference) bool {
    if 0 == len(instance.stack.keys) {
        return false
    }

    topIndex := len(instance.stack.keys) - 1

    if containerNameNodeKey(reference.ServiceName) == instance.stack.keys[topIndex] {
        return true
    }

    topType := instance.stack.types[topIndex]
    if nil == topType || topType != reference.ServiceType {
        return false
    }

    instance.containerInstance.mutex.RLock()
    state, isCreating := instance.containerInstance.creatingByName[reference.ServiceName]
    instance.containerInstance.mutex.RUnlock()

    if true == isCreating {
        return state.ownerContextId == instance.contextId
    }

    /* an idle sibling name of the collector's type */
    return false
}

var (
    _ containercontract.Resolver   = (*resolverContext)(nil)
    _ containercontract.TypeLister = (*resolverContext)(nil)
    _ referenceResolutionChecker   = (*resolverContext)(nil)
    _ closedScopeChecker           = (*resolverContext)(nil)
)
