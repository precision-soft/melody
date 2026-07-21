package container

import (
    "errors"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

var errFailingProvider = errors.New("the provider failed")

type collectableHandler interface {
    Handle() string
}

type unrelatedContract interface {
    Unrelated()
}

type invoiceHandler struct {
}

func (instance *invoiceHandler) Handle() string {
    return "invoice"
}

type auditHandler struct {
}

func (instance *auditHandler) Handle() string {
    return "audit"
}

type plainService struct {
}

func newCollectionContainer(t *testing.T) containercontract.Container {
    t.Helper()

    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*invoiceHandler, error) {
        return &invoiceHandler{}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*auditHandler, error) {
        return &auditHandler{}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*plainService, error) {
        return &plainService{}, nil
    })

    return serviceContainer
}

func TestAllImplementing_CollectsOnlyTheServicesSatisfyingTheInterface(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    handlers, allImplementingErr := AllImplementing[collectableHandler](serviceContainer)
    if nil != allImplementingErr {
        t.Fatalf("expected the collection to succeed, got %v", allImplementingErr)
    }

    if 2 != len(handlers) {
        t.Fatalf("expected two handlers, got %d", len(handlers))
    }

    /* @info the order is sorted by type name rather than by registration, so a collection does not reorder between runs on map iteration */
    if "audit" != handlers[0].Handle() || "invoice" != handlers[1].Handle() {
        t.Fatalf("expected a stable order, got %q and %q", handlers[0].Handle(), handlers[1].Handle())
    }
}

func TestAllImplementing_ReturnsEmptyWhenNothingMatches(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    services, allImplementingErr := AllImplementing[unrelatedContract](serviceContainer)
    if nil != allImplementingErr {
        t.Fatalf("expected the collection to succeed, got %v", allImplementingErr)
    }

    if 0 != len(services) {
        t.Fatalf("expected no services, got %d", len(services))
    }
}

func TestAllImplementing_RejectsANonInterfaceType(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    _, allImplementingErr := AllImplementing[*invoiceHandler](serviceContainer)
    if nil == allImplementingErr {
        t.Fatalf("expected a non-interface collection to be refused")
    }
}

/* @info a provider that fails must abort the collection: a caller handed a partial set would dispatch to some handlers and silently drop the rest */
func TestAllImplementing_FailsWhenACollectedProviderFails(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*invoiceHandler, error) {
        return &invoiceHandler{}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*auditHandler, error) {
        return nil, errFailingProvider
    })

    _, allImplementingErr := AllImplementing[collectableHandler](serviceContainer)
    if nil == allImplementingErr {
        t.Fatalf("expected the failing provider to abort the collection")
    }
}

func TestTypesImplementing_IgnoresTheInterfaceRegisteredUnderItself(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (collectableHandler, error) {
        return &invoiceHandler{}, nil
    })

    typeLister, isTypeLister := serviceContainer.(containercontract.TypeLister)
    if false == isTypeLister {
        t.Fatalf("expected the container to enumerate its types")
    }

    handlers, allImplementingErr := AllImplementing[collectableHandler](serviceContainer)
    if nil != allImplementingErr {
        t.Fatalf("expected the collection to succeed, got %v", allImplementingErr)
    }

    /* the interface registered as its own service type would otherwise match itself and be collected twice */
    if 0 != len(handlers) {
        t.Fatalf("expected the self-registration to be skipped, got %d", len(handlers))
    }

    _ = typeLister
}
