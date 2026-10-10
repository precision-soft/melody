package openapi

import (
    nethttp "net/http"
    "reflect"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
)

type registryProbePayload struct {
    Name string `json:"name"`
}

/* TypeOf is the door every Describe call names its request and response shapes through, and it must answer the ELEMENT type: handing back the pointer type would make every descriptor describe a pointer, which the generator has no schema for */
func TestTypeOf_AnswersTheElementTypeForEveryShape(t *testing.T) {
    if reflect.TypeOf(registryProbePayload{}) != TypeOf[registryProbePayload]() {
        t.Fatalf("expected the struct type, got %v", TypeOf[registryProbePayload]())
    }

    if reflect.Pointer != TypeOf[*registryProbePayload]().Kind() {
        t.Fatalf("expected a pointer type to stay a pointer, got %v", TypeOf[*registryProbePayload]().Kind())
    }

    if reflect.Slice != TypeOf[[]registryProbePayload]().Kind() {
        t.Fatalf("expected a slice type, got %v", TypeOf[[]registryProbePayload]().Kind())
    }

    /* an interface type is the shape a generic reflect.TypeOf(value) cannot produce at all: reflect.TypeOf on an interface value answers the dynamic type, so the Elem() form is what makes an interface describable */
    if reflect.Interface != TypeOf[error]().Kind() {
        t.Fatalf("expected an interface type to be answered as an interface, got %v", TypeOf[error]().Kind())
    }
}

func TestRegistry_GetAnswersFalseForARouteNobodyDescribed(t *testing.T) {
    descriptor, exists := NewRegistry().Get("example.absent")
    if true == exists {
        t.Fatalf("expected an undescribed route to be absent, got %+v", descriptor)
    }

    if (reflect.DeepEqual(Descriptor{}, descriptor)) == false {
        t.Fatalf("expected the zero descriptor when absent, got %+v", descriptor)
    }
}

func TestRegistry_DescribeRecordsTheDescriptorUnderItsRouteName(t *testing.T) {
    registry := NewRegistry()

    registry.Describe("example.create", Descriptor{
        Summary:     "Create",
        Tags:        []string{"example"},
        RequestType: TypeOf[registryProbePayload](),
        Responses:   map[int]reflect.Type{nethttp.StatusCreated: TypeOf[registryProbePayload]()},
    })

    descriptor, exists := registry.Get("example.create")
    if false == exists {
        t.Fatal("expected the described route to be found")
    }

    if "Create" != descriptor.Summary {
        t.Fatalf("expected the recorded summary, got %q", descriptor.Summary)
    }

    if TypeOf[registryProbePayload]() != descriptor.RequestType {
        t.Fatalf("expected the recorded request type, got %v", descriptor.RequestType)
    }

    if TypeOf[registryProbePayload]() != descriptor.Responses[nethttp.StatusCreated] {
        t.Fatalf("expected the recorded response type, got %v", descriptor.Responses[nethttp.StatusCreated])
    }
}

/* Describe answers the registry so a module can chain its whole surface in one expression, and the last description of a route name wins rather than being refused — a module re-describing its own route is how an override is expressed */
func TestRegistry_DescribeChainsAndTheLastDescriptionWins(t *testing.T) {
    registry := NewRegistry()

    chained := registry.Describe("example.one", Descriptor{Summary: "first"}).Describe("example.one", Descriptor{Summary: "second"})

    if registry != chained {
        t.Fatal("expected Describe to answer the registry it was called on")
    }

    descriptor, _ := registry.Get("example.one")
    if "second" != descriptor.Summary {
        t.Fatalf("expected the last description to win, got %q", descriptor.Summary)
    }
}

/* A Describe after MarkServing is refused at the door, naming the route: the map it would write is read by the spec handler on the request path with nothing synchronizing the two, so the refusal is the same one the router gives a late route */
func TestRegistry_DescribeIsRefusedOnceTheRegistryIsMarkedServing(t *testing.T) {
    registry := NewRegistry().Describe("example.one", Descriptor{Summary: "first"})

    registry.MarkServing()

    testhelper.AssertPanicsWithError(
        t,
        func() {
            registry.Describe("example.late", Descriptor{Summary: "late"})
        },
        "may not describe a route after the application started serving",
    )

    if _, exists := registry.Get("example.late"); true == exists {
        t.Fatal("expected the refused description to have written nothing")
    }

    descriptor, exists := registry.Get("example.one")
    if false == exists || "first" != descriptor.Summary {
        t.Fatalf("expected the description recorded before serving to stand, got %v %v", descriptor, exists)
    }
}

/* the refusal carries the route under routeName, which is what an operator reads to find the module describing too late */
func TestRegistry_ALateDescribeNamesTheRouteInItsRefusal(t *testing.T) {
    registry := NewRegistry()
    registry.MarkServing()

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatal("expected the late Describe to be refused")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected the refusal to be an error, got %T", recoveredValue)
        }

        if "example.late" != exception.LogContext(recoveredErr)["routeName"] {
            t.Fatalf("expected the refusal to name the route, got %v", exception.LogContext(recoveredErr))
        }
    }()

    registry.Describe("example.late", Descriptor{Summary: "late"})
}

type descriptorCopyBody struct{}

/* Describe stored the caller's Tags slice and Responses map by reference and Get handed them back the same way, so a tag list reused across routes rewrote every earlier description and a reader of Get could write into the registry; both doors copy. The double hands over its own slice and map deliberately — nothing in the package copies on the way in. */
func TestRegistry_DescribeAndGetKeepNoReferenceToTheCallersTagsAndResponses(t *testing.T) {
    registry := NewRegistry()

    tags := []string{"catalogue"}
    responses := map[int]reflect.Type{200: TypeOf[descriptorCopyBody]()}
    registry.Describe("products.list", Descriptor{Tags: tags, Responses: responses})

    tags[0] = "rewritten"
    responses[500] = TypeOf[descriptorCopyBody]()

    stored, _ := registry.Get("products.list")
    if "catalogue" != stored.Tags[0] || 1 != len(stored.Responses) {
        t.Fatalf("expected the registry to keep its own copy of the description, got tags %v responses %v", stored.Tags, stored.Responses)
    }

    stored.Tags[0] = "rewritten"
    stored.Responses[404] = TypeOf[descriptorCopyBody]()

    again, _ := registry.Get("products.list")
    if "catalogue" != again.Tags[0] || 1 != len(again.Responses) {
        t.Fatalf("expected Get to answer a copy, got tags %v responses %v", again.Tags, again.Responses)
    }
}
