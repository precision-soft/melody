package container

import (
    "context"
    "errors"
    "reflect"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type bootScopedProbe struct{}

type bootLateScopedProbe struct{}

type bootSingletonProbe struct{}

type bootDependentSingletonProbe struct {
    err error
}

func TestNewBootScope_RefusesAScopedServiceByNameAndByTypeAndAScopedRegistration(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegisterScoped[*bootScopedProbe](serviceContainer, "app.boot.scoped", func(resolver containercontract.Resolver) (*bootScopedProbe, error) {
        return &bootScopedProbe{}, nil
    }, RegisterOptions{})
    MustRegister[*bootSingletonProbe](serviceContainer, "app.boot.singleton", func(resolver containercontract.Resolver) (*bootSingletonProbe, error) {
        return &bootSingletonProbe{}, nil
    }, RegisterOptions{})

    bootScope := serviceContainer.NewBootScope()
    defer func() {
        _ = bootScope.Close()
    }()

    MustRegisterScopedType[*bootLateScopedProbe](serviceContainer, func(resolver containercontract.Resolver) (*bootLateScopedProbe, error) {
        return &bootLateScopedProbe{}, nil
    }, RegisterOptions{})
    MustRegister[*bootDependentSingletonProbe](serviceContainer, "app.boot.dependent", func(resolver containercontract.Resolver) (*bootDependentSingletonProbe, error) {
        _, scopedErr := resolver.Get(resolver.Runtime().Context(), "app.boot.scoped")

        return &bootDependentSingletonProbe{err: scopedErr}, nil
    }, RegisterOptions{})

    if containercontract.KindBoot != bootScope.(*scope).boundRuntimeKind() {
        t.Fatalf("expected the boot scope to serve kind boot")
    }

    if _, nameErr := bootScope.Get(context.Background(), "app.boot.scoped"); false == errors.Is(nameErr, ErrScopedServiceAtBoot) {
        t.Fatalf("expected a scoped name to be refused at boot, got %v", nameErr)
    }

    if _, typeErr := bootScope.GetByType(context.Background(), reflect.TypeOf((*bootLateScopedProbe)(nil))); false == errors.Is(typeErr, ErrScopedServiceAtBoot) {
        t.Fatalf("expected a scoped type registered after the boot scope opened to be refused, got %v", typeErr)
    }

    dependent := MustFromResolver[*bootDependentSingletonProbe](context.Background(), bootScope, "app.boot.dependent")
    if false == errors.Is(dependent.err, ErrScopedServiceAtBoot) {
        t.Fatalf("expected a container-owned provider built at boot to read the refusal, got %v", dependent.err)
    }

    if true == bootScope.Has("app.boot.scoped") {
        t.Fatalf("expected Has to answer false for a scoped name at boot")
    }

    if _, singletonErr := bootScope.Get(context.Background(), "app.boot.singleton"); nil != singletonErr {
        t.Fatalf("expected a container service to resolve through the boot scope, got %v", singletonErr)
    }

    registerErr := bootScope.RegisterScoped("app.boot.own", func(resolver containercontract.Resolver) (*bootScopedProbe, error) {
        return &bootScopedProbe{}, nil
    }, RegisterOptions{})
    if false == errors.Is(registerErr, ErrScopedServiceAtBoot) {
        t.Fatalf("expected a scoped registration on the boot scope to be refused, got %v", registerErr)
    }

    if _, missingErr := bootScope.Get(context.Background(), "app.boot.missing"); nil == missingErr || true == errors.Is(missingErr, ErrScopedServiceAtBoot) {
        t.Fatalf("expected a name nothing registered to answer not registered, got %v", missingErr)
    }
}

func TestReservePhaseService_AScopeOfAnotherKindRefusesTheNameNamingBothKinds(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if reserveErr := serviceContainer.ReservePhaseService("app.phase.request", containercontract.KindRequest); nil != reserveErr {
        t.Fatalf("unexpected reserve error: %v", reserveErr)
    }

    bootScope := serviceContainer.NewBootScope()
    defer func() {
        _ = bootScope.Close()
    }()

    _, bootErr := bootScope.Get(context.Background(), "app.phase.request")
    if false == errors.Is(bootErr, containercontract.ErrRuntimeKindRefused) {
        t.Fatalf("expected the boot scope to refuse a request name, got %v", bootErr)
    }

    var refusal *exception.Error
    if false == errors.As(bootErr, &refusal) || "request" != refusal.Context()["required"] || "boot" != refusal.Context()["given"] {
        t.Fatalf("expected the refusal to name both kinds, got %v", bootErr)
    }

    if true == bootScope.Has("app.phase.request") {
        t.Fatalf("expected Has to stay false for a reserved name")
    }

    unboundScope := serviceContainer.NewScope()
    defer func() {
        _ = unboundScope.Close()
    }()

    for name, resolver := range map[string]containercontract.Resolver{
        "request scope":  boundScope(t, serviceContainer, containercontract.KindRequest),
        "unbound scope":  unboundScope,
        "container door": serviceContainer,
    } {
        if _, lookupErr := resolver.Get(context.Background(), "app.phase.request"); nil == lookupErr || true == errors.Is(lookupErr, containercontract.ErrRuntimeKindRefused) {
            t.Fatalf("expected the %s without the name installed to answer not registered, got %v", name, lookupErr)
        }
    }
}

func TestReservePhaseService_RefusesAnEmptyNameAKindThatIsNoPhaseAndAConflictingKind(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if nil == serviceContainer.ReservePhaseService("", containercontract.KindRun) {
        t.Fatalf("expected an empty name to be refused")
    }

    for _, kind := range []containercontract.Kind{containercontract.KindUnspecified, containercontract.KindDetached} {
        if nil == serviceContainer.ReservePhaseService("app.phase", kind) {
            t.Fatalf("expected kind %s to be refused", kind)
        }
    }

    if reserveErr := serviceContainer.ReservePhaseService("app.phase", containercontract.KindRun); nil != reserveErr {
        t.Fatalf("unexpected reserve error: %v", reserveErr)
    }

    if reserveErr := serviceContainer.ReservePhaseService("app.phase", containercontract.KindRun); nil != reserveErr {
        t.Fatalf("expected the same reservation again to be admitted, got %v", reserveErr)
    }

    if nil == serviceContainer.ReservePhaseService("app.phase", containercontract.KindRequest) {
        t.Fatalf("expected a conflicting kind to be refused")
    }
}

type bootReplacingProbe struct {
    scoped bool
}

type bootCollectedMember interface {
    BootMember() string
}

type bootContainerMember struct{}

func (instance *bootContainerMember) BootMember() string { return "container" }

type bootScopedMember struct{}

func (instance *bootScopedMember) BootMember() string { return "scoped" }

func TestNewBootScope_RefusesAScopedServiceThatReplacesAContainerOne(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*bootReplacingProbe](serviceContainer, "app.boot.replaced", func(resolver containercontract.Resolver) (*bootReplacingProbe, error) {
        return &bootReplacingProbe{}, nil
    }, RegisterOptions{})
    MustRegisterScoped[*bootReplacingProbe](serviceContainer, "app.boot.replaced", func(resolver containercontract.Resolver) (*bootReplacingProbe, error) {
        return &bootReplacingProbe{scoped: true}, nil
    }, RegisterOptions{ReplacesContainerService: true})

    bootScope := serviceContainer.NewBootScope()
    defer func() {
        _ = bootScope.Close()
    }()

    if _, nameErr := bootScope.Get(context.Background(), "app.boot.replaced"); false == errors.Is(nameErr, ErrScopedServiceAtBoot) {
        t.Fatalf("expected a scoped service replacing a container one to be refused at boot by name, got %v", nameErr)
    }

    if _, typeErr := bootScope.GetByType(context.Background(), reflect.TypeOf((*bootReplacingProbe)(nil))); false == errors.Is(typeErr, ErrScopedServiceAtBoot) {
        t.Fatalf("expected it refused at boot by type, got %v", typeErr)
    }
}

func TestNewBootScope_RefusesACollectionWithAScopedMember(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegisterType[*bootContainerMember](serviceContainer, func(resolver containercontract.Resolver) (*bootContainerMember, error) {
        return &bootContainerMember{}, nil
    }, RegisterOptions{})
    MustRegisterScopedType[*bootScopedMember](serviceContainer, func(resolver containercontract.Resolver) (*bootScopedMember, error) {
        return &bootScopedMember{}, nil
    }, RegisterOptions{})

    bootScope := serviceContainer.NewBootScope()
    defer func() {
        _ = bootScope.Close()
    }()

    if _, collectErr := AllImplementing[bootCollectedMember](context.Background(), bootScope); false == errors.Is(collectErr, ErrScopedServiceAtBoot) {
        t.Fatalf("expected a collection with a scoped member refused at boot, got %v", collectErr)
    }

    requestScope := serviceContainer.NewScope()
    defer func() {
        _ = requestScope.Close()
    }()

    members, collectErr := AllImplementing[bootCollectedMember](context.Background(), requestScope)
    if nil != collectErr || 2 != len(members) {
        t.Fatalf("expected a scope to gather both members, got %d, %v", len(members), collectErr)
    }
}
