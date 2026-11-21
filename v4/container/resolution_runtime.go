package container

import (
    "context"
    "reflect"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/internal/runtimeseam"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* resolutionRuntime is the runtime a resolver answers: assembled from the resolution, never the caller's runtime object. */
type resolutionRuntime struct {
    ctx   context.Context
    kind  containercontract.Kind
    scope containercontract.Scope
}

func newDetachedRuntime(scope containercontract.Scope) *resolutionRuntime {
    return &resolutionRuntime{
        ctx:   context.Background(),
        kind:  containercontract.KindDetached,
        scope: scope,
    }
}

func (instance *resolutionRuntime) Context() context.Context {
    return instance.ctx
}

func (instance *resolutionRuntime) Scope() containercontract.Scope {
    return instance.scope
}

func (instance *resolutionRuntime) Kind() containercontract.Kind {
    return instance.kind
}

/* suspendedScope resolves through the resolver behind and refuses every change, so a process singleton never holds or ends one request's scope */
type suspendedScope struct {
    resolver containercontract.Resolver
}

func (instance *suspendedScope) Get(ctx context.Context, serviceName string) (any, error) {
    return instance.resolver.Get(ctx, serviceName)
}

func (instance *suspendedScope) MustGet(ctx context.Context, serviceName string) any {
    return instance.resolver.MustGet(ctx, serviceName)
}

func (instance *suspendedScope) GetByType(ctx context.Context, targetType reflect.Type) (any, error) {
    return instance.resolver.GetByType(ctx, targetType)
}

func (instance *suspendedScope) MustGetByType(ctx context.Context, targetType reflect.Type) any {
    return instance.resolver.MustGetByType(ctx, targetType)
}

func (instance *suspendedScope) Has(serviceName string) bool {
    return instance.resolver.Has(serviceName)
}

func (instance *suspendedScope) HasType(targetType reflect.Type) bool {
    return instance.resolver.HasType(targetType)
}

func (instance *suspendedScope) Runtime() containercontract.Runtime {
    return instance.resolver.Runtime()
}

/* the resolver's own goroutine view, never this scope, so the goroutine stays off the provider's chain */
func (instance *suspendedScope) ForGoroutine() containercontract.Resolver {
    return ForGoroutine(instance.resolver)
}


func (instance *suspendedScope) Closed() bool {
    return sourceReportsClosed(instance.resolver)
}

func (instance *suspendedScope) readsContainer() bool {
    readingSource, isReading := instance.resolver.(containerReadingSource)

    return true == isReading && true == readingSource.readsContainer()
}

func (instance *suspendedScope) closedScopeCause() error {
    return scopeClosedCause(instance.resolver)
}

func (instance *suspendedScope) isScopeClosed() bool {
    closedChecker, isClosedChecker := instance.resolver.(closedScopeChecker)

    return true == isClosedChecker && true == closedChecker.isScopeClosed()
}

func (instance *suspendedScope) isResolvingReference(reference containercontract.ServiceReference) bool {
    resolutionChecker, isResolutionChecker := instance.resolver.(referenceResolutionChecker)

    return true == isResolutionChecker && true == resolutionChecker.isResolvingReference(reference)
}

func (instance *suspendedScope) scopedMembersRefusal(interfaceType reflect.Type) error {
    if bootChecker, checksBoot := instance.resolver.(scopedMembersChecker); true == checksBoot {
        return bootChecker.scopedMembersRefusal(interfaceType)
    }

    return nil
}

func (instance *suspendedScope) recordLazyHandle(record lazyHandleRecord) {
    if recorder, records := instance.resolver.(lazyHandleRecorder); true == records {
        recorder.recordLazyHandle(record)
    }
}

func (instance *suspendedScope) TypesImplementing(interfaceType reflect.Type) []reflect.Type {
    if typeLister, isTypeLister := instance.resolver.(containercontract.TypeLister); true == isTypeLister {
        return typeLister.TypesImplementing(interfaceType)
    }

    return nil
}

func (instance *suspendedScope) ReferencesImplementing(interfaceType reflect.Type) []containercontract.ServiceReference {
    if typeLister, isTypeLister := instance.resolver.(containercontract.TypeLister); true == isTypeLister {
        return typeLister.ReferencesImplementing(interfaceType)
    }

    return nil
}

/* a runtime built over this scope may claim only the kind its creations read */
func (instance *suspendedScope) BindRuntimeKind(token runtimeseam.Token, kind containercontract.Kind) error {
    answeredKind := instance.resolver.Runtime().Kind()
    if kind == answeredKind {
        return nil
    }

    return exception.NewError(
        "the scope already serves a runtime of another kind",
        exceptioncontract.Context{
            "boundKind": answeredKind.String(),
            "kind":      kind.String(),
        },
        nil,
    )
}

func (instance *suspendedScope) RegisterScoped(serviceName string, provider any, options containercontract.RegisterOptions) error {
    return refuseSuspendedScopeDoor("RegisterScoped", serviceName)
}

func (instance *suspendedScope) MustRegisterScoped(serviceName string, provider any, options containercontract.RegisterOptions) {
    exception.Panic(refuseSuspendedScopeDoor("MustRegisterScoped", serviceName))
}

func (instance *suspendedScope) OverrideInstance(serviceName string, value any, options containercontract.OverrideOptions) error {
    return refuseSuspendedScopeDoor("OverrideInstance", serviceName)
}

func (instance *suspendedScope) MustOverrideInstance(serviceName string, value any, options containercontract.OverrideOptions) {
    exception.Panic(refuseSuspendedScopeDoor("MustOverrideInstance", serviceName))
}

func (instance *suspendedScope) OverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) error {
    return refuseSuspendedScopeDoor("OverrideProtectedInstance", serviceName)
}

func (instance *suspendedScope) MustOverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) {
    exception.Panic(refuseSuspendedScopeDoor("MustOverrideProtectedInstance", serviceName))
}

func (instance *suspendedScope) Close() error {
    return nil
}

func (instance *suspendedScope) CloseWithContext(closeContext context.Context) error {
    return nil
}

func refuseSuspendedScopeDoor(door string, serviceName string) *exception.Error {
    return exception.NewError(
        "a suspended scope cannot change what a scope holds",
        exceptioncontract.Context{
            "door":        door,
            "serviceName": serviceName,
        },
        nil,
    )
}

/* providerScope resolves through the scoped provider's chain, so a cycle through its runtime is refused, and hands every change to the real scope */
type providerScope struct {
    *suspendedScope
    scope *scope
}

func newProviderScope(view *resolverContext, scopeInstance *scope) *providerScope {
    return &providerScope{
        suspendedScope: &suspendedScope{resolver: view},
        scope:          scopeInstance,
    }
}

func (instance *providerScope) BindRuntimeKind(token runtimeseam.Token, kind containercontract.Kind) error {
    return instance.scope.BindRuntimeKind(token, kind)
}

func (instance *providerScope) RegisterScoped(serviceName string, provider any, options containercontract.RegisterOptions) error {
    return instance.scope.RegisterScoped(serviceName, provider, options)
}

func (instance *providerScope) MustRegisterScoped(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.scope.MustRegisterScoped(serviceName, provider, options)
}

func (instance *providerScope) OverrideInstance(serviceName string, value any, options containercontract.OverrideOptions) error {
    return instance.scope.OverrideInstance(serviceName, value, options)
}

func (instance *providerScope) MustOverrideInstance(serviceName string, value any, options containercontract.OverrideOptions) {
    instance.scope.MustOverrideInstance(serviceName, value, options)
}

func (instance *providerScope) OverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) error {
    return instance.scope.OverrideProtectedInstance(serviceName, value, options)
}

func (instance *providerScope) MustOverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) {
    instance.scope.MustOverrideProtectedInstance(serviceName, value, options)
}

func (instance *providerScope) Close() error {
    return instance.scope.Close()
}

func (instance *providerScope) CloseWithContext(closeContext context.Context) error {
    return instance.scope.CloseWithContext(closeContext)
}

func (instance *providerScope) SpawnChildScope(token runtimeseam.Token) (containercontract.Scope, error) {
    return instance.scope.SpawnChildScope(token)
}

var _ containercontract.Scope = (*suspendedScope)(nil)

var _ runtimeseam.KindBinder = (*suspendedScope)(nil)

var _ containercontract.Scope = (*providerScope)(nil)

var _ runtimeseam.KindBinder = (*providerScope)(nil)

var _ runtimeseam.ChildScopeSpawner = (*providerScope)(nil)

var _ containercontract.GoroutineResolver = (*suspendedScope)(nil)
