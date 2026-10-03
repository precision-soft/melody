package container

import (
    "reflect"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* the two listers are what a collection is gathered through, and both must refuse anything that is not an interface: a concrete type reaches Implements() as a question with no answer, and a nil one would panic on the first Kind() call */
func TestTypeListers_RefuseAnythingThatIsNotAnInterface(t *testing.T) {
    serviceContainer := newCollectionContainer(t).(*container)

    for _, probe := range []reflect.Type{nil, reflect.TypeOf(invoiceHandler{}), reflect.TypeOf(&invoiceHandler{}), reflect.TypeOf("")} {
        types := serviceContainer.TypesImplementing(probe)
        if nil == types || 0 != len(types) {
            t.Fatalf("%v: expected an empty slice rather than nil or a match, got %v", probe, types)
        }

        references := serviceContainer.ReferencesImplementing(probe)
        if nil == references || 0 != len(references) {
            t.Fatalf("%v: expected an empty slice rather than nil or a match, got %v", probe, references)
        }
    }
}

func TestTypesImplementing_ListsOnlyTheRegisteredTypesSatisfyingTheInterface(t *testing.T) {
    serviceContainer := newCollectionContainer(t).(*container)

    types := serviceContainer.TypesImplementing(reflect.TypeOf((*collectableHandler)(nil)).Elem())
    if 2 != len(types) {
        t.Fatalf("expected the two handlers, got %v", types)
    }

    /* registration order is a map iteration away from being arbitrary, and a collection that reorders between runs turns into an unreproducible bug in whatever consumes it */
    if types[0].String() > types[1].String() {
        t.Fatalf("expected the types in a stable order, got %v", types)
    }

    if 0 != len(serviceContainer.TypesImplementing(reflect.TypeOf((*unrelatedContract)(nil)).Elem())) {
        t.Fatalf("expected no type to satisfy an interface nothing implements")
    }
}

/* a type registered under several names is the multi-instance pattern — admitted by the lenient type registration, since the strict one refuses the second name outright — and the reference lister must contribute EVERY name, where a by-type resolution would refuse the ambiguity */
func TestReferencesImplementing_ContributesOneReferencePerRegisteredName(t *testing.T) {
    serviceContainer := NewContainer()

    for _, serviceName := range []string{"handler.primary", "handler.secondary"} {
        MustRegister[*invoiceHandler](
            serviceContainer,
            serviceName,
            func(resolver containercontract.Resolver) (*invoiceHandler, error) {
                return &invoiceHandler{}, nil
            },
            WithTypeRegistration(false),
        )
    }

    references := serviceContainer.(*container).ReferencesImplementing(reflect.TypeOf((*collectableHandler)(nil)).Elem())
    if 2 != len(references) {
        t.Fatalf("expected one reference per registered name, got %v", references)
    }

    if "handler.primary" != references[0].ServiceName || "handler.secondary" != references[1].ServiceName {
        t.Fatalf("expected the names ordered on a priority tie, got %v", references)
    }
}

/* the order a collection is dispatched in: descending priority first, then type, then name — and the name is what breaks a tie between two references of the SAME type, which is where map iteration would otherwise leak into the result */
func TestSortServiceReferences_OrdersByPriorityThenTypeThenName(t *testing.T) {
    invoiceType := reflect.TypeOf(&invoiceHandler{})
    auditType := reflect.TypeOf(&auditHandler{})

    sorted := sortServiceReferences([]prioritizedReference{
        {reference: containercontract.ServiceReference{ServiceName: "b", ServiceType: invoiceType}, priority: 0},
        {reference: containercontract.ServiceReference{ServiceName: "a", ServiceType: invoiceType}, priority: 0},
        {reference: containercontract.ServiceReference{ServiceName: "z", ServiceType: auditType}, priority: 0},
        {reference: containercontract.ServiceReference{ServiceName: "late", ServiceType: invoiceType}, priority: 10},
    })

    if "late" != sorted[0].ServiceName {
        t.Fatalf("expected the highest priority first, got %v", sorted)
    }

    if auditType != sorted[1].ServiceType {
        t.Fatalf("expected the type to order a priority tie, got %v", sorted)
    }

    if "a" != sorted[2].ServiceName || "b" != sorted[3].ServiceName {
        t.Fatalf("expected the name to break a tie within one type, got %v", sorted)
    }
}

func TestSortServiceReferences_AnswersAnEmptySliceForNoReferences(t *testing.T) {
    sorted := sortServiceReferences(nil)
    if nil == sorted || 0 != len(sorted) {
        t.Fatalf("expected an empty slice rather than nil, got %v", sorted)
    }
}
