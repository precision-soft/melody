package container

import (
    "context"
    "errors"
    "sync/atomic"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestApplyRegisterServiceOptions_DefaultsToAStrictTypeRegistration(t *testing.T) {
    option := applyRegisterServiceOptions(nil)

    if false == option.AlsoRegisterType {
        t.Fatalf("expected a registration to declare its type by default")
    }

    if false == option.TypeRegistrationIsStrict {
        t.Fatalf("expected the type registration to be strict by default")
    }

    if true == option.ReplacesContainerService {
        t.Fatalf("expected no waiver by default")
    }
}

func TestRegisterOptions_EachMovesExactlyItsOwnField(t *testing.T) {
    withoutType := applyRegisterServiceOptions([]containercontract.RegisterOption{WithoutTypeRegistration()})
    if true == withoutType.AlsoRegisterType {
        t.Fatalf("expected the type registration to be opted out of")
    }
    if false == withoutType.TypeRegistrationIsStrict {
        t.Fatalf("expected strictness to be left where it was")
    }

    lenient := applyRegisterServiceOptions([]containercontract.RegisterOption{WithTypeRegistration(false)})
    if false == lenient.AlsoRegisterType {
        t.Fatalf("expected the type registration to be turned back on")
    }
    if true == lenient.TypeRegistrationIsStrict {
        t.Fatalf("expected strictness to be lifted")
    }

    strict := applyRegisterServiceOptions([]containercontract.RegisterOption{WithTypeRegistration(true)})
    if false == strict.AlsoRegisterType || false == strict.TypeRegistrationIsStrict {
        t.Fatalf("expected an explicit strict type registration")
    }

    replacing := applyRegisterServiceOptions([]containercontract.RegisterOption{Replacing()})
    if false == replacing.ReplacesContainerService {
        t.Fatalf("expected the waiver to be declared")
    }
    if false == replacing.AlsoRegisterType || false == replacing.TypeRegistrationIsStrict {
        t.Fatalf("expected Replacing to leave the type registration untouched")
    }

    teardown := applyRegisterServiceOptions([]containercontract.RegisterOption{WithTeardownDependency("service.logger")})
    if 1 != len(teardown.TeardownDependencyNames) || "service.logger" != teardown.TeardownDependencyNames[0] {
        t.Fatalf("expected the declared teardown dependency, got %v", teardown.TeardownDependencyNames)
    }
    if false == teardown.AlsoRegisterType || false == teardown.TypeRegistrationIsStrict || true == teardown.ReplacesContainerService {
        t.Fatalf("expected WithTeardownDependency to leave every other field untouched")
    }
}

/* TestWithTeardownDependency_ComposesAcrossCallsAndArguments pins the promise the option's documentation makes about composing: two declarations add to one list rather than the second replacing the first, and one call naming several services keeps all of them in the order they were written. A replacing fold would silently drop the first collaborator, which is the failure no ordering test can see — the edge that is missing simply never constrains anything. */
func TestWithTeardownDependency_ComposesAcrossCallsAndArguments(t *testing.T) {
    option := applyRegisterServiceOptions([]containercontract.RegisterOption{
        WithTeardownDependency("service.logger", "service.metrics"),
        WithTeardownDependency("service.tracer"),
    })

    expected := []string{"service.logger", "service.metrics", "service.tracer"}

    if len(expected) != len(option.TeardownDependencyNames) {
        t.Fatalf("expected %v, got %v", expected, option.TeardownDependencyNames)
    }

    for index, name := range expected {
        if name != option.TeardownDependencyNames[index] {
            t.Fatalf("expected %v, got %v", expected, option.TeardownDependencyNames)
        }
    }
}

func TestApplyRegisterServiceOptions_FoldsInOrderAndSkipsANilOption(t *testing.T) {
    option := applyRegisterServiceOptions([]containercontract.RegisterOption{
        WithoutTypeRegistration(),
        nil,
        WithTypeRegistration(false),
    })

    if false == option.AlsoRegisterType {
        t.Fatalf("expected the later option to win over the earlier one")
    }

    if true == option.TypeRegistrationIsStrict {
        t.Fatalf("expected the later option to carry its own strictness")
    }

    reversed := applyRegisterServiceOptions([]containercontract.RegisterOption{
        WithTypeRegistration(false),
        WithoutTypeRegistration(),
    })

    if true == reversed.AlsoRegisterType {
        t.Fatalf("expected the reversed order to end with the type registration opted out of")
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

func TestWithCloser_TheTeardownClosesTheValueThroughTheCloserUnderItsDeadlineAndNotThroughItsOwnClose(t *testing.T) {
    serviceContainer := NewContainer()
    built := &foreignClosingService{}

    var closedValue *foreignClosingService
    var closerHadDeadline bool

    serviceContainer.MustRegister(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            return built, nil
        },
        WithCloser(func(closeContext context.Context, value *foreignClosingService) error {
            closedValue = value
            _, closerHadDeadline = closeContext.Deadline()

            return nil
        }),
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

func TestWithCloser_AServiceWithoutTheOptionIsClosedThroughItsOwnClose(t *testing.T) {
    serviceContainer := NewContainer()
    built := &foreignClosingService{}

    serviceContainer.MustRegister("app.foreign", func(resolver containercontract.Resolver) (*foreignClosingService, error) {
        return built, nil
    })

    _ = MustFromResolver[*foreignClosingService](serviceContainer, "app.foreign")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 1 != built.ownCloses.Load() {
        t.Fatalf("expected the value's own Close called once, got %d", built.ownCloses.Load())
    }
}

func TestWithCloser_AnInstanceAnOverrideEvictedIsClosedThroughTheCloser(t *testing.T) {
    serviceContainer := NewContainer()
    built := &foreignClosingService{}
    closerCalls := atomic.Int32{}

    serviceContainer.MustRegister(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            return built, nil
        },
        WithCloser(func(closeContext context.Context, value *foreignClosingService) error {
            if built == value {
                closerCalls.Add(1)
            }

            return nil
        }),
    )

    _ = MustFromResolver[*foreignClosingService](serviceContainer, "app.foreign")

    if overrideErr := serviceContainer.OverrideInstance("app.foreign", &foreignClosingService{}); nil != overrideErr {
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

func TestWithCloser_AnOverrideOfATypeTheCloserDoesNotTakeIsClosedThroughItsOwnClose(t *testing.T) {
    serviceContainer := NewContainer()
    built := &foreignClosingService{}
    closerCalls := atomic.Int32{}

    serviceContainer.MustRegister(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            return built, nil
        },
        WithCloser(func(closeContext context.Context, value *foreignClosingService) error {
            closerCalls.Add(1)

            return nil
        }),
        WithoutTypeRegistration(),
    )

    _ = MustFromResolver[*foreignClosingService](serviceContainer, "app.foreign")

    override := &otherClosingService{}
    if overrideErr := serviceContainer.OverrideProtectedInstance("app.foreign", override); nil != overrideErr {
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

func TestWithCloser_AnOverrideOfTheTypeTheCloserTakesIsClosedThroughTheCloser(t *testing.T) {
    serviceContainer := NewContainer()
    override := &foreignClosingService{}
    var closedOverride atomic.Bool

    serviceContainer.MustRegister(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            return &foreignClosingService{}, nil
        },
        WithCloser(func(closeContext context.Context, value *foreignClosingService) error {
            if override == value {
                closedOverride.Store(true)
            }

            return nil
        }),
    )

    if overrideErr := serviceContainer.OverrideInstance("app.foreign", override); nil != overrideErr {
        t.Fatalf("override: %v", overrideErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if false == closedOverride.Load() || 0 != override.ownCloses.Load() {
        t.Fatalf("expected the override closed through the closer only, got closer %v own %d", closedOverride.Load(), override.ownCloses.Load())
    }
}

func TestWithCloser_ACloserThatDoesNotTakeTheServicesTypeIsRefusedAtRegistration(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.foreign",
        func(resolver containercontract.Resolver) (*foreignClosingService, error) {
            return &foreignClosingService{}, nil
        },
        WithCloser(func(closeContext context.Context, value string) error { return nil }),
    )

    if false == errors.Is(registerErr, ErrCloserTypeMismatch) {
        t.Fatalf("expected the registration refused with ErrCloserTypeMismatch, got %v", registerErr)
    }
}

func TestWithCloser_TheScopedRegistrationsRefuseTheOption(t *testing.T) {
    serviceContainer := NewContainer()
    closer := WithCloser(func(closeContext context.Context, value *foreignClosingService) error { return nil })
    provider := func(resolver containercontract.Resolver) (*foreignClosingService, error) {
        return &foreignClosingService{}, nil
    }

    if registerErr := serviceContainer.RegisterScoped("app.scoped", provider, closer); false == errors.Is(registerErr, ErrScopedCloserUnsupported) {
        t.Fatalf("expected the container's scoped registration refused, got %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()
    defer func() { _ = scopeInstance.Close() }()

    if registerErr := scopeInstance.(*scope).RegisterScoped("app.scoped.own", provider, closer); false == errors.Is(registerErr, ErrScopedCloserUnsupported) {
        t.Fatalf("expected the scope's own registration refused, got %v", registerErr)
    }
}
