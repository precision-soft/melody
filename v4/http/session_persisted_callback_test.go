package http

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/session"
    "github.com/precision-soft/melody/v4/session/contract"
)

type sessionPersistedProbe struct {
    runtimeInstance runtimecontract.Runtime
    logger          *errorContextRecordingLogger
    request         httpcontract.Request
    calls           []error
}

func newSessionPersistedProbe(t *testing.T, sessionInstance contract.Session) *sessionPersistedProbe {
    t.Helper()

    recordingLogger := &errorContextRecordingLogger{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, recordingLogger, container.OverrideOptions{})

    netRequest := httptest.NewRequest(nethttp.MethodPost, "http://example.com/logout", nil)
    netRequest.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: sessionInstance.Id()})

    request := NewRequest(netRequest, nil, nil, nil)
    request.Attributes().Set(requestAttributeSession, sessionInstance)

    return &sessionPersistedProbe{
        runtimeInstance: runtime.New(context.Background(), scope, serviceContainer),
        logger:          recordingLogger,
        request:         request,
    }
}

func (instance *sessionPersistedProbe) register(t *testing.T, answer error) {
    t.Helper()

    registerErr := OnSessionPersisted(instance.request, func(persistErr error) error {
        instance.calls = append(instance.calls, persistErr)

        return answer
    })
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }
}

func (instance *sessionPersistedProbe) write(
    sessionManager contract.Manager,
    writer nethttp.ResponseWriter,
    raiseCallbackError func(error) httpcontract.Response,
) httpcontract.Response {
    return writeResponse(
        instance.runtimeInstance,
        instance.request,
        writer,
        TextResponse(nethttp.StatusOK, "bye"),
        sessionManager,
        httpcontract.ForwardedHeadersPolicy{},
        httpcontract.SessionCookiePolicy{Path: "/"},
        7*time.Second,
        raiseCallbackError,
    )
}

func TestOnSessionPersisted_RefusesANilRequest(t *testing.T) {
    if registerErr := OnSessionPersisted(nil, func(persistErr error) error { return nil }); nil == registerErr {
        t.Fatal("expected a nil request refused")
    }
}

func TestOnSessionPersisted_RefusesARequestWithoutAttributes(t *testing.T) {
    if registerErr := OnSessionPersisted(&nilAttributesRequest{}, func(persistErr error) error { return nil }); nil == registerErr {
        t.Fatal("expected a request without attributes refused")
    }
}

func TestOnSessionPersisted_RefusesANilCallback(t *testing.T) {
    request := NewRequest(httptest.NewRequest(nethttp.MethodPost, "/", nil), nil, nil, nil)

    if registerErr := OnSessionPersisted(request, nil); nil == registerErr {
        t.Fatal("expected a nil callback refused")
    }
}

func TestWriteResponse_RunsTheCallbackWithNoErrorOnceTheDeleteLanded(t *testing.T) {
    probe := newSessionPersistedProbe(t, &stubSession{id: "session-1", isCleared: true})
    probe.register(t, nil)

    sessionManager := &stubSessionManager{}
    response := probe.write(sessionManager, httptest.NewRecorder(), nil)

    if 1 != sessionManager.deleteCalled || 1 != len(probe.calls) || nil != probe.calls[0] {
        t.Fatalf("expected one delete then one callback with no error, got %d deletes and %v", sessionManager.deleteCalled, probe.calls)
    }

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the response kept, got %d", response.StatusCode())
    }
}

func TestWriteResponse_HandsTheCallbackTheFailedDelete(t *testing.T) {
    probe := newSessionPersistedProbe(t, &stubSession{id: "session-1", isCleared: true})
    probe.register(t, nil)

    deleteErr := exception.MarkInfrastructureFailure(errors.New("connection refused"))
    response := probe.write(&stubSessionManager{deleteErr: deleteErr}, httptest.NewRecorder(), nil)

    if 1 != len(probe.calls) || false == errors.Is(probe.calls[0], deleteErr) {
        t.Fatalf("expected one callback with the delete's error, got %v", probe.calls)
    }

    if nethttp.StatusServiceUnavailable != response.StatusCode() {
        t.Fatalf("expected the delete's 503 kept, got %d", response.StatusCode())
    }
}

func TestWriteResponse_HandsTheCallbackTheFailedSave(t *testing.T) {
    probe := newSessionPersistedProbe(t, &stubSession{id: "session-1", isModified: true})
    probe.register(t, nil)

    saveErr := errors.New("disk full")
    probe.write(&stubSessionManager{saveErr: saveErr}, httptest.NewRecorder(), nil)

    if 1 != len(probe.calls) || false == errors.Is(probe.calls[0], saveErr) {
        t.Fatalf("expected one callback with the save's error, got %v", probe.calls)
    }
}

func TestWriteResponse_HandsTheCallbackTheSaveARotationRefused(t *testing.T) {
    probe := newSessionPersistedProbe(t, &stubSession{id: "session-1", isModified: true})
    probe.register(t, nil)

    probe.write(&stubSessionManager{saveErr: session.ErrSessionRotated}, httptest.NewRecorder(), nil)

    if 1 != len(probe.calls) || false == errors.Is(probe.calls[0], session.ErrSessionRotated) {
        t.Fatalf("expected one callback with the rotation's refusal, got %v", probe.calls)
    }
}

func TestWriteResponse_RunsTheCallbacksOnceInRegistrationOrder(t *testing.T) {
    probe := newSessionPersistedProbe(t, &stubSession{id: "session-1", isCleared: true})

    order := []string{}
    _ = OnSessionPersisted(probe.request, func(persistErr error) error { order = append(order, "first"); return nil })
    _ = OnSessionPersisted(probe.request, func(persistErr error) error { order = append(order, "second"); return nil })

    probe.write(&stubSessionManager{}, httptest.NewRecorder(), nil)
    probe.write(&stubSessionManager{}, httptest.NewRecorder(), nil)

    if 2 != len(order) || "first" != order[0] || "second" != order[1] {
        t.Fatalf("expected both callbacks once in order, got %v", order)
    }
}

func TestWriteResponse_RunsTheCallbackWhenNoSessionIsPersisted(t *testing.T) {
    probe := newSessionPersistedProbe(t, &stubSession{id: "session-1"})
    probe.register(t, nil)

    probe.write(nil, httptest.NewRecorder(), nil)

    if 1 != len(probe.calls) || nil != probe.calls[0] {
        t.Fatalf("expected one callback with no error, got %v", probe.calls)
    }
}

func TestWriteResponse_ACallbackErrorIsRaisedAndKeepsTheCookie(t *testing.T) {
    probe := newSessionPersistedProbe(t, &stubSession{id: "session-1", isCleared: true})
    callbackErr := errors.New("audit unavailable")
    probe.register(t, callbackErr)

    var raisedErr error
    writer := httptest.NewRecorder()
    response := probe.write(&stubSessionManager{}, writer, func(err error) httpcontract.Response {
        raisedErr = err

        return EmptyResponse(nethttp.StatusConflict)
    })

    if callbackErr != raisedErr || nethttp.StatusConflict != response.StatusCode() || nethttp.StatusConflict != writer.Code {
        t.Fatalf("expected the callback's error raised and its answer written, got %v and %d", raisedErr, writer.Code)
    }

    cookies := writer.Result().Cookies()
    if 1 != len(cookies) || 0 <= cookies[0].MaxAge {
        t.Fatalf("expected the expiring cookie kept on the raised answer, got %v", cookies)
    }
}

func TestWriteResponse_ACallbackErrorWithoutARaiserIsLogged(t *testing.T) {
    probe := newSessionPersistedProbe(t, &stubSession{id: "session-1", isCleared: true})
    probe.register(t, errors.New("audit unavailable"))

    response := probe.write(&stubSessionManager{}, httptest.NewRecorder(), nil)

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the response kept, got %d", response.StatusCode())
    }

    if _, recorded := probe.logger.errorContextFor("session persisted callback failed and its error cannot replace the response"); false == recorded {
        t.Fatal("expected the callback's error logged")
    }
}

func TestWriteResponse_ACallbackErrorOnACommittedResponseIsLogged(t *testing.T) {
    probe := newSessionPersistedProbe(t, &stubSession{id: "session-1", isCleared: true})
    probe.register(t, errors.New("audit unavailable"))

    raised := false
    response := probe.write(&stubSessionManager{}, &hijackedHeadersWriter{ResponseRecorder: httptest.NewRecorder()}, func(err error) httpcontract.Response {
        raised = true

        return EmptyResponse(nethttp.StatusConflict)
    })

    if true == raised || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected nothing raised on a committed response, got %v and %d", raised, response.StatusCode())
    }

    if _, recorded := probe.logger.errorContextFor("session persisted callback failed and its error cannot replace the response"); false == recorded {
        t.Fatal("expected the callback's error logged")
    }
}

func TestWriteResponse_HandsTheCallbackTheSaveTheBoundCutAsAnOutage(t *testing.T) {
    probe := newSessionPersistedProbe(t, &stubSession{id: "session-1", isModified: true})
    probe.register(t, nil)

    probe.write(&hangingSessionManager{stubSessionManager: stubSessionManager{storageTimeout: bound.Of(10 * time.Millisecond)}}, httptest.NewRecorder(), nil)

    if 1 != len(probe.calls) || false == exception.AnswersUnavailable(probe.calls[0]) {
        t.Fatalf("expected one callback with the cut save as an outage, got %v", probe.calls)
    }
}
