package openapi

import (
    nethttp "net/http"
    "reflect"
    "testing"
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
