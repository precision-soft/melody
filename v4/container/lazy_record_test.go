package container

import (
    "errors"
    "fmt"
    goruntime "runtime"
    "strings"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type lazyRecordProbe struct {
    label string
}

type lazyRecordNamed interface {
    Label() string
}

func (instance *lazyRecordProbe) Label() string {
    return instance.label
}

type lazyRecordForeignResolver struct {
    containercontract.Resolver
}

func verifyLazyNamesOf(t *testing.T, serviceContainer containercontract.Container) error {
    t.Helper()

    verifier, verifies := serviceContainer.(interface{ VerifyLazyNames() error })
    if false == verifies {
        t.Fatalf("the container carries no VerifyLazyNames door")
    }

    return verifier.VerifyLazyNames()
}

func lazyRecordRefusalOf(t *testing.T, verifyErr error, cause error) *exception.Error {
    t.Helper()

    if nil == verifyErr {
        t.Fatalf("the verification admitted a handle it should refuse")
    }

    if false == errors.Is(verifyErr, cause) {
        t.Fatalf("the refusal does not carry %v: %v", cause, verifyErr)
    }

    var refusal *exception.Error
    if false == errors.As(verifyErr, &refusal) {
        t.Fatalf("the refusal is not an exception error: %v", verifyErr)
    }

    return refusal
}

func TestVerifyLazyNames_RefusesAHandleNamingAnUnregisteredServiceAtItsSite(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    Lazy[*lazyRecordProbe](serviceContainer, "app.absent")
    _, file, line, _ := goruntime.Caller(0)

    refusal := lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleNameNotRegistered)

    if "app.absent" != refusal.Context()["serviceName"] {
        t.Fatalf("the refusal does not name the service: %v", refusal.Context())
    }

    if fmt.Sprintf("%s:%d", file, line-1) != refusal.Context()["site"] {
        t.Fatalf("the refusal does not name the handle's site: %v", refusal.Context())
    }
}

func TestVerifyLazyNames_AdmitsARegisteredServiceNeverBuilt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    built := false
    MustRegister[*lazyRecordProbe](serviceContainer, "app.present", func(resolver containercontract.Resolver) (*lazyRecordProbe, error) {
        built = true

        return &lazyRecordProbe{}, nil
    }, RegisterOptions{})

    Lazy[*lazyRecordProbe](serviceContainer, "app.present")
    LazyByType[*lazyRecordProbe](serviceContainer)

    if verifyErr := verifyLazyNamesOf(t, serviceContainer); nil != verifyErr {
        t.Fatalf("a registered service was refused: %v", verifyErr)
    }

    if true == built {
        t.Fatalf("the verification built the service")
    }
}

func TestVerifyLazyNames_RefusesAHandleByTypeNothingRegistered(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    LazyByType[*lazyRecordProbe](serviceContainer)

    refusal := lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleNameNotRegistered)

    if "*container.lazyRecordProbe" != refusal.Context()["type"] {
        t.Fatalf("the refusal does not name the type: %v", refusal.Context())
    }
}

func TestVerifyLazyNames_RefusesAHandleByTypeSeveralServicesAreRegisteredUnder(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    for _, serviceName := range []string{"app.secondary", "app.primary"} {
        MustRegister[*lazyRecordProbe](serviceContainer, serviceName, func(resolver containercontract.Resolver) (*lazyRecordProbe, error) {
            return &lazyRecordProbe{}, nil
        }, RegisterOptions{TypeRegistrationRelaxed: true})
    }

    LazyByType[*lazyRecordProbe](serviceContainer)

    refusal := lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleTypeIsAmbiguous)

    conflicts, isList := refusal.Context()["conflicts"].([]string)
    if false == isList || "app.primary,app.secondary" != strings.Join(conflicts, ",") {
        t.Fatalf("the refusal does not name the sorted conflicts: %v", refusal.Context())
    }
}

func TestVerifyLazyNames_RefusesAContainerHandleNamingAScopedService(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegisterScoped[*lazyRecordProbe](serviceContainer, "app.scoped", func(resolver containercontract.Resolver) (*lazyRecordProbe, error) {
        return &lazyRecordProbe{}, nil
    }, RegisterOptions{})

    Lazy[*lazyRecordProbe](serviceContainer, "app.scoped")

    lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleNameNotRegistered)
}

func TestVerifyLazyNames_RecordsNothingOverAScopeOrAScopeVisibleView(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegisterScoped[*lazyRecordProbe](serviceContainer, "app.scoped.holder", func(resolver containercontract.Resolver) (*lazyRecordProbe, error) {
        Lazy[*lazyRecordProbe](resolver, "app.absent.from.view")

        return &lazyRecordProbe{}, nil
    }, RegisterOptions{})

    requestScope := serviceContainer.NewScope()
    Lazy[*lazyRecordProbe](requestScope, "app.absent.from.scope")
    MustFromResolver[*lazyRecordProbe](t.Context(), requestScope, "app.scoped.holder")

    if verifyErr := verifyLazyNamesOf(t, serviceContainer); nil != verifyErr {
        t.Fatalf("a handle over a scope was verified: %v", verifyErr)
    }
}

func TestVerifyLazyNames_RefusesAHandleBuiltOverAProvidersContainerReadingView(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*lazyRecordProbe](serviceContainer, "app.holder", func(resolver containercontract.Resolver) (*lazyRecordProbe, error) {
        Lazy[*lazyRecordProbe](resolver, "app.absent.from.provider")

        return &lazyRecordProbe{}, nil
    }, RegisterOptions{})

    MustFromResolver[*lazyRecordProbe](t.Context(), serviceContainer, "app.holder")

    refusal := lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleNameNotRegistered)

    if "app.absent.from.provider" != refusal.Context()["serviceName"] {
        t.Fatalf("the refusal does not name the provider's handle: %v", refusal.Context())
    }
}

func TestVerifyLazyNames_RecordsNothingOverAForeignResolver(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    Lazy[*lazyRecordProbe](lazyRecordForeignResolver{Resolver: serviceContainer}, "app.absent")

    if verifyErr := verifyLazyNamesOf(t, serviceContainer); nil != verifyErr {
        t.Fatalf("a handle over a foreign resolver was verified: %v", verifyErr)
    }
}

func TestVerifyLazyNames_AHandleBuiltAfterTheVerificationIsNotRecorded(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*lazyRecordProbe](serviceContainer, "app.present", func(resolver containercontract.Resolver) (*lazyRecordProbe, error) {
        return &lazyRecordProbe{}, nil
    }, RegisterOptions{})

    Lazy[*lazyRecordProbe](serviceContainer, "app.present")

    if verifyErr := verifyLazyNamesOf(t, serviceContainer); nil != verifyErr {
        t.Fatalf("a registered handle was refused before the verification: %v", verifyErr)
    }

    if 0 != len(serviceContainer.(*container).lazyHandles) {
        t.Fatalf("the verification kept its record")
    }

    Lazy[*lazyRecordProbe](serviceContainer, "app.absent")

    if verifyErr := verifyLazyNamesOf(t, serviceContainer); nil != verifyErr {
        t.Fatalf("a handle built after the verification was verified: %v", verifyErr)
    }

    if 0 != len(serviceContainer.(*container).lazyHandles) {
        t.Fatalf("a handle built after the verification was recorded")
    }
}

func TestVerifyLazyNames_OneSiteBuildingManyHandlesIsOneRecord(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    for range 3 {
        Lazy[*lazyRecordProbe](serviceContainer, "app.absent")
    }

    if 1 != len(serviceContainer.(*container).lazyHandles) {
        t.Fatalf("expected one record for one site, got %d", len(serviceContainer.(*container).lazyHandles))
    }
}

func TestVerifyLazyNames_RefusesUnderTheSequentialTeardown(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

    Lazy[*lazyRecordProbe](serviceContainer, "app.absent")

    lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleNameNotRegistered)
}

func TestVerifyLazyNames_AClosedContainerRefusesTheDoor(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    refusal := lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrContainerClosed)

    if "VerifyLazyNames" != refusal.Context()["door"] {
        t.Fatalf("the refusal does not name the door: %v", refusal.Context())
    }
}

func TestLazyHandleSite_NamesAHelpersLineAndItsCaller(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    helperLine := lazyRecordHelper(serviceContainer)
    _, file, line, _ := goruntime.Caller(0)

    refusal := lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleNameNotRegistered)

    if false == strings.HasSuffix(refusal.Context()["site"].(string), "lazy_record_test.go:"+fmt.Sprint(helperLine)) {
        t.Fatalf("the site is not the helper's line: %v", refusal.Context())
    }

    if fmt.Sprintf("%s:%d", file, line-1) != refusal.Context()["caller"] {
        t.Fatalf("the caller is not the helper's caller: %v", refusal.Context())
    }
}

func lazyRecordHelper(resolver containercontract.Resolver) int {
    Lazy[*lazyRecordProbe](resolver, "app.absent.through.helper")
    _, _, line, _ := goruntime.Caller(0)

    return line - 1
}

func TestVerifyLazyNames_RefusesAHandleBuiltInAContainerProviderResolvedThroughAScope(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*lazyRecordProbe](serviceContainer, "app.holder", func(resolver containercontract.Resolver) (*lazyRecordProbe, error) {
        Lazy[*lazyRecordProbe](resolver, "app.absent.from.suspended.view")

        return &lazyRecordProbe{}, nil
    }, RegisterOptions{})

    MustFromResolver[*lazyRecordProbe](t.Context(), serviceContainer.NewScope(), "app.holder")

    refusal := lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleNameNotRegistered)

    if "app.absent.from.suspended.view" != refusal.Context()["serviceName"] {
        t.Fatalf("the refusal does not name the suspended view's handle: %v", refusal.Context())
    }
}

func TestVerifyLazyNames_RefusesAHandleBuiltOverAProvidersGoroutineView(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*lazyRecordProbe](serviceContainer, "app.holder", func(resolver containercontract.Resolver) (*lazyRecordProbe, error) {
        Lazy[*lazyRecordProbe](ForGoroutine(resolver), "app.absent.from.goroutine.view")

        return &lazyRecordProbe{}, nil
    }, RegisterOptions{})

    MustFromResolver[*lazyRecordProbe](t.Context(), serviceContainer, "app.holder")

    refusal := lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleNameNotRegistered)

    if "app.absent.from.goroutine.view" != refusal.Context()["serviceName"] {
        t.Fatalf("the refusal does not name the goroutine view's handle: %v", refusal.Context())
    }
}

func TestVerifyLazyNames_RefusesAHandleByNameAssertingAnotherConcreteType(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*lazyRecordProbe](serviceContainer, "app.present", func(resolver containercontract.Resolver) (*lazyRecordProbe, error) {
        return &lazyRecordProbe{}, nil
    }, RegisterOptions{})

    Lazy[lazyRecordProbe](serviceContainer, "app.present")

    refusal := lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleTypeMismatch)

    if "*container.lazyRecordProbe" != refusal.Context()["serviceType"] || "container.lazyRecordProbe" != refusal.Context()["handleType"] {
        t.Fatalf("the refusal does not name both types: %v", refusal.Context())
    }
}

func TestVerifyLazyNames_RefusesAHandleByTypeAssertingTheValueFormOfAPointerRegistration(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*lazyRecordProbe](serviceContainer, "app.present", func(resolver containercontract.Resolver) (*lazyRecordProbe, error) {
        return &lazyRecordProbe{}, nil
    }, RegisterOptions{})

    LazyByType[lazyRecordProbe](serviceContainer)

    lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleTypeMismatch)
}

func TestVerifyLazyNames_RefusesAHandleAssertingATypeTheBuiltValueIsNot(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[lazyRecordNamed](serviceContainer, "app.named", func(resolver containercontract.Resolver) (lazyRecordNamed, error) {
        return &lazyRecordProbe{label: "built"}, nil
    }, RegisterOptions{})

    MustFromResolver[lazyRecordNamed](t.Context(), serviceContainer, "app.named")

    Lazy[*lazyRecordForeignResolver](serviceContainer, "app.named")

    lazyRecordRefusalOf(t, verifyLazyNamesOf(t, serviceContainer), ErrLazyHandleTypeMismatch)
}

func TestVerifyLazyNames_AdmitsAConcreteHandleOverAnInterfaceDeclaredProviderNeverBuilt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[lazyRecordNamed](serviceContainer, "app.named", func(resolver containercontract.Resolver) (lazyRecordNamed, error) {
        return &lazyRecordProbe{label: "built"}, nil
    }, RegisterOptions{})

    handle := Lazy[*lazyRecordProbe](serviceContainer, "app.named")
    Lazy[lazyRecordNamed](serviceContainer, "app.named")

    if verifyErr := verifyLazyNamesOf(t, serviceContainer); nil != verifyErr {
        t.Fatalf("a handle over an interface-declared provider was refused: %v", verifyErr)
    }

    if "built" != handle.Get(t.Context()).Label() {
        t.Fatalf("the admitted handle does not resolve the value")
    }
}

func TestVerifyLazyNames_ATypedNilContainerRecordsNothing(t *testing.T) {
    var nilContainer *container

    defer func() {
        if recovered := recover(); nil != recovered {
            t.Fatalf("a handle over a typed nil container panicked at construction: %v", recovered)
        }
    }()

    Lazy[*lazyRecordProbe](nilContainer, "app.absent")
}
