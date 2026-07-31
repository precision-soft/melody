package container

import (
    "errors"
    "reflect"
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
    alpha "github.com/precision-soft/melody/container/internal/collisionalpha/contract"
    beta "github.com/precision-soft/melody/container/internal/collisionbeta/contract"
    "github.com/precision-soft/melody/exception"
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

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = MustFromResolver[*testService](serviceContainer, "service.missing")
}

var _ containercontract.Resolver = (*resolverTestResolver)(nil)

/* @info same-String() types from different packages */

/* @info two distinct types from same-named packages share a String() ("contract.Bus"); a nested by-type resolution of one from the other's provider must resolve and close cleanly, not read as a self-cycle on the aliased key */
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

/* @info a melody error passes back through FromResolver whole, with the service name added to its context in place. The old rebuild produced a lookalike that had shed the already-logged mark, the log level and every wrapper above the found error — a refusal already logged at its source was logged again at the kernel boundary. */
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
