package user

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    melodycache "github.com/precision-soft/melody/v4/cache"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    "sort"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/service"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

type recordingUserRepository struct {
    mutex        sync.Mutex
    users        map[string]*entity.User
    writeRefusal error
}

func newRecordingUserRepository(users ...*entity.User) *recordingUserRepository {
    stored := map[string]*entity.User{}
    for _, user := range users {
        copied := *user
        stored[user.Id] = &copied
    }

    return &recordingUserRepository{users: stored}
}

func (instance *recordingUserRepository) All(ctx context.Context) ([]*entity.User, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    identifierList := make([]string, 0, len(instance.users))
    for id := range instance.users {
        identifierList = append(identifierList, id)
    }
    sort.Strings(identifierList)

    result := make([]*entity.User, 0, len(identifierList))
    for _, id := range identifierList {
        copied := *instance.users[id]
        result = append(result, &copied)
    }

    return result, nil
}

func (instance *recordingUserRepository) FindById(ctx context.Context, id string) (*entity.User, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    user, exists := instance.users[id]
    if false == exists {
        return nil, false, nil
    }

    copied := *user

    return &copied, true, nil
}

func (instance *recordingUserRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, user := range instance.users {
        if username == user.Username {
            copied := *user

            return &copied, true, nil
        }
    }

    return nil, false, nil
}

func (instance *recordingUserRepository) refuseWrites(writeErr error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.writeRefusal = writeErr
}

func (instance *recordingUserRepository) Create(ctx context.Context, user *entity.User) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.writeRefusal {
        return instance.writeRefusal
    }

    if nil == user {
        return errors.New("the user is nil")
    }

    copied := *user
    instance.users[user.Id] = &copied

    return nil
}

func (instance *recordingUserRepository) Update(ctx context.Context, id string, change repository.UserChange, guard repository.UserGuard) (*entity.User, *entity.User, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.writeRefusal {
        return nil, nil, instance.writeRefusal
    }

    stored, exists := instance.users[id]
    if false == exists {
        return nil, nil, nil
    }

    if nil != guard {
        if guardErr := guard(stored); nil != guardErr {
            return nil, nil, guardErr
        }
    }

    before := *stored
    changed := *stored
    if nil != change.Username {
        changed.Username = *change.Username
    }
    if nil != change.PasswordHash {
        changed.Password = *change.PasswordHash
    }
    if nil != change.Roles {
        changed.Roles = append([]string{}, change.Roles...)
    }
    instance.users[id] = &changed

    after := changed

    return &before, &after, nil
}

func (instance *recordingUserRepository) GrantRole(ctx context.Context, id string, role string) (*entity.User, repository.GrantRoleOutcome, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    user, exists := instance.users[id]
    if false == exists {
        return nil, repository.GrantRoleAccountAbsent, nil
    }

    for _, held := range user.Roles {
        if role == held {
            return user, repository.GrantRoleAlreadyHeld, nil
        }
    }

    granted := *user
    granted.Roles = append(append([]string{}, user.Roles...), role)
    instance.users[id] = &granted

    return &granted, repository.GrantRoleGranted, nil
}

func (instance *recordingUserRepository) DeleteById(ctx context.Context, id string, guard repository.UserGuard) (*entity.User, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    stored, exists := instance.users[id]
    if false == exists {
        return nil, nil
    }

    if nil != guard {
        if guardErr := guard(stored); nil != guardErr {
            return nil, guardErr
        }
    }

    delete(instance.users, id)

    return stored, nil
}

func (instance *recordingUserRepository) storedRoles(t *testing.T, id string) []string {
    t.Helper()

    user, exists, err := instance.FindById(context.Background(), id)
    if nil != err {
        t.Fatalf("the repository refused the read: %v", err)
    }

    if false == exists {
        t.Fatalf("the repository no longer holds %q", id)
    }

    return user.Roles
}

var _ repository.UserRepository = (*recordingUserRepository)(nil)

type valueCache struct {
    mutex  sync.Mutex
    values map[string]any
}

func (instance *valueCache) Get(runtimeInstance melodycontainercontract.Runtime, key string, target any) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    value, exists := instance.values[key]
    if false == exists {
        return false, nil
    }

    if assignErr := assignCachedTestValue(key, target, value); nil != assignErr {
        return false, assignErr
    }

    return true, nil
}

func (instance *valueCache) Set(runtimeInstance melodycontainercontract.Runtime, key string, value any, ttl melodybound.Duration, _ melodycachecontract.SetOptions) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values[key] = value

    return nil
}

func (instance *valueCache) Delete(runtimeInstance melodycontainercontract.Runtime, key string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.values, key)

    return nil
}

func (instance *valueCache) Has(runtimeInstance melodycontainercontract.Runtime, key string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    _, exists := instance.values[key]

    return exists, nil
}

func (instance *valueCache) Clear(runtimeInstance melodycontainercontract.Runtime) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values = map[string]any{}

    return nil
}

func (instance *valueCache) Many(runtimeInstance melodycontainercontract.Runtime, keys []string, target any) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return assignCachedTestValues(keys, target, instance.values)
}

/* this fake holds values, not payloads, so its copy is the value itself */
func (instance *valueCache) RoundTrip(value any, target any) error {
    return assignCachedTestValue("", target, value)
}

func (instance *valueCache) SetMultiple(runtimeInstance melodycontainercontract.Runtime, items map[string]any, ttl melodybound.Duration, _ melodycachecontract.SetOptions) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for key, value := range items {
        instance.values[key] = value
    }

    return nil
}

func (instance *valueCache) DeleteMultiple(runtimeInstance melodycontainercontract.Runtime, keys []string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, key := range keys {
        delete(instance.values, key)
    }

    return nil
}

func (instance *valueCache) Increment(runtimeInstance melodycontainercontract.Runtime, key string, delta int64, options melodycachecontract.CounterOptions) (int64, error) {
    return 0, nil
}

func (instance *valueCache) GetCounter(runtimeInstance melodycontainercontract.Runtime, key string) (int64, bool, error) {
    return 0, false, nil
}

func (instance *valueCache) Decrement(runtimeInstance melodycontainercontract.Runtime, key string, delta int64, options melodycachecontract.CounterOptions) (int64, error) {
    return 0, nil
}

func (instance *valueCache) CloseWithContext(closeContext context.Context) error {
    return nil
}

var _ melodycachecontract.Cache = (*valueCache)(nil)

func assignCachedTestValue(key string, target any, value any) error {
    targetValue := reflect.ValueOf(target)
    if reflect.Pointer != targetValue.Kind() || true == targetValue.IsNil() {
        return fmt.Errorf("cache read target %T is not a non-nil pointer", target)
    }

    /* a nil pointer is a remembered absence, which gob reads back as the zero value */
    valueOf := reflect.ValueOf(value)
    if nil == value || (reflect.Pointer == valueOf.Kind() && true == valueOf.IsNil()) {
        targetValue.Elem().SetZero()

        return nil
    }

    if false == valueOf.Type().AssignableTo(targetValue.Elem().Type()) {
        return melodycache.NewDeserializationError([]string{key}, fmt.Errorf("the cached %T cannot be held by a %s target", value, targetValue.Elem().Type()))
    }

    targetValue.Elem().Set(valueOf)

    return nil
}

func assignCachedTestValues(keys []string, target any, values map[string]any) error {
    targetValue := reflect.ValueOf(target)
    if reflect.Pointer != targetValue.Kind() || true == targetValue.IsNil() || reflect.Map != targetValue.Elem().Kind() {
        return fmt.Errorf("cache batch read target %T is not a pointer to a map", target)
    }

    mapType := targetValue.Elem().Type()
    result := reflect.MakeMap(mapType)
    corruptKeys := make([]string, 0)
    var firstCauseErr error
    for _, key := range keys {
        value, exists := values[key]
        if false == exists {
            continue
        }

        element := reflect.New(mapType.Elem())
        if assignErr := assignCachedTestValue(key, element.Interface(), value); nil != assignErr {
            corruptKeys = append(corruptKeys, key)
            if nil == firstCauseErr {
                firstCauseErr = assignErr
            }
            continue
        }

        result.SetMapIndex(reflect.ValueOf(key), element.Elem())
    }

    targetValue.Elem().Set(result)

    if 0 < len(corruptKeys) {
        return melodycache.NewDeserializationError(corruptKeys, firstCauseErr)
    }

    return nil
}

type silentEventDispatcher struct{}

func (instance *silentEventDispatcher) AddListener(eventName string, listener melodyeventcontract.EventListener, config melodyeventcontract.ListenerConfig) melodyeventcontract.ListenerRegistration {
    return melodyeventcontract.ListenerRegistration{}
}

func (instance *silentEventDispatcher) InstallFloorListener(eventName string, listener melodyeventcontract.EventListener, config melodyeventcontract.ListenerConfig) (melodyeventcontract.ListenerRegistration, bool) {
    return melodyeventcontract.ListenerRegistration{}, false
}

func (instance *silentEventDispatcher) RegisteredEvents() []melodyeventcontract.RegisteredEvent {
    return nil
}

func (instance *silentEventDispatcher) RemoveListener(registration melodyeventcontract.ListenerRegistration) bool {
    return false
}

func (instance *silentEventDispatcher) DeclareEvent(eventName string, config melodyeventcontract.EventConfig) {
}

func (instance *silentEventDispatcher) AddSubscriber(subscriber melodyeventcontract.EventSubscriber) melodyeventcontract.SubscriberRegistration {
    return melodyeventcontract.SubscriberRegistration{}
}

func (instance *silentEventDispatcher) RemoveSubscriber(registration melodyeventcontract.SubscriberRegistration) int {
    return 0
}

func (instance *silentEventDispatcher) Dispatch(runtimeInstance melodycontainercontract.Runtime, event melodyeventcontract.Event) (melodyeventcontract.Event, error) {
    return event, nil
}

func (instance *silentEventDispatcher) DispatchName(runtimeInstance melodycontainercontract.Runtime, eventName string, payload any) (melodyeventcontract.Event, error) {
    return nil, nil
}

var _ melodyeventcontract.EventDispatcher = (*silentEventDispatcher)(nil)

func adminRuntime(t *testing.T, userRepository repository.UserRepository, actorId string, actorRoles []string) melodycontainercontract.Runtime {
    t.Helper()

    return adminRuntimeOverCache(t, userRepository, &valueCache{values: map[string]any{}}, actorId, actorRoles)
}

func adminRuntimeOverCache(t *testing.T, userRepository repository.UserRepository, cacheInstance *valueCache, actorId string, actorRoles []string) melodycontainercontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    registerErr := melodycontainer.Register[*service.UserService](
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return service.NewUserService(userRepository, cacheInstance, &silentEventDispatcher{}), nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register the user service: %v", registerErr)
    }

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
        MatcherDescription: "prefix /",
    })

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken(actorId, actorRoles, nil)),
    )

    return runtimeInstance
}

func callDoor(
    t *testing.T,
    runtimeInstance melodycontainercontract.Runtime,
    door melodyhttpcontract.Handler,
    method string,
    path string,
    routeParameters map[string]string,
    body string,
) (int, string) {
    t.Helper()

    return callDoorBoundedTo(t, runtimeInstance, door, method, path, routeParameters, body, 0)
}

func callDoorBoundedTo(
    t *testing.T,
    runtimeInstance melodycontainercontract.Runtime,
    door melodyhttpcontract.Handler,
    method string,
    path string,
    routeParameters map[string]string,
    body string,
    bodyLimit int64,
) (int, string) {
    t.Helper()

    recorder := httptest.NewRecorder()
    httpRequest := httptest.NewRequest(method, path, bytes.NewBufferString(body))
    httpRequest.Header.Set("Content-Type", "application/json")
    if 0 < bodyLimit {
        httpRequest.Body = nethttp.MaxBytesReader(recorder, httpRequest.Body, bodyLimit)
    }

    request := melodyhttp.NewRequest(
        httpRequest,
        runtimeInstance,
        melodyhttp.RequestConfig{
            RouteParameters: routeParameters,
            RequestContext:  melodyhttp.NewRequestContext("user-door-test", time.Now()),
        },
    )

    response, handlerErr := door(runtimeInstance, recorder, request)
    if nil != handlerErr {
        t.Fatalf("the door failed: %v", handlerErr)
    }

    if nil == response {
        t.Fatal("the door answered no response")
    }

    payload, readErr := readAll(response)
    if nil != readErr {
        t.Fatalf("read the response body: %v", readErr)
    }

    return response.StatusCode(), payload
}

func readAll(response melodyhttpcontract.Response) (string, error) {
    reader := response.BodyReader()
    if nil == reader {
        return "", nil
    }

    buffer := &bytes.Buffer{}
    if _, err := buffer.ReadFrom(reader); nil != err {
        return "", err
    }

    return buffer.String(), nil
}

func administrator(id string) *entity.User {
    return &entity.User{Id: id, Username: id, Password: "hash", Roles: []string{entity.RoleUser, entity.RoleAdmin}}
}

func editor(id string) *entity.User {
    return &entity.User{Id: id, Username: id, Password: "hash", Roles: []string{entity.RoleUser, entity.RoleEditor}}
}

func (instance *recordingUserRepository) stored(t *testing.T, id string) *entity.User {
    t.Helper()

    user, exists, err := instance.FindById(context.Background(), id)
    if nil != err || false == exists {
        t.Fatalf("the repository no longer holds %q (err %v)", id, err)
    }

    return user
}

type canceledRuntime struct {
    melodycontainercontract.Runtime
    ctx context.Context
}

func newCanceledRuntime(runtimeInstance melodycontainercontract.Runtime) melodycontainercontract.Runtime {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    return &canceledRuntime{Runtime: runtimeInstance, ctx: ctx}
}

func (instance *canceledRuntime) Context() context.Context {
    return instance.ctx
}

type slowUserRepository struct {
    *recordingUserRepository
}

func waitForTheCallerContext(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(5 * time.Second):
        return nil
    }
}

func (instance *slowUserRepository) All(ctx context.Context) ([]*entity.User, error) {
    return nil, waitForTheCallerContext(ctx)
}

func (instance *slowUserRepository) FindById(ctx context.Context, id string) (*entity.User, bool, error) {
    return nil, false, waitForTheCallerContext(ctx)
}

func (instance *slowUserRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    return nil, false, waitForTheCallerContext(ctx)
}
