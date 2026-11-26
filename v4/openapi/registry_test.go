package openapi

import (
    "strings"
    nethttp "net/http"
    "reflect"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/openapiseam"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

type registryProbePayload struct {
    Name string `json:"name"`
}

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
        Responses:   []ResponseDescription{Response[registryProbePayload](nethttp.StatusCreated)},
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

    if 1 != len(descriptor.Responses) || nethttp.StatusCreated != descriptor.Responses[0].Status || TypeOf[registryProbePayload]() != descriptor.Responses[0].Type {
        t.Fatalf("expected the recorded response, got %v", descriptor.Responses)
    }
}

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

func TestRegistry_DescribeIsRefusedOnceTheRegistryIsMarkedServing(t *testing.T) {
    registry := NewRegistry().Describe("example.one", Descriptor{Summary: "first"})

    registry.MarkServing(openapiseam.NewToken())

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

func TestRegistry_ALateDescribeNamesTheRouteInItsRefusal(t *testing.T) {
    registry := NewRegistry()
    registry.MarkServing(openapiseam.NewToken())

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatal("expected the late Describe to be refused")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected the refusal to be an error, got %T", recoveredValue)
        }

        if "example.late" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["routeName"] {
            t.Fatalf("expected the refusal to name the route, got %v", exception.LogContext(recoveredErr, exception.LogContextConfig{}))
        }
    }()

    registry.Describe("example.late", Descriptor{Summary: "late"})
}

type descriptorCopyBody struct{}

func TestRegistry_DescribeAndGetKeepNoReferenceToTheCallersTagsAndResponses(t *testing.T) {
    registry := NewRegistry()

    tags := []string{"catalogue"}
    responses := []ResponseDescription{Response[descriptorCopyBody](200)}
    registry.Describe("products.list", Descriptor{Tags: tags, Responses: responses})

    tags[0] = "rewritten"
    responses[0].Status = 500

    stored, _ := registry.Get("products.list")
    if "catalogue" != stored.Tags[0] || 200 != stored.Responses[0].Status {
        t.Fatalf("expected the registry to keep its own copy of the description, got tags %v responses %v", stored.Tags, stored.Responses)
    }

    stored.Tags[0] = "rewritten"
    stored.Responses[0].Status = 404

    again, _ := registry.Get("products.list")
    if "catalogue" != again.Tags[0] || 200 != again.Responses[0].Status {
        t.Fatalf("expected Get to answer a copy, got tags %v responses %v", again.Tags, again.Responses)
    }
}

type describeNotFoundBody struct {
    Reason string `json:"reason"`
}

func TestRegistry_DescribeALaterResponseReplacesAnEarlierOneOfTheSameStatus(t *testing.T) {
    registry := NewRegistry()

    registry.Describe("products.read", Descriptor{
        Responses: []ResponseDescription{
            Response[registryProbePayload](nethttp.StatusOK),
            Response[describeNotFoundBody](nethttp.StatusNotFound),
            Response[descriptorCopyBody](nethttp.StatusOK),
        },
    })

    descriptor, _ := registry.Get("products.read")
    expected := []ResponseDescription{
        {Status: nethttp.StatusOK, Type: TypeOf[descriptorCopyBody]()},
        {Status: nethttp.StatusNotFound, Type: TypeOf[describeNotFoundBody]()},
    }
    if false == reflect.DeepEqual(expected, descriptor.Responses) {
        t.Fatalf("expected the later 200 to replace the earlier one in its place, got %v", descriptor.Responses)
    }
}

func TestResponse_AnswersTheStatusAndTheElementType(t *testing.T) {
    response := Response[registryProbePayload](nethttp.StatusCreated)

    if nethttp.StatusCreated != response.Status || TypeOf[registryProbePayload]() != response.Type {
        t.Fatalf("unexpected response description %+v", response)
    }
}

func TestMarkServing_IsNotCallableWithoutTheSeamToken(t *testing.T) {
    ownerType := reflect.TypeOf(NewRegistry())

    method, hasMethod := ownerType.MethodByName("MarkServing")
    if false == hasMethod {
        t.Fatal("expected Registry.MarkServing")
    }

    if 2 != method.Type.NumIn() || reflect.TypeOf(openapiseam.Token{}) != method.Type.In(1) {
        t.Fatalf("expected MarkServing to take the seam token and nothing else, got %s", method.Type)
    }

    for index := 0; index < ownerType.NumMethod(); index++ {
        candidate := ownerType.Method(index)
        if false == strings.Contains(candidate.Name, "Serving") || 0 != candidate.Type.NumOut() {
            continue
        }

        if 2 > candidate.Type.NumIn() || reflect.TypeOf(openapiseam.Token{}) != candidate.Type.In(1) {
            t.Errorf("Registry.%s reaches the serving state without the seam token", candidate.Name)
        }
    }
}
