package http

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

type countingSessionManager struct {
    sessioncontract.Manager
    sessionCalls    int64
    newSessionCalls int64
    sessionErr      error
    newSessionErr   error
    typedNil        bool
}

func (instance *countingSessionManager) Session(sessionId string) (sessioncontract.Session, error) {
    atomic.AddInt64(&instance.sessionCalls, 1)

    if nil != instance.sessionErr {
        return nil, instance.sessionErr
    }

    if true == instance.typedNil {
        var typedNil *session.Session

        return typedNil, nil
    }

    return instance.Manager.Session(sessionId)
}

func (instance *countingSessionManager) NewSession() (sessioncontract.Session, error) {
    atomic.AddInt64(&instance.newSessionCalls, 1)

    if nil != instance.newSessionErr {
        return nil, instance.newSessionErr
    }

    return instance.Manager.NewSession()
}

func newCountingSessionManager() *countingSessionManager {
    return &countingSessionManager{
        Manager: session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)}),
    }
}

func requestOverHandle(manager sessioncontract.Manager, logger *errorCountingLogger, cookieValue string, cookiePresent bool) *Request {
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)
    request.Attributes().Set(requestAttributeSession, newRequestSession(manager, request, logger, cookieValue, cookiePresent))

    return request
}

func TestRequestSession_LoadsAtTheFirstCallAndOnlyOnce(t *testing.T) {
    manager := newCountingSessionManager()
    request := requestOverHandle(manager, &errorCountingLogger{}, "", false)

    if 0 != atomic.LoadInt64(&manager.newSessionCalls) || nil != effectiveRequestSession(request) {
        t.Fatalf("expected nothing loaded before the first read")
    }

    first, firstErr := RequestSession(request)
    second, secondErr := RequestSession(request)
    if nil != firstErr || nil != secondErr || first != second {
        t.Fatalf("expected the same session twice, got %v %v %v %v", first, firstErr, second, secondErr)
    }

    if 1 != atomic.LoadInt64(&manager.newSessionCalls) {
        t.Fatalf("expected one mint, got %d", atomic.LoadInt64(&manager.newSessionCalls))
    }

    if first != effectiveRequestSession(request) {
        t.Fatalf("expected the response path to read the loaded session")
    }

    concurrentManager := newCountingSessionManager()
    concurrentRequest := requestOverHandle(concurrentManager, &errorCountingLogger{}, "", false)

    var waitGroup sync.WaitGroup
    for range 8 {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            _, _ = RequestSession(concurrentRequest)
        }()
    }
    waitGroup.Wait()

    if 1 != atomic.LoadInt64(&concurrentManager.newSessionCalls) {
        t.Fatalf("expected one mint across concurrent readers, got %d", atomic.LoadInt64(&concurrentManager.newSessionCalls))
    }
}

func TestRequestSession_AnswersTheManagerErrorMarkedAndCachesIt(t *testing.T) {
    storeErr := exception.MarkInfrastructureFailure(errors.New("session store is down"))

    cases := map[string]struct {
        manager       *countingSessionManager
        cookiePresent bool
    }{
        "a load with a cookie":    {manager: &countingSessionManager{Manager: newCountingSessionManager().Manager, sessionErr: storeErr}, cookiePresent: true},
        "a mint without a cookie": {manager: &countingSessionManager{Manager: newCountingSessionManager().Manager, newSessionErr: storeErr}, cookiePresent: false},
    }

    for name, testCase := range cases {
        logger := &errorCountingLogger{}
        request := requestOverHandle(testCase.manager, logger, sessionOutageTestCookieValue, testCase.cookiePresent)

        first, firstErr := RequestSession(request)
        second, secondErr := RequestSession(request)

        if nil != first || nil != second || false == errors.Is(firstErr, storeErr) || firstErr != secondErr {
            t.Fatalf("%s: expected the same error twice and no session, got %v %v %v %v", name, first, firstErr, second, secondErr)
        }

        if false == exception.AnswersUnavailable(firstErr) || false == exception.IsAlreadyLogged(firstErr) {
            t.Fatalf("%s: expected the error answered unavailable and filed", name)
        }

        if 1 != atomic.LoadInt64(&testCase.manager.sessionCalls)+atomic.LoadInt64(&testCase.manager.newSessionCalls) {
            t.Fatalf("%s: expected the manager asked once, got %d loads and %d mints", name, testCase.manager.sessionCalls, testCase.manager.newSessionCalls)
        }

        if 1 != atomic.LoadInt64(&logger.errorCount) {
            t.Fatalf("%s: expected one error record, got %d", name, atomic.LoadInt64(&logger.errorCount))
        }

        if nil != effectiveRequestSession(request) {
            t.Fatalf("%s: expected no session for the response path", name)
        }
    }
}

func TestRequestSession_AFailedLoadWithoutALoggerIsNotMarkedLogged(t *testing.T) {
    manager := &countingSessionManager{Manager: newCountingSessionManager().Manager, newSessionErr: exception.MarkInfrastructureFailure(errors.New("session store is down"))}

    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)
    request.Attributes().Set(requestAttributeSession, newRequestSession(manager, request, nil, "", false))

    _, sessionErr := RequestSession(request)
    if false == exception.AnswersUnavailable(sessionErr) || true == exception.IsAlreadyLogged(sessionErr) {
        t.Fatalf("expected the unfiled error answered unavailable and unmarked, got %v", sessionErr)
    }
}

func TestRequestSession_MintsWhenTheCookieNamesNoSession(t *testing.T) {
    cases := map[string]struct {
        cookieValue string
        typedNil    bool
    }{
        "an unknown id":    {cookieValue: sessionOutageTestCookieValue},
        "a malformed id":   {cookieValue: "not-a-session-id"},
        "a typed-nil load": {cookieValue: sessionOutageTestCookieValue, typedNil: true},
    }

    for name, testCase := range cases {
        manager := newCountingSessionManager()
        manager.typedNil = testCase.typedNil

        request := requestOverHandle(manager, &errorCountingLogger{}, testCase.cookieValue, true)

        sessionInstance, sessionErr := RequestSession(request)
        if nil != sessionErr || nil == sessionInstance || testCase.cookieValue == sessionInstance.Id() {
            t.Fatalf("%s: expected a fresh session, got %v %v", name, sessionInstance, sessionErr)
        }

        if 1 != atomic.LoadInt64(&manager.newSessionCalls) {
            t.Fatalf("%s: expected one mint, got %d", name, atomic.LoadInt64(&manager.newSessionCalls))
        }
    }
}

func TestSetRequestSession_PublishesTheSessionTheDoorAnswers(t *testing.T) {
    manager := newCountingSessionManager()
    request := requestOverHandle(manager, &errorCountingLogger{}, "", false)

    published := mustSession(manager.Manager.NewSession())
    if publishErr := SetRequestSession(request, published); nil != publishErr {
        t.Fatalf("unexpected error: %v", publishErr)
    }

    answered, answerErr := RequestSession(request)
    if nil != answerErr || published != answered || published != effectiveRequestSession(request) {
        t.Fatalf("expected the published session answered, got %v %v", answered, answerErr)
    }

    if 0 != atomic.LoadInt64(&manager.newSessionCalls) {
        t.Fatalf("expected the handle never loaded once a session was published")
    }
}

func TestSetRequestSession_RefusesANilSession(t *testing.T) {
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)

    var typedNil *session.Session

    for name, sessionInstance := range map[string]sessioncontract.Session{"nil": nil, "typed nil": typedNil} {
        if publishErr := SetRequestSession(request, sessionInstance); nil == publishErr {
            t.Fatalf("%s: expected the session refused", name)
        }
    }

    if publishErr := SetRequestSession(nil, mustSession(newCountingSessionManager().NewSession())); nil == publishErr {
        t.Fatalf("expected a nil request refused")
    }

    if _, exists := request.Attributes().Get(requestAttributeSession); true == exists {
        t.Fatalf("expected nothing published")
    }
}

func TestRequestSession_ARequestTheKernelDidNotServeAnswersAnError(t *testing.T) {
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)

    sessionInstance, sessionErr := RequestSession(request)
    if nil != sessionInstance || nil == sessionErr || true == exception.AnswersUnavailable(sessionErr) {
        t.Fatalf("expected an unmarked error and no session, got %v %v", sessionInstance, sessionErr)
    }

    if _, nilErr := RequestSession(nil); nil == nilErr {
        t.Fatalf("expected a nil request answered an error")
    }
}

func TestRequestSession_ConcurrentReadersOfAFailingLoadFileOneRecord(t *testing.T) {
    manager := &countingSessionManager{Manager: newCountingSessionManager().Manager, newSessionErr: exception.MarkInfrastructureFailure(errors.New("session store is down"))}
    logger := &errorCountingLogger{}
    request := requestOverHandle(manager, logger, "", false)

    var waitGroup sync.WaitGroup
    for range 8 {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            if _, sessionErr := RequestSession(request); false == errors.Is(sessionErr, session.ErrSessionUnavailable) {
                t.Errorf("expected the session outage in the chain, got %v", sessionErr)
            }
        }()
    }
    waitGroup.Wait()

    if 1 != atomic.LoadInt64(&manager.newSessionCalls) || 1 != atomic.LoadInt64(&logger.errorCount) {
        t.Fatalf("expected one mint and one record across concurrent readers, got %d mints, %d records", atomic.LoadInt64(&manager.newSessionCalls), atomic.LoadInt64(&logger.errorCount))
    }
}
