package container

import (
    "reflect"
    "runtime"
    "sort"
    "strings"
    "sync"
    "sync/atomic"

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

/* resolutionStack is the live chain of node keys one resolution is in the middle of building, held apart from the context so a provider's own view of the resolution can carry a different owner while still pushing and popping the one chain the cycle detection reads. */
type resolutionStack struct {
    /* mutex serialises the chain: it belongs to one resolution, but a goroutine a provider starts before it returns can reach it through a retained view (container.Lazy) while the provider still resolves on it. That use is outside the Lazy contract and may read an interleaved chain; the mutex keeps it from being a memory race. */
    mutex sync.Mutex
    keys  []string
}

func newResolutionStack() *resolutionStack {
    return &resolutionStack{keys: make([]string, 0, 8)}
}

/* chainWithRepeatLocked spells the chain with the key that closes a cycle appended; the caller holds the mutex */
func (instance *resolutionStack) chainWithRepeatLocked(repeatedKey string) string {
    parts := make([]string, 0, len(instance.keys)+1)
    parts = append(parts, instance.keys...)
    parts = append(parts, repeatedKey)

    return strings.Join(parts, " -> ")
}

type resolverContext struct {
    containerInstance *container
    scopeInstance     *scope
    contextId         uint64
    rootRequestedKey  string
    stack             *resolutionStack
    /* ownerKey is the node whose provider receives this view: a resolution through it after that provider returned is recorded as that node's dependency, so a service that keeps its resolver (container.Lazy, the ContainerCarrier pattern) is still closed before what it resolves. A handle built over the container itself has no owner. */
    ownerKey string
    /* scopeSuspended is set while a provider registered on the container builds its service: a process-lifetime singleton may read only what the container holds, never one request's values. Suspension is a refusal, not a substitution: a scope-only service is reported as not existing, the logger resolves to the container's own, and only the service actually requested is looked up through the scope. */
    scopeSuspended bool
    /* providerReturned is set once the provider that received this view has returned; from then on the live chain belongs to the resolution above it, and a resolution through the view starts a chain of its own */
    providerReturned atomic.Bool
    /* late marks the view lateResolution answers: the edge it writes from its owner is weak, since a retained resolver — a Lazy handle — resolves what its owner was built without */
    late bool
}

/* childOwnedBy is the view of this resolution handed to the provider of one node: the same container, scope, resolution id and live stack, with the owning node written on it. The suspension rides on the view rather than on the shared context, so the caller's resolution above this frame keeps seeing the scope, a panic included. */
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

/* lateResolution answers the view a resolution through this one starts from once its provider has returned: a chain and a resolution id of its own, with the owner, the scope and the suspension kept, so the owner's dependency edge is still recorded while concurrent resolutions through one retained view — a Lazy built over a provider's resolver — never push onto one chain. The new chain is recorded in the wait graph as awaited by the resolution that handed this view out, so a cycle closed through the retained view while that resolution is still building is refused as circular instead of waiting on a creation its own caller holds; the edge can refuse only a wait on a creation that resolution owns, so it is written only while that resolution owns one, which spares a retained resolver used after the boot the exclusive lock, and the release, deferred by the caller, removes it. A view whose provider is still running answers nil and resolves on the live chain. */
func (instance *resolverContext) lateResolution() (*resolverContext, func()) {
    if false == instance.providerReturned.Load() {
        return nil, nil
    }

    lateResolver := &resolverContext{
        containerInstance: instance.containerInstance,
        scopeInstance:     instance.scopeInstance,
        contextId:         instance.containerInstance.resolverContextIdCounter.Add(1),
        rootRequestedKey:  "",
        stack:             newResolutionStack(),
        ownerKey:          instance.ownerKey,
        scopeSuspended:    instance.scopeSuspended,
        late:              true,
    }

    containerInstance := instance.containerInstance

    containerInstance.mutex.RLock()
    ancestorBuilding := containerInstance.ownsCreationInFlightLocked(instance.contextId, instance.scopeInstance)
    containerInstance.mutex.RUnlock()

    if false == ancestorBuilding {
        return lateResolver, func() {}
    }

    containerInstance.mutex.Lock()
    containerInstance.recordResolverWaitEdgeLocked(instance.contextId, lateResolver.contextId)
    containerInstance.mutex.Unlock()

    return lateResolver, func() {
        containerInstance.mutex.Lock()
        defer containerInstance.mutex.Unlock()

        containerInstance.clearResolverWaitLocked(instance.contextId, lateResolver.contextId)
    }
}

/* writesWeakEdgeFrom answers whether the edge from parentKey is weak: one a late view writes from its owner, the resolution a retained resolver makes of what its owner was built without. An edge from a node on the late view's own chain is a provider's, written while it builds. */
func (instance *resolverContext) writesWeakEdgeFrom(parentKey string) bool {
    return true == instance.late && "" != parentKey && parentKey == instance.ownerKey
}

/* registerContainerEdgeLocked writes the edge into the container's graph, weak or strong as writesWeakEdgeFrom reads it. The container mutex is held. */
func (instance *resolverContext) registerContainerEdgeLocked(parentKey string, nodeKey string) {
    if true == instance.writesWeakEdgeFrom(parentKey) {
        instance.containerInstance.registerWeakDependencyLocked(parentKey, nodeKey)

        return
    }

    instance.containerInstance.registerDependencyLocked(parentKey, nodeKey)
}

/* registerScopedEdgeLocked writes the edge into the scope's graph, weak or strong as writesWeakEdgeFrom reads it. The container mutex is held. */
func (instance *resolverContext) registerScopedEdgeLocked(scopeInstance *scope, parentKey string, nodeKey string) {
    if true == instance.writesWeakEdgeFrom(parentKey) {
        registerScopedWeakDependencyLocked(scopeInstance, parentKey, nodeKey)

        return
    }

    registerScopedDependencyLocked(scopeInstance, parentKey, nodeKey)
}

/* parentNodeKey answers the node a resolution starting here depends on: the node currently being built while a provider is running, and the node that owns this view once it has returned. */
func (instance *resolverContext) parentNodeKey() string {
    instance.stack.mutex.Lock()
    defer instance.stack.mutex.Unlock()

    if 0 < len(instance.stack.keys) {
        return instance.stack.keys[len(instance.stack.keys)-1]
    }

    return instance.ownerKey
}

/* claimRootRequestedKey records the key the resolution started from, once, and answers it; it reads and writes under the chain's mutex, since the views sharing one chain can be used from a goroutine the provider started */
func (instance *resolverContext) claimRootRequestedKey(requestedKey string) string {
    instance.stack.mutex.Lock()
    defer instance.stack.mutex.Unlock()

    if "" == instance.rootRequestedKey {
        instance.rootRequestedKey = requestedKey
    }

    return instance.rootRequestedKey
}

/* scopeVisible reports whether this resolution may read the request scope. */
func (instance *resolverContext) scopeVisible() bool {
    return nil != instance.scopeInstance && false == instance.scopeSuspended
}

/* Closed reports whether what this resolution reads has stopped answering resolutions, the liveness question a LazyService built over a provider's resolver asks. A resolution that reads the request scope ends with its request; one that reads the container, a container provider's suspended view included, ends only when the teardown has finished. */
func (instance *resolverContext) Closed() bool {
    if true == instance.scopeVisible() {
        return instance.scopeInstance.Closed()
    }

    return instance.containerInstance.resolutionsRefused()
}

/* containerNameStore keeps a finished service under its name in the container's maps, and under the canonical type too when the resolution was type-keyed; it runs under the container mutex. An override installed while the provider ran wins and the built value is handed back as the loser; otherwise the name is marked container-built, so a later override knows the value it evicts is the container's to close. */
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

    if lateResolver, release := instance.lateResolution(); nil != lateResolver {
        defer release()

        return lateResolver.Get(serviceName)
    }

    requestedKey := instance.claimRootRequestedKey(serviceName)

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

    parentKey := instance.parentNodeKey()

    /* a resolution with nothing to write takes the read lock: no scope layered over it, no node to record an edge for, and the instance already built. A resolution that would record an edge, one through a resolver a provider kept included, takes the exclusive path that writes the graph. */
    if false == instance.scopeVisible() && "" == parentKey {
        instance.containerInstance.mutex.RLock()
        memoizedValue, memoized := instance.containerInstance.instances[serviceName]
        teardownFinished := instance.containerInstance.teardownFinished
        instance.containerInstance.mutex.RUnlock()

        /* the fast path answers out of the map, so it asks whether resolutions are still answered: a memoized instance never reaches the creation guard's closed check. */
        if true == teardownFinished {
            return nil, newContainerClosedError(serviceName)
        }

        if true == memoized {
            return memoizedValue, nil
        }
    }

    pushKeyErr := instance.pushKey(nodeKey)
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
            /* the edge is recorded even though nothing is built: a scoped dependent depends as hard on a scoped service the scope already holds as on one it builds, whichever resolution came first. */
            if "" != parentKey && true == isScopedNodeKey(parentKey) && true == isScopedNodeKey(nodeKey) {
                instance.containerInstance.mutex.Lock()
                instance.registerScopedEdgeLocked(instance.scopeInstance, parentKey, nodeKey)
                instance.containerInstance.mutex.Unlock()
            }

            return value, nil
        }

        if true == scopedProviderExists {
            scopeInstance := instance.scopeInstance

            instance.containerInstance.mutex.Lock()
            defer instance.containerInstance.mutex.Unlock()

            if "" != parentKey && true == isScopedNodeKey(parentKey) {
                instance.registerScopedEdgeLocked(scopeInstance, parentKey, nodeKey)
            }

            return instance.scopedServiceByName(scopeInstance, serviceName, scopedProvider, nil)
        }
    }

    instance.containerInstance.mutex.Lock()
    defer instance.containerInstance.mutex.Unlock()

    /* a scoped parent writes no edge into the container's graph: the teardown walks that graph only over container-created representatives, so a scope-keyed dependent is skipped there, while the scope's own teardown reads only the scope's graph — the entry would never be consulted, and the container's graph is never pruned, so a live-scope registration under a request-derived name would leave one permanent entry per name for the life of the process */
    if "" != parentKey && false == isScopedNodeKey(parentKey) && false == isScopedNodeKey(nodeKey) {
        instance.registerContainerEdgeLocked(
            parentKey,
            nodeKey,
        )
    }

    /* snapshot the provider under the container mutex before serviceWithCreationGuardLocked releases it; the create closure runs unlocked, so reading the providers map there would race concurrent Register writes. */
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

/* MustGet panics with the failure FromResolver would return: a melody error travels out whole with the service name written into its context, so it keeps its log level, its already-logged mark and its capture stack, and only a foreign error is wrapped naming the service. */
func (instance *resolverContext) MustGet(serviceName string) any {
    value, getErr := instance.Get(serviceName)
    if nil != getErr {
        melodyErr, isMelodyErr := getErr.(*exception.Error)
        /* the service name is written only where none is named yet, so a nested failure keeps the name of the service that failed */
        if true == isMelodyErr && nil != melodyErr {
            if _, named := melodyErr.Context()["serviceName"]; false == named {
                melodyErr.SetContextValue("serviceName", serviceName)
            }

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

    if lateResolver, release := instance.lateResolution(); nil != lateResolver {
        defer release()

        return lateResolver.GetByType(targetType)
    }

    requestedKey := instance.claimRootRequestedKey("type:" + typeIdentityKey(canonicalTargetType))
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

    parentKey := instance.parentNodeKey()

    pushKeyErr := instance.pushKey(nodeKey)
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
            /* mirror Get: an already-held scoped instance is depended on as hard as one built by this resolution */
            if "" != parentKey && true == isScopedNodeKey(parentKey) && true == isScopedNodeKey(nodeKey) {
                instance.containerInstance.mutex.Lock()
                instance.registerScopedEdgeLocked(instance.scopeInstance, parentKey, nodeKey)
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
                instance.registerScopedEdgeLocked(scopeInstance, parentKey, scopedNameNodeKey(serviceName))
            }

            /* the type resolves through the name it is registered under, so a scoped service reached by name and by type is one instance rather than two */
            return instance.scopedServiceByName(scopeInstance, serviceName, scopedProvider, canonicalTargetType)
        }

        if true == scopedTypeProviderExists {
            scopeInstance := instance.scopeInstance

            instance.containerInstance.mutex.Lock()
            defer instance.containerInstance.mutex.Unlock()

            if "" != parentKey && true == isScopedNodeKey(parentKey) {
                instance.registerScopedEdgeLocked(scopeInstance, parentKey, nodeKey)
            }

            return instance.scopedServiceByType(scopeInstance, typeKey, canonicalTargetType, scopedTypeProvider)
        }
    }

    instance.containerInstance.mutex.Lock()
    defer instance.containerInstance.mutex.Unlock()

    /* the same parent filter as the by-name path: a scope-keyed dependent is skipped by the container teardown and the entry is never pruned, so recording it would only grow the graph */
    if "" != parentKey && false == isScopedNodeKey(parentKey) && false == isScopedNodeKey(nodeKey) {
        instance.registerContainerEdgeLocked(
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

        /* snapshot the provider under the container mutex before serviceWithCreationGuardLocked releases it; the create closure runs unlocked, so reading the providers map there would race concurrent Register writes. */
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

    /* snapshot the provider under the container mutex before serviceWithCreationGuardLocked releases it; the create closure runs unlocked, so reading the typeProviders map there would race concurrent Register writes. */
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

/* MustGetByType panics with the failure the way FromResolverByType returns it: the melody error travels out whole with the type written into its context in place, and only a foreign error is wrapped naming the type. */
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

/* Has answers under the same suspension Get enforces, so a container-owned provider asking about a scope-only name hears no, as its Get would. */
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

func (instance *resolverContext) pushKey(creatingKey string) error {
    if "" == creatingKey {
        return exception.NewError(
            "creating key is empty",
            nil,
            nil,
        )
    }

    instance.stack.mutex.Lock()
    defer instance.stack.mutex.Unlock()

    for _, key := range instance.stack.keys {
        if key == creatingKey {
            return exception.NewError(
                "circular service dependency detected",
                exceptioncontract.Context{
                    "creatingKey": creatingKey,
                    "stack":       instance.stack.chainWithRepeatLocked(creatingKey),
                },
                nil,
            )
        }
    }

    instance.stack.keys = append(instance.stack.keys, creatingKey)

    return nil
}

func (instance *resolverContext) popKey() {
    instance.stack.mutex.Lock()
    defer instance.stack.mutex.Unlock()

    if 0 == len(instance.stack.keys) {
        return
    }

    instance.stack.keys = instance.stack.keys[:len(instance.stack.keys)-1]
}

func (instance *resolverContext) stackStringWithRepeat(repeatedKey string) string {
    instance.stack.mutex.Lock()
    defer instance.stack.mutex.Unlock()

    return instance.stack.chainWithRepeatLocked(repeatedKey)
}

/* Container answers the container behind this resolution, the door a process-lifetime service uses to replay deferred work after this context's own resolution has ended. */
func (instance *resolverContext) Container() containercontract.Container {
    if nil == instance.containerInstance {
        return nil
    }

    return instance.containerInstance
}

var (
    _ containercontract.Resolver         = (*resolverContext)(nil)
    _ containercontract.ContainerCarrier = (*resolverContext)(nil)
)
