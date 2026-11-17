package container

import (
    "context"
    "errors"
    "fmt"
    "sync"
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

type doorlessService struct {
    label string
}

func TestWithCloser_AnEvictedInstanceHeldUnderASecondNameIsClosedThroughTheCloser(t *testing.T) {
    for _, armed := range []bool{false, true} {
        serviceContainer := NewContainer()
        original := &doorlessService{label: "original"}
        override := &doorlessService{label: "override"}

        var bookMutex sync.Mutex
        closed := []string{}

        serviceContainer.MustRegister(
            "a",
            func(resolver containercontract.Resolver) (*doorlessService, error) {
                return original, nil
            },
            WithoutTypeRegistration(),
            WithCloser(func(closeContext context.Context, value *doorlessService) error {
                bookMutex.Lock()
                defer bookMutex.Unlock()

                closed = append(closed, value.label)

                return nil
            }),
        )
        serviceContainer.MustRegister(
            "b",
            func(resolver containercontract.Resolver) (*doorlessService, error) {
                return MustFromResolver[*doorlessService](resolver, "a"), nil
            },
            WithoutTypeRegistration(),
        )

        if true == armed {
            armParallelTeardown(t, serviceContainer)
        }

        _ = MustFromResolver[*doorlessService](serviceContainer, "b")

        if overrideErr := serviceContainer.OverrideInstance("a", override); nil != overrideErr {
            t.Fatalf("armed %v: override: %v", armed, overrideErr)
        }

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("armed %v: close: %v", armed, closeErr)
        }

        if "[original override]" != fmt.Sprint(closed) {
            t.Fatalf("armed %v: expected the evicted instance closed through the closer at the second name's place, before the override, got %v", armed, closed)
        }
    }
}

func TestWithCloser_AnEvictedInstanceWithItsOwnCloseHeldUnderASecondNameIsClosedThroughTheCloserOnly(t *testing.T) {
    for _, armed := range []bool{false, true} {
        serviceContainer := NewContainer()
        original := &foreignClosingService{}
        override := &foreignClosingService{}
        originalCloserCalls := atomic.Int32{}

        serviceContainer.MustRegister(
            "a",
            func(resolver containercontract.Resolver) (*foreignClosingService, error) {
                return original, nil
            },
            WithoutTypeRegistration(),
            WithCloser(func(closeContext context.Context, value *foreignClosingService) error {
                if original == value {
                    originalCloserCalls.Add(1)
                }

                return nil
            }),
        )
        serviceContainer.MustRegister(
            "b",
            func(resolver containercontract.Resolver) (*foreignClosingService, error) {
                return MustFromResolver[*foreignClosingService](resolver, "a"), nil
            },
            WithoutTypeRegistration(),
        )

        if true == armed {
            armParallelTeardown(t, serviceContainer)
        }

        _ = MustFromResolver[*foreignClosingService](serviceContainer, "b")

        if overrideErr := serviceContainer.OverrideInstance("a", override); nil != overrideErr {
            t.Fatalf("armed %v: override: %v", armed, overrideErr)
        }

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("armed %v: close: %v", armed, closeErr)
        }

        if 1 != originalCloserCalls.Load() || 0 != original.ownCloses.Load() {
            t.Fatalf("armed %v: expected the evicted instance closed once through the closer only, got closer %d own %d", armed, originalCloserCalls.Load(), original.ownCloses.Load())
        }

        if 0 != override.ownCloses.Load() {
            t.Fatalf("armed %v: expected the override closed through the closer, its own Close called %d times", armed, override.ownCloses.Load())
        }
    }
}

func TestWithCloser_AnEvictedInstanceTwoDeclaringNamesHoldIsClosedOnce(t *testing.T) {
    serviceContainer := NewContainer()
    original := &doorlessService{label: "original"}
    originalCloserCalls := atomic.Int32{}

    countingCloser := WithCloser(func(closeContext context.Context, value *doorlessService) error {
        if original == value {
            originalCloserCalls.Add(1)
        }

        return nil
    })

    serviceContainer.MustRegister(
        "a",
        func(resolver containercontract.Resolver) (*doorlessService, error) {
            return original, nil
        },
        WithoutTypeRegistration(),
        countingCloser,
    )
    serviceContainer.MustRegister(
        "c",
        func(resolver containercontract.Resolver) (*doorlessService, error) {
            return MustFromResolver[*doorlessService](resolver, "a"), nil
        },
        WithoutTypeRegistration(),
        countingCloser,
    )

    _ = MustFromResolver[*doorlessService](serviceContainer, "c")

    if overrideErr := serviceContainer.OverrideInstance("a", &doorlessService{label: "override"}); nil != overrideErr {
        t.Fatalf("override: %v", overrideErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if 1 != originalCloserCalls.Load() {
        t.Fatalf("expected the evicted instance closed once, got %d closer calls", originalCloserCalls.Load())
    }
}

type closingValue struct {
    label     string
    ownCloses *atomic.Int32
}

func (instance closingValue) Close() error {
    instance.ownCloses.Add(1)

    return nil
}

func TestWithCloser_AnEvictedValueWithItsOwnCloseHeldUnderASecondNameIsClosedThroughTheCloserOnly(t *testing.T) {
    serviceContainer := NewContainer()
    ownCloses := &atomic.Int32{}
    closed := []string{}

    serviceContainer.MustRegister(
        "a",
        func(resolver containercontract.Resolver) (closingValue, error) {
            return closingValue{label: "original", ownCloses: ownCloses}, nil
        },
        WithoutTypeRegistration(),
        WithCloser(func(closeContext context.Context, value closingValue) error {
            closed = append(closed, value.label)

            return nil
        }),
    )
    serviceContainer.MustRegister(
        "b",
        func(resolver containercontract.Resolver) (closingValue, error) {
            return MustFromResolver[closingValue](resolver, "a"), nil
        },
        WithoutTypeRegistration(),
    )

    _ = MustFromResolver[closingValue](serviceContainer, "b")

    if overrideErr := serviceContainer.OverrideInstance("a", closingValue{label: "override", ownCloses: ownCloses}); nil != overrideErr {
        t.Fatalf("override: %v", overrideErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    if "[original override]" != fmt.Sprint(closed) || 0 != ownCloses.Load() {
        t.Fatalf("expected the evicted value closed through the closer only at the second name's place, before the override, got %v own %d", closed, ownCloses.Load())
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

    if registerErr := scopedRegistrarOf(t, serviceContainer).RegisterScoped("app.scoped", provider, closer); false == errors.Is(registerErr, ErrScopedCloserUnsupported) {
        t.Fatalf("expected the container's scoped registration refused, got %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()
    defer func() { _ = scopeInstance.Close() }()

    if registerErr := scopeInstance.(*scope).RegisterScoped("app.scoped.own", provider, closer); false == errors.Is(registerErr, ErrScopedCloserUnsupported) {
        t.Fatalf("expected the scope's own registration refused, got %v", registerErr)
    }
}

func TestWithCloser_TwoNamesOnOneInstanceCloseThroughTheCloserWhateverTheNamesSort(t *testing.T) {
    for _, names := range [][2]string{{"a", "b"}, {"z", "c"}} {
        for _, armed := range []bool{false, true} {
            declaringName, aliasName := names[0], names[1]
            serviceContainer := NewContainer()
            shared := &foreignClosingService{}
            closerCalls := atomic.Int32{}

            serviceContainer.MustRegister(
                declaringName,
                func(resolver containercontract.Resolver) (*foreignClosingService, error) {
                    return shared, nil
                },
                WithoutTypeRegistration(),
                WithCloser(func(closeContext context.Context, value *foreignClosingService) error {
                    if shared == value {
                        closerCalls.Add(1)
                    }

                    return nil
                }),
            )
            serviceContainer.MustRegister(
                aliasName,
                func(resolver containercontract.Resolver) (*foreignClosingService, error) {
                    return MustFromResolver[*foreignClosingService](resolver, declaringName), nil
                },
                WithoutTypeRegistration(),
            )

            if true == armed {
                armParallelTeardown(t, serviceContainer)
            }

            _ = MustFromResolver[*foreignClosingService](serviceContainer, aliasName)

            if closeErr := serviceContainer.Close(); nil != closeErr {
                t.Fatalf("names %v armed %v: close: %v", names, armed, closeErr)
            }

            if 1 != closerCalls.Load() || 0 != shared.ownCloses.Load() {
                t.Fatalf("names %v armed %v: expected the shared instance closed once through the closer only, got closer %d own %d", names, armed, closerCalls.Load(), shared.ownCloses.Load())
            }
        }
    }
}

func TestWithCloser_ADoorlessInstanceTwoNamesHoldIsClosedThroughTheCloserWhateverTheNamesSort(t *testing.T) {
    for _, names := range [][2]string{{"a", "b"}, {"z", "c"}} {
        declaringName, aliasName := names[0], names[1]
        serviceContainer := NewContainer()
        shared := &doorlessService{label: "shared"}
        closerCalls := atomic.Int32{}

        serviceContainer.MustRegister(
            declaringName,
            func(resolver containercontract.Resolver) (*doorlessService, error) {
                return shared, nil
            },
            WithoutTypeRegistration(),
            WithCloser(func(closeContext context.Context, value *doorlessService) error {
                closerCalls.Add(1)

                return nil
            }),
        )
        serviceContainer.MustRegister(
            aliasName,
            func(resolver containercontract.Resolver) (*doorlessService, error) {
                return MustFromResolver[*doorlessService](resolver, declaringName), nil
            },
            WithoutTypeRegistration(),
        )

        _ = MustFromResolver[*doorlessService](serviceContainer, aliasName)

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("names %v: close: %v", names, closeErr)
        }

        if 1 != closerCalls.Load() {
            t.Fatalf("names %v: expected the doorless instance closed once through the closer, got %d", names, closerCalls.Load())
        }
    }
}

type orderedClosingService struct {
    label  string
    closed *[]string
    mutex  *sync.Mutex
}

func (instance *orderedClosingService) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    *instance.closed = append(*instance.closed, instance.label)

    return nil
}

func TestWithCloser_AnEvictedInstanceASecondNameHoldsClosesBeforeWhatItDependsOn(t *testing.T) {
    for _, armed := range []bool{false, true} {
        serviceContainer := NewContainer()
        var bookMutex sync.Mutex
        closed := []string{}
        original := &doorlessService{label: "original"}

        serviceContainer.MustRegister(
            "dependency",
            func(resolver containercontract.Resolver) (*orderedClosingService, error) {
                return &orderedClosingService{label: "dependency", closed: &closed, mutex: &bookMutex}, nil
            },
            WithoutTypeRegistration(),
        )
        serviceContainer.MustRegister(
            "a",
            func(resolver containercontract.Resolver) (*doorlessService, error) {
                _ = MustFromResolver[*orderedClosingService](resolver, "dependency")

                return original, nil
            },
            WithoutTypeRegistration(),
            WithCloser(func(closeContext context.Context, value *doorlessService) error {
                bookMutex.Lock()
                defer bookMutex.Unlock()

                closed = append(closed, value.label)

                return nil
            }),
        )
        serviceContainer.MustRegister(
            "b",
            func(resolver containercontract.Resolver) (*doorlessService, error) {
                return MustFromResolver[*doorlessService](resolver, "a"), nil
            },
            WithoutTypeRegistration(),
        )

        if true == armed {
            armParallelTeardown(t, serviceContainer)
        }

        _ = MustFromResolver[*doorlessService](serviceContainer, "b")

        if overrideErr := serviceContainer.OverrideInstance("a", &doorlessService{label: "override"}); nil != overrideErr {
            t.Fatalf("armed %v: override: %v", armed, overrideErr)
        }

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("armed %v: close: %v", armed, closeErr)
        }

        originalAt, dependencyAt := -1, -1
        for index, label := range closed {
            switch label {
            case "original":
                originalAt = index
            case "dependency":
                dependencyAt = index
            }
        }

        if -1 == originalAt || -1 == dependencyAt || originalAt > dependencyAt || 3 != len(closed) {
            t.Fatalf("armed %v: expected the evicted instance closed once through the closer before what it depends on, got %v", armed, closed)
        }
    }
}
