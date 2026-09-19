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

    /* the order is sorted by type name rather than by registration, so a collection does not reorder between runs on map iteration */
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

/* a provider that fails must abort the collection: a caller handed a partial set would dispatch to some handlers and silently drop the rest */
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

type handlerDispatcher struct {
    handlers []collectableHandler
}

/* the case the GoDoc promises: a component that dispatches to every handler collects them inside its own provider, so the resolver a provider receives must enumerate the registered types */
func TestAllImplementing_CollectsFromInsideAProvider(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*handlerDispatcher, error) {
        handlers, allImplementingErr := AllImplementing[collectableHandler](resolver)
        if nil != allImplementingErr {
            return nil, allImplementingErr
        }

        return &handlerDispatcher{
            handlers: handlers,
        }, nil
    })

    dispatcher, getErr := FromResolverByType[*handlerDispatcher](serviceContainer)
    if nil != getErr {
        t.Fatalf("expected the dispatcher to resolve, got %v", getErr)
    }

    if 2 != len(dispatcher.handlers) {
        t.Fatalf("expected the provider to collect two handlers, got %d", len(dispatcher.handlers))
    }
}

func TestAllImplementing_CollectsThroughAScope(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    requestScope := serviceContainer.NewScope()
    defer func() {
        _ = requestScope.Close()
    }()

    handlers, allImplementingErr := AllImplementing[collectableHandler](requestScope)
    if nil != allImplementingErr {
        t.Fatalf("expected the collection to succeed, got %v", allImplementingErr)
    }

    if 2 != len(handlers) {
        t.Fatalf("expected two handlers, got %d", len(handlers))
    }
}

/* a service registered under the interface type itself satisfies it trivially: AllImplementing holds every implementation of T, and the single-implementation pattern is one of them */
func TestAllImplementing_CollectsTheServiceRegisteredUnderTheInterfaceItself(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (collectableHandler, error) {
        return &invoiceHandler{}, nil
    })

    handlers, allImplementingErr := AllImplementing[collectableHandler](serviceContainer)
    if nil != allImplementingErr {
        t.Fatalf("expected the collection to succeed, got %v", allImplementingErr)
    }

    if 1 != len(handlers) || "invoice" != handlers[0].Handle() {
        t.Fatalf("expected the interface-registered service to be collected, got %v", handlers)
    }
}

type namedHandler struct {
    name string
}

func (instance *namedHandler) Handle() string {
    return instance.name
}

/* a type registered non-strictly under several names is the multi-instance pattern; the collection resolves each name instead of failing on the ambiguity of the type alone */
func TestAllImplementing_CollectsEveryInstanceOfAMultiNameType(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister(serviceContainer, "handler.first", func(resolver containercontract.Resolver) (*namedHandler, error) {
        return &namedHandler{name: "first"}, nil
    }, WithTypeRegistration(false))

    MustRegister(serviceContainer, "handler.second", func(resolver containercontract.Resolver) (*namedHandler, error) {
        return &namedHandler{name: "second"}, nil
    }, WithTypeRegistration(false))

    handlers, allImplementingErr := AllImplementing[collectableHandler](serviceContainer)
    if nil != allImplementingErr {
        t.Fatalf("expected the collection to succeed, got %v", allImplementingErr)
    }

    if 2 != len(handlers) {
        t.Fatalf("expected both instances of the multi-name type, got %d", len(handlers))
    }

    if "first" != handlers[0].Handle() || "second" != handlers[1].Handle() {
        t.Fatalf("expected both named instances in name order, got %q and %q", handlers[0].Handle(), handlers[1].Handle())
    }
}

/* a higher priority is collected earlier, and services without one keep the stable type-and-name order */
func TestAllImplementing_OrdersByCollectionPriority(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    handlers, allImplementingErr := AllImplementing[collectableHandler](serviceContainer)
    if nil != allImplementingErr {
        t.Fatalf("expected the collection to succeed, got %v", allImplementingErr)
    }

    if 2 != len(handlers) || "audit" != handlers[0].Handle() {
        t.Fatalf("expected the default type-name order, got %v", handlers)
    }

    prioritizedContainer := NewContainer()

    MustRegisterType(prioritizedContainer, func(resolver containercontract.Resolver) (*auditHandler, error) {
        return &auditHandler{}, nil
    })

    MustRegisterType(prioritizedContainer, func(resolver containercontract.Resolver) (*invoiceHandler, error) {
        return &invoiceHandler{}, nil
    }, WithCollectionPriority(10))

    prioritizedHandlers, prioritizedErr := AllImplementing[collectableHandler](prioritizedContainer)
    if nil != prioritizedErr {
        t.Fatalf("expected the collection to succeed, got %v", prioritizedErr)
    }

    if "invoice" != prioritizedHandlers[0].Handle() || "audit" != prioritizedHandlers[1].Handle() {
        t.Fatalf("expected the prioritized service first, got %v", prioritizedHandlers)
    }
}

type compositeDispatcher struct {
    handlers []collectableHandler
}

func (instance *compositeDispatcher) Handle() string {
    return "composite"
}

/* the composite pattern: the dispatcher is itself one of the handlers it dispatches to, and collecting from its own provider must yield the others instead of failing on the self-reference — the way a tagged iterator excludes its referencing service */
func TestAllImplementing_ExcludesTheServiceBeingCreated(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*compositeDispatcher, error) {
        handlers, allImplementingErr := AllImplementing[collectableHandler](resolver)
        if nil != allImplementingErr {
            return nil, allImplementingErr
        }

        return &compositeDispatcher{
            handlers: handlers,
        }, nil
    })

    dispatcher, getErr := FromResolverByType[*compositeDispatcher](serviceContainer)
    if nil != getErr {
        t.Fatalf("expected the composite dispatcher to resolve, got %v", getErr)
    }

    if 2 != len(dispatcher.handlers) {
        t.Fatalf("expected the dispatcher to collect the other handlers, got %d", len(dispatcher.handlers))
    }

    /* collected from outside any provider, the dispatcher itself takes part */
    all, allErr := AllImplementing[collectableHandler](serviceContainer)
    if nil != allErr {
        t.Fatalf("expected the outer collection to succeed, got %v", allErr)
    }

    if 3 != len(all) {
        t.Fatalf("expected the dispatcher to be part of the outer collection, got %d", len(all))
    }
}

/* a per-request override installed under a registered name takes the registration's place in a collection gathered on the scope, because the references resolve by name through the scope. The registration is made under the INTERFACE, which is what admits a different implementation as the override: an override must fit every type its name is registered under, so substituting an *auditHandler for a name registered under *invoiceHandler is refused before anything is written. */
func TestAllImplementing_ScopeOverrideTakesPart(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister(serviceContainer, "handler.invoice", func(resolver containercontract.Resolver) (collectableHandler, error) {
        return &invoiceHandler{}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*auditHandler, error) {
        return &auditHandler{}, nil
    })

    requestScope := serviceContainer.NewScope()
    defer func() {
        _ = requestScope.Close()
    }()

    overrideErr := requestScope.OverrideProtectedInstance("handler.invoice", &auditHandler{})
    if nil != overrideErr {
        t.Fatalf("expected the override to install, got %v", overrideErr)
    }

    handlers, allImplementingErr := AllImplementing[collectableHandler](requestScope)
    if nil != allImplementingErr {
        t.Fatalf("expected the collection to succeed, got %v", allImplementingErr)
    }

    if 2 != len(handlers) {
        t.Fatalf("expected two handlers, got %d", len(handlers))
    }

    if "audit" != handlers[0].Handle() || "audit" != handlers[1].Handle() {
        t.Fatalf("expected the scope override to take the registration's place, got %q and %q", handlers[0].Handle(), handlers[1].Handle())
    }
}

/* excluding anything but the collector itself would freeze a collection whose content depends on boot order; a deeper service on the same path must fail as the cycle it is */
func TestAllImplementing_AncestorOnTheResolutionPathFailsLoudly(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*auditHandler, error) {
        _, dispatcherErr := FromResolverByType[*handlerDispatcher](resolver)
        if nil != dispatcherErr {
            return nil, dispatcherErr
        }

        return &auditHandler{}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*handlerDispatcher, error) {
        handlers, allImplementingErr := AllImplementing[collectableHandler](resolver)
        if nil != allImplementingErr {
            return nil, allImplementingErr
        }

        return &handlerDispatcher{handlers: handlers}, nil
    })

    _, getErr := FromResolverByType[*auditHandler](serviceContainer)
    if nil == getErr {
        t.Fatalf("expected the handler-resolves-dispatcher-collects-handler cycle to fail loudly")
    }
}

/* a closed scope's Get refuses for the request-outliving goroutine, and the collection must refuse the same way instead of handing that goroutine an empty set to dispatch to */
func TestAllImplementing_RefusesAClosedScope(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    requestScope := serviceContainer.NewScope()

    closeErr := requestScope.Close()
    if nil != closeErr {
        t.Fatalf("expected the scope to close, got %v", closeErr)
    }

    _, allImplementingErr := AllImplementing[collectableHandler](requestScope)
    if nil == allImplementingErr {
        t.Fatalf("expected the collection on a closed scope to be refused")
    }

    if false == errors.Is(allImplementingErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to classify as ErrScopeClosed")
    }
}

/* equal priorities keep the stable type-and-name order, and a negative one sorts after every service that declared nothing */
func TestAllImplementing_EqualAndNegativePrioritiesKeepAStableOrder(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister(serviceContainer, "handler.low", func(resolver containercontract.Resolver) (*namedHandler, error) {
        return &namedHandler{name: "low"}, nil
    }, WithTypeRegistration(false), WithCollectionPriority(-5))

    MustRegister(serviceContainer, "handler.b", func(resolver containercontract.Resolver) (*namedHandler, error) {
        return &namedHandler{name: "b"}, nil
    }, WithTypeRegistration(false))

    MustRegister(serviceContainer, "handler.a", func(resolver containercontract.Resolver) (*namedHandler, error) {
        return &namedHandler{name: "a"}, nil
    }, WithTypeRegistration(false))

    MustRegister(serviceContainer, "handler.first", func(resolver containercontract.Resolver) (*namedHandler, error) {
        return &namedHandler{name: "first"}, nil
    }, WithTypeRegistration(false), WithCollectionPriority(10))

    handlers, allImplementingErr := AllImplementing[collectableHandler](serviceContainer)
    if nil != allImplementingErr {
        t.Fatalf("expected the collection to succeed, got %v", allImplementingErr)
    }

    collected := make([]string, 0, len(handlers))
    for _, handler := range handlers {
        collected = append(collected, handler.Handle())
    }

    expected := []string{"first", "a", "b", "low"}
    for index, name := range expected {
        if name != collected[index] {
            t.Fatalf("unexpected order: got %v, want %v", collected, expected)
        }
    }
}

/* the exclusion on a type node pins to the name this context holds in creation: a sibling name of the same type, registered while the collector's provider runs, stays collectable */
func TestAllImplementing_SiblingNameOfTheCollectorTypeIsCollected(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister(serviceContainer, "handler.collector", func(resolver containercontract.Resolver) (*namedHandler, error) {
        MustRegister(serviceContainer, "handler.sibling", func(innerResolver containercontract.Resolver) (*namedHandler, error) {
            return &namedHandler{name: "sibling"}, nil
        }, WithTypeRegistration(false))

        handlers, allImplementingErr := AllImplementing[collectableHandler](resolver)
        if nil != allImplementingErr {
            return nil, allImplementingErr
        }

        collected := ""
        for _, handler := range handlers {
            collected = collected + handler.Handle()
        }

        return &namedHandler{name: "collector(" + collected + ")"}, nil
    }, WithTypeRegistration(false))

    collector, getErr := FromResolver[*namedHandler](serviceContainer, "handler.collector")
    if nil != getErr {
        t.Fatalf("expected the collector to resolve, got %v", getErr)
    }

    if "collector(sibling)" != collector.name {
        t.Fatalf("expected the sibling to be collected and the collector excluded, got %q", collector.name)
    }
}

/* the same pinning holds when the collector is resolved by type: its own reference is caught through the name-keyed creation entry, and the idle sibling stays collectable */
func TestAllImplementing_SiblingNameIsCollectedWhenTheCollectorResolvesByType(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*compositeDispatcher, error) {
        MustRegister(serviceContainer, "handler.sibling.typed", func(innerResolver containercontract.Resolver) (*namedHandler, error) {
            return &namedHandler{name: "sibling"}, nil
        }, WithTypeRegistration(false))

        handlers, allImplementingErr := AllImplementing[collectableHandler](resolver)
        if nil != allImplementingErr {
            return nil, allImplementingErr
        }

        return &compositeDispatcher{handlers: handlers}, nil
    })

    dispatcher, getErr := FromResolverByType[*compositeDispatcher](serviceContainer)
    if nil != getErr {
        t.Fatalf("expected the dispatcher to resolve, got %v", getErr)
    }

    if 1 != len(dispatcher.handlers) || "sibling" != dispatcher.handlers[0].Handle() {
        t.Fatalf("expected the sibling to be collected and the collector excluded, got %v", dispatcher.handlers)
    }
}

type requestHandler struct {
}

func (instance *requestHandler) Handle() string {
    return "request"
}

/* A scoped registration absent from a collection is the quietest failure this feature can produce: the handler is simply never dispatched to, with no error anywhere to say a member is missing. The scope therefore merges its own registrations with the container's rather than delegating. */
func TestAllImplementing_CollectsScopedRegistrationsOnAScope(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    MustRegisterScopedType(serviceContainer, func(resolver containercontract.Resolver) (*requestHandler, error) {
        return &requestHandler{}, nil
    })

    scopeInstance := serviceContainer.NewScope()

    handlers, allImplementingErr := AllImplementing[collectableHandler](scopeInstance)
    if nil != allImplementingErr {
        t.Fatalf("unexpected collection error: %v", allImplementingErr)
    }

    collected := make(map[string]struct{}, len(handlers))
    for _, handler := range handlers {
        collected[handler.Handle()] = struct{}{}
    }

    if _, gathered := collected["request"]; false == gathered {
        t.Fatalf("expected the scoped handler to take part in a collection gathered on the scope, got %v", collected)
    }

    if _, gathered := collected["invoice"]; false == gathered {
        t.Fatalf("expected the container handlers to keep taking part, got %v", collected)
    }
}

/* The container's own collection must stay free of scoped members: a process-lifetime dispatcher holding a handler built for one request would hold that request for the life of the process. */
func TestAllImplementing_AContainerCollectionExcludesScopedRegistrations(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    MustRegisterScopedType(serviceContainer, func(resolver containercontract.Resolver) (*requestHandler, error) {
        return &requestHandler{}, nil
    })

    handlers, allImplementingErr := AllImplementing[collectableHandler](serviceContainer)
    if nil != allImplementingErr {
        t.Fatalf("unexpected collection error: %v", allImplementingErr)
    }

    for _, handler := range handlers {
        if "request" == handler.Handle() {
            t.Fatalf("expected the container collection to leave the scoped handler out")
        }
    }

    /* the loop above is satisfied by an empty collection, which is also what a collector that gathers NOTHING returns — the sibling below carries this same guard */
    if 0 == len(handlers) {
        t.Fatalf("expected the container collection to gather the container handlers")
    }
}

/* A container provider collecting through the resolver it was handed must gather only container members, even while the resolution that reached it came through a scope: the dispatcher it is building is a process singleton, and a handler built for one request would be held by it for the life of the process. */
func TestAllImplementing_AContainerProviderCollectingThroughAScopeExcludesScopedRegistrations(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    MustRegisterScopedType(serviceContainer, func(resolver containercontract.Resolver) (*requestHandler, error) {
        return &requestHandler{}, nil
    })

    collectedNames := make([]string, 0)

    registerErr := serviceContainer.Register(
        "app.dispatcher",
        func(resolver containercontract.Resolver) (*plainService, error) {
            handlers, allImplementingErr := AllImplementing[collectableHandler](resolver)
            if nil != allImplementingErr {
                return nil, allImplementingErr
            }

            for _, handler := range handlers {
                collectedNames = append(collectedNames, handler.Handle())
            }

            return &plainService{}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    scopeInstance := serviceContainer.NewScope()

    _, getErr := scopeInstance.Get("app.dispatcher")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    for _, collectedName := range collectedNames {
        if "request" == collectedName {
            t.Fatalf("expected a container provider's collection to leave the scoped handler out, got %v", collectedNames)
        }
    }

    if 0 == len(collectedNames) {
        t.Fatalf("expected the container provider to collect the container handlers")
    }
}

/* the resolver a provider receives is the one the AllImplementing godoc tells a dispatcher to collect with, so it has to refuse a closed scope exactly the way collecting with the scope itself does; otherwise the request-outliving goroutine the refusal exists for is handed an empty set and dispatches to nothing */
func TestAllImplementing_RefusesAClosedScopeThroughAProvidersResolver(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    var capturedResolver containercontract.Resolver

    MustRegisterScopedType(serviceContainer, func(resolver containercontract.Resolver) (*handlerDispatcher, error) {
        capturedResolver = resolver

        return &handlerDispatcher{}, nil
    })

    requestScope := serviceContainer.NewScope()

    if _, getErr := FromResolverByType[*handlerDispatcher](requestScope); nil != getErr {
        t.Fatalf("expected the scoped dispatcher to resolve, got %v", getErr)
    }

    if nil == capturedResolver {
        t.Fatalf("expected the provider to have captured its resolver")
    }

    if closeErr := requestScope.Close(); nil != closeErr {
        t.Fatalf("expected the scope to close, got %v", closeErr)
    }

    handlers, allImplementingErr := AllImplementing[collectableHandler](capturedResolver)
    if nil == allImplementingErr {
        t.Fatalf("expected the collection to be refused after the scope closed, got %d handlers and no error", len(handlers))
    }

    if false == errors.Is(allImplementingErr, ErrScopeClosed) {
        t.Fatalf("expected the refusal to carry ErrScopeClosed, got %v", allImplementingErr)
    }
}
