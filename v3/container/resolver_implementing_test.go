package container

import (
    "errors"
    "testing"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestAllImplementing_CollectsOnlyTheServicesSatisfyingTheInterface(t *testing.T) {
    serviceContainer := newCollectionContainer(t)

    handlers, allImplementingErr := AllImplementing[collectableHandler](serviceContainer)
    if nil != allImplementingErr {
        t.Fatalf("expected the collection to succeed, got %v", allImplementingErr)
    }

    if 2 != len(handlers) {
        t.Fatalf("expected two handlers, got %d", len(handlers))
    }

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

    all, allErr := AllImplementing[collectableHandler](serviceContainer)
    if nil != allErr {
        t.Fatalf("expected the outer collection to succeed, got %v", allErr)
    }

    if 3 != len(all) {
        t.Fatalf("expected the dispatcher to be part of the outer collection, got %d", len(all))
    }
}

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

    if 0 == len(handlers) {
        t.Fatalf("expected the container collection to gather the container handlers")
    }
}

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
