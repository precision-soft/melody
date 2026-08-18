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
    keys []string
}

func newResolutionStack() *resolutionStack {
    return &resolutionStack{keys: make([]string, 0, 8)}
}

type resolverContext struct {
    containerInstance *container
    scopeInstance     *scope
    contextId         uint64
    rootRequestedKey  string
    stack             *resolutionStack
    /* ownerKey is the node whose provider was handed this view of the resolution, and it is what a resolution performed after that provider returned belongs to. The live stack answers the question while the provider is running and is empty the moment it returns, so a service that holds its resolver and reaches through it later — container.Lazy, the ContainerCarrier pattern, any replay of deferred work — used to record no dependency edge at all, and the teardown then closed its dependencies in name order instead of after it. A handle built over the container itself has no owner and keeps that behaviour, because there is no node the container could be said to belong to. */
    ownerKey string
    /* scopeSuspended is set while a provider registered on the CONTAINER builds its service, and it is what keeps the two apart. The container is request-agnostic: a service it owns is one instance for the whole process, so its construction may read only what the container holds. A request scope layers over the container for the code that runs inside a request, not underneath the container's own wiring, and a factory that reached through it would assemble a process-lifetime singleton out of one request's values.

       Suspension is a refusal, not a substitution. A container provider that asks for something only a scope carries — the request context — is told the service does not exist, which is a wiring mistake reported where it is made; a provider that asks for the logger gets the container's agnostic one, because that is the logger a process-lifetime service should hold. Only the service actually being requested is looked up through the scope, which is the layering a caller means by resolving through a scope at all. */
    scopeSuspended bool
}

/* childOwnedBy is the view of this resolution handed to the provider of one node: the same container, the same scope, the same resolution id and the same live stack, so cycle detection and the wait graph are unchanged, with the owning node written on it. A provider that keeps it and resolves through it after it has returned is then still recorded as depending on what it resolves.

The suspension rides on the view rather than being set on the shared context and restored afterwards: the caller's own resolution continues above this frame and must keep seeing the scope, and a restore that runs at the wrong moment — after a panic unwound past it, say — would leave the wrong answer behind for everything further up. */
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

/* parentNodeKey answers the node a resolution starting here depends on: the node currently being built while a provider is running, and the node that owns this view once it has returned. */
func (instance *resolverContext) parentNodeKey() string {
    if 0 < len(instance.stack.keys) {
        return instance.stack.keys[len(instance.stack.keys)-1]
    }

    return instance.ownerKey
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

    parentKey := instance.parentNodeKey()

    /* a resolution that has nothing to write takes the read lock instead of the exclusive one. Every resolution used to take the container's exclusive lock, even one that only reads a singleton built long ago, so dependency injection had a hard ceiling that did not move with the number of cores — the http kernel alone resolves four services per request, and an application cannot route around it without giving up the container.

       Nothing to write means all three at once: no scope layered over this resolution, no node to record an edge for, and the instance already built. The last two are what keep the ordering guarantee intact — a resolution that would record an edge, including one made through a resolver a provider kept, falls through to the exclusive path that writes the graph. */
    if false == instance.scopeVisible() && "" == parentKey {
        instance.containerInstance.mutex.RLock()
        memoizedValue, memoized := instance.containerInstance.instances[serviceName]
        teardownFinished := instance.containerInstance.teardownFinished
        instance.containerInstance.mutex.RUnlock()

        /* the fast path answers out of the map, so it is the path that must ask whether the map still means anything: the creation guard below refuses a closed container, but a memoized instance never reaches it, and a resolution performed after the teardown was answered with a closed service and a nil error */
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
            /* the edge is recorded even though nothing is built: a scoped dependent resolving a scoped service the scope ALREADY holds depends on it exactly as hard as the resolution that built it, and without the edge the teardown falls back to closing the two in name order — the graph guarantee would hold only for whichever resolution happened to come first. */
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

    /* a scoped parent writes no edge into the container's graph: the teardown walks that graph only over container-created representatives, so a scope-keyed dependent is skipped there, while the scope's own teardown reads only the scope's graph — the entry would never be consulted, and the container's graph is never pruned, so a live-scope registration under a request-derived name would leave one permanent entry per name for the life of the process */
    if "" != parentKey && false == isScopedNodeKey(parentKey) && false == isScopedNodeKey(nodeKey) {
        instance.containerInstance.registerDependencyLocked(
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

/* MustGet panics with the failure the way FromResolver returns it: a melody error travels out whole with the service name written into its context in place, and only a foreign error is wrapped naming the service — a rebuilt copy would shed the log level, the already-logged mark, the capture stack and every wrapper above it, and the mark shed here made one logged provider failure file a second record at the recovery site. */
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

    /* the same parent filter as the by-name path: a scope-keyed dependent is skipped by the container teardown and the entry is never pruned, so recording it would only grow the graph */
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

/* Has answers under the same suspension Get enforces: a container-owned provider asking about a scope-only name used to hear "yes" from the very entries its Get would refuse, and an existence check that disagrees with the resolution it gates turns into a wiring decision made on one request's substitutes — or a Has-then-MustGet panic. */
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

    return nil
}

func (instance *resolverContext) popKey() {
    if 0 == len(instance.stack.keys) {
        return
    }

    instance.stack.keys = instance.stack.keys[:len(instance.stack.keys)-1]
}

func (instance *resolverContext) stackStringWithRepeat(repeatedKey string) string {
    parts := make([]string, 0, len(instance.stack.keys)+1)
    parts = append(parts, instance.stack.keys...)
    parts = append(parts, repeatedKey)

    return strings.Join(parts, " -> ")
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
