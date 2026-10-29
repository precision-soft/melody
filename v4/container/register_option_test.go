package container

import (
    "context"
    "errors"
    "reflect"
    "sync/atomic"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type optionsService struct {
    label string
}

func newOptionsServiceProvider(label string) func(resolver containercontract.Resolver) (*optionsService, error) {
    return func(resolver containercontract.Resolver) (*optionsService, error) {
        return &optionsService{label: label}, nil
    }
}

func TestRegisterOptions_TheZeroValueRegistersTheTypeStrictly(t *testing.T) {
    serviceContainer := NewContainer()
    serviceContainer.MustRegister("app.first", newOptionsServiceProvider("first"), RegisterOptions{})

    if false == serviceContainer.HasType(reflect.TypeFor[*optionsService]()) {
        t.Fatalf("expected the zero options to register the service under its type")
    }

    registerErr := serviceContainer.Register("app.second", newOptionsServiceProvider("second"), RegisterOptions{})
    if false == errors.Is(registerErr, ErrServiceTypeAlreadyRegistered) {
        t.Fatalf("expected a second registration of the type refused, got %v", registerErr)
    }
}

func TestRegisterOptions_TypeRegistrationDisabledKeepsTheServiceOffTheTypeIndex(t *testing.T) {
    serviceContainer := NewContainer()
    serviceContainer.MustRegister("app.first", newOptionsServiceProvider("first"), RegisterOptions{TypeRegistrationDisabled: true})

    if true == serviceContainer.HasType(reflect.TypeFor[*optionsService]()) {
        t.Fatalf("expected the service kept off the type index")
    }

    if false == serviceContainer.Has("app.first") {
        t.Fatalf("expected the service registered under its name")
    }
}

func TestRegisterOptions_TypeRegistrationRelaxedAdmitsASecondRegistrationOfTheType(t *testing.T) {
    serviceContainer := NewContainer()
    serviceContainer.MustRegister("app.first", newOptionsServiceProvider("first"), RegisterOptions{TypeRegistrationRelaxed: true})

    if registerErr := serviceContainer.Register("app.second", newOptionsServiceProvider("second"), RegisterOptions{TypeRegistrationRelaxed: true}); nil != registerErr {
        t.Fatalf("expected the relaxed second registration admitted, got %v", registerErr)
    }

    if _, getErr := serviceContainer.GetByType(reflect.TypeFor[*optionsService]()); nil == getErr {
        t.Fatalf("expected the by-type resolution refused as ambiguous")
    }
}

func TestRegisterOptions_TypeRegistrationRelaxedIsInertWhileTheTypeIsNotRegistered(t *testing.T) {
    serviceContainer := NewContainer()
    serviceContainer.MustRegister("app.first", newOptionsServiceProvider("first"), RegisterOptions{TypeRegistrationDisabled: true, TypeRegistrationRelaxed: true})

    if true == serviceContainer.HasType(reflect.TypeFor[*optionsService]()) {
        t.Fatalf("expected the relaxed flag not to register the type")
    }

    serviceContainer.MustRegister("app.second", newOptionsServiceProvider("second"), RegisterOptions{})

    service := MustFromResolverByType[*optionsService](serviceContainer)
    if "second" != service.label {
        t.Fatalf("expected the strict registration to own the type, got %q", service.label)
    }
}

func TestRegisterType_TypeRegistrationDisabledRegistersUnderTheDerivedNameOnly(t *testing.T) {
    serviceContainer := NewContainer()
    MustRegisterType[*optionsService](serviceContainer, newOptionsServiceProvider("first"), RegisterOptions{TypeRegistrationDisabled: true})

    if true == serviceContainer.HasType(reflect.TypeFor[*optionsService]()) {
        t.Fatalf("expected the service kept off the type index")
    }

    if false == serviceContainer.Has(defaultServiceNameForType(reflect.TypeFor[*optionsService]())) {
        t.Fatalf("expected the service registered under the name its type derives")
    }
}

func TestNewCloser_ANilCloserPanics(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a nil closer to panic")
        }
    }()

    NewCloser[*optionsService](nil)
}

func TestRegisterOptions_ACloserMissingOneOfItsHalvesIsRefusedNamingTheField(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        closer Closer
    }{
        {name: "close without a value type", closer: Closer{Close: func(closeContext context.Context, value any) error { return nil }}},
        {name: "value type without a close", closer: Closer{ValueType: reflect.TypeFor[*optionsService]()}},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            serviceContainer := NewContainer()

            registerErr := serviceContainer.Register("app.first", newOptionsServiceProvider("first"), RegisterOptions{Closer: testCase.closer})
            if false == errors.Is(registerErr, ErrCloserTypeMismatch) {
                t.Fatalf("expected the incomplete closer refused, got %v", registerErr)
            }

            var registerError *exception.Error
            if false == errors.As(registerErr, &registerError) || "RegisterOptions.Closer" != registerError.Context()["field"] {
                t.Fatalf("expected the refusal to name the field, got %v", registerErr)
            }

            if true == serviceContainer.Has("app.first") {
                t.Fatalf("expected nothing written by the refused registration")
            }
        })
    }
}

/* foreignClosingService stands for a value of a type the application does not own, whose own Close the teardown must not reach */
type foreignClosingService struct {
    ownCloses atomic.Int32
}

func (instance *foreignClosingService) Close() error {
    instance.ownCloses.Add(1)

    return nil
}

func TestRegisterOptionsCloser_TheTeardownClosesTheValueThroughTheCloserUnderItsDeadlineAndNotThroughItsOwnClose(t *testing.T) {
    serviceContainer := NewContainer()
    built := &foreignClosingService{}

    var closedValue *foreignClosingService
    var closerHadDeadline bool

    serviceContainer.MustRegister(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            return built, nil
        },
        RegisterOptions{Closer: NewCloser(func(closeContext context.Context, value *foreignClosingService) error {
            closedValue = value
            _, closerHadDeadline = closeContext.Deadline()

            return nil
        })},
    )

    _ = MustFromResolver[*foreignClosingService](serviceContainer, "app.foreign")

    closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    if closeErr := serviceContainer.(containercontract.ContextCloser).CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if built != closedValue || false == closerHadDeadline {
        t.Fatalf("expected the closer handed the built value and the teardown's deadline, got %v deadline %v", closedValue, closerHadDeadline)
    }

    if 0 != built.ownCloses.Load() {
        t.Fatalf("expected the value's own Close never called, it was called %d times", built.ownCloses.Load())
    }
}

func TestRegisterOptionsCloser_AServiceWithoutTheOptionIsClosedThroughItsOwnClose(t *testing.T) {
    serviceContainer := NewContainer()
    built := &foreignClosingService{}

    serviceContainer.MustRegister("app.foreign", func(resolver containercontract.Resolver) (*foreignClosingService, error) {
        return built, nil
    }, RegisterOptions{})

    _ = MustFromResolver[*foreignClosingService](serviceContainer, "app.foreign")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 1 != built.ownCloses.Load() {
        t.Fatalf("expected the value's own Close called once, got %d", built.ownCloses.Load())
    }
}

func TestRegisterOptionsCloser_AnInstanceAnOverrideEvictedIsClosedThroughTheCloser(t *testing.T) {
    serviceContainer := NewContainer()
    built := &foreignClosingService{}
    closerCalls := atomic.Int32{}

    serviceContainer.MustRegister(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            return built, nil
        },
        RegisterOptions{Closer: NewCloser(func(closeContext context.Context, value *foreignClosingService) error {
            if built == value {
                closerCalls.Add(1)
            }

            return nil
        })},
    )

    _ = MustFromResolver[*foreignClosingService](serviceContainer, "app.foreign")

    if overrideErr := serviceContainer.OverrideInstance("app.foreign", &foreignClosingService{}, OverrideOptions{}); nil != overrideErr {
        t.Fatalf("override: %v", overrideErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 1 != closerCalls.Load() || 0 != built.ownCloses.Load() {
        t.Fatalf("expected the evicted instance closed once through the closer, got closer %d own %d", closerCalls.Load(), built.ownCloses.Load())
    }
}

/* otherClosingService is an override of another type than the closer takes, installed under a name registered without its type */
type otherClosingService struct {
    ownCloses atomic.Int32
}

func (instance *otherClosingService) Close() error {
    instance.ownCloses.Add(1)

    return nil
}

func TestRegisterOptionsCloser_AnOverrideOfATypeTheCloserDoesNotTakeIsClosedThroughItsOwnClose(t *testing.T) {
    serviceContainer := NewContainer()
    built := &foreignClosingService{}
    closerCalls := atomic.Int32{}

    serviceContainer.MustRegister(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            return built, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true, Closer: NewCloser(func(closeContext context.Context, value *foreignClosingService) error {
            closerCalls.Add(1)

            return nil
        })},
    )

    _ = MustFromResolver[*foreignClosingService](serviceContainer, "app.foreign")

    override := &otherClosingService{}
    if overrideErr := serviceContainer.OverrideProtectedInstance("app.foreign", override, OverrideOptions{}); nil != overrideErr {
        t.Fatalf("override: %v", overrideErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 1 != override.ownCloses.Load() {
        t.Fatalf("expected the override closed once through its own Close, got %d", override.ownCloses.Load())
    }

    if 1 != closerCalls.Load() || 0 != built.ownCloses.Load() {
        t.Fatalf("expected the evicted instance closed once through the closer, got closer %d own %d", closerCalls.Load(), built.ownCloses.Load())
    }
}

func TestRegisterOptionsCloser_AnOverrideOfTheTypeTheCloserTakesIsClosedThroughTheCloser(t *testing.T) {
    serviceContainer := NewContainer()
    override := &foreignClosingService{}
    var closedOverride atomic.Bool

    serviceContainer.MustRegister(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            return &foreignClosingService{}, nil
        },
        RegisterOptions{Closer: NewCloser(func(closeContext context.Context, value *foreignClosingService) error {
            if override == value {
                closedOverride.Store(true)
            }

            return nil
        })},
    )

    if overrideErr := serviceContainer.OverrideInstance("app.foreign", override, OverrideOptions{}); nil != overrideErr {
        t.Fatalf("override: %v", overrideErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if false == closedOverride.Load() || 0 != override.ownCloses.Load() {
        t.Fatalf("expected the override closed through the closer only, got closer %v own %d", closedOverride.Load(), override.ownCloses.Load())
    }
}

func TestRegisterOptionsCloser_ACloserThatDoesNotTakeTheServicesTypeIsRefusedAtRegistration(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            return &foreignClosingService{}, nil
        },
        RegisterOptions{Closer: NewCloser(func(closeContext context.Context, value string) error { return nil })},
    )

    if false == errors.Is(registerErr, ErrCloserTypeMismatch) {
        t.Fatalf("expected the registration refused with ErrCloserTypeMismatch, got %v", registerErr)
    }
}

func TestRegisterOptionsCloser_TheScopedRegistrationsRefuseEveryFormOfACloser(t *testing.T) {
    serviceContainer := NewContainer()
    scopeInstance := serviceContainer.NewScope()
    defer func() { _ = scopeInstance.Close() }()

    provider := func(resolver containercontract.Resolver) (*foreignClosingService, error) {
        return &foreignClosingService{}, nil
    }

    closers := []struct {
        name   string
        closer Closer
    }{
        {name: "complete", closer: NewCloser(func(closeContext context.Context, value *foreignClosingService) error { return nil })},
        {name: "value type only", closer: Closer{ValueType: reflect.TypeFor[*foreignClosingService]()}},
        {name: "close only", closer: Closer{Close: func(closeContext context.Context, value any) error { return nil }}},
    }

    registrars := []struct {
        name      string
        registrar containercontract.ScopedRegistrar
    }{
        {name: "container", registrar: serviceContainer},
        {name: "scope", registrar: scopeInstance},
    }

    for _, registrarCase := range registrars {
        for _, closerCase := range closers {
            t.Run(registrarCase.name+" "+closerCase.name, func(t *testing.T) {
                serviceName := "app.scoped." + registrarCase.name + "." + closerCase.name

                registerErr := registrarCase.registrar.RegisterScoped(serviceName, provider, RegisterOptions{Closer: closerCase.closer})
                if false == errors.Is(registerErr, ErrScopedCloserUnsupported) {
                    t.Fatalf("expected the scoped registration refused, got %v", registerErr)
                }

                if retryErr := registrarCase.registrar.RegisterScoped(serviceName, provider, RegisterOptions{TypeRegistrationDisabled: true}); nil != retryErr {
                    t.Fatalf("expected the refused registration to have written nothing, got %v", retryErr)
                }
            })
        }
    }
}

func TestRegisterOptionsCloser_AValueBuiltByTypeWhileCloseRanIsClosedThroughTheCloser(t *testing.T) {
    serviceContainer := NewContainer()
    built := &foreignClosingService{}
    closerCalls := atomic.Int32{}

    providerStarted := make(chan struct{})
    providerRelease := make(chan struct{})

    serviceContainer.MustRegister(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            close(providerStarted)
            <-providerRelease

            return built, nil
        },
        RegisterOptions{Closer: NewCloser(func(closeContext context.Context, value *foreignClosingService) error {
            if built == value {
                closerCalls.Add(1)
            }

            return nil
        })},
    )

    resultChannel := make(chan error, 1)
    go func() {
        _, getErr := FromResolverByType[*foreignClosingService](serviceContainer)
        resultChannel <- getErr
    }()

    <-providerStarted

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    close(providerRelease)

    if getErr := <-resultChannel; nil == getErr {
        t.Fatalf("expected the resolution that finished after Close to fail")
    }

    if 1 != closerCalls.Load() || 0 != built.ownCloses.Load() {
        t.Fatalf("expected the value built while Close ran closed once through the closer, got closer %d own %d", closerCalls.Load(), built.ownCloses.Load())
    }
}
