package httpclient

import (
    "context"
    "errors"
    "reflect"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    httpclientcontract "github.com/precision-soft/melody/v4/httpclient/contract"
)

var _ interface {
    Get(urlString string, options RequestOptions) (httpclientcontract.Response, error)
    Post(urlString string, body any, options RequestOptions) (httpclientcontract.Response, error)
    Put(urlString string, body any, options RequestOptions) (httpclientcontract.Response, error)
    Patch(urlString string, body any, options RequestOptions) (httpclientcontract.Response, error)
    Delete(urlString string, options RequestOptions) (httpclientcontract.Response, error)
    Request(method string, urlString string, options RequestOptions) (httpclientcontract.Response, error)
    RequestStream(method string, urlString string, options RequestOptions) (httpclientcontract.StreamResponse, error)
    Runtime() containercontract.Runtime
    Client() httpclientcontract.Client
} = Bound{}

func TestBound_ForwardsEveryRuntimeDoorOfTheContract(t *testing.T) {
    contractType := reflect.TypeFor[httpclientcontract.Client]()
    runtimeType := reflect.TypeFor[containercontract.Runtime]()
    boundType := reflect.TypeFor[Bound]()

    forwarded := map[string]bool{"Runtime": true, "Client": true}
    for index := 0; index < contractType.NumMethod(); index = index + 1 {
        door := contractType.Method(index)
        if 0 == door.Type.NumIn() || runtimeType != door.Type.In(0) {
            t.Fatalf("expected every door of the contract to take the runtime first, %s does not", door.Name)
        }

        viewMethod, found := boundType.MethodByName(door.Name)
        if false == found {
            t.Fatalf("the view does not forward %s", door.Name)
        }

        /* the view's method type carries its receiver first */
        if viewMethod.Type.NumIn() != door.Type.NumIn() || viewMethod.Type.NumOut() != door.Type.NumOut() || viewMethod.Type.IsVariadic() != door.Type.IsVariadic() {
            t.Fatalf("%s: the view's signature %s is not the contract's %s less the runtime", door.Name, viewMethod.Type, door.Type)
        }

        for parameterIndex := 1; parameterIndex < door.Type.NumIn(); parameterIndex = parameterIndex + 1 {
            if door.Type.In(parameterIndex) != viewMethod.Type.In(parameterIndex) {
                t.Fatalf("%s: parameter %d differs", door.Name, parameterIndex)
            }
        }

        for resultIndex := 0; resultIndex < door.Type.NumOut(); resultIndex = resultIndex + 1 {
            if door.Type.Out(resultIndex) != viewMethod.Type.Out(resultIndex) {
                t.Fatalf("%s: result %d differs", door.Name, resultIndex)
            }
        }

        forwarded[door.Name] = true
    }

    if reflect.TypeFor[*Bound]().NumMethod() != boundType.NumMethod() {
        t.Fatalf("expected every method of the view declared on the value")
    }

    for index := 0; index < boundType.NumMethod(); index = index + 1 {
        if false == forwarded[boundType.Method(index).Name] {
            t.Fatalf("the view declares %s, which forwards no door of the contract", boundType.Method(index).Name)
        }
    }
}

var boundRecordedErr = errors.New("the contract's own answer")

type boundRecordedCall struct {
    door            string
    runtimeInstance containercontract.Runtime
    arguments       []any
}

type boundRecordedResponse struct {
    httpclientcontract.Response
    door string
}

type boundRecordedStream struct {
    httpclientcontract.StreamResponse
    door string
}

type boundRecordingClient struct {
    callList []boundRecordedCall
}

func (instance *boundRecordingClient) answer(door string, runtimeInstance containercontract.Runtime, arguments ...any) (httpclientcontract.Response, error) {
    instance.callList = append(instance.callList, boundRecordedCall{door: door, runtimeInstance: runtimeInstance, arguments: arguments})

    return &boundRecordedResponse{door: door}, boundRecordedErr
}

func (instance *boundRecordingClient) Get(runtimeInstance containercontract.Runtime, urlString string, options RequestOptions) (httpclientcontract.Response, error) {
    return instance.answer("Get", runtimeInstance, urlString, options)
}

func (instance *boundRecordingClient) Post(runtimeInstance containercontract.Runtime, urlString string, body any, options RequestOptions) (httpclientcontract.Response, error) {
    return instance.answer("Post", runtimeInstance, urlString, body, options)
}

func (instance *boundRecordingClient) Put(runtimeInstance containercontract.Runtime, urlString string, body any, options RequestOptions) (httpclientcontract.Response, error) {
    return instance.answer("Put", runtimeInstance, urlString, body, options)
}

func (instance *boundRecordingClient) Patch(runtimeInstance containercontract.Runtime, urlString string, body any, options RequestOptions) (httpclientcontract.Response, error) {
    return instance.answer("Patch", runtimeInstance, urlString, body, options)
}

func (instance *boundRecordingClient) Delete(runtimeInstance containercontract.Runtime, urlString string, options RequestOptions) (httpclientcontract.Response, error) {
    return instance.answer("Delete", runtimeInstance, urlString, options)
}

func (instance *boundRecordingClient) Request(runtimeInstance containercontract.Runtime, method string, urlString string, options RequestOptions) (httpclientcontract.Response, error) {
    return instance.answer("Request", runtimeInstance, method, urlString, options)
}

func (instance *boundRecordingClient) RequestStream(runtimeInstance containercontract.Runtime, method string, urlString string, options RequestOptions) (httpclientcontract.StreamResponse, error) {
    instance.callList = append(instance.callList, boundRecordedCall{door: "RequestStream", runtimeInstance: runtimeInstance, arguments: []any{method, urlString, options}})

    return &boundRecordedStream{door: "RequestStream"}, boundRecordedErr
}

func TestBound_EveryMethodReachesItsDoorWithTheBoundRuntimeAndAnswersWhatItAnswered(t *testing.T) {
    runtimeInstance := testRuntime()
    recordingClient := &boundRecordingClient{}
    boundClient := For(runtimeInstance, recordingClient)

    getOptions := RequestOptions{Headers: map[string]string{"X-Door": "get"}}
    postBody := map[string]int{"post": 1}
    putBody := map[string]int{"put": 2}
    patchBody := map[string]int{"patch": 3}

    responses := map[string]httpclientcontract.Response{}
    errorsByDoor := map[string]error{}
    responses["Get"], errorsByDoor["Get"] = boundClient.Get("/get", getOptions)
    responses["Post"], errorsByDoor["Post"] = boundClient.Post("/post", postBody, RequestOptions{})
    responses["Put"], errorsByDoor["Put"] = boundClient.Put("/put", putBody, RequestOptions{})
    responses["Patch"], errorsByDoor["Patch"] = boundClient.Patch("/patch", patchBody, RequestOptions{})
    responses["Delete"], errorsByDoor["Delete"] = boundClient.Delete("/delete", RequestOptions{})
    responses["Request"], errorsByDoor["Request"] = boundClient.Request("OPTIONS", "/request", RequestOptions{})
    stream, streamErr := boundClient.RequestStream("GET", "/stream", RequestOptions{})

    expected := []boundRecordedCall{
        {door: "Get", arguments: []any{"/get", getOptions}},
        {door: "Post", arguments: []any{"/post", postBody, RequestOptions{}}},
        {door: "Put", arguments: []any{"/put", putBody, RequestOptions{}}},
        {door: "Patch", arguments: []any{"/patch", patchBody, RequestOptions{}}},
        {door: "Delete", arguments: []any{"/delete", RequestOptions{}}},
        {door: "Request", arguments: []any{"OPTIONS", "/request", RequestOptions{}}},
        {door: "RequestStream", arguments: []any{"GET", "/stream", RequestOptions{}}},
    }

    if len(expected) != len(recordingClient.callList) {
        t.Fatalf("expected %d calls, got %d", len(expected), len(recordingClient.callList))
    }

    for index, expectedCall := range expected {
        recorded := recordingClient.callList[index]
        if expectedCall.door != recorded.door || runtimeInstance != recorded.runtimeInstance || false == reflect.DeepEqual(expectedCall.arguments, recorded.arguments) {
            t.Fatalf("call %d: expected %s %v under the bound runtime, got %s %v", index, expectedCall.door, expectedCall.arguments, recorded.door, recorded.arguments)
        }
    }

    for doorName, response := range responses {
        recordedResponse, isRecorded := response.(*boundRecordedResponse)
        if false == isRecorded || doorName != recordedResponse.door || boundRecordedErr != errorsByDoor[doorName] {
            t.Fatalf("%s: expected the view to answer the contract's response and error", doorName)
        }
    }

    if recordedStream, isRecorded := stream.(*boundRecordedStream); false == isRecorded || "RequestStream" != recordedStream.door || boundRecordedErr != streamErr {
        t.Fatalf("expected the view to answer the contract's stream and error")
    }

    if runtimeInstance != boundClient.Runtime() || httpclientcontract.Client(recordingClient) != boundClient.Client() {
        t.Fatalf("expected the halves to answer what the view was bound over")
    }
}

func TestBound_AViewOverNoClientRefusesEveryDoor(t *testing.T) {
    var typedNilClient *HttpClient

    for _, boundClient := range []Bound{{}, For(testRuntime(), nil), For(testRuntime(), typedNilClient)} {
        _, getErr := boundClient.Get("/", RequestOptions{})
        _, postErr := boundClient.Post("/", nil, RequestOptions{})
        _, putErr := boundClient.Put("/", nil, RequestOptions{})
        _, patchErr := boundClient.Patch("/", nil, RequestOptions{})
        _, deleteErr := boundClient.Delete("/", RequestOptions{})
        _, requestErr := boundClient.Request("GET", "/", RequestOptions{})
        _, streamErr := boundClient.RequestStream("GET", "/", RequestOptions{})

        for doorName, doorErr := range map[string]error{"Get": getErr, "Post": postErr, "Put": putErr, "Patch": patchErr, "Delete": deleteErr, "Request": requestErr, "RequestStream": streamErr} {
            if nil == doorErr || "http client instance is nil" != doorErr.Error() {
                t.Fatalf("%s: expected the absent client refused, got %v", doorName, doorErr)
            }
        }
    }
}

/* a package variable, so the direct call is made through the contract as the view makes it, never devirtualized */
var boundAllocationClient httpclientcontract.Client

func TestBound_BindingAddsNoAllocationToTheContractsCall(t *testing.T) {
    runtimeInstance := testRuntime()
    boundAllocationClient = &boundCountingClient{}

    directAllocations := testing.AllocsPerRun(1000, func() {
        _, _ = boundAllocationClient.Get(runtimeInstance, "/", RequestOptions{})
    })
    boundAllocations := testing.AllocsPerRun(1000, func() {
        _, _ = For(runtimeInstance, boundAllocationClient).Get("/", RequestOptions{})
    })
    if directAllocations != boundAllocations {
        t.Fatalf("expected a bind and a call to allocate what the direct call does, %v, got %v", directAllocations, boundAllocations)
    }
}

type boundCountingClient struct {
    boundRecordingClient
}

func (instance *boundCountingClient) Get(runtimeInstance containercontract.Runtime, urlString string, options RequestOptions) (httpclientcontract.Response, error) {
    return nil, nil
}

func TestFromRuntime_BindsTheClientRegisteredUnderTheApplicationsName(t *testing.T) {
    client := newTestHttpClient(t, HttpClientConfig{BaseUrl: "http://127.0.0.1:1"})
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    if registerErr := serviceContainer.Register(
        "app.rates.client",
        func(resolver containercontract.Resolver) (*HttpClient, error) {
            return client, nil
        },
        container.RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())
    boundClient := FromRuntime(runtimeInstance, "app.rates.client")
    if httpclientcontract.Client(client) != boundClient.Client() || runtimeInstance != boundClient.Runtime() {
        t.Fatalf("expected the registered client bound to the runtime")
    }

    panicked := func() (recovered bool) {
        defer func() {
            recovered = nil != recover()
        }()

        FromRuntime(runtimeInstance, "app.missing.client")

        return false
    }()
    if false == panicked {
        t.Fatalf("expected a name nothing registered to panic")
    }
}
