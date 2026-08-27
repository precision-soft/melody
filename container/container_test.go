package container

import (
    "errors"
    "reflect"
    "strings"
    "sync"
    "sync/atomic"
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
    collisionalpha "github.com/precision-soft/melody/container/internal/collisionalpha/contract"
    collisionbeta "github.com/precision-soft/melody/container/internal/collisionbeta/contract"
    "github.com/precision-soft/melody/exception"
)

type testService struct {
    Value string
}

type testInterface interface {
    Name() string
}

type testImplementation struct {
    name string
}

func (instance *testImplementation) Name() string {
    return instance.name
}

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

type hasTypeProbe struct {
    value string
}

/* Has and Get have to agree about the same container. A registration made from a provider returning *T is filed under *T, and GetByType canonicalises before it looks — so asking HasType with the value type was answered "no" for a service the very next GetByType resolves happily. A caller that guards a resolution with HasType then took the branch for a service that is registered. */
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

/* An override installed on a scope is filed under the canonical type of the value, so the scope has to canonicalise before it answers for one too. */
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

/* a closed container used to accept registrations and overrides silently: the registration named a service no resolution would ever build, and the override landed in a map the teardown had already swept — served by later lookups, closed by nobody. Both refuse now, the way the scoped registrar always has. */
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

    /* the late registration opts out of the type registration: the pre-close service holds the same type, and the strict duplicate-type refusal would otherwise refuse this registration for a reason that is not the closed container — the guard under test has to be the only thing standing */
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

/* the override propagates to every type its name is registered under, and a type-keyed resolution hands out whatever sits there without a re-check — the provider contract's call-time guard never sees overrides. A value the registered type cannot hold used to ride that hole straight through GetByType, poisoning the type cache with it. */
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

/* an override of one name answers the types that name is registered under and not another service's type-keyed resolution: the concrete service keeps answering its own type, and the overridden name's own type answers the override. A container type-keyed resolution prefers the name registration at every door, so this pins the contract; the observable half lives on the scope twin, whose own lookup does read the exposed entry. */
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

/* the identity key of a pointer-to-unnamed-composite type drops its package path, so two such types from same-short-named packages share one key — one creation-guard entry and one close node for two distinct types, which read as false cycles at resolution and merged nodes at teardown. The second type is refused at the boot line that declares it. */
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

/* the panicking by-type door on the container had never been executed: nothing proved it resolves at all, and nothing proved its failure carries the by-type message rather than the by-name one — a caller reading a boot log has only that message to tell which door it came in through. */
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

        if "" == melodyErr.Context()["type"] {
            t.Fatalf("expected the type written into the original failure's context, got %#v", melodyErr.Context())
        }
    }()

    _ = serviceContainer.MustGetByType(reflect.TypeOf((*testImplementation)(nil)))
}

/* the panicking override on the container had never been executed either. It has to install the value, and its refusal has to carry its own message rather than the unprotected one it delegates to — the two answer differently and a caller has to be able to tell which verb it called. */
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

/* the protected namespace is the framework's, and the override door is the one place an application can substitute a service by name — the refusal is what keeps "service." meaning the same thing for the whole process. */
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

/* an empty name is refused before anything is written, and the refusal has to be the empty-name one rather than the not-registered one that would answer next: the two describe different mistakes, and a caller whose configuration resolved a name away needs to be told which. */
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

/* an override of nil would file a nil under a name every later lookup answers with, so the first caller to use it dereferences it on the request path — and a typed nil boxed into an interface is NOT equal to nil, so the plain comparison lets it straight through. Both spellings are refused with the same message because they are the same mistake. */
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

/* an override of a name the container never declared would install a service no provider backs, reachable by Get and closed by the teardown as though the container had built it — the refusal keeps the override a substitution rather than a second, undeclared registration door. */
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

/* two registrations of one name would make which provider answers depend on the order the modules ran in, and the second write simply wins — the refusal carries a cause of its own so a caller can tell it apart from a collision with the scoped lifetime, which is a different mistake. */
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

/* strictness is what decides whether two names may share a type at the container lifetime, and it defaults to on: without the refusal, GetByType would answer with whichever of the two the map happened to hold, silently and differently between runs. The non-strict registration is included so the guard is proven to be strictness — not the mere presence of a second name — that refuses. */
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

/* Names is what an introspection command prints and what a boot report enumerates, and nothing called it: it could have returned the empty slice for the whole life of the package without a test noticing. It lists the DECLARED container names, sorted so the output is stable between runs, and it must not leak the scoped registrations — those belong to a lifetime the container never resolves. */
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

/* Has and HasType answer false for the empty name and the nil type, which is the verdict a caller relies on rather than a panic on the request path. Both refusals are SHADOWED by the lookup underneath them — nothing can ever be filed under the empty name, and canonicalServiceType answers nil for a nil type, so the guard below returns the same false — so this test pins the verdict, not the position of the guard. */
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

/* the assignability guard judges the value the way the readers will: a string service is registered under *string, and the values its own provider builds sit raw under that canonical key, so a raw string override occupies exactly the slot a built value occupies — refusing it contradicted the registration's own storage */
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

type containerOverrideOutsideValue struct{}

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

/* the descriptions are what lets an introspection command list a container without building it: both lifetimes answer, and no provider runs while they do */
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

    for _, description := range reporter.ServiceDescriptions() {
        if "described.container" == description.Name {
            if false == description.IsBuilt || "string" != description.TypeName {
                t.Fatalf("expected the built service to be described as built, got %+v", description)
            }
        }
    }
}

func TestContainer_ContainerAnswersItself(t *testing.T) {
    serviceContainer := NewContainer()

    carrier, isCarrier := serviceContainer.(containercontract.ContainerCarrier)
    if false == isCarrier {
        t.Fatal("expected the container to satisfy ContainerCarrier")
    }

    if serviceContainer != carrier.Container() {
        t.Fatal("expected the container to answer itself")
    }
}
