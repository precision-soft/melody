package security

import (
    "context"
    "errors"
    "reflect"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

var _ interface {
    Lookup(tokenString string) (securitycontract.Claims, bool, error)
    Put(tokenString string, claims securitycontract.Claims, ttl bound.Duration) error
    Delete(tokenString string) error
    DeleteByUser(userIdentifier string) (int, error)
    PurgeExpired() (int, error)
    RevokeBefore(userIdentifier string, deviceIdentifier string, instant time.Time) error
    RevocationEpoch(userIdentifier string, deviceIdentifier string) (time.Time, error)
    Runtime() containercontract.Runtime
    TokenStore() securitycontract.EpochRevocableTokenStore
} = BoundTokenStore{}

func boundTokenStoreDoorNames(t *testing.T, contractType reflect.Type) map[string]bool {
    t.Helper()

    runtimeType := reflect.TypeFor[containercontract.Runtime]()
    boundType := reflect.TypeFor[BoundTokenStore]()

    doorNames := map[string]bool{}
    for index := 0; index < contractType.NumMethod(); index = index + 1 {
        door := contractType.Method(index)
        if 0 == door.Type.NumIn() || runtimeType != door.Type.In(0) {
            t.Fatalf("expected every door of %s to take the runtime first, %s does not", contractType, door.Name)
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

        doorNames[door.Name] = true
    }

    return doorNames
}

func TestBoundTokenStore_ForwardsEveryRuntimeDoorOfTheContract(t *testing.T) {
    composedDoors := boundTokenStoreDoorNames(t, reflect.TypeFor[securitycontract.EpochRevocableTokenStore]())

    /* a door added to one half only must still be seen by the composed contract the view binds */
    unionDoors := map[string]bool{}
    for _, halfType := range []reflect.Type{reflect.TypeFor[securitycontract.RevocableTokenStore](), reflect.TypeFor[securitycontract.RevocationEpochStore]()} {
        for doorName := range boundTokenStoreDoorNames(t, halfType) {
            unionDoors[doorName] = true
        }
    }

    if false == reflect.DeepEqual(composedDoors, unionDoors) {
        t.Fatalf("expected the composed contract to be the union of its halves, %v against %v", composedDoors, unionDoors)
    }

    if 7 != len(composedDoors) {
        t.Fatalf("expected the seven doors of the composed contract, got %v", composedDoors)
    }

    boundType := reflect.TypeFor[BoundTokenStore]()
    if reflect.TypeFor[*BoundTokenStore]().NumMethod() != boundType.NumMethod() {
        t.Fatalf("expected every method of the view declared on the value")
    }

    for index := 0; index < boundType.NumMethod(); index = index + 1 {
        methodName := boundType.Method(index).Name
        if false == composedDoors[methodName] && "Runtime" != methodName && "TokenStore" != methodName {
            t.Fatalf("the view declares %s, which forwards no door of the contract", methodName)
        }
    }
}

var boundTokenStoreRecordedErr = errors.New("the store's own answer")

type boundTokenStoreRecordedCall struct {
    door            string
    runtimeInstance containercontract.Runtime
    arguments       []any
}

type boundRecordingTokenStore struct {
    callList []boundTokenStoreRecordedCall
}

func (instance *boundRecordingTokenStore) record(door string, runtimeInstance containercontract.Runtime, arguments ...any) {
    instance.callList = append(instance.callList, boundTokenStoreRecordedCall{door: door, runtimeInstance: runtimeInstance, arguments: arguments})
}

func (instance *boundRecordingTokenStore) Lookup(runtimeInstance containercontract.Runtime, tokenString string) (securitycontract.Claims, bool, error) {
    instance.record("Lookup", runtimeInstance, tokenString)

    return boundTokenStoreLookedUpClaims(), true, boundTokenStoreRecordedErr
}

func (instance *boundRecordingTokenStore) Put(runtimeInstance containercontract.Runtime, tokenString string, claims securitycontract.Claims, ttl bound.Duration) error {
    instance.record("Put", runtimeInstance, tokenString, claims, ttl)

    return boundTokenStoreRecordedErr
}

func (instance *boundRecordingTokenStore) Delete(runtimeInstance containercontract.Runtime, tokenString string) error {
    instance.record("Delete", runtimeInstance, tokenString)

    return boundTokenStoreRecordedErr
}

func (instance *boundRecordingTokenStore) DeleteByUser(runtimeInstance containercontract.Runtime, userIdentifier string) (int, error) {
    instance.record("DeleteByUser", runtimeInstance, userIdentifier)

    return 3, boundTokenStoreRecordedErr
}

func (instance *boundRecordingTokenStore) PurgeExpired(runtimeInstance containercontract.Runtime) (int, error) {
    instance.record("PurgeExpired", runtimeInstance)

    return 5, boundTokenStoreRecordedErr
}

func (instance *boundRecordingTokenStore) RevokeBefore(runtimeInstance containercontract.Runtime, userIdentifier string, deviceIdentifier string, instant time.Time) error {
    instance.record("RevokeBefore", runtimeInstance, userIdentifier, deviceIdentifier, instant)

    return boundTokenStoreRecordedErr
}

func (instance *boundRecordingTokenStore) RevocationEpoch(runtimeInstance containercontract.Runtime, userIdentifier string, deviceIdentifier string) (time.Time, error) {
    instance.record("RevocationEpoch", runtimeInstance, userIdentifier, deviceIdentifier)

    return time.Unix(1700000000, 0).UTC(), boundTokenStoreRecordedErr
}

/* every field set, so a view that drops or rebuilds a part of the claims is seen in either direction */
func boundTokenStoreClaims(prefix string) securitycontract.Claims {
    return securitycontract.Claims{
        UserIdentifier:   prefix + "-user",
        Roles:            []string{prefix + "-role"},
        IssuedAt:         time.Unix(1500000000, 0).UTC(),
        DeviceIdentifier: prefix + "-device",
        Scope:            map[string]any{"scope": prefix},
        Attributes:       map[string]any{"attribute": prefix},
        OriginatingActor: &securitycontract.ActorData{Identifier: prefix + "-actor"},
    }
}

func boundTokenStoreLookedUpClaims() securitycontract.Claims {
    return boundTokenStoreClaims("looked-up")
}

func TestBoundTokenStore_EveryMethodReachesItsDoorWithTheBoundRuntimeAndAnswersWhatItAnswered(t *testing.T) {
    runtimeInstance := testRuntime()
    recordingStore := &boundRecordingTokenStore{}
    boundStore := TokenStoreFor(runtimeInstance, recordingStore)

    ttl := bound.Of(90 * time.Second)
    instant := time.Unix(1600000000, 0).UTC()

    claims, found, lookupErr := boundStore.Lookup("looked-up-token")
    putErr := boundStore.Put("put-token", boundTokenStoreClaims("put"), ttl)
    deleteErr := boundStore.Delete("deleted-token")
    deletedCount, deleteByUserErr := boundStore.DeleteByUser("deleted-user")
    purgedCount, purgeErr := boundStore.PurgeExpired()
    revokeErr := boundStore.RevokeBefore("revoked-user", "revoked-device", instant)
    epoch, epochErr := boundStore.RevocationEpoch("epoch-user", "epoch-device")

    expected := []boundTokenStoreRecordedCall{
        {door: "Lookup", arguments: []any{"looked-up-token"}},
        {door: "Put", arguments: []any{"put-token", boundTokenStoreClaims("put"), ttl}},
        {door: "Delete", arguments: []any{"deleted-token"}},
        {door: "DeleteByUser", arguments: []any{"deleted-user"}},
        {door: "PurgeExpired", arguments: nil},
        {door: "RevokeBefore", arguments: []any{"revoked-user", "revoked-device", instant}},
        {door: "RevocationEpoch", arguments: []any{"epoch-user", "epoch-device"}},
    }

    if len(expected) != len(recordingStore.callList) {
        t.Fatalf("expected %d calls, got %d", len(expected), len(recordingStore.callList))
    }

    for index, expectedCall := range expected {
        recorded := recordingStore.callList[index]
        if expectedCall.door != recorded.door || runtimeInstance != recorded.runtimeInstance || false == reflect.DeepEqual(expectedCall.arguments, recorded.arguments) {
            t.Fatalf("call %d: expected %s %v under the bound runtime, got %s %v", index, expectedCall.door, expectedCall.arguments, recorded.door, recorded.arguments)
        }
    }

    if false == reflect.DeepEqual(boundTokenStoreLookedUpClaims(), claims) || false == found || boundTokenStoreRecordedErr != lookupErr {
        t.Fatalf("expected Lookup to answer the store's claims, found and its error, got %v %v %v", claims, found, lookupErr)
    }

    for doorName, doorErr := range map[string]error{"Put": putErr, "Delete": deleteErr, "RevokeBefore": revokeErr} {
        if boundTokenStoreRecordedErr != doorErr {
            t.Fatalf("%s: expected the store's error, got %v", doorName, doorErr)
        }
    }

    /* a count removed before a failure is answered beside the error */
    if 3 != deletedCount || boundTokenStoreRecordedErr != deleteByUserErr {
        t.Fatalf("expected DeleteByUser to answer 3 beside the store's error, got %d %v", deletedCount, deleteByUserErr)
    }

    if 5 != purgedCount || boundTokenStoreRecordedErr != purgeErr {
        t.Fatalf("expected PurgeExpired to answer 5 beside the store's error, got %d %v", purgedCount, purgeErr)
    }

    if false == time.Unix(1700000000, 0).UTC().Equal(epoch) || boundTokenStoreRecordedErr != epochErr {
        t.Fatalf("expected RevocationEpoch to answer the store's epoch and error, got %v %v", epoch, epochErr)
    }

    if runtimeInstance != boundStore.Runtime() || securitycontract.EpochRevocableTokenStore(recordingStore) != boundStore.TokenStore() {
        t.Fatalf("expected the halves to answer what the view was bound over")
    }
}

func TestBoundTokenStore_AViewOverNoStoreRefusesEveryDoor(t *testing.T) {
    var typedNilStore *InMemoryTokenStore

    for _, boundStore := range []BoundTokenStore{{}, TokenStoreFor(testRuntime(), nil), TokenStoreFor(testRuntime(), typedNilStore)} {
        claims, found, lookupErr := boundStore.Lookup("token")
        putErr := boundStore.Put("token", securitycontract.Claims{UserIdentifier: "user"}, bound.Of(time.Minute))
        deleteErr := boundStore.Delete("token")
        deletedCount, deleteByUserErr := boundStore.DeleteByUser("user")
        purgedCount, purgeErr := boundStore.PurgeExpired()
        revokeErr := boundStore.RevokeBefore("user", "device", time.Now())
        epoch, epochErr := boundStore.RevocationEpoch("user", "device")

        for doorName, doorErr := range map[string]error{"Lookup": lookupErr, "Put": putErr, "Delete": deleteErr, "DeleteByUser": deleteByUserErr, "PurgeExpired": purgeErr, "RevokeBefore": revokeErr, "RevocationEpoch": epochErr} {
            if nil == doorErr || "token store instance is nil" != doorErr.Error() {
                t.Fatalf("%s: expected the absent store refused, got %v", doorName, doorErr)
            }
        }

        if "" != claims.UserIdentifier || true == found || 0 != deletedCount || 0 != purgedCount || false == epoch.IsZero() {
            t.Fatalf("expected the refused doors to answer zero values, got %v %v %d %d %v", claims, found, deletedCount, purgedCount, epoch)
        }
    }
}

/* a package variable, so the direct call is made through the contract as the view makes it, never devirtualized */
var boundAllocationTokenStore securitycontract.EpochRevocableTokenStore

func TestBoundTokenStore_BindingAddsNoAllocationToTheContractsCall(t *testing.T) {
    runtimeInstance := testRuntime()
    boundAllocationTokenStore = &boundCountingTokenStore{}

    directAllocations := testing.AllocsPerRun(1000, func() {
        _, _, _ = boundAllocationTokenStore.Lookup(runtimeInstance, "token")
    })
    boundAllocations := testing.AllocsPerRun(1000, func() {
        _, _, _ = TokenStoreFor(runtimeInstance, boundAllocationTokenStore).Lookup("token")
    })
    if directAllocations != boundAllocations {
        t.Fatalf("expected a bind and a call to allocate what the direct call does, %v, got %v", directAllocations, boundAllocations)
    }
}

type boundCountingTokenStore struct {
    boundRecordingTokenStore
}

func (instance *boundCountingTokenStore) Lookup(runtimeInstance containercontract.Runtime, tokenString string) (securitycontract.Claims, bool, error) {
    return securitycontract.Claims{}, false, nil
}

func TestTokenStoreFromRuntime_BindsTheStoreRegisteredUnderTheApplicationsName(t *testing.T) {
    firstStore := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    secondStore := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    /* the narrower registration a redis module makes, resolved as the composed contract */
    if registerErr := serviceContainer.Register(
        "app.first.token_store",
        func(resolver containercontract.Resolver) (securitycontract.RevocableTokenStore, error) {
            return firstStore, nil
        },
        container.RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.second.token_store",
        func(resolver containercontract.Resolver) (*InMemoryTokenStore, error) {
            return secondStore, nil
        },
        container.RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    firstBound := TokenStoreFromRuntime(runtimeInstance, "app.first.token_store")
    if securitycontract.EpochRevocableTokenStore(firstStore) != firstBound.TokenStore() || runtimeInstance != firstBound.Runtime() {
        t.Fatalf("expected the first registered store bound to the runtime")
    }

    secondBound := TokenStoreFromRuntime(runtimeInstance, "app.second.token_store")
    if securitycontract.EpochRevocableTokenStore(secondStore) != secondBound.TokenStore() {
        t.Fatalf("expected each name to bind its own store")
    }

    panicked := func() (recovered bool) {
        defer func() {
            recovered = nil != recover()
        }()

        TokenStoreFromRuntime(runtimeInstance, "app.missing.token_store")

        return false
    }()
    if false == panicked {
        t.Fatalf("expected a name nothing registered to panic")
    }

    if registerErr := serviceContainer.Register(
        "app.revocable_only.token_store",
        func(resolver containercontract.Resolver) (boundRevocableOnlyTokenStore, error) {
            return boundRevocableOnlyTokenStore{}, nil
        },
        container.RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }

    /* a store without the epoch half is called through its contract; the view refuses it at the bind */
    revocableOnlyPanicked := func() (recovered bool) {
        defer func() {
            recovered = nil != recover()
        }()

        TokenStoreFromRuntime(runtime.NewRun(context.Background(), serviceContainer.NewScope()), "app.revocable_only.token_store")

        return false
    }()
    if false == revocableOnlyPanicked {
        t.Fatalf("expected a store without the revocation epochs refused at the bind")
    }
}

type boundRevocableOnlyTokenStore struct{}

func (instance boundRevocableOnlyTokenStore) Lookup(_ containercontract.Runtime, _ string) (securitycontract.Claims, bool, error) {
    return securitycontract.Claims{}, false, nil
}

func (instance boundRevocableOnlyTokenStore) Put(_ containercontract.Runtime, _ string, _ securitycontract.Claims, _ bound.Duration) error {
    return nil
}

func (instance boundRevocableOnlyTokenStore) Delete(_ containercontract.Runtime, _ string) error {
    return nil
}

func (instance boundRevocableOnlyTokenStore) DeleteByUser(_ containercontract.Runtime, _ string) (int, error) {
    return 0, nil
}

func (instance boundRevocableOnlyTokenStore) PurgeExpired(_ containercontract.Runtime) (int, error) {
    return 0, nil
}

func TestBoundTokenStore_OverTheInMemoryStore_ADetachedBindOutlivesTheRequest(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    requestContext, cancel := context.WithCancel(context.Background())
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    requestRuntime := runtime.NewRequest(requestContext, serviceContainer.NewScope())

    if putErr := TokenStoreFor(requestRuntime, store).Put("minted", securitycontract.Claims{UserIdentifier: "user"}, bound.Of(time.Minute)); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    cancel()

    if deleteErr := TokenStoreFor(requestRuntime, store).Delete("minted"); nil == deleteErr || false == errors.Is(deleteErr, context.Canceled) {
        t.Fatalf("expected a view bound to the cancelled request refused with its context's error, got %v", deleteErr)
    }

    detachedRuntime := runtime.WithContext(requestRuntime, context.WithoutCancel(requestContext))
    if deleteErr := TokenStoreFor(detachedRuntime, store).Delete("minted"); nil != deleteErr {
        t.Fatalf("expected a view bound again over the detached runtime to withdraw the token, got %v", deleteErr)
    }

    if _, found, lookupErr := TokenStoreFor(detachedRuntime, store).Lookup("minted"); true == found || nil != lookupErr {
        t.Fatalf("expected the token withdrawn, got found %v, %v", found, lookupErr)
    }
}
