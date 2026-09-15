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

    if "service resolution failed" != typedError.Message() {
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

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = MustFromResolver[*testService](serviceContainer, "service.missing")
        },
        "service is not registered",
    )
}

func TestResolution_SameStringTypesFromDifferentPackagesDoNotAlias(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister(serviceContainer, "bus.beta", func(resolver containercontract.Resolver) (*beta.Bus, error) {
        return &beta.Bus{Region: "beta"}, nil
    }, WithTypeRegistration(true))

    MustRegister(serviceContainer, "bus.alpha", func(resolver containercontract.Resolver) (*alpha.Bus, error) {
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

func TestFromResolverDoesNotMislabelProviderFailure(t *testing.T) {
    serviceContainer := NewContainer()
    cause := errors.New("driver connection refused")
    serviceContainer.MustRegister("broken", func(_ containercontract.Resolver) (*resolverTestService, error) {
        return nil, cause
    })
    _, namedErr := FromResolver[*resolverTestService](serviceContainer, "broken")
    _, typedErr := FromResolverByType[*resolverTestService](serviceContainer)
    for _, err := range []error{namedErr, typedErr} {
        if false == errors.Is(err, cause) || strings.Contains(err.Error(), "not registered") {
            t.Fatalf("registered provider failure was mislabeled: %v", err)
        }
    }
}
