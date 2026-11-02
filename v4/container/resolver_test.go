package container

import (
    "context"
    "errors"
    "io"
    "reflect"
    "strings"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    alpha "github.com/precision-soft/melody/v4/container/internal/collisionalpha/contract"
    beta "github.com/precision-soft/melody/v4/container/internal/collisionbeta/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

type resolverTestService struct {
    value string
}

type resolverTestResolver struct {
    servicesByName map[string]any
    servicesByType map[reflect.Type]any
    getErr         error
}

func (instance *resolverTestResolver) Get(ctx context.Context, serviceName string) (any, error) {
    if nil != instance.getErr {
        return nil, instance.getErr
    }

    value, exists := instance.servicesByName[serviceName]
    if false == exists {
        return nil, errors.New("service missing")
    }

    return value, nil
}

func (instance *resolverTestResolver) MustGet(ctx context.Context, serviceName string) any {
    value, err := instance.Get(ctx, serviceName)
    if nil != err {
        exception.Panic(
            exception.FromError(err),
        )
    }

    return value
}

func (instance *resolverTestResolver) GetByType(ctx context.Context, targetType reflect.Type) (any, error) {
    value, exists := instance.servicesByType[targetType]
    if false == exists {
        return nil, errors.New("service missing")
    }

    return value, nil
}

func (instance *resolverTestResolver) MustGetByType(ctx context.Context, targetType reflect.Type) any {
    value, err := instance.GetByType(ctx, targetType)
    if nil != err {
        exception.Panic(
            exception.FromError(err),
        )
    }

    return value
}

func (instance *resolverTestResolver) Has(serviceName string) bool {
    _, exists := instance.servicesByName[serviceName]

    return true == exists
}

func (instance *resolverTestResolver) HasType(targetType reflect.Type) bool {
    _, exists := instance.servicesByType[targetType]

    return true == exists
}

func TestFromResolver_HappyPath(t *testing.T) {
    resolver := &resolverTestResolver{
        servicesByName: map[string]any{
            "service.test": &resolverTestService{value: "ok"},
        },
        servicesByType: map[reflect.Type]any{},
    }

    value, err := FromResolver[*resolverTestService](t.Context(), resolver, "service.test")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "ok" != value.value {
        t.Fatalf("unexpected value: %s", value.value)
    }
}

func TestFromResolver_MissingService_ReturnsError(t *testing.T) {
    resolver := &resolverTestResolver{
        servicesByName: map[string]any{},
        servicesByType: map[reflect.Type]any{},
    }

    _, err := FromResolver[*resolverTestService](t.Context(), resolver, "service.missing")
    if nil == err {
        t.Fatalf("expected error")
    }

    typedError, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected *exception.Error, got: %T", err)
    }

    if "service resolution failed in resolver" != typedError.Message() {
        t.Fatalf("unexpected error message: %s", typedError.Message())
    }
}

/* the double's refusal is a foreign error, the shape a provider handing back its driver's error raw takes: it is kept as the cause under the title of the resolution, and the title does not call the service unregistered — the twin door by type dresses it the same way */
func TestFromResolver_AForeignProviderErrorKeepsItsCauseUnderTheResolutionTitle(t *testing.T) {
    cause := errors.New("dial tcp: connection refused")

    byName := &resolverTestResolver{
        servicesByName: map[string]any{},
        servicesByType: map[reflect.Type]any{},
        getErr:         cause,
    }

    _, nameErr := FromResolver[*resolverTestService](t.Context(), byName, "service.dialing")
    assertResolutionTitleKeepsTheCause(t, nameErr, cause, "serviceName")

    serviceContainer := NewContainer(ContainerConfig{})
    serviceContainer.MustRegister(
        "service.dialing",
        func(resolver containercontract.Resolver) (*resolverTestService, error) {
            return nil, cause
        },
        RegisterOptions{},
    )

    _, typeErr := FromResolverByType[*resolverTestService](t.Context(), serviceContainer)
    assertResolutionTitleKeepsTheCause(t, typeErr, cause, "serviceType")
}

func assertResolutionTitleKeepsTheCause(t *testing.T, err error, cause error, contextKey string) {
    t.Helper()

    typedError, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected *exception.Error, got %T", err)
    }

    if "service resolution failed in resolver" != typedError.Message() {
        t.Fatalf("expected the resolution title, got %q", typedError.Message())
    }

    if false == errors.Is(err, cause) {
        t.Fatalf("expected the provider's own error to stay the cause")
    }

    if _, named := typedError.Context()[contextKey]; false == named {
        t.Fatalf("expected the refusal to carry %s, got %+v", contextKey, typedError.Context())
    }
}

func TestFromResolver_TypeMismatch(t *testing.T) {
    resolver := &resolverTestResolver{
        servicesByName: map[string]any{
            "service.test": "not a service",
        },
        servicesByType: map[reflect.Type]any{},
    }

    _, err := FromResolver[*resolverTestService](t.Context(), resolver, "service.test")
    if nil == err {
        t.Fatalf("expected error")
    }

    typedError, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected *exception.Error, got: %T", err)
    }

    if "service has wrong type" != typedError.Message() {
        t.Fatalf("unexpected error message: %s", typedError.Message())
    }
}

func TestMustFromResolver_PanicsOnError(t *testing.T) {
    resolver := &resolverTestResolver{
        servicesByName: map[string]any{},
        servicesByType: map[reflect.Type]any{},
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    _ = MustFromResolver[*resolverTestService](t.Context(), resolver, "service.missing")
}

func TestMustFromResolverByType_PanicsOnNilValue(t *testing.T) {
    targetType := reflect.TypeOf(&resolverTestService{})
    resolver := &resolverTestResolver{
        servicesByName: map[string]any{},
        servicesByType: map[reflect.Type]any{
            targetType: (*resolverTestService)(nil),
        },
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic when the resolver returns a nil value by type")
        }
    }()

    _ = MustFromResolverByType[*resolverTestService](t.Context(), resolver)
}

func TestFromResolverByType_HappyPath(t *testing.T) {
    targetType := reflect.TypeOf(&resolverTestService{})
    resolver := &resolverTestResolver{
        servicesByName: map[string]any{},
        servicesByType: map[reflect.Type]any{
            targetType: &resolverTestService{value: "ok"},
        },
    }

    value, err := FromResolverByType[*resolverTestService](t.Context(), resolver)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "ok" != value.value {
        t.Fatalf("unexpected value: %s", value.value)
    }
}

func TestFromResolverByType_MissingService_ReturnsError(t *testing.T) {
    resolver := &resolverTestResolver{
        servicesByName: map[string]any{},
        servicesByType: map[reflect.Type]any{},
    }

    _, err := FromResolverByType[*resolverTestService](t.Context(), resolver)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestFromResolverByType_TypeMismatch(t *testing.T) {
    targetType := reflect.TypeOf(&resolverTestService{})
    resolver := &resolverTestResolver{
        servicesByName: map[string]any{},
        servicesByType: map[reflect.Type]any{
            targetType: "not a service",
        },
    }

    _, err := FromResolverByType[*resolverTestService](t.Context(), resolver)
    if nil == err {
        t.Fatalf("expected error")
    }

    typedError, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected *exception.Error, got: %T", err)
    }

    if "resolved service has unexpected type" != typedError.Message() {
        t.Fatalf("unexpected error message: %s", typedError.Message())
    }
}

func TestContainer_MustFromResolver_PanicsWhenMissing(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    /* an unqualified recover accepts any panic at all, including one thrown by an unrelated guard in the same call; the message is what names the refusal under test */
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = MustFromResolver[*testService](t.Context(), serviceContainer, "service.missing")
        },
        "service is not registered",
    )
}

var _ containercontract.Resolver = (*resolverTestResolver)(nil)

func TestResolution_SameStringTypesFromDifferentPackagesDoNotAlias(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister(serviceContainer, "bus.beta", func(resolver containercontract.Resolver) (*beta.Bus, error) {
        return &beta.Bus{Region: "beta"}, nil
    }, RegisterOptions{})

    MustRegister(serviceContainer, "bus.alpha", func(resolver containercontract.Resolver) (*alpha.Bus, error) {
        /* the alpha provider resolves the beta bus by type: both nodes key on "contract.Bus" through String(), so a shared key would flag a false cycle here */
        betaBus, betaErr := FromResolverByType[*beta.Bus](ContextOf(resolver), resolver)
        if nil != betaErr {
            return nil, betaErr
        }

        return &alpha.Bus{Region: "alpha:" + betaBus.Region}, nil
    }, RegisterOptions{})

    alphaBus, getErr := FromResolverByType[*alpha.Bus](t.Context(), serviceContainer)
    if nil != getErr {
        t.Fatalf("expected the alpha bus to resolve through the beta bus, got %v", getErr)
    }

    if "alpha:beta" != alphaBus.Region {
        t.Fatalf("expected the nested by-type resolution to compose, got %q", alphaBus.Region)
    }

    betaBus, betaGetErr := FromResolverByType[*beta.Bus](t.Context(), serviceContainer)
    if nil != betaGetErr {
        t.Fatalf("expected the beta bus to resolve, got %v", betaGetErr)
    }

    if "beta" != betaBus.Region {
        t.Fatalf("expected the beta bus to keep its own value, got %q", betaBus.Region)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected the container with same-string types to close cleanly, got %v", closeErr)
    }
}

type melodyErrorResolver struct {
    err *exception.Error
}

func (instance *melodyErrorResolver) Get(ctx context.Context, serviceName string) (any, error) {
    return nil, instance.err
}

func (instance *melodyErrorResolver) MustGet(ctx context.Context, serviceName string) any {
    exception.Panic(instance.err)

    return nil
}

func (instance *melodyErrorResolver) GetByType(ctx context.Context, targetType reflect.Type) (any, error) {
    return nil, instance.err
}

func (instance *melodyErrorResolver) MustGetByType(ctx context.Context, targetType reflect.Type) any {
    exception.Panic(instance.err)

    return nil
}

func (instance *melodyErrorResolver) Has(serviceName string) bool {
    return false
}

func (instance *melodyErrorResolver) HasType(targetType reflect.Type) bool {
    return false
}

func TestFromResolver_MelodyErrorPassesThroughWithServiceName(t *testing.T) {
    originalErr := exception.NewError(
        "the original refusal",
        map[string]any{"detail": "kept"},
        errors.New("the root cause"),
    )
    originalErr.MarkAsLogged()

    resolver := &melodyErrorResolver{err: originalErr}

    _, fromResolverErr := FromResolver[*resolverTestService](t.Context(), resolver, "service.original")
    if nil == fromResolverErr {
        t.Fatalf("expected the resolution error to propagate")
    }

    var typedError *exception.Error
    if false == errors.As(fromResolverErr, &typedError) {
        t.Fatalf("expected a melody error, got %T", fromResolverErr)
    }

    if false == typedError.AlreadyLogged() {
        t.Fatalf("expected the already-logged mark to survive the pass-through")
    }

    if "the original refusal" != typedError.Message() {
        t.Fatalf("unexpected error message: %s", typedError.Message())
    }

    if "service.original" != typedError.Context()["serviceName"] {
        t.Fatalf("expected the service name to be added to the original error's context")
    }

    if "kept" != typedError.Context()["detail"] {
        t.Fatalf("expected the original context to survive")
    }
}

type resolverTestTypedNilError struct{}

func (instance *resolverTestTypedNilError) Error() string {
    return "never reached on a nil receiver"
}

type resolverTestTypedNilErrorResolver struct {
    value any
}

func (instance *resolverTestTypedNilErrorResolver) Get(ctx context.Context, serviceName string) (any, error) {
    var typedNil *resolverTestTypedNilError
    return instance.value, typedNil
}

func (instance *resolverTestTypedNilErrorResolver) MustGet(ctx context.Context, serviceName string) any {
    return instance.value
}

func (instance *resolverTestTypedNilErrorResolver) GetByType(ctx context.Context, targetType reflect.Type) (any, error) {
    var typedNil *resolverTestTypedNilError
    return instance.value, typedNil
}

func (instance *resolverTestTypedNilErrorResolver) MustGetByType(ctx context.Context, targetType reflect.Type) any {
    return instance.value
}

func (instance *resolverTestTypedNilErrorResolver) Has(serviceName string) bool { return true }

func (instance *resolverTestTypedNilErrorResolver) HasType(targetType reflect.Type) bool {
    return true
}

func TestFromResolver_TypedNilErrorReadsAsSuccess(t *testing.T) {
    resolver := &resolverTestTypedNilErrorResolver{value: &resolverTestService{value: "alive"}}

    resolved, fromResolverErr := FromResolver[*resolverTestService](t.Context(), resolver, "service.probe")
    if nil != fromResolverErr {
        t.Fatalf("expected the typed-nil error to read as success, got %v", fromResolverErr)
    }

    if "alive" != resolved.value {
        t.Fatalf("expected the resolved value to come through")
    }
}

func TestFromResolverByType_TypedNilErrorReadsAsSuccess(t *testing.T) {
    resolver := &resolverTestTypedNilErrorResolver{value: &resolverTestService{value: "alive"}}

    resolved, fromResolverErr := FromResolverByType[*resolverTestService](t.Context(), resolver)
    if nil != fromResolverErr {
        t.Fatalf("expected the typed-nil error to read as success, got %v", fromResolverErr)
    }

    if "alive" != resolved.value {
        t.Fatalf("expected the resolved value to come through")
    }
}

func TestMustFromResolverByType_TypedNilErrorWithNilValueNamesTheFailure(t *testing.T) {
    resolver := &resolverTestTypedNilErrorResolver{value: (*resolverTestService)(nil)}

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected a panic for the nil value")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if false == strings.Contains(recoveredErr.Error(), "resolver returned nil value") {
            t.Fatalf("expected the refusal to name the nil value, got %q", recoveredErr.Error())
        }
    }()

    _ = MustFromResolverByType[*resolverTestService](t.Context(), resolver)
}

func TestFromResolver_NilResolverIsRefusedInsteadOfDereferenced(t *testing.T) {
    value, fromResolverErr := FromResolver[*resolverTestService](t.Context(), nil, "app.service")

    if nil == fromResolverErr {
        t.Fatalf("expected a nil resolver to be refused")
    }

    if "resolver is nil" != fromResolverErr.Error() {
        t.Fatalf("unexpected refusal message: %q", fromResolverErr.Error())
    }

    if nil != value {
        t.Fatalf("expected the zero value alongside the refusal")
    }

    melodyErr := exception.FromError(fromResolverErr)
    if "app.service" != melodyErr.Context()["serviceName"] {
        t.Fatalf("expected the refusal to name the service, got %v", melodyErr.Context()["serviceName"])
    }
}

func TestFromResolverByType_NilResolverIsRefusedInsteadOfDereferenced(t *testing.T) {
    _, fromResolverByTypeErr := FromResolverByType[*resolverTestService](t.Context(), nil)

    if nil == fromResolverByTypeErr {
        t.Fatalf("expected a nil resolver to be refused by the type door as well")
    }

    if "resolver is nil" != fromResolverByTypeErr.Error() {
        t.Fatalf("unexpected refusal message: %q", fromResolverByTypeErr.Error())
    }
}

func TestLazy_NilResolverSurfacesAsAnErrorAtFirstUse(t *testing.T) {
    lazyService := Lazy[*resolverTestService](nil, "app.service")

    _, resolveErr := lazyService.Resolve(t.Context())

    if nil == resolveErr {
        t.Fatalf("expected the deferred resolution to report the nil resolver")
    }

    if "resolver is nil" != resolveErr.Error() {
        t.Fatalf("unexpected refusal message: %q", resolveErr.Error())
    }
}

/* the type-keyed door dresses its failure the way the name-keyed twin does: a melody error travels out whole with the type written into its context in place, and a foreign error is wrapped naming the type, so the operator reading either record learns which resolution failed. */
func TestFromResolverByType_EnrichesTheFailureLikeTheNameKeyedTwin(t *testing.T) {
    melodyErr := exception.NewError("service creation failed", nil, nil)

    _, enrichedErr := FromResolverByType[*resolverTestService](t.Context(), &melodyErrorResolver{err: melodyErr})
    if enrichedErr != melodyErr {
        t.Fatalf("expected the melody error to travel out whole, got %v", enrichedErr)
    }

    serviceType, exists := melodyErr.Context()["serviceType"]
    if false == exists || "" == serviceType {
        t.Fatalf("expected the failure context to carry the service type, got %v", melodyErr.Context())
    }

    _, wrappedErr := FromResolverByType[*resolverTestService](t.Context(), &resolverTestResolver{
        servicesByName: map[string]any{},
        servicesByType: map[reflect.Type]any{},
    })
    if nil == wrappedErr {
        t.Fatal("expected the foreign failure to be reported")
    }

    var wrappedMelodyErr *exception.Error
    if false == errors.As(wrappedErr, &wrappedMelodyErr) || nil == wrappedMelodyErr {
        t.Fatalf("expected the foreign failure to be wrapped, got %v", wrappedErr)
    }

    if _, typeExists := wrappedMelodyErr.Context()["serviceType"]; false == typeExists {
        t.Fatalf("expected the wrapper to name the service type, got %v", wrappedMelodyErr.Context())
    }
}

/* a failure that already names a service — the nested resolution that failed — keeps that name through the outer resolution: the outer name would point the reader at a service that is registered */
func TestFromResolver_KeepsTheServiceNameANestedFailureCarries(t *testing.T) {
    nestedErr := exception.NewError(
        "service is not registered",
        map[string]any{"serviceName": "service.nested"},
        nil,
    )

    _, fromResolverErr := FromResolver[*resolverTestService](t.Context(), &melodyErrorResolver{err: nestedErr}, "service.outer")

    var typedError *exception.Error
    if false == errors.As(fromResolverErr, &typedError) {
        t.Fatalf("expected a melody error, got %T", fromResolverErr)
    }

    if "service.nested" != typedError.Context()["serviceName"] {
        t.Fatalf("expected the nested failure to keep the name of the service that failed, got %v", typedError.Context()["serviceName"])
    }
}

/* the type-keyed twin keeps a nested failure's type as the name-keyed twin keeps a nested name: written only where none is named yet. */
func TestFromResolverByType_KeepsTheServiceTypeANestedFailureCarries(t *testing.T) {
    nestedErr := exception.NewError(
        "service type is not registered",
        map[string]any{"serviceType": "*probe.Inner"},
        nil,
    )

    _, enrichedErr := FromResolverByType[*resolverTestService](t.Context(), &melodyErrorResolver{err: nestedErr})
    if enrichedErr != nestedErr {
        t.Fatalf("expected the nested failure to travel out whole, got %v", enrichedErr)
    }

    if "*probe.Inner" != nestedErr.Context()["serviceType"] {
        t.Fatalf("expected the nested type to be kept, got %v", nestedErr.Context())
    }
}

/* a type the door asks for directly and the container does not register is refused naming it under `type`; the door still writes its own key, `serviceType`, the same type, since the failure is its own and not a nested one. */
func TestFromResolverByType_NamesItsOwnUnregisteredTypeUnderItsKey(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    _, resolveErr := FromResolverByType[*resolverTestService](t.Context(), serviceContainer.NewScope())

    var melodyErr *exception.Error
    if false == errors.As(resolveErr, &melodyErr) {
        t.Fatalf("expected a melody error, got %v", resolveErr)
    }

    requested := reflect.TypeOf((*resolverTestService)(nil)).String()
    if requested != melodyErr.Context()["serviceType"] || requested != melodyErr.Context()["type"] {
        t.Fatalf("expected the requested type under both keys, got %v", melodyErr.Context())
    }
}

/* a nested failure that names its type under the other door's key, `type`, is kept as named: the outer type is not written beside it. */
func TestFromResolverByType_KeepsTheTypeANestedFailureNamesUnderTheOtherKey(t *testing.T) {
    nestedErr := exception.NewError(
        "service type is not registered",
        map[string]any{"type": "*probe.Inner"},
        nil,
    )

    _, enrichedErr := FromResolverByType[*resolverTestService](t.Context(), &melodyErrorResolver{err: nestedErr})
    if enrichedErr != nestedErr {
        t.Fatalf("expected the nested failure to travel out whole, got %v", enrichedErr)
    }

    if _, written := nestedErr.Context()["serviceType"]; true == written {
        t.Fatalf("expected no outer type written beside the nested one, got %v", nestedErr.Context())
    }
}

/* a value type requested where its pointer is registered is resolved under the canonical pointer type and refused by the assertion on the value type: the refusal names the type that was asked for beside the canonical one it resolved, so the two read apart. */
func TestFromResolverByType_MismatchNamesTheRequestedTypeBesideTheCanonical(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    serviceContainer.MustRegister(
        "service.plain",
        func(resolver containercontract.Resolver) (*resolverTestService, error) {
            return &resolverTestService{}, nil
        },
        RegisterOptions{},
    )

    _, mismatchErr := FromResolverByType[resolverTestService](t.Context(), serviceContainer)
    typedError, isTyped := mismatchErr.(*exception.Error)
    if false == isTyped || "resolved service has unexpected type" != typedError.Message() {
        t.Fatalf("expected the value type to be refused by the assertion, got %v", mismatchErr)
    }

    context := typedError.Context()
    if "container.resolverTestService" != context["expectedType"] || "*container.resolverTestService" != context["canonicalType"] || "*container.resolverTestService" != context["actualType"] {
        t.Fatalf("expected the requested, canonical and actual types to be told apart, got %v", context)
    }
}

type resolutionContextKey struct{}

func TestContextOf_AProvidersResolverAnswersTheContextOfTheCallThatStartedTheCreation(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    seen := make(chan context.Context, 1)
    MustRegister[*testService](serviceContainer, "app.context.of", func(resolver containercontract.Resolver) (*testService, error) {
        seen <- ContextOf(resolver)

        return &testService{Value: "built"}, nil
    }, RegisterOptions{})

    outerContext := context.WithValue(context.Background(), resolutionContextKey{}, "outer call")

    if _, getErr := serviceContainer.Get(outerContext, "app.context.of"); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    if "outer call" != (<-seen).Value(resolutionContextKey{}) {
        t.Fatalf("expected the provider's resolver to carry the outer call's context")
    }
}

func TestContextOf_TheContainerAScopeAndAForeignResolverAnswerBackground(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    scopeInstance := serviceContainer.NewScope()
    defer func() {
        _ = scopeInstance.Close()
    }()

    for name, resolver := range map[string]containercontract.Resolver{
        "container": serviceContainer,
        "scope":     scopeInstance,
        "foreign":   &resolverTestResolver{},
    } {
        if context.Background() != ContextOf(resolver) {
            t.Fatalf("expected %s to answer context.Background()", name)
        }
    }
}

func TestResolverDoors_ANilContextIsRefused(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    MustRegister[*testService](serviceContainer, "app.nil.context", func(resolver containercontract.Resolver) (*testService, error) {
        return &testService{Value: "built"}, nil
    }, RegisterOptions{})

    serviceType := reflect.TypeOf((*testService)(nil))

    var nilContext context.Context

    viewErrors := make(chan error, 2)
    MustRegister[*waitingResolverProbe](serviceContainer, "app.nil.context.view", func(resolver containercontract.Resolver) (*waitingResolverProbe, error) {
        _, getErr := resolver.Get(nilContext, "app.nil.context")
        viewErrors <- getErr

        _, getByTypeErr := resolver.GetByType(nilContext, serviceType)
        viewErrors <- getByTypeErr

        return &waitingResolverProbe{value: "view"}, nil
    }, RegisterOptions{})

    scopeInstance := serviceContainer.NewScope()
    defer func() {
        _ = scopeInstance.Close()
    }()

    for name, resolver := range map[string]containercontract.Resolver{"container": serviceContainer, "scope": scopeInstance} {
        if _, getErr := resolver.Get(nilContext, "app.nil.context"); nil == getErr || false == strings.Contains(getErr.Error(), "context is required in get") {
            t.Fatalf("expected %s Get to refuse a nil context, got %v", name, getErr)
        }

        if _, getByTypeErr := resolver.GetByType(nilContext, serviceType); nil == getByTypeErr || false == strings.Contains(getByTypeErr.Error(), "context is required in get by type") {
            t.Fatalf("expected %s GetByType to refuse a nil context, got %v", name, getByTypeErr)
        }
    }

    if _, getErr := serviceContainer.Get(context.Background(), "app.nil.context.view"); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    for _, viewErr := range []error{<-viewErrors, <-viewErrors} {
        if nil == viewErr || false == strings.Contains(viewErr.Error(), "context is required") {
            t.Fatalf("expected the provider's resolver to refuse a nil context, got %v", viewErr)
        }
    }
}

func TestContextOf_ARetainedProviderResolverAnswersBackgroundOnceItsProviderReturned(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    retained := make(chan containercontract.Resolver, 1)
    MustRegister[*testService](serviceContainer, "app.context.retained", func(resolver containercontract.Resolver) (*testService, error) {
        retained <- resolver

        return &testService{Value: "built"}, nil
    }, RegisterOptions{})

    outerContext := context.WithValue(context.Background(), resolutionContextKey{}, "outer call")

    if _, getErr := serviceContainer.Get(outerContext, "app.context.retained"); nil != getErr {
        t.Fatalf("unexpected error: %v", getErr)
    }

    if context.Background() != ContextOf(<-retained) {
        t.Fatalf("expected a provider's resolver retained past its provider to answer context.Background()")
    }
}

type typedNilContext struct {
    context.Context
}

func TestResolverDoors_ATypedNilContextAndTheTypedDoorsRefuseANilContext(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    MustRegister[*testService](serviceContainer, "app.typed.nil.context", func(resolver containercontract.Resolver) (*testService, error) {
        return &testService{Value: "built"}, nil
    }, RegisterOptions{})

    typedNil := (*typedNilContext)(nil)

    if _, getErr := serviceContainer.Get(typedNil, "app.typed.nil.context"); nil == getErr || false == strings.Contains(getErr.Error(), "context is required in get") {
        t.Fatalf("expected Get to refuse a typed-nil context, got %v", getErr)
    }

    if _, getByTypeErr := serviceContainer.GetByType(typedNil, reflect.TypeOf((*testService)(nil))); nil == getByTypeErr || false == strings.Contains(getByTypeErr.Error(), "context is required in get by type") {
        t.Fatalf("expected GetByType to refuse a typed-nil context, got %v", getByTypeErr)
    }

    var nilContext context.Context

    if _, fromErr := FromResolver[*testService](nilContext, serviceContainer, "app.typed.nil.context"); nil == fromErr || false == strings.Contains(fromErr.Error(), "context is required in from resolver") {
        t.Fatalf("expected FromResolver to refuse a nil context, got %v", fromErr)
    }

    if _, fromByTypeErr := FromResolverByType[*testService](nilContext, serviceContainer); nil == fromByTypeErr || false == strings.Contains(fromByTypeErr.Error(), "context is required in from resolver by type") {
        t.Fatalf("expected FromResolverByType to refuse a nil context, got %v", fromByTypeErr)
    }

    emptyContainer := NewContainer(ContainerConfig{})
    if _, collectErr := AllImplementing[io.Closer](nilContext, emptyContainer); nil == collectErr || false == strings.Contains(collectErr.Error(), "context is required in all implementing") {
        t.Fatalf("expected an empty collection to refuse a nil context, got %v", collectErr)
    }

    handle := Lazy[*testService](serviceContainer, "app.typed.nil.context")
    if _, resolveErr := handle.Resolve(context.Background()); nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if _, resolveErr := handle.Resolve(nilContext); nil == resolveErr || false == strings.Contains(resolveErr.Error(), "context is required in lazy resolve") {
        t.Fatalf("expected a memoized handle to refuse a nil context, got %v", resolveErr)
    }
}
