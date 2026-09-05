package container

import (
    "errors"
    "reflect"
    "strings"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    alpha "github.com/precision-soft/melody/v3/container/internal/collisionalpha/contract"
    beta "github.com/precision-soft/melody/v3/container/internal/collisionbeta/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
)

type resolverTestService struct {
    value string
}

type resolverTestResolver struct {
    servicesByName map[string]any
    servicesByType map[reflect.Type]any
}

func (instance *resolverTestResolver) Get(serviceName string) (any, error) {
    value, exists := instance.servicesByName[serviceName]
    if false == exists {
        return nil, errors.New("service missing")
    }

    return value, nil
}

func (instance *resolverTestResolver) MustGet(serviceName string) any {
    value, err := instance.Get(serviceName)
    if nil != err {
        exception.Panic(
            exception.FromError(err),
        )
    }

    return value
}

func (instance *resolverTestResolver) GetByType(targetType reflect.Type) (any, error) {
    value, exists := instance.servicesByType[targetType]
    if false == exists {
        return nil, errors.New("service missing")
    }

    return value, nil
}

func (instance *resolverTestResolver) MustGetByType(targetType reflect.Type) any {
    value, err := instance.GetByType(targetType)
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

    value, err := FromResolver[*resolverTestService](resolver, "service.test")
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

    _, err := FromResolver[*resolverTestService](resolver, "service.missing")
    if nil == err {
        t.Fatalf("expected error")
    }

    typedError, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected *exception.Error, got: %T", err)
    }

    if "service not registered in resolver" != typedError.Message() {
        t.Fatalf("unexpected error message: %s", typedError.Message())
    }
}

func TestFromResolver_TypeMismatch(t *testing.T) {
    resolver := &resolverTestResolver{
        servicesByName: map[string]any{
            "service.test": "not a service",
        },
        servicesByType: map[reflect.Type]any{},
    }

    _, err := FromResolver[*resolverTestService](resolver, "service.test")
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

    _ = MustFromResolver[*resolverTestService](resolver, "service.missing")
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

    _ = MustFromResolverByType[*resolverTestService](resolver)
}

func TestFromResolverByType_HappyPath(t *testing.T) {
    targetType := reflect.TypeOf(&resolverTestService{})
    resolver := &resolverTestResolver{
        servicesByName: map[string]any{},
        servicesByType: map[reflect.Type]any{
            targetType: &resolverTestService{value: "ok"},
        },
    }

    value, err := FromResolverByType[*resolverTestService](resolver)
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

    _, err := FromResolverByType[*resolverTestService](resolver)
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

    _, err := FromResolverByType[*resolverTestService](resolver)
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
    serviceContainer := NewContainer()

    /* an unqualified recover accepts any panic at all, including one thrown by a guard three lines away; the message is what names the refusal under test */
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = MustFromResolver[*testService](serviceContainer, "service.missing")
        },
        "service is not registered",
    )
}

var _ containercontract.Resolver = (*resolverTestResolver)(nil)

func TestResolution_SameStringTypesFromDifferentPackagesDoNotAlias(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister(serviceContainer, "bus.beta", func(resolver containercontract.Resolver) (*beta.Bus, error) {
        return &beta.Bus{Region: "beta"}, nil
    }, WithTypeRegistration(true))

    MustRegister(serviceContainer, "bus.alpha", func(resolver containercontract.Resolver) (*alpha.Bus, error) {
        /* the alpha provider resolves the beta bus by type: both nodes key on "contract.Bus" through String(), so a shared key would flag a false cycle here */
        betaBus, betaErr := FromResolverByType[*beta.Bus](resolver)
        if nil != betaErr {
            return nil, betaErr
        }

        return &alpha.Bus{Region: "alpha:" + betaBus.Region}, nil
    }, WithTypeRegistration(true))

    alphaBus, getErr := FromResolverByType[*alpha.Bus](serviceContainer)
    if nil != getErr {
        t.Fatalf("expected the alpha bus to resolve through the beta bus, got %v", getErr)
    }

    if "alpha:beta" != alphaBus.Region {
        t.Fatalf("expected the nested by-type resolution to compose, got %q", alphaBus.Region)
    }

    betaBus, betaGetErr := FromResolverByType[*beta.Bus](serviceContainer)
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

func (instance *melodyErrorResolver) Get(serviceName string) (any, error) {
    return nil, instance.err
}

func (instance *melodyErrorResolver) MustGet(serviceName string) any {
    exception.Panic(instance.err)

    return nil
}

func (instance *melodyErrorResolver) GetByType(targetType reflect.Type) (any, error) {
    return nil, instance.err
}

func (instance *melodyErrorResolver) MustGetByType(targetType reflect.Type) any {
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

    _, fromResolverErr := FromResolver[*resolverTestService](resolver, "service.original")
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

func (instance *resolverTestTypedNilErrorResolver) Get(serviceName string) (any, error) {
    var typedNil *resolverTestTypedNilError
    return instance.value, typedNil
}

func (instance *resolverTestTypedNilErrorResolver) MustGet(serviceName string) any {
    return instance.value
}

func (instance *resolverTestTypedNilErrorResolver) GetByType(targetType reflect.Type) (any, error) {
    var typedNil *resolverTestTypedNilError
    return instance.value, typedNil
}

func (instance *resolverTestTypedNilErrorResolver) MustGetByType(targetType reflect.Type) any {
    return instance.value
}

func (instance *resolverTestTypedNilErrorResolver) Has(serviceName string) bool { return true }

func (instance *resolverTestTypedNilErrorResolver) HasType(targetType reflect.Type) bool {
    return true
}

func TestFromResolver_TypedNilErrorReadsAsSuccess(t *testing.T) {
    resolver := &resolverTestTypedNilErrorResolver{value: &resolverTestService{value: "alive"}}

    resolved, fromResolverErr := FromResolver[*resolverTestService](resolver, "service.probe")
    if nil != fromResolverErr {
        t.Fatalf("expected the typed-nil error to read as success, got %v", fromResolverErr)
    }

    if "alive" != resolved.value {
        t.Fatalf("expected the resolved value to come through")
    }
}

func TestFromResolverByType_TypedNilErrorReadsAsSuccess(t *testing.T) {
    resolver := &resolverTestTypedNilErrorResolver{value: &resolverTestService{value: "alive"}}

    resolved, fromResolverErr := FromResolverByType[*resolverTestService](resolver)
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

    _ = MustFromResolverByType[*resolverTestService](resolver)
}

func TestFromResolver_NilResolverIsRefusedInsteadOfDereferenced(t *testing.T) {
    value, fromResolverErr := FromResolver[*resolverTestService](nil, "app.service")

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
    _, fromResolverByTypeErr := FromResolverByType[*resolverTestService](nil)

    if nil == fromResolverByTypeErr {
        t.Fatalf("expected a nil resolver to be refused by the type door as well")
    }

    if "resolver is nil" != fromResolverByTypeErr.Error() {
        t.Fatalf("unexpected refusal message: %q", fromResolverByTypeErr.Error())
    }
}

func TestLazy_NilResolverSurfacesAsAnErrorAtFirstUse(t *testing.T) {
    lazyService := Lazy[*resolverTestService](nil, "app.service")

    _, resolveErr := lazyService.Resolve()

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

    _, enrichedErr := FromResolverByType[*resolverTestService](&melodyErrorResolver{err: melodyErr})
    if enrichedErr != melodyErr {
        t.Fatalf("expected the melody error to travel out whole, got %v", enrichedErr)
    }

    serviceType, exists := melodyErr.Context()["serviceType"]
    if false == exists || "" == serviceType {
        t.Fatalf("expected the failure context to carry the service type, got %v", melodyErr.Context())
    }

    _, wrappedErr := FromResolverByType[*resolverTestService](&resolverTestResolver{
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
