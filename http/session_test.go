package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/session"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

func TestRegenerateRequestSession_RotatesAndRepublishesInOneCall(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage())
    sessionManager := session.SessionMustFromContainer(serviceContainer)

    preRotationId := ""
    rotatedId := ""

    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/login",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            sessionValue, exists := request.Attributes().Get(RequestAttributeSession)
            if false == exists {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance, ok := sessionValue.(sessioncontract.Session)
            if false == ok {
                t.Fatal("expected the session attribute to be a session")
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

    handler := NewKernel(router).ServeHttp(serviceContainer)

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

    if nil != sessionManager.Session(preRotationId) {
        t.Fatalf("expected the pre-rotation session to be gone from storage")
    }

    reloaded := sessionManager.Session(rotatedId)
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

    request.Attributes().Set(
        RequestAttributeSession,
        session.NewManager(session.NewInMemoryStorage(), 0).NewSession(),
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
    serviceContainer := newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage())
    sessionManager := session.SessionMustFromContainer(serviceContainer)

    existingSession := sessionManager.NewSession()
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
            sessionValue, exists := request.Attributes().Get(RequestAttributeSession)
            if false == exists {
                t.Fatal("expected the request to carry a session")
            }

            sessionInstance, ok := sessionValue.(sessioncontract.Session)
            if false == ok {
                t.Fatal("expected the session attribute to be a session")
            }

            if _, rotateErr := sessionManager.RegenerateSession(sessionInstance); nil != rotateErr {
                return nil, rotateErr
            }

            sessionInstance.Set("userId", "u-1")

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

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

    if nil != sessionManager.Session(existingId) {
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
