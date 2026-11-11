package http

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "sync/atomic"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

func TestRegenerateRequestSession_RotatesAndRepublishesInOneCall(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage(session.InMemoryStorageConfig{}))
    sessionManager := session.SessionMustFromContainer(t.Context(), serviceContainer)

    preRotationId := ""
    rotatedId := ""

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/login",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance.Set("userId", "anonymous")

            saveErr := sessionManager.SaveSession(sessionInstance)
            if nil != saveErr {
                return nil, saveErr
            }

            preRotationId = sessionInstance.Id()

            rotated, rotateErr := RegenerateRequestSession(request)
            if nil != rotateErr {
                return nil, rotateErr
            }

            rotated.Set("userId", "u-1")

            rotatedId = rotated.Id()

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/login", nil))

    if preRotationId == rotatedId {
        t.Fatalf("expected the rotation to change the session id")
    }

    cookies := recorder.Result().Cookies()
    if 1 != len(cookies) {
        t.Fatalf("expected one session cookie, got %d", len(cookies))
    }

    if rotatedId != cookies[0].Value {
        t.Fatalf("expected the rotated session id %q in the cookie, got %q", rotatedId, cookies[0].Value)
    }

    if 0 != cookies[0].MaxAge {
        t.Fatalf("expected a live session cookie, got MaxAge %d", cookies[0].MaxAge)
    }

    if nil != mustSession(sessionManager.Session(preRotationId)) {
        t.Fatalf("expected the pre-rotation session to be gone from storage")
    }

    reloaded := mustSession(sessionManager.Session(rotatedId))
    if nil == reloaded {
        t.Fatalf("expected the rotated session to be stored")
    }

    if "u-1" != reloaded.String("userId") {
        t.Fatalf("expected the identity written after the rotation to be stored, got %q", reloaded.String("userId"))
    }
}

func TestRegenerateRequestSession_ReturnsErrorWhenTheRequestIsNil(t *testing.T) {
    rotated, err := RegenerateRequestSession(nil)
    if nil == err {
        t.Fatalf("expected error")
    }

    if nil != rotated {
        t.Fatalf("expected no session when the rotation failed")
    }
}

func TestRegenerateRequestSession_ReturnsErrorWhenTheRequestCarriesNoSession(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    request := NewRequest(netRequest, nil, newTestRuntime(), nil)

    rotated, err := RegenerateRequestSession(request)
    if nil == err {
        t.Fatalf("expected error")
    }

    if nil != rotated {
        t.Fatalf("expected no session when the rotation failed")
    }
}

func TestRegenerateRequestSession_ReturnsErrorWhenTheSessionManagerIsNotRegistered(t *testing.T) {
    netRequest := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
    request := NewRequest(netRequest, nil, newTestRuntime(), nil)

    _ = SetRequestSession(
        request,
        mustSession(session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.UnlimitedDuration()}).NewSession()),
    )

    rotated, err := RegenerateRequestSession(request)
    if nil == err {
        t.Fatalf("expected error")
    }

    if nil != rotated {
        t.Fatalf("expected no session when the rotation failed")
    }
}

func TestRegenerateSession_AWriteToTheAbandonedSessionDoesNotResurrectItsId(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage(session.InMemoryStorageConfig{}))
    sessionManager := session.SessionMustFromContainer(t.Context(), serviceContainer)

    existingSession := mustSession(sessionManager.NewSession())
    existingSession.Set("userId", "anonymous")

    saveErr := sessionManager.SaveSession(existingSession)
    if nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    existingId := existingSession.Id()

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/login-writing-the-original",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionInstance, sessionReadErr := RequestSession(request)
            if nil != sessionReadErr {
                t.Fatal("expected the request to carry a session")
            }

            if _, rotateErr := sessionManager.RegenerateSession(sessionInstance); nil != rotateErr {
                return nil, rotateErr
            }

            sessionInstance.Set("userId", "u-1")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/login-writing-the-original", nil)
    request.AddCookie(
        &nethttp.Cookie{
            Name:  session.SessionCookieName,
            Value: existingId,
        },
    )

    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    cookies := recorder.Result().Cookies()
    if 1 != len(cookies) {
        t.Fatalf("expected one session cookie, got %d", len(cookies))
    }

    if -1 != cookies[0].MaxAge {
        t.Fatalf("expected a clearing cookie for the abandoned session even though it was written to after the rotation, got MaxAge %d", cookies[0].MaxAge)
    }

    if existingId == cookies[0].Value {
        t.Fatalf("expected the rotated-away id not to be re-issued to the client")
    }

    if nil != mustSession(sessionManager.Session(existingId)) {
        t.Fatalf("expected the rotated-away id to stay gone from storage instead of being re-created under the authenticated identity")
    }
}

/* The request is an application-implementable contract, so a nil pointer of a request type reaches this
door as a non-nil interface and the read below dereferences it. The untyped literal a sibling probe passes
is the only shape a bare comparison already catches. */
func TestRegenerateRequestSession_RefusesATypedNilRequest(t *testing.T) {
    var unassignedRequest *testhelper.HttpTestRequest

    rotated, err := RegenerateRequestSession(unassignedRequest)
    if nil == err {
        t.Fatalf("expected a typed nil request to be refused")
    }
    if nil != rotated {
        t.Fatalf("expected no session for a typed nil request, got %v", rotated)
    }

    /* the refusal is asserted by NAME: the session lookup below this guard carries its own reading of the
       same typed nil and answers the missing-session refusal, so a probe that only asks whether an error came
       back cannot tell the two apart. */
    if "request is nil in regenerate request session" != err.Error() {
        t.Fatalf("expected the request refusal to be the one that answered, got %q", err.Error())
    }
}

/* a logout landing while a request holds the session: the request's rotation must not mint a fresh id carrying the pre-logout identity, so nothing is stored and no live session cookie is issued */
func TestRegenerateRequestSession_RefusesASessionALogoutBuriedWhileTheRequestRan(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage(session.InMemoryStorageConfig{}))
    sessionManager := session.SessionMustFromContainer(t.Context(), serviceContainer)

    existingSession := mustSession(sessionManager.NewSession())
    existingSession.Set("userId", "u-7")

    if saveErr := sessionManager.SaveSession(existingSession); nil != saveErr {
        t.Fatalf("unexpected error seeding the session: %v", saveErr)
    }

    existingId := existingSession.Id()

    var rotationErr error

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/rotate-after-logout",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            /* the copy is read before the concurrent logout buries it */
            if _, readErr := RequestSession(request); nil != readErr {
                return nil, readErr
            }

            if deleteErr := sessionManager.DeleteSession(existingId); nil != deleteErr {
                return nil, deleteErr
            }

            _, rotationErr = RegenerateRequestSession(request)
            if nil != rotationErr {
                return nil, rotationErr
            }

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodGet, "/rotate-after-logout", nil)
    request.AddCookie(
        &nethttp.Cookie{
            Name:  session.SessionCookieName,
            Value: existingId,
        },
    )

    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if false == errors.Is(rotationErr, session.ErrSessionDeleted) {
        t.Fatalf("expected the rotation to be refused with ErrSessionDeleted, got %v", rotationErr)
    }

    if nethttp.StatusOK == recorder.Code {
        t.Fatalf("expected the refused rotation not to answer 200")
    }

    for _, cookie := range recorder.Result().Cookies() {
        if session.SessionCookieName != cookie.Name || 0 > cookie.MaxAge {
            continue
        }

        if stored := mustSession(sessionManager.Session(cookie.Value)); nil != stored {
            t.Fatalf("expected no live session cookie, got one naming a stored session holding userId %q", stored.String("userId"))
        }
    }

    if nil != mustSession(sessionManager.Session(existingId)) {
        t.Fatalf("expected the logged-out session to stay gone")
    }
}

func TestRegenerateRequestSession_LoadsTheSessionNothingRead(t *testing.T) {
    manager := newCountingSessionManager()
    serviceContainer := newHttpTestContainerWithSessionManager(manager)

    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer), nil)
    request.Attributes().Set(requestAttributeSession, newRequestSession(manager, request, &errorCountingLogger{}, "", false, httpcontract.SessionPolicy{}))

    rotated, rotateErr := RegenerateRequestSession(request)
    if nil != rotateErr || nil == rotated {
        t.Fatalf("expected the rotation over a session loaded for it, got %v %v", rotated, rotateErr)
    }

    if 1 != atomic.LoadInt64(&manager.newSessionCalls) || rotated != effectiveRequestSession(request) {
        t.Fatalf("expected one load and the rotated session published, got %d mints", atomic.LoadInt64(&manager.newSessionCalls))
    }
}

func TestRegenerateRequestSession_AnswersTheLoadErrorMarked(t *testing.T) {
    storeErr := exception.MarkInfrastructureFailure(errors.New("session store is down"))
    manager := &countingSessionManager{Manager: newCountingSessionManager().Manager, newSessionErr: storeErr}
    serviceContainer := newHttpTestContainerWithSessionManager(manager)

    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer), nil)
    request.Attributes().Set(requestAttributeSession, newRequestSession(manager, request, &errorCountingLogger{}, "", false, httpcontract.SessionPolicy{}))

    rotated, rotateErr := RegenerateRequestSession(request)
    if nil != rotated || false == errors.Is(rotateErr, storeErr) || false == exception.AnswersUnavailable(rotateErr) {
        t.Fatalf("expected the marked load error and no session, got %v %v", rotated, rotateErr)
    }
}

func TestRegenerateRequestSession_RefusesAnUnavailableSessionMarked(t *testing.T) {
    storeErr := exception.MarkInfrastructureFailure(errors.New("session store is down"))
    manager := &countingSessionManager{Manager: newCountingSessionManager().Manager, newSessionErr: storeErr}
    serviceContainer := newHttpTestContainerWithSessionManager(manager)

    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer), nil)
    request.Attributes().Set(requestAttributeSession, newRequestSession(manager, request, &errorCountingLogger{}, "", false, httpcontract.SessionPolicy{EmptyOnOutage: true}))

    rotated, rotateErr := RegenerateRequestSession(request)
    if nil != rotated || false == errors.Is(rotateErr, storeErr) || false == errors.Is(rotateErr, session.ErrSessionUnavailable) || false == exception.AnswersUnavailable(rotateErr) {
        t.Fatalf("expected the rotation of the unavailable session refused marked, got %v %v", rotated, rotateErr)
    }

    if false == exception.IsAlreadyLogged(rotateErr) {
        t.Fatalf("expected the refusal to carry the filed cause, so its answerer files nothing more")
    }

    if _, isUnavailable := session.SessionUnavailableCause(effectiveRequestSession(request)); false == isUnavailable {
        t.Fatalf("expected nothing published over the unavailable session")
    }
}

type rotationCountingSessionManager struct {
    sessioncontract.Manager
    regenerateCalls int64
}

func (instance *rotationCountingSessionManager) RegenerateSession(sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    atomic.AddInt64(&instance.regenerateCalls, 1)

    return instance.Manager.RegenerateSession(sessionInstance)
}

func TestRegenerateRequestSession_RefusesAnUnavailableSessionWithoutAskingTheManager(t *testing.T) {
    manager := &rotationCountingSessionManager{Manager: newCountingSessionManager().Manager}
    serviceContainer := newHttpTestContainerWithSessionManager(manager)

    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer), nil)
    cause := errors.New("the application's store is down")
    if publishErr := SetRequestSession(request, session.NewUnavailableSession(cause)); nil != publishErr {
        t.Fatalf("unexpected publish error: %v", publishErr)
    }

    rotated, rotateErr := RegenerateRequestSession(request)
    if nil != rotated || false == errors.Is(rotateErr, cause) || false == errors.Is(rotateErr, session.ErrSessionUnavailable) || false == exception.AnswersUnavailable(rotateErr) {
        t.Fatalf("expected the rotation refused as an outage, got %v %v", rotated, rotateErr)
    }

    if 0 != atomic.LoadInt64(&manager.regenerateCalls) {
        t.Fatalf("expected the manager not asked, got %d rotations", manager.regenerateCalls)
    }
}
