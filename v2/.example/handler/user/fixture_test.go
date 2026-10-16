package user

import (
    "bytes"
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v2/.example/cache"
    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/repository"
    "github.com/precision-soft/melody/v2/.example/service"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyevent "github.com/precision-soft/melody/v2/event"
    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodylogging "github.com/precision-soft/melody/v2/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodysecurity "github.com/precision-soft/melody/v2/security"
)

/* userDoorFixture carries the user service over the seeded in-memory directory (user-1 user, user-2 editor, user-3 admin), so a door is pinned on what it stored */
type userDoorFixture struct {
    /* bodyLimit bounds the request body as the kernel's MELODY_HTTP_MAX_REQUEST_BODY_BYTES does; zero leaves it unbounded */
    bodyLimit int64
    container      melodycontainercontract.Container
    userRepository repository.UserRepository
}

func newUserDoorFixture(t *testing.T) *userDoorFixture {
    t.Helper()

    clockInstance := melodyclock.NewSystemClock()
    cacheInstance := melodycache.NewManagerOwningBackend(melodycache.NewInMemoryBackend(128, time.Minute, clockInstance), examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = cacheInstance.Close() })

    userRepository := repository.NewInMemoryUserRepository()
    userService := service.NewUserService(userRepository, cacheInstance, melodyevent.NewEventDispatcher(clockInstance))

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    /* the dispatcher resolves the logger for every dispatch */
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    melodycontainer.MustRegister(
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return userService, nil
        },
    )

    return &userDoorFixture{container: containerInstance, userRepository: userRepository}
}

/* call runs a door as the seeded administrator with the body and the route parameters given and answers the status and the body */
func (instance *userDoorFixture) call(t *testing.T, handler melodyhttpcontract.Handler, method string, body string, params map[string]string) (int, string) {
    t.Helper()

    runtimeInstance := melodyruntime.New(context.Background(), instance.container.NewScope(), instance.container)

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
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("user-3", []string{entity.RoleUser, entity.RoleEditor, entity.RoleAdmin})),
    )

    httpRequest := httptest.NewRequest(method, "/users/api/", bytes.NewBufferString(body))
    httpRequest.Header.Set("Content-Type", "application/json")
    httpRequest.Header.Set("Accept", "application/json")

    recorder := httptest.NewRecorder()
    if 0 < instance.bodyLimit {
        httpRequest.Body = nethttp.MaxBytesReader(recorder, httpRequest.Body, instance.bodyLimit)
    }

    request := melodyhttp.NewRequest(httpRequest, params, runtimeInstance, melodyhttp.NewRequestContext("user-door-test", time.Now()))

    response, handlerErr := handler(runtimeInstance, recorder, request)
    if nil != handlerErr {
        t.Fatalf("the door failed: %v", handlerErr)
    }

    if nil == response {
        t.Fatalf("expected a response")
    }

    reader := response.BodyReader()
    if nil == reader {
        return response.StatusCode(), ""
    }

    buffer := &bytes.Buffer{}
    if _, copyErr := buffer.ReadFrom(reader); nil != copyErr {
        t.Fatalf("read body: %v", copyErr)
    }

    return response.StatusCode(), buffer.String()
}

func (instance *userDoorFixture) stored(t *testing.T, id string) *entity.User {
    t.Helper()

    user, found, findErr := instance.userRepository.FindById(context.Background(), id)
    if nil != findErr || false == found {
        t.Fatalf("the directory no longer holds %q (err %v)", id, findErr)
    }

    return user
}

func (instance *userDoorFixture) holds(t *testing.T, username string) bool {
    t.Helper()

    _, found, findErr := instance.userRepository.FindByUsername(context.Background(), username)
    if nil != findErr {
        t.Fatalf("find %q: %v", username, findErr)
    }

    return found
}
