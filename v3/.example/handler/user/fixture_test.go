package user

import (
    "bytes"
    "context"
    "errors"
    "net/http/httptest"
    "sort"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysession "github.com/precision-soft/melody/v3/session"
    melodysessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

/* The shared test material of the package lives here, and only here: this is the one test file the layout rule exempts from having a source of its own. */

/* recordingUserRepository is the directory the admin doors write into, kept in this process. The doors under test are asked what they STORE, so a double that only answers reads would let the test agree with itself about what an update wrote. */
type recordingUserRepository struct {
    mutex sync.Mutex
    users map[string]*entity.User
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

func (instance *recordingUserRepository) Create(ctx context.Context, user *entity.User) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == user {
        return errors.New("the user is nil")
    }

    copied := *user
    instance.users[user.Id] = &copied

    return nil
}

func (instance *recordingUserRepository) Update(ctx context.Context, user *entity.User) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if _, exists := instance.users[user.Id]; false == exists {
        return false, nil
    }

    copied := *user
    instance.users[user.Id] = &copied

    return true, nil
}

func (instance *recordingUserRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if _, exists := instance.users[id]; false == exists {
        return false, nil
    }

    delete(instance.users, id)

    return true, nil
}

/* storedRoles reads the roles the repository holds, which is the only reading that answers what an update actually wrote. */
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

/* valueCache keeps the values it is given, as they are. The doors under test are not about serialization, and a cache that round-tripped through json would answer a map where the service asserts an entity — a failure of the double rather than of the door. */
type valueCache struct {
    mutex  sync.Mutex
    values map[string]any
}

func (instance *valueCache) Get(key string) (any, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    value, exists := instance.values[key]

    return value, exists, nil
}

func (instance *valueCache) Set(key string, value any, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values[key] = value

    return nil
}

func (instance *valueCache) Delete(key string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.values, key)

    return nil
}

func (instance *valueCache) Has(key string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    _, exists := instance.values[key]

    return exists, nil
}

func (instance *valueCache) Clear() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.values = map[string]any{}

    return nil
}

func (instance *valueCache) Many(keys []string) (map[string]any, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    result := map[string]any{}
    for _, key := range keys {
        if value, exists := instance.values[key]; true == exists {
            result[key] = value
        }
    }

    return result, nil
}

func (instance *valueCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for key, value := range items {
        instance.values[key] = value
    }

    return nil
}

func (instance *valueCache) DeleteMultiple(keys []string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, key := range keys {
        delete(instance.values, key)
    }

    return nil
}

func (instance *valueCache) Increment(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *valueCache) Decrement(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *valueCache) Close() error {
    return nil
}

var _ melodycachecontract.Cache = (*valueCache)(nil)

/* silentEventDispatcher stands in for the wired dispatcher: the doors under test dispatch a domain event on the way out, and what the listeners then do belongs to their own tests. */
type silentEventDispatcher struct{}

func (instance *silentEventDispatcher) AddListener(eventName string, listener melodyeventcontract.EventListener, priority int) melodyeventcontract.ListenerRegistration {
    return melodyeventcontract.ListenerRegistration{}
}

func (instance *silentEventDispatcher) RemoveListener(registration melodyeventcontract.ListenerRegistration) bool {
    return false
}

func (instance *silentEventDispatcher) AddSubscriber(subscriber melodyeventcontract.EventSubscriber) melodyeventcontract.SubscriberRegistration {
    return melodyeventcontract.SubscriberRegistration{}
}

func (instance *silentEventDispatcher) RemoveSubscriber(registration melodyeventcontract.SubscriberRegistration) int {
    return 0
}

func (instance *silentEventDispatcher) Dispatch(runtimeInstance melodyruntimecontract.Runtime, event melodyeventcontract.Event) (melodyeventcontract.Event, error) {
    return event, nil
}

func (instance *silentEventDispatcher) DispatchName(runtimeInstance melodyruntimecontract.Runtime, eventName string, payload any) (melodyeventcontract.Event, error) {
    return nil, nil
}

var _ melodyeventcontract.EventDispatcher = (*silentEventDispatcher)(nil)

/* adminRuntime builds the runtime an admin door needs: a container carrying the user service, and a security context whose token holds the roles the caller is to be granted. The firewall under the context is the smallest compiled one that answers a role question, because the doors read nothing else off it. */
func adminRuntime(t *testing.T, userRepository repository.UserRepository, actorId string, actorRoles []string) melodyruntimecontract.Runtime {
    t.Helper()

    cacheInstance := &valueCache{values: map[string]any{}}

    containerInstance := melodycontainer.NewContainer()

    registerErr := melodycontainer.Register[*service.UserService](
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return service.NewUserService(userRepository, cacheInstance, &silentEventDispatcher{}), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register the user service: %v", registerErr)
    }

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    firewall := melodysecurity.NewCompiledFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        "prefix /",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "",
        "",
        nil,
        nil,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
    )

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken(actorId, actorRoles)),
    )

    return runtimeInstance
}

/* callDoor drives one admin door with a json body and the route parameters it reads, and answers the status it decided on. */
func callDoor(
    t *testing.T,
    runtimeInstance melodyruntimecontract.Runtime,
    door melodyhttpcontract.Handler,
    method string,
    path string,
    routeParams map[string]string,
    body string,
) (int, string) {
    t.Helper()

    httpRequest := httptest.NewRequest(method, path, bytes.NewBufferString(body))
    httpRequest.Header.Set("Content-Type", "application/json")

    request := melodyhttp.NewRequest(
        httpRequest,
        routeParams,
        runtimeInstance,
        melodyhttp.NewRequestContext("user-door-test", time.Now()),
    )

    response, handlerErr := door(runtimeInstance, httptest.NewRecorder(), request)
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

/* sessionCarrying hands back a real session holding the values named. A real one is used rather than a double: the helpers under test read through the typed getters, and a double would let the test agree with itself about what a session stores. */
func sessionCarrying(t *testing.T, values map[string]any) melodysessioncontract.Session {
    t.Helper()

    sessionInstance := melodysession.NewManager(melodysession.NewInMemoryStorage(), time.Hour).NewSession()
    for key, value := range values {
        sessionInstance.Set(key, value)
    }

    return sessionInstance
}

func administrator(id string) *entity.User {
    return &entity.User{Id: id, Username: id, Password: "hash", Roles: []string{entity.RoleUser, entity.RoleAdmin}}
}

func editor(id string) *entity.User {
    return &entity.User{Id: id, Username: id, Password: "hash", Roles: []string{entity.RoleUser, entity.RoleEditor}}
}
