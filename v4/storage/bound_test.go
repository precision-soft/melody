package storage

import (
    "context"
    "errors"
    "io"
    "net/http"
    "reflect"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

var _ interface {
    Put(key string, reader io.Reader, size int64, options storagecontract.PutOptions) error
    Get(key string, options storagecontract.GetOptions) (storagecontract.Object, error)
    Stat(key string, options storagecontract.StatOptions) (storagecontract.ObjectInfo, error)
    Delete(key string, options storagecontract.DeleteOptions) error
    PresignedUrl(key string, config storagecontract.PresignConfig) (string, error)
    Runtime() containercontract.Runtime
    Storage() storagecontract.Storage
} = Bound{}

func TestBound_ForwardsEveryRuntimeDoorOfTheContract(t *testing.T) {
    runtimeType := reflect.TypeFor[containercontract.Runtime]()
    contractType := reflect.TypeFor[storagecontract.Storage]()
    boundType := reflect.TypeFor[Bound]()

    doorNames := map[string]bool{}
    for index := 0; index < contractType.NumMethod(); index = index + 1 {
        door := contractType.Method(index)
        if 0 == door.Type.NumIn() || runtimeType != door.Type.In(0) {
            t.Fatalf("expected every door of the storage contract to take the runtime first, %s does not", door.Name)
        }

        viewMethod, found := boundType.MethodByName(door.Name)
        if false == found {
            t.Fatalf("the view does not forward %s", door.Name)
        }

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

        doorNames[door.Name] = true
    }

    if reflect.TypeFor[*Bound]().NumMethod() != boundType.NumMethod() {
        t.Fatalf("expected every method of the view declared on the value")
    }

    for index := 0; index < boundType.NumMethod(); index = index + 1 {
        methodName := boundType.Method(index).Name
        if false == doorNames[methodName] && "Runtime" != methodName && "Storage" != methodName {
            t.Fatalf("the view declares %s, which forwards no door of the contract", methodName)
        }
    }
}

var boundStorageRecordedErr = errors.New("the storage's own answer")

type boundStorageRecordedCall struct {
    door            string
    runtimeInstance containercontract.Runtime
    arguments       []any
}

type boundRecordingStorage struct {
    callList []boundStorageRecordedCall
    body     io.ReadCloser
}

func (instance *boundRecordingStorage) record(door string, runtimeInstance containercontract.Runtime, arguments ...any) {
    instance.callList = append(instance.callList, boundStorageRecordedCall{door: door, runtimeInstance: runtimeInstance, arguments: arguments})
}

func (instance *boundRecordingStorage) Put(runtimeInstance containercontract.Runtime, key string, reader io.Reader, size int64, options storagecontract.PutOptions) error {
    instance.record("Put", runtimeInstance, key, reader, size, options)

    return boundStorageRecordedErr
}

func (instance *boundRecordingStorage) Get(runtimeInstance containercontract.Runtime, key string, options storagecontract.GetOptions) (storagecontract.Object, error) {
    instance.record("Get", runtimeInstance, key, options)

    return storagecontract.Object{Body: instance.body, Info: boundStorageObjectInfo("got")}, boundStorageRecordedErr
}

func (instance *boundRecordingStorage) Stat(runtimeInstance containercontract.Runtime, key string, _ storagecontract.StatOptions) (storagecontract.ObjectInfo, error) {
    instance.record("Stat", runtimeInstance, key)

    return boundStorageObjectInfo("stated"), boundStorageRecordedErr
}

func (instance *boundRecordingStorage) Delete(runtimeInstance containercontract.Runtime, key string, _ storagecontract.DeleteOptions) error {
    instance.record("Delete", runtimeInstance, key)

    return boundStorageRecordedErr
}

func (instance *boundRecordingStorage) PresignedUrl(runtimeInstance containercontract.Runtime, key string, config storagecontract.PresignConfig) (string, error) {
    instance.record("PresignedUrl", runtimeInstance, key, config)

    return "https://signed.example/" + key, boundStorageRecordedErr
}

func boundStorageObjectInfo(prefix string) storagecontract.ObjectInfo {
    return storagecontract.ObjectInfo{
        Size:        int64(len(prefix)) + 40,
        ContentType: prefix + "/type",
        ModifiedAt:  time.Unix(1600000000+int64(len(prefix)), 0).UTC(),
        EntityTag:   prefix + "-tag",
        Key:         prefix + "-key",
    }
}

func TestBound_EveryMethodReachesItsDoorWithTheBoundRuntimeAndAnswersWhatItAnswered(t *testing.T) {
    runtimeInstance := testRuntime()
    body := io.NopCloser(strings.NewReader("body"))
    recordingStorage := &boundRecordingStorage{body: body}
    boundStorage := For(runtimeInstance, recordingStorage)

    reader := strings.NewReader("payload")
    putOptions := storagecontract.PutOptions{ContentType: "application/pdf"}
    presignConfig := storagecontract.PresignConfig{Expiry: bound.Of(90 * time.Second), Method: http.MethodPut}

    putErr := boundStorage.Put("put-key", reader, 7, putOptions)
    getOptions := storagecontract.GetOptions{Range: storagecontract.ByteRange{Offset: 3, Length: 4}}
    object, getErr := boundStorage.Get("got-key", getOptions)
    objectInfo, statErr := boundStorage.Stat("stated-key", storagecontract.StatOptions{})
    deleteErr := boundStorage.Delete("deleted-key", storagecontract.DeleteOptions{})
    url, presignErr := boundStorage.PresignedUrl("signed-key", presignConfig)

    expected := []boundStorageRecordedCall{
        {door: "Put", arguments: []any{"put-key", reader, int64(7), putOptions}},
        {door: "Get", arguments: []any{"got-key", getOptions}},
        {door: "Stat", arguments: []any{"stated-key"}},
        {door: "Delete", arguments: []any{"deleted-key"}},
        {door: "PresignedUrl", arguments: []any{"signed-key", presignConfig}},
    }

    if len(expected) != len(recordingStorage.callList) {
        t.Fatalf("expected %d calls, got %d", len(expected), len(recordingStorage.callList))
    }

    for index, expectedCall := range expected {
        recorded := recordingStorage.callList[index]
        if expectedCall.door != recorded.door || runtimeInstance != recorded.runtimeInstance || false == reflect.DeepEqual(expectedCall.arguments, recorded.arguments) {
            t.Fatalf("call %d: expected %s %v under the bound runtime, got %s %v", index, expectedCall.door, expectedCall.arguments, recorded.door, recorded.arguments)
        }
    }

    if io.Reader(reader) != recordingStorage.callList[0].arguments[1] {
        t.Fatalf("expected Put to hand the caller's reader by identity")
    }

    if body != object.Body || boundStorageObjectInfo("got") != object.Info || boundStorageRecordedErr != getErr {
        t.Fatalf("expected Get to answer the storage's body by identity, its info and its error, got %v %v", object, getErr)
    }

    if boundStorageObjectInfo("stated") != objectInfo || boundStorageRecordedErr != statErr {
        t.Fatalf("expected Stat to answer the storage's info and its error, got %v %v", objectInfo, statErr)
    }

    if "https://signed.example/signed-key" != url || boundStorageRecordedErr != presignErr {
        t.Fatalf("expected PresignedUrl to answer the storage's url and its error, got %q %v", url, presignErr)
    }

    for doorName, doorErr := range map[string]error{"Put": putErr, "Delete": deleteErr} {
        if boundStorageRecordedErr != doorErr {
            t.Fatalf("%s: expected the storage's error, got %v", doorName, doorErr)
        }
    }

    if runtimeInstance != boundStorage.Runtime() || storagecontract.Storage(recordingStorage) != boundStorage.Storage() {
        t.Fatalf("expected the halves to answer what the view was bound over")
    }
}

func TestBound_AViewOverNoStorageRefusesEveryDoor(t *testing.T) {
    var typedNilStorage *LocalStorage

    for _, boundStorage := range []Bound{{}, For(testRuntime(), nil), For(testRuntime(), typedNilStorage)} {
        putErr := boundStorage.Put("key", strings.NewReader("x"), 1, storagecontract.PutOptions{})
        object, getErr := boundStorage.Get("key", storagecontract.GetOptions{})
        objectInfo, statErr := boundStorage.Stat("key", storagecontract.StatOptions{})
        deleteErr := boundStorage.Delete("key", storagecontract.DeleteOptions{})
        exists, existsErr := Exists(boundStorage, "key")
        url, presignErr := boundStorage.PresignedUrl("key", storagecontract.PresignConfig{})

        for doorName, doorErr := range map[string]error{"Put": putErr, "Get": getErr, "Stat": statErr, "Delete": deleteErr, "Exists": existsErr, "PresignedUrl": presignErr} {
            if nil == doorErr || "storage instance is nil" != doorErr.Error() {
                t.Fatalf("%s: expected the absent storage refused, got %v", doorName, doorErr)
            }
        }

        if nil != object.Body || (storagecontract.ObjectInfo{}) != object.Info || (storagecontract.ObjectInfo{}) != objectInfo || true == exists || "" != url {
            t.Fatalf("expected the refused doors to answer zero values, got %v %v %v %q", object, objectInfo, exists, url)
        }
    }
}

/* a package variable, so the call goes through the contract and is never devirtualized */
var boundAllocationStorage storagecontract.Storage

func TestBound_BindingAddsNoAllocationToTheContractsCall(t *testing.T) {
    runtimeInstance := testRuntime()
    boundAllocationStorage = &boundCountingStorage{}

    directAllocations := testing.AllocsPerRun(1000, func() {
        _, _ = boundAllocationStorage.Stat(runtimeInstance, "key", storagecontract.StatOptions{})
    })
    boundAllocations := testing.AllocsPerRun(1000, func() {
        _, _ = For(runtimeInstance, boundAllocationStorage).Stat("key", storagecontract.StatOptions{})
    })
    if directAllocations != boundAllocations {
        t.Fatalf("expected a bind and a call to allocate what the direct call does, %v, got %v", directAllocations, boundAllocations)
    }
}

type boundCountingStorage struct {
    boundRecordingStorage
}

func (instance *boundCountingStorage) Stat(runtimeInstance containercontract.Runtime, key string, _ storagecontract.StatOptions) (storagecontract.ObjectInfo, error) {
    return storagecontract.ObjectInfo{}, nil
}

func TestFromRuntime_BindsTheApplicationsStorage(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    if registerErr := serviceContainer.Register(
        ServiceStorage,
        func(resolver containercontract.Resolver) (storagecontract.Storage, error) {
            return local, nil
        },
        container.RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    boundStorage := FromRuntime(runtimeInstance)
    if storagecontract.Storage(local) != boundStorage.Storage() || runtimeInstance != boundStorage.Runtime() {
        t.Fatalf("expected the registered storage bound to the runtime")
    }

    panicked := func() (recovered bool) {
        defer func() {
            recovered = nil != recover()
        }()

        FromRuntime(runtime.NewRun(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope()))

        return false
    }()
    if false == panicked {
        t.Fatalf("expected a runtime whose container registers no storage to panic")
    }
}
