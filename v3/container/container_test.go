package container

import (
    "errors"
    "reflect"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    collisionalpha "github.com/precision-soft/melody/v3/container/internal/collisionalpha/contract"
    collisionbeta "github.com/precision-soft/melody/v3/container/internal/collisionbeta/contract"
    "github.com/precision-soft/melody/v3/exception"
)

func TestContainer_SingletonInstantiation(t *testing.T) {
    serviceContainer := NewContainer()

    calls := 0

    err := serviceContainer.Register(
        "service.singleton",
        func(resolver containercontract.Resolver) (*testService, error) {
            calls++
            return &testService{Value: "ok"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    _, err = serviceContainer.Get("service.singleton")
    if nil != err {
        t.Fatalf("unexpected error")
    }

    _, err = serviceContainer.Get("service.singleton")
    if nil != err {
        t.Fatalf("unexpected error")
    }

    if 1 != calls {
        t.Fatalf("expected factory to be called once")
    }
}

func TestContainer_FactoryErrorIsPropagated(t *testing.T) {
    serviceContainer := NewContainer()

    expectedErr := errors.New("factory error")

    err := serviceContainer.Register(
        "service.fail",
        func(resolver containercontract.Resolver) (*testService, error) {
            return nil, expectedErr
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error")
    }

    _, err = serviceContainer.Get("service.fail")
    if nil == err {
        t.Fatalf("expected get error")
    }
}

func TestContainer_Has(t *testing.T) {
    serviceContainer := NewContainer()

    if true == serviceContainer.Has("a") {
        t.Fatalf("expected false")
    }

    _ = serviceContainer.Register(
        "a",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{}, nil
        },
    )

    if false == serviceContainer.Has("a") {
        t.Fatalf("expected true")
    }
}

func TestContainer_OverrideInstance(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "test.service",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "original"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    err = serviceContainer.OverrideInstance(
        "test.service",
        &testService{Value: "override"},
    )
    if nil != err {
        t.Fatalf("unexpected override error: %v", err)
    }

    valueAny, err := serviceContainer.Get("test.service")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    service := valueAny.(*testService)
    if "override" != service.Value {
        t.Fatalf("expected override to win")
    }
}

func TestContainer_OverrideInstance_OverridesRegisteredTypeMappings(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.interface",
        func(resolver containercontract.Resolver) (testInterface, error) {
            return &testImplementation{name: "original"}, nil
        },
        WithTypeRegistration(true),
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    err = serviceContainer.OverrideProtectedInstance(
        "service.interface",
        &testImplementation{name: "override"},
    )
    if nil != err {
        t.Fatalf("unexpected override error: %v", err)
    }

    resolved := MustFromResolverByType[testInterface](serviceContainer)
    if "override" != resolved.Name() {
        t.Fatalf("expected override to win for type resolution")
    }
}

func TestContainer_GetByType_ReturnsCompleteConflictReport(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.a",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "a"}, nil
        },
        WithTypeRegistration(false),
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    err = serviceContainer.Register(
        "service.b",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "b"}, nil
        },
        WithTypeRegistration(false),
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    _, err = serviceContainer.GetByType(reflect.TypeOf((*testService)(nil)))
    if nil == err {
        t.Fatalf("expected error")
    }

    var conflicts []string

    currentErr := err
    for nil != currentErr {
        melodyError, ok := currentErr.(*exception.Error)
        if true == ok {
            context := melodyError.Context()
            conflictsAny, exists := context["conflicts"]
            if true == exists {
                typedConflicts, ok := conflictsAny.([]string)
                if false == ok {
                    t.Fatalf("expected conflicts to be []string")
                }
                conflicts = typedConflicts
                break
            }
        }

        currentErr = errors.Unwrap(currentErr)
    }

    if nil == conflicts {
        t.Fatalf("expected conflicts in error chain")
    }

    if 2 != len(conflicts) {
        t.Fatalf("expected 2 conflicts")
    }

    foundA := false
    foundB := false

    for _, conflictName := range conflicts {
        if "service.a" == conflictName {
            foundA = true
        }
        if "service.b" == conflictName {
            foundB = true
        }
    }

    if false == foundA {
        t.Fatalf("expected conflicts to include service.a")
    }
    if false == foundB {
        t.Fatalf("expected conflicts to include service.b")
    }
}

func TestContainer_ConcurrentGet_SingleFactoryCall(t *testing.T) {
    serviceContainer := NewContainer()

    var calls atomic.Int64

    err := serviceContainer.Register(
        "service.concurrent",
        func(resolver containercontract.Resolver) (*testService, error) {
            calls.Add(1)
            return &testService{Value: "ok"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    errorChannel := make(chan error, 32)

    var waitGroup sync.WaitGroup
    waitGroup.Add(32)

    for i := 0; i < 32; i++ {
        go func() {
            defer waitGroup.Done()

            _, err := serviceContainer.Get("service.concurrent")
            if nil != err {
                errorChannel <- err
            }
        }()
    }

    waitGroup.Wait()
    close(errorChannel)

    for err := range errorChannel {
        if nil != err {
            t.Fatalf("unexpected error: %v", err)
        }
    }

    if 1 != calls.Load() {
        t.Fatalf("expected factory to be called once")
    }
}

func TestHasType_AnswersForTheValueTypeOfAPointerRegistration(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.probe",
        func(resolver containercontract.Resolver) (*hasTypeProbe, error) {
            return &hasTypeProbe{value: "probe"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    valueType := reflect.TypeOf(hasTypeProbe{})

    if false == serviceContainer.HasType(valueType) {
        t.Fatalf("expected HasType to answer for the value type of a pointer registration")
    }

    _, getByTypeErr := serviceContainer.GetByType(valueType)
    if nil != getByTypeErr {
        t.Fatalf("expected GetByType to resolve the same type HasType reported: %v", getByTypeErr)
    }

    scopeInstance := serviceContainer.NewScope()

    if false == scopeInstance.HasType(valueType) {
        t.Fatalf("expected the scope to answer the same way the container does")
    }

    _, scopeGetErr := scopeInstance.GetByType(valueType)
    if nil != scopeGetErr {
        t.Fatalf("expected the scope to resolve the type it reported: %v", scopeGetErr)
    }
}

func TestHasType_AnswersForTheValueTypeOfAScopeOverride(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    overrideErr := scopeInstance.OverrideInstance("app.override", &hasTypeProbe{value: "override"})
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    if false == scopeInstance.HasType(reflect.TypeOf(hasTypeProbe{})) {
        t.Fatalf("expected the scope to answer for the value type of an installed override")
    }
}

func TestContainer_RegisterAndOverrideRefusedAfterClose(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.pre.close",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "alive"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    lateRegisterErr := serviceContainer.Register(
        "app.post.close",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "late"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil == lateRegisterErr {
        t.Fatalf("expected the registration on a closed container to be refused")
    }

    lateOverrideErr := serviceContainer.OverrideProtectedInstance("app.pre.close", &testService{Value: "late"})
    if nil == lateOverrideErr {
        t.Fatalf("expected the override on a closed container to be refused")
    }
}

func TestContainer_OverrideTypeIncompatibleValueRefused(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.typed.override",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "real"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    overrideErr := serviceContainer.OverrideInstance("app.typed.override", "a plain string")
    if nil == overrideErr {
        t.Fatalf("expected the type-incompatible override to be refused")
    }

    value, getErr := serviceContainer.GetByType(reflect.TypeOf((*testService)(nil)))
    if nil != getErr {
        t.Fatalf("unexpected get by type error: %v", getErr)
    }

    if _, isTyped := value.(*testService); false == isTyped {
        t.Fatalf("expected GetByType to keep answering with the registered type, got %T", value)
    }
}

func TestContainer_OverrideOfOneNameDoesNotAnswerAnotherServicesGetByType(t *testing.T) {
    serviceContainer := NewContainer()

    interfaceRegisterErr := serviceContainer.Register(
        "app.poison.contract",
        func(resolver containercontract.Resolver) (testInterface, error) {
            return &testImplementation{name: "contract owner"}, nil
        },
    )
    if nil != interfaceRegisterErr {
        t.Fatalf("unexpected register error: %v", interfaceRegisterErr)
    }

    concreteRegisterErr := serviceContainer.Register(
        "app.poison.concrete",
        func(resolver containercontract.Resolver) (*testImplementation, error) {
            return &testImplementation{name: "concrete owner"}, nil
        },
    )
    if nil != concreteRegisterErr {
        t.Fatalf("unexpected register error: %v", concreteRegisterErr)
    }

    overrideErr := serviceContainer.OverrideProtectedInstance("app.poison.contract", &testImplementation{name: "override"})
    if nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    concreteValue, concreteErr := serviceContainer.GetByType(reflect.TypeOf((*testImplementation)(nil)))
    if nil != concreteErr {
        t.Fatalf("unexpected get by type error: %v", concreteErr)
    }
    if implementation, isTyped := concreteValue.(*testImplementation); false == isTyped || "concrete owner" != implementation.name {
        t.Fatalf("expected the concrete service to keep answering its own type, got %#v", concreteValue)
    }

    contractValue, contractErr := serviceContainer.GetByType(reflect.TypeOf((*testInterface)(nil)).Elem())
    if nil != contractErr {
        t.Fatalf("unexpected get by type error: %v", contractErr)
    }
    if implementation, isTyped := contractValue.(*testImplementation); false == isTyped || "override" != implementation.name {
        t.Fatalf("expected the overridden name's own type to answer the override, got %#v", contractValue)
    }
}

func TestContainer_RegisterTypeIdentityKeyCollisionRefused(t *testing.T) {
    serviceContainer := NewContainer()

    firstRegisterErr := serviceContainer.Register(
        "app.collision.alpha",
        func(resolver containercontract.Resolver) (*struct{ Bus collisionalpha.Bus }, error) {
            return &struct{ Bus collisionalpha.Bus }{}, nil
        },
    )
    if nil != firstRegisterErr {
        t.Fatalf("unexpected register error: %v", firstRegisterErr)
    }

    secondRegisterErr := serviceContainer.Register(
        "app.collision.beta",
        func(resolver containercontract.Resolver) (*struct{ Bus collisionbeta.Bus }, error) {
            return &struct{ Bus collisionbeta.Bus }{}, nil
        },
    )
    if nil == secondRegisterErr {
        t.Fatalf("expected the colliding identity key to be refused")
    }
}

func TestContainer_MustGetByType_AnswersAndNamesItsOwnFailure(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.typed",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "typed"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    value := serviceContainer.MustGetByType(reflect.TypeOf((*testService)(nil)))

    service, isService := value.(*testService)
    if false == isService || "typed" != service.Value {
        t.Fatalf("unexpected resolved value: %#v", value)
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the missing type to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "service type is not registered" != recoveredErr.Error() {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }

        melodyErr, isMelodyErr := recoveredValue.(*exception.Error)
        if false == isMelodyErr || nil == melodyErr {
            t.Fatalf("expected the original melody error to travel out whole, got %#v", recoveredValue)
        }

        typeContextValue, hasTypeContextValue := melodyErr.Context()["type"].(string)
        if false == hasTypeContextValue || "" == typeContextValue {
            t.Fatalf("expected the type written into the original failure's context, got %#v", melodyErr.Context())
        }
    }()

    _ = serviceContainer.MustGetByType(reflect.TypeOf((*testImplementation)(nil)))
}

func TestContainer_MustOverrideInstance_InstallsAndNamesItsOwnFailure(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.overridable",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "real"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    serviceContainer.MustOverrideInstance("app.overridable", &testService{Value: "installed"})

    value, getErr := serviceContainer.Get("app.overridable")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    service, isService := value.(*testService)
    if false == isService || "installed" != service.Value {
        t.Fatalf("expected the installed override to answer, got %#v", value)
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the protected name to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "failed to override service instance" != recoveredErr.Error() {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }
    }()

    serviceContainer.MustOverrideInstance("service.protected", &testService{Value: "installed"})
}

func TestContainer_OverrideInstance_ProtectedNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "service.protected",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "framework"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    overrideErr := serviceContainer.OverrideInstance("service.protected", &testService{Value: "substitute"})
    if nil == overrideErr {
        t.Fatalf("expected the protected name to be refused by the unprotected override door")
    }

    if "service is protected and cannot be overridden" != overrideErr.Error() {
        t.Fatalf("unexpected refusal message: %q", overrideErr.Error())
    }

    protectedErr := serviceContainer.OverrideProtectedInstance("service.protected", &testService{Value: "substitute"})
    if nil != protectedErr {
        t.Fatalf("expected the protected door to admit the same name, got %v", protectedErr)
    }
}

func TestContainer_OverrideProtectedInstance_EmptyNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    overrideErr := serviceContainer.OverrideProtectedInstance("", &testService{Value: "installed"})
    if nil == overrideErr {
        t.Fatalf("expected an empty name to be refused")
    }

    if "service name is empty in override instance" != overrideErr.Error() {
        t.Fatalf("unexpected refusal message: %q", overrideErr.Error())
    }
}

func TestContainer_OverrideProtectedInstance_NilValueRefusedInBothSpellings(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.overridable",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "real"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    untypedErr := serviceContainer.OverrideProtectedInstance("app.overridable", nil)
    if nil == untypedErr {
        t.Fatalf("expected an untyped nil override to be refused")
    }

    if "value is nil in override instance" != untypedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", untypedErr.Error())
    }

    var typedNil *testService

    typedErr := serviceContainer.OverrideProtectedInstance("app.overridable", typedNil)
    if nil == typedErr {
        t.Fatalf("expected a typed nil override to be refused")
    }

    if "value is nil in override instance" != typedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", typedErr.Error())
    }
}

func TestContainer_OverrideProtectedInstance_UnregisteredNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    overrideErr := serviceContainer.OverrideProtectedInstance("app.never.declared", &testService{Value: "installed"})
    if nil == overrideErr {
        t.Fatalf("expected an override of an unregistered name to be refused")
    }

    if "service not registered in container" != overrideErr.Error() {
        t.Fatalf("unexpected refusal message: %q", overrideErr.Error())
    }
}

func TestContainer_Register_RefusesADuplicateName(t *testing.T) {
    serviceContainer := NewContainer()

    firstErr := serviceContainer.Register(
        "app.thing",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "first"}, nil
        },
    )
    if nil != firstErr {
        t.Fatalf("unexpected register error: %v", firstErr)
    }

    secondErr := serviceContainer.Register(
        "app.thing",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "second"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil == secondErr {
        t.Fatalf("expected the duplicate name to be refused")
    }

    if "service already registered" != secondErr.Error() {
        t.Fatalf("unexpected refusal message: %q", secondErr.Error())
    }

    if false == errors.Is(secondErr, ErrServiceIdAlreadyRegistered) {
        t.Fatalf("expected a duplicate name cause, got %v", secondErr)
    }
}

func TestContainer_RegisterType_StrictDuplicateTypeRefused(t *testing.T) {
    serviceContainer := NewContainer()

    firstErr := serviceContainer.Register(
        "app.first",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "first"}, nil
        },
    )
    if nil != firstErr {
        t.Fatalf("unexpected register error: %v", firstErr)
    }

    strictErr := serviceContainer.Register(
        "app.second",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "second"}, nil
        },
    )
    if nil == strictErr {
        t.Fatalf("expected the strict duplicate type to be refused")
    }

    if "service type already registered" != strictErr.Error() {
        t.Fatalf("unexpected refusal message: %q", strictErr.Error())
    }

    if false == errors.Is(strictErr, ErrServiceTypeAlreadyRegistered) {
        t.Fatalf("expected a duplicate type cause, got %v", strictErr)
    }

    lenientErr := serviceContainer.Register(
        "app.third",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "third"}, nil
        },
        WithTypeRegistration(false),
    )
    if nil != lenientErr {
        t.Fatalf("expected a non-strict registration to share the type, got %v", lenientErr)
    }
}

func TestContainer_Names_ListsTheDeclaredContainerNamesSorted(t *testing.T) {
    serviceContainer := NewContainer()

    if 0 != len(serviceContainer.Names()) {
        t.Fatalf("expected a fresh container to declare no names, got %v", serviceContainer.Names())
    }

    registerErr := serviceContainer.Register(
        "app.zebra",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "zebra"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerErr = serviceContainer.Register(
        "app.alpaca",
        func(resolver containercontract.Resolver) (*testImplementation, error) {
            return &testImplementation{name: "alpaca"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.mole",
        func(resolver containercontract.Resolver) (*collisionalpha.Bus, error) {
            return &collisionalpha.Bus{Region: "mole"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    serviceNames := serviceContainer.Names()

    if 2 != len(serviceNames) {
        t.Fatalf("expected only the two container registrations, got %v", serviceNames)
    }

    if "app.alpaca" != serviceNames[0] || "app.zebra" != serviceNames[1] {
        t.Fatalf("expected the names sorted so the listing is stable between runs, got %v", serviceNames)
    }
}

func TestContainer_HasAndHasType_AnswerFalseForTheEmptyNameAndTheNilType(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.thing",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "thing"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if true == serviceContainer.Has("") {
        t.Fatalf("expected the empty name to be absent")
    }

    if true == serviceContainer.HasType(nil) {
        t.Fatalf("expected the nil type to be absent")
    }

    if false == serviceContainer.Has("app.thing") {
        t.Fatalf("expected the declared name to be present")
    }

    if false == serviceContainer.HasType(reflect.TypeOf((*testService)(nil))) {
        t.Fatalf("expected the declared type to be present")
    }
}

func TestContainerOverride_AcceptsAValueTypedOverrideForAValueTypedService(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.label",
        func(resolver containercontract.Resolver) (string, error) {
            return "built", nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if overrideErr := serviceContainer.OverrideProtectedInstance("app.label", "overridden"); nil != overrideErr {
        t.Fatalf("expected the value-typed override to be accepted, got %v", overrideErr)
    }

    if "overridden" != serviceContainer.MustGet("app.label") {
        t.Fatalf("expected the override to answer the name")
    }
}

func TestContainerOverride_RefusesAValueTheRegisteredTypeCannotHold(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.greeter",
        func(resolver containercontract.Resolver) (fitsProbeGreeter, error) {
            return &fitsProbeImplementer{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    overrideErr := serviceContainer.OverrideProtectedInstance("app.greeter", &containerOverrideOutsideValue{})
    if nil == overrideErr {
        t.Fatalf("expected the unassignable override to be refused")
    }

    if false == strings.Contains(overrideErr.Error(), "override value is not assignable to the registered service type") {
        t.Fatalf("expected the assignability refusal, got %q", overrideErr.Error())
    }
}

func TestServiceDescriptions_DescribesBothLifetimesWithoutBuilding(t *testing.T) {
    buildCount := 0

    instance := NewContainer()

    instance.MustRegister(
        "described.container",
        func(resolver containercontract.Resolver) (string, error) {
            buildCount = buildCount + 1

            return "built value", nil
        },
    )

    instance.MustRegisterScoped(
        "described.scoped",
        func(resolver containercontract.Resolver) (int, error) {
            buildCount = buildCount + 1

            return 7, nil
        },
    )

    reporter, ok := instance.(interface {
        ServiceDescriptions() []containercontract.ServiceDescription
    })
    if false == ok {
        t.Fatalf("expected the container to describe its registrations")
    }

    descriptionByName := map[string]containercontract.ServiceDescription{}
    for _, description := range reporter.ServiceDescriptions() {
        descriptionByName[description.Name] = description
    }

    if 0 != buildCount {
        t.Fatalf("expected the descriptions to run no provider, ran %d times", buildCount)
    }

    containerDescription, exists := descriptionByName["described.container"]
    if false == exists || containercontract.ServiceLifetimeContainer != containerDescription.Lifetime {
        t.Fatalf("expected the container-lifetime description, got %+v", containerDescription)
    }
    if true == containerDescription.IsBuilt || "string" != containerDescription.TypeName {
        t.Fatalf("expected the unbuilt declared type, got %+v", containerDescription)
    }

    scopedDescription, exists := descriptionByName["described.scoped"]
    if false == exists || containercontract.ServiceLifetimeScoped != scopedDescription.Lifetime || "int" != scopedDescription.TypeName {
        t.Fatalf("expected the scoped description with its declared type, got %+v", scopedDescription)
    }

    if _, getErr := instance.Get("described.container"); nil != getErr {
        t.Fatalf("unexpected build error: %v", getErr)
    }

    describedBuiltService := false
    for _, description := range reporter.ServiceDescriptions() {
        if "described.container" != description.Name {
            continue
        }

        describedBuiltService = true
        if false == description.IsBuilt || "string" != description.TypeName {
            t.Fatalf("expected the built service to be described as built, got %+v", description)
        }
    }

    if false == describedBuiltService {
        t.Fatalf("expected the built service to be described at all, got %+v", reporter.ServiceDescriptions())
    }
}

func TestContainer_MustGet_PassesTheOriginalMelodyErrorThroughWhole(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.must.get.failure",
        func(resolver containercontract.Resolver) (*testService, error) {
            return nil, exception.NewError(
                "the provider refused",
                map[string]any{
                    "detail": "kept",
                },
                nil,
            )
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the failing provider to panic through MustGet")
        }

        melodyErr, isMelodyErr := recoveredValue.(*exception.Error)
        if false == isMelodyErr || nil == melodyErr {
            t.Fatalf("expected the original melody error to travel out whole, got %#v", recoveredValue)
        }

        if "the provider refused" != melodyErr.Message() {
            t.Fatalf("expected the original message to survive, got %q", melodyErr.Message())
        }

        if "kept" != melodyErr.Context()["detail"] {
            t.Fatalf("expected the original context to survive, got %#v", melodyErr.Context())
        }

        serviceNameContextValue, hasServiceNameContextValue := melodyErr.Context()["serviceName"].(string)
        if false == hasServiceNameContextValue || "" == serviceNameContextValue {
            t.Fatalf("expected the service name written into the original failure's context, got %#v", melodyErr.Context())
        }
    }()

    _ = serviceContainer.MustGet("app.must.get.failure")
}

func TestContainer_Register_AfterArmingRefusesADeclaredDependencyOnAServiceThatWasNeverRegistered(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    registerErr := serviceContainer.Register(
        "app.late",
        func(resolver containercontract.Resolver) (*armedTypedProbe, error) { return &armedTypedProbe{}, nil },
        WithoutTypeRegistration(),
        WithTeardownDependency("app.missing"),
    )

    if false == errors.Is(registerErr, ErrTeardownDependencyWasNeverRegistered) {
        t.Fatalf("expected the late declaration on a service nobody registered to be refused, got %v", registerErr)
    }
}

func TestContainer_Register_AfterArmingAdmitsADeclaredDependencyOnARegisteredService(t *testing.T) {
    serviceContainer := NewContainer()

    if registerErr := serviceContainer.Register(
        "app.store",
        func(resolver containercontract.Resolver) (*armedTypedProbe, error) { return &armedTypedProbe{}, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    armParallelTeardown(t, serviceContainer)

    if registerErr := serviceContainer.Register(
        "app.late",
        func(resolver containercontract.Resolver) (*armedTypedProbe, error) { return &armedTypedProbe{}, nil },
        WithoutTypeRegistration(),
        WithTeardownDependency("app.store"),
    ); nil != registerErr {
        t.Fatalf("expected the late declaration on a registered service to be admitted, got %v", registerErr)
    }

    buildEveryRegisteredService(t, serviceContainer, "app.store", "app.late")

    entries := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    storePlanned := false

    for _, entry := range entries {
        if "service:app.store" != entry.NodeKey {
            continue
        }

        storePlanned = true

        if 1 != entry.WaveIndex {
            t.Fatalf("expected the late declaration to order the store one wave past its dependent, got %+v", entries)
        }
    }

    if false == storePlanned {
        t.Fatalf("expected the store in the plan, got %+v", entries)
    }
}

func TestContainer_Register_AfterArmingRefusesASecondRegistrationUnderADeclaredType(t *testing.T) {
    serviceContainer := NewContainer()

    if registerErr := serviceContainer.Register(
        "app.declarer",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{}, nil },
        WithoutTypeRegistration(),
        WithTeardownDependencyOfType[*armedTypedProbe](),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.first",
        func(resolver containercontract.Resolver) (*armedTypedProbe, error) { return &armedTypedProbe{}, nil },
        WithTypeRegistration(false),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    armParallelTeardown(t, serviceContainer)

    registerErr := serviceContainer.Register(
        "app.second",
        func(resolver containercontract.Resolver) (*armedTypedProbe, error) { return &armedTypedProbe{}, nil },
        WithTypeRegistration(false),
    )

    if false == errors.Is(registerErr, ErrTeardownDependencyTypeIsAmbiguous) {
        t.Fatalf("expected the second registration under the declared type to be refused as ambiguous, got %v", registerErr)
    }

    buildEveryRegisteredService(t, serviceContainer, "app.declarer", "app.first")

    entries := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    firstPlanned := false

    for _, entry := range entries {
        if "service:app.first" != entry.NodeKey {
            continue
        }

        firstPlanned = true

        if 1 != entry.WaveIndex {
            t.Fatalf("expected the declaration to keep ordering the one service the type names, got %+v", entries)
        }
    }

    if false == firstPlanned {
        t.Fatalf("expected the first service in the plan, got %+v", entries)
    }
}

func TestContainer_OverrideProtectedInstance_ArmedRecordsWhatTheOverrideHolds(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    recorder := &closeOrderRecorder{mutex: &sync.Mutex{}, closeSequence: &[]string{}}
    held := &closeOrderServiceB{recorder: recorder}

    if registerErr := serviceContainer.Register(
        "app.held",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) { return held, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(resolver containercontract.Resolver) (*capturingHolder, error) { return &capturingHolder{recorder: recorder}, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    buildEveryRegisteredService(t, serviceContainer, "app.held", "app.holder")

    if overrideErr := serviceContainer.OverrideProtectedInstance("app.holder", &capturingHolder{recorder: recorder, held: held}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    entries := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    heldPlanned := false

    for _, entry := range entries {
        if "service:app.held" != entry.NodeKey {
            continue
        }

        heldPlanned = true

        if 1 != entry.WaveIndex {
            t.Fatalf("expected the held service to be one wave past the override holding it, got %+v", entries)
        }
    }

    if false == heldPlanned {
        t.Fatalf("expected the held service in the plan, got %+v", entries)
    }
}

func TestContainer_OverrideProtectedInstance_ReplacesTheRecordOfTheValueItEvicted(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    recorder := &closeOrderRecorder{mutex: &sync.Mutex{}, closeSequence: &[]string{}}
    held := &closeOrderServiceB{recorder: recorder}

    if registerErr := serviceContainer.Register(
        "app.held",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) { return held, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(resolver containercontract.Resolver) (*capturingHolder, error) { return &capturingHolder{recorder: recorder, held: held}, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    buildEveryRegisteredService(t, serviceContainer, "app.held", "app.holder")

    if overrideErr := serviceContainer.OverrideProtectedInstance("app.holder", &capturingHolder{recorder: recorder}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    entries := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    holderPlanned := false

    for _, entry := range entries {
        if "service:app.holder" != entry.NodeKey {
            continue
        }

        holderPlanned = true

        if 0 != len(entry.Dependencies) {
            t.Fatalf("expected the evicted value's record to go with it, got %+v", entries)
        }
    }

    if false == holderPlanned {
        t.Fatalf("expected the holder in the plan, got %+v", entries)
    }
}

func TestContainer_OverrideProtectedInstance_ArmedReleasesTheMemoOfTheValueItEvicted(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    serviceContainer.MustRegister(
        "app.holder",
        func(_ containercontract.Resolver) (*capturingHolder, error) { return &capturingHolder{}, nil },
        WithoutTypeRegistration(),
    )

    first := &capturingHolder{held: &closeOrderServiceB{}}

    if overrideErr := serviceContainer.OverrideProtectedInstance("app.holder", first); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    firstKey, _ := pointerKeyOf(first)

    concrete := serviceContainer.(*container)

    concrete.mutex.RLock()
    _, memoised := concrete.heldIdentitiesByValue[firstKey]
    concrete.mutex.RUnlock()

    if false == memoised {
        t.Fatalf("expected the installed value's walk memoised while it is installed")
    }

    if overrideErr := serviceContainer.OverrideProtectedInstance("app.holder", &capturingHolder{held: &closeOrderServiceB{}}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    concrete.mutex.RLock()
    _, stillMemoised := concrete.heldIdentitiesByValue[firstKey]
    concrete.mutex.RUnlock()

    if true == stillMemoised {
        t.Fatalf("expected the evicted value's memo released with it")
    }
}

func TestContainer_Register_AfterArmingAdmitsADeclaredDependencyOnARegisteredType(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister(
        "app.pool",
        func(_ containercontract.Resolver) (*sharedPoolService, error) { return &sharedPoolService{label: "pool"}, nil },
    )

    armParallelTeardown(t, serviceContainer)

    if registerErr := serviceContainer.Register(
        "app.declarer",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "declarer"}, nil },
        WithTeardownDependencyOfType[*sharedPoolService](),
    ); nil != registerErr {
        t.Fatalf("expected the declaration on a registered type admitted after arming, got %v", registerErr)
    }

    MustFromResolver[*poolDeclarerService](serviceContainer, "app.declarer")
    MustFromResolver[*sharedPoolService](serviceContainer, "app.pool")

    declarerPlanned := false

    for _, entry := range serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan() {
        if "service:app.declarer" != entry.NodeKey {
            continue
        }

        declarerPlanned = true

        if 1 != len(entry.Dependencies) || "service:app.pool" != entry.Dependencies[0] {
            t.Fatalf("expected the declared type edge in the plan, got %+v", entry)
        }
    }

    if false == declarerPlanned {
        t.Fatalf("expected the declarer in the plan")
    }
}

func TestContainer_RefusesCollidingDeclaredTypesInEitherRegistrationOrder(t *testing.T) {
    for _, declarationFirst := range []bool{false, true} {
        t.Run(map[bool]string{false: "provider-first", true: "declaration-first"}[declarationFirst], func(t *testing.T) {
            serviceContainer := NewContainer()
            declare := func() error {
                return serviceContainer.Register("app.declarer", func(_ containercontract.Resolver) (*testService, error) {
                    return &testService{}, nil
                }, WithoutTypeRegistration(), WithTeardownDependencyOfType[*[]collisionalpha.Bus]())
            }
            register := func() error {
                return serviceContainer.Register("app.beta", func(_ containercontract.Resolver) (*[]collisionbeta.Bus, error) {
                    return new([]collisionbeta.Bus), nil
                })
            }
            first, second := register, declare
            if true == declarationFirst {
                first, second = declare, register
            }
            if err := first(); nil != err {
                t.Fatal(err)
            }
            if err := second(); nil == err || false == strings.Contains(err.Error(), "identity key collides") {
                t.Fatalf("expected explicit type identity collision, got %v", err)
            }
        })
    }
}

func TestContainer_RefusedDeclarationDoesNotReserveTypeIdentities(t *testing.T) {
    serviceContainer := NewContainer()
    err := serviceContainer.Register("app.declarer", func(_ containercontract.Resolver) (*testService, error) {
        return &testService{}, nil
    }, WithoutTypeRegistration(), WithTeardownDependencyOfType[*[]collisionalpha.Bus](), WithTeardownDependencyOfType[*[]collisionbeta.Bus]())
    if nil == err || false == strings.Contains(err.Error(), "identity key collides") {
        t.Fatalf("expected colliding declarations to be refused, got %v", err)
    }
    if err := serviceContainer.Register("app.declarer", func(_ containercontract.Resolver) (*[]collisionbeta.Bus, error) {
        return new([]collisionbeta.Bus), nil
    }); nil != err {
        t.Fatalf("refused registration left a service or identity behind: %v", err)
    }
}

func TestContainer_DeclaredCompositeTypeStillAcceptsItsOwnProvider(t *testing.T) {
    serviceContainer := NewContainer()
    if err := serviceContainer.Register("app.declarer", func(_ containercontract.Resolver) (*testService, error) {
        return &testService{}, nil
    }, WithoutTypeRegistration(), WithTeardownDependencyOfType[*[]collisionalpha.Bus]()); nil != err {
        t.Fatal(err)
    }
    if err := serviceContainer.Register("app.alpha", func(_ containercontract.Resolver) (*[]collisionalpha.Bus, error) {
        return new([]collisionalpha.Bus), nil
    }); nil != err {
        t.Fatal(err)
    }
    armParallelTeardown(t, serviceContainer)
}
