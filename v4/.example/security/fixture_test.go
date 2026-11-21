package security

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodyhttpmiddleware "github.com/precision-soft/melody/v4/http/middleware"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
    melodysessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

func plainRequest(t *testing.T) melodyhttpcontract.Request {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    return melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/products/", nil), nil, runtimeInstance, nil)
}

func requestCarryingSession(t *testing.T) (melodyhttpcontract.Request, melodysessioncontract.Session) {
    t.Helper()

    request := plainRequest(t)

    manager := melodysession.NewManager(melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{}), melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)})
    sessionInstance := mustSession(manager.NewSession(exampleTestRuntime()))

    _ = melodyhttp.SetRequestSession(request, sessionInstance)

    return request, sessionInstance
}

func sessionOnRequest(request melodyhttpcontract.Request) melodysessioncontract.Session {
    sessionInstance, sessionErr := melodyhttp.RequestSession(request)
    if nil != sessionErr {
        return nil
    }

    return sessionInstance
}

const testPasswordHash = "test-password-hash"

func currentAccountLookup(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
    roles := []string{"ROLE_USER", "ROLE_EDITOR"}
    if "user-3" == userId {
        roles = []string{"ROLE_USER", "ROLE_ADMIN"}
    }

    return entity.NewUser(userId, "test-user", testPasswordHash, roles), true, nil
}

var errAccountRepositoryUnavailable = errors.New("the account repository is unavailable")

func refusingAccountLookup(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
    return nil, false, errAccountRepositoryUnavailable
}

func withCurrentCredential(sessionInstance melodysessioncontract.Session) {
    sessionInstance.Set(SessionKeySecurityCredentialVersion, SessionCredentialVersion(testPasswordHash))
}

func passwordRequest(t *testing.T, username string, password string) melodyhttpcontract.Request {
    t.Helper()

    request := plainRequest(t)
    if "" != username {
        request.Attributes().Set(RequestAttributeLoginUsername, username)
    }
    if "" != password {
        request.Attributes().Set(RequestAttributeLoginPassword, password)
    }

    return request
}

const budgetTestAllowance = 2

func budgetOverPassword() *SecondFactorBudget {
    return budgetOverPasswordWithLimiter(melodyhttpmiddleware.NewSlidingWindowLimiter("test", budgetTestAllowance, time.Minute, melodyhttpmiddleware.LimiterConfig{Clock: melodyclock.NewFrozenClock(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))}))
}

func budgetOverPasswordWithLimiter(limiter melodyhttpcontract.RateLimiter) *SecondFactorBudget {
    password := NewPasswordAuthenticator(func(runtimeInstance melodycontainercontract.Runtime, username string, password string) (*entity.User, bool, error) {
        if "secret" != password {
            return nil, false, nil
        }

        return entity.NewUser("user-"+username, username, testPasswordHash, []string{"ROLE_EDITOR"}), true, nil
    })

    return NewSecondFactorBudget(password, limiter, melodysecurity.DefaultTotpCodeHeaderName, melodysecurity.DefaultTotpRecoveryHeaderName)
}

func budgetRequestWithHeader(t *testing.T, password string, headerName string) melodyhttpcontract.Request {
    t.Helper()

    request := passwordRequest(t, "editor", password)
    if "" != headerName {
        request.HttpRequest().Header.Set(headerName, "123456")
    }

    return request
}

type sessionAdmission struct {
    userId            string
    previousSessionId string
    sessionId         string
    createdAt         time.Time
}

type recordingSessionIndex struct {
    admitErr     error
    admittedList []sessionAdmission
    releasedList []string
}

func (instance *recordingSessionIndex) Admit(ctx context.Context, userId string, previousSessionId string, sessionId string, createdAt time.Time, sessionLive func(sessionId string) (bool, error), release func(sessionId string) error) error {
    if nil != instance.admitErr {
        return instance.admitErr
    }

    instance.admittedList = append(instance.admittedList, sessionAdmission{userId: userId, previousSessionId: previousSessionId, sessionId: sessionId, createdAt: createdAt})

    return nil
}

func (instance *recordingSessionIndex) Release(ctx context.Context, sessionId string) error {
    instance.releasedList = append(instance.releasedList, sessionId)

    return nil
}

func (instance *recordingSessionIndex) lookup(request melodyhttpcontract.Request) (SessionIndex, error) {
    return instance, nil
}

var sessionAdmissionTestInstant = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func registerAdmissionClock(t *testing.T, containerInstance melodycontainercontract.Container) {
    t.Helper()

    registerErr := melodycontainer.Register[melodyclockcontract.Clock](
        containerInstance,
        melodyclock.ServiceClock,
        func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
            return melodyclock.NewFrozenClock(sessionAdmissionTestInstant), nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register clock: %v", registerErr)
    }
}

func mustSession(sessionInstance melodysessioncontract.Session, sessionErr error) melodysessioncontract.Session {
    if nil != sessionErr {
        panic(sessionErr)
    }

    return sessionInstance
}

/* withItsRuntime hands a door the request together with the runtime the request carries */
func withItsRuntime(request melodyhttpcontract.Request) (melodycontainercontract.Runtime, melodyhttpcontract.Request) {
    if nil == request {
        return nil, nil
    }

    return request.RuntimeInstance(), request
}

/* resolveToken answers the resolver's token; the example's resolver answers its outages through the unavailable token, so an error is a defect of the test */
func resolveToken(resolver melodysecuritycontract.TokenResolver, request melodyhttpcontract.Request) melodysecuritycontract.Token {
    token, resolveErr := resolver(withItsRuntime(request))
    if nil != resolveErr {
        panic(resolveErr)
    }

    return token
}

func exampleTestRuntime() melodycontainercontract.Runtime {
    return exampleTestRuntimeUnder(context.Background())
}

func exampleTestRuntimeUnder(ctx context.Context) melodycontainercontract.Runtime {
    return melodyruntime.NewRun(ctx, melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
}
