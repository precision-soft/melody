package http

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
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

func (instance *countingSessionManager) Session(runtimeInstance containercontract.Runtime, sessionId string) (sessioncontract.Session, error) {
    atomic.AddInt64(&instance.sessionCalls, 1)

    if nil != instance.sessionErr {
        return nil, instance.sessionErr
    }

    if true == instance.typedNil {
        var typedNil *session.Session

        return typedNil, nil
    }

    return instance.Manager.Session(runtimeInstance, sessionId)
}

func (instance *countingSessionManager) NewSession(runtimeInstance containercontract.Runtime) (sessioncontract.Session, error) {
    atomic.AddInt64(&instance.newSessionCalls, 1)

    if nil != instance.newSessionErr {
        return nil, instance.newSessionErr
    }

    return instance.Manager.NewSession(runtimeInstance)
}

func newCountingSessionManager() *countingSessionManager {
    return &countingSessionManager{
        Manager: session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)}),
    }
}

func requestOverHandle(manager sessioncontract.Manager, logger *errorCountingLogger, cookieValue string, cookiePresent bool) *Request {
    return requestOverHandleWithPolicy(manager, logger, cookieValue, cookiePresent, httpcontract.SessionPolicy{})
}

func requestOverHandleWithPolicy(manager sessioncontract.Manager, logger *errorCountingLogger, cookieValue string, cookiePresent bool, policy httpcontract.SessionPolicy) *Request {
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)
    request.Attributes().Set(requestAttributeSession, newRequestSession(sessionRuntime(), manager, request, logger, cookieValue, cookiePresent, policy))

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
    request.Attributes().Set(requestAttributeSession, newRequestSession(sessionRuntime(), manager, request, nil, "", false, httpcontract.SessionPolicy{}))

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

    published := mustSession(manager.Manager.NewSession(sessionRuntime()))
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

    if publishErr := SetRequestSession(nil, mustSession(newCountingSessionManager().NewSession(sessionRuntime()))); nil == publishErr {
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

func TestRequestSession_EmptyOnOutageAnswersTheUnavailableSession(t *testing.T) {
    storeErr := exception.MarkInfrastructureFailure(errors.New("session store is down"))

    cases := map[string]struct {
        manager       *countingSessionManager
        cookiePresent bool
    }{
        "a load with a cookie":    {manager: &countingSessionManager{Manager: newCountingSessionManager().Manager, sessionErr: storeErr}, cookiePresent: true},
        "a mint without a cookie": {manager: &countingSessionManager{Manager: newCountingSessionManager().Manager, newSessionErr: storeErr}, cookiePresent: false},
    }

    for name, testCase := range cases {
        request := requestOverHandleWithPolicy(testCase.manager, &errorCountingLogger{}, sessionOutageTestCookieValue, testCase.cookiePresent, httpcontract.SessionPolicy{EmptyOnOutage: true})

        sessionInstance, sessionErr := RequestSession(request)
        if nil != sessionErr || nil == sessionInstance || "" != sessionInstance.Id() {
            t.Fatalf("%s: expected an empty session with no id and no error, got %v %v", name, sessionInstance, sessionErr)
        }

        cause, isUnavailable := session.SessionUnavailableCause(sessionInstance)
        if false == isUnavailable || false == errors.Is(cause, storeErr) || false == errors.Is(cause, session.ErrSessionUnavailable) {
            t.Fatalf("%s: expected the session to carry the outage, got %v %v", name, cause, isUnavailable)
        }

        if false == exception.AnswersUnavailable(cause) || false == exception.IsAlreadyLogged(cause) {
            t.Fatalf("%s: expected the cause answered unavailable and filed", name)
        }

        if sessionInstance != effectiveRequestSession(request) {
            t.Fatalf("%s: expected the response path to read the unavailable session", name)
        }
    }
}

func TestRequestSession_EmptyOnOutageKeepsAnUnmarkedErrorAnError(t *testing.T) {
    manager := &countingSessionManager{Manager: newCountingSessionManager().Manager, newSessionErr: errors.New("could not generate unique session id")}
    logger := &errorCountingLogger{}
    request := requestOverHandleWithPolicy(manager, logger, "", false, httpcontract.SessionPolicy{EmptyOnOutage: true})

    sessionInstance, sessionErr := RequestSession(request)
    if nil != sessionInstance || nil == sessionErr || true == exception.AnswersUnavailable(sessionErr) {
        t.Fatalf("expected the unmarked error answered as it is, got %v %v", sessionInstance, sessionErr)
    }

    if 1 != atomic.LoadInt64(&logger.errorCount) || 0 != atomic.LoadInt64(&logger.warningCount) {
        t.Fatalf("expected one error record and no warning, got %d errors and %d warnings", logger.errorCount, logger.warningCount)
    }
}

func TestRequestSession_EmptyOnOutageFilesOneWarningForTheRequest(t *testing.T) {
    manager := &countingSessionManager{Manager: newCountingSessionManager().Manager, sessionErr: exception.MarkInfrastructureFailure(errors.New("session store is down"))}
    logger := &errorCountingLogger{}
    request := requestOverHandleWithPolicy(manager, logger, sessionOutageTestCookieValue, true, httpcontract.SessionPolicy{EmptyOnOutage: true})

    first, firstErr := RequestSession(request)
    second, secondErr := RequestSession(request)
    if nil != firstErr || nil != secondErr || first != second {
        t.Fatalf("expected the same unavailable session twice, got %v %v %v %v", first, firstErr, second, secondErr)
    }

    if 1 != atomic.LoadInt64(&logger.warningCount) || 0 != atomic.LoadInt64(&logger.errorCount) {
        t.Fatalf("expected one warning and no error record, got %d warnings and %d errors", logger.warningCount, logger.errorCount)
    }

    if 1 != atomic.LoadInt64(&manager.sessionCalls) {
        t.Fatalf("expected the manager asked once, got %d", manager.sessionCalls)
    }
}

func TestRequestSession_EmptyOnOutageNamesTheSessionTheCookiePointsAt(t *testing.T) {
    for _, cookiePresent := range []bool{true, false} {
        manager := &countingSessionManager{Manager: newCountingSessionManager().Manager, sessionErr: exception.MarkInfrastructureFailure(errors.New("session store is down")), newSessionErr: exception.MarkInfrastructureFailure(errors.New("session store is down"))}
        logger := &warningRecordingLogger{}

        request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)
        request.Attributes().Set(requestAttributeSession, newRequestSession(sessionRuntime(), manager, request, logger, sessionOutageTestCookieValue, cookiePresent, httpcontract.SessionPolicy{EmptyOnOutage: true}))

        if _, sessionErr := RequestSession(request); nil != sessionErr {
            t.Fatalf("unexpected error: %v", sessionErr)
        }

        if 1 != len(logger.warningContexts) {
            t.Fatalf("expected one warning, got %d", len(logger.warningContexts))
        }

        sessionReference, named := logger.warningContexts[0]["sessionReference"]
        if _, hasRetiredKey := logger.warningContexts[0]["sessionRef"]; true == hasRetiredKey {
            t.Fatalf("expected the reference under sessionReference only, got %v", logger.warningContexts[0])
        }

        if true == cookiePresent && (false == named || sessionIdLogReference(sessionOutageTestCookieValue) != sessionReference) {
            t.Fatalf("expected the warning to name the cookie's session, got %v", logger.warningContexts[0])
        }

        if false == cookiePresent && true == named {
            t.Fatalf("expected no session reference without a cookie, got %v", logger.warningContexts[0])
        }
    }
}

type requestPlantedKey struct{}

type contextRecordingSessionManager struct {
    sessioncontract.Manager
    sessionContext    context.Context
    newSessionContext context.Context
}

func (instance *contextRecordingSessionManager) Session(runtimeInstance containercontract.Runtime, sessionId string) (sessioncontract.Session, error) {
    ctx := runtimeInstance.Context()
    instance.sessionContext = ctx

    return instance.Manager.Session(runtimeInstance, sessionId)
}

func (instance *contextRecordingSessionManager) NewSession(runtimeInstance containercontract.Runtime) (sessioncontract.Session, error) {
    ctx := runtimeInstance.Context()
    instance.newSessionContext = ctx

    return instance.Manager.NewSession(runtimeInstance)
}

func TestRequestSession_LoadsUnderTheRequestsContext(t *testing.T) {
    manager := &contextRecordingSessionManager{Manager: newCountingSessionManager().Manager}
    requestContext := context.WithValue(context.Background(), requestPlantedKey{}, "planted")

    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)
    request.Attributes().Set(requestAttributeSession, newRequestSession(sessionRuntimeUnder(requestContext), manager, request, nil, sessionOutageTestCookieValue, true, httpcontract.SessionPolicy{}))

    if _, sessionErr := RequestSession(request); nil != sessionErr {
        t.Fatalf("unexpected error: %v", sessionErr)
    }

    for door, received := range map[string]context.Context{"Session": manager.sessionContext, "NewSession": manager.newSessionContext} {
        if nil == received || "planted" != received.Value(requestPlantedKey{}) {
            t.Fatalf("expected %s to run on the request's context", door)
        }
    }
}

type levelRecordingLogger struct {
    mutex  sync.Mutex
    levels []loggingcontract.Level
}

func (instance *levelRecordingLogger) record(level loggingcontract.Level) {
    instance.mutex.Lock()
    instance.levels = append(instance.levels, level)
    instance.mutex.Unlock()
}

func (instance *levelRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.record(level)
}

func (instance *levelRecordingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *levelRecordingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *levelRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.record(loggingcontract.LevelWarning)
}

func (instance *levelRecordingLogger) Error(message string, context loggingcontract.Context) {
    instance.record(loggingcontract.LevelError)
}

func (instance *levelRecordingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.record(loggingcontract.LevelEmergency)
}

func (instance *levelRecordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *levelRecordingLogger) CauseChainDepth() int {
    return 8
}

func TestRequestSession_ALoadTheClientsHangupCancelledIsRecordedAtWarning(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    for _, row := range []struct {
        name           string
        requestContext context.Context
        cause          error
        expected       loggingcontract.Level
    }{
        {"cancelled", cancelledContext, context.Canceled, loggingcontract.LevelWarning},
        {"storage cancelled under a live request", context.Background(), context.Canceled, loggingcontract.LevelError},
        {"deadline", cancelledContext, context.DeadlineExceeded, loggingcontract.LevelError},
        {"outage", cancelledContext, errors.New("session store is down"), loggingcontract.LevelError},
    } {
        t.Run(row.name, func(t *testing.T) {
            loadErr := exception.NewError("could not load the session", nil, exception.MarkInfrastructureFailure(row.cause))
            manager := &countingSessionManager{Manager: newCountingSessionManager().Manager, sessionErr: loadErr}
            logger := &levelRecordingLogger{}

            request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)
            request.Attributes().Set(requestAttributeSession, newRequestSession(sessionRuntimeUnder(row.requestContext), manager, request, logger, sessionOutageTestCookieValue, true, httpcontract.SessionPolicy{}))

            if _, sessionErr := RequestSession(request); false == exception.ChainIs(sessionErr, row.cause) || false == exception.ChainIs(sessionErr, session.ErrSessionUnavailable) {
                t.Fatalf("expected the load error with its cause, got %v", sessionErr)
            }

            if 1 != len(logger.levels) || row.expected != logger.levels[0] {
                t.Fatalf("expected one record at %v, got %v", row.expected, logger.levels)
            }
        })
    }
}

/* boundedSessionManager declares a storage bound, records the context each storage call received, and can hang until that context ends, answering its bare error */
type boundedSessionManager struct {
    sessioncontract.Manager
    mutex          sync.Mutex
    storageTimeout bound.Duration
    hangLoad       bool
    hangMint       bool
    hangRotation   bool
    panicLoad      bool
    contextList    []context.Context
}

func (instance *boundedSessionManager) record(ctx context.Context) {
    instance.mutex.Lock()
    instance.contextList = append(instance.contextList, ctx)
    instance.mutex.Unlock()
}

func (instance *boundedSessionManager) recorded() []context.Context {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]context.Context{}, instance.contextList...)
}

func (instance *boundedSessionManager) Session(runtimeInstance containercontract.Runtime, sessionId string) (sessioncontract.Session, error) {
    ctx := runtimeInstance.Context()
    instance.record(ctx)

    if true == instance.panicLoad {
        panic("the load panicked")
    }

    if true == instance.hangLoad {
        return nil, awaitStorageContext(ctx)
    }

    return nil, nil
}

func (instance *boundedSessionManager) NewSession(runtimeInstance containercontract.Runtime) (sessioncontract.Session, error) {
    ctx := runtimeInstance.Context()
    instance.record(ctx)

    if true == instance.hangMint {
        return nil, awaitStorageContext(ctx)
    }

    return instance.Manager.NewSession(runtimeInstance)
}

func (instance *boundedSessionManager) RegenerateSession(runtimeInstance containercontract.Runtime, sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    ctx := runtimeInstance.Context()
    instance.record(ctx)

    if true == instance.hangRotation {
        return nil, awaitStorageContext(ctx)
    }

    return instance.Manager.RegenerateSession(runtimeInstance, sessionInstance)
}

func (instance *boundedSessionManager) StorageTimeout() bound.Duration {
    return instance.storageTimeout
}

func newBoundedSessionManager(storageTimeout bound.Duration) *boundedSessionManager {
    return &boundedSessionManager{
        Manager:        session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)}),
        storageTimeout: storageTimeout,
    }
}

type messageContextRecordingLogger struct {
    levelRecordingLogger
    contextList []loggingcontract.Context
}

func (instance *messageContextRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.record(loggingcontract.LevelWarning)
    instance.contextList = append(instance.contextList, context)
}

func (instance *messageContextRecordingLogger) Error(message string, context loggingcontract.Context) {
    instance.record(loggingcontract.LevelError)
    instance.contextList = append(instance.contextList, context)
}

func boundedLoadRequest(requestContext context.Context, manager sessioncontract.Manager, logger loggingcontract.Logger, cookiePresent bool, policy httpcontract.SessionPolicy) *Request {
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, nil, nil)
    request.Attributes().Set(requestAttributeSession, newRequestSession(sessionRuntimeUnder(requestContext), manager, request, logger, sessionOutageTestCookieValue, cookiePresent, policy))

    return request
}

func TestRequestSession_AHungLoadIsCutByTheStorageTimeoutAndAnswers503(t *testing.T) {
    manager := newBoundedSessionManager(bound.Of(50 * time.Millisecond))
    manager.hangLoad = true

    startedAt := time.Now()
    _, sessionErr := RequestSession(boundedLoadRequest(context.Background(), manager, &errorCountingLogger{}, true, httpcontract.SessionPolicy{}))

    if elapsed := time.Since(startedAt); time.Second < elapsed {
        t.Fatalf("expected the hung load cut by the 50ms bound, it took %s", elapsed)
    }

    if false == exception.AnswersUnavailable(sessionErr) || false == errors.Is(sessionErr, session.ErrSessionUnavailable) || false == errors.Is(sessionErr, context.DeadlineExceeded) {
        t.Fatalf("expected an unavailable load carrying the deadline, got %v", sessionErr)
    }
}

func TestRequestSession_TheBoundCoversTheMintAfterAMiss(t *testing.T) {
    manager := newBoundedSessionManager(bound.Of(50 * time.Millisecond))
    manager.hangMint = true

    startedAt := time.Now()
    _, sessionErr := RequestSession(boundedLoadRequest(context.Background(), manager, &errorCountingLogger{}, true, httpcontract.SessionPolicy{}))

    if elapsed := time.Since(startedAt); time.Second < elapsed || false == exception.AnswersUnavailable(sessionErr) {
        t.Fatalf("expected the hung mint cut by the bound as an outage, got %v after %s", sessionErr, elapsed)
    }

    contextList := manager.recorded()
    if 2 != len(contextList) || contextList[0] != contextList[1] {
        t.Fatalf("expected the load and the mint under one bounded context, got %d contexts", len(contextList))
    }

    if _, hasDeadline := contextList[0].Deadline(); false == hasDeadline {
        t.Fatalf("expected the load context to carry the bound")
    }
}

func TestRequestSession_AnUnlimitedStorageTimeoutArmsNoTimer(t *testing.T) {
    manager := newBoundedSessionManager(bound.UnlimitedDuration())

    if _, sessionErr := RequestSession(boundedLoadRequest(context.Background(), manager, &errorCountingLogger{}, true, httpcontract.SessionPolicy{})); nil != sessionErr {
        t.Fatalf("unexpected load failure: %v", sessionErr)
    }

    for _, ctx := range manager.recorded() {
        if _, hasDeadline := ctx.Deadline(); true == hasDeadline {
            t.Fatalf("expected no timer under an unlimited bound")
        }
    }
}

func TestRequestSession_ALoadCutByTheBoundIsRecordedAtErrorNamingTheBound(t *testing.T) {
    manager := newBoundedSessionManager(bound.Of(20 * time.Millisecond))
    manager.hangLoad = true
    logger := &messageContextRecordingLogger{}

    _, _ = RequestSession(boundedLoadRequest(context.Background(), manager, logger, true, httpcontract.SessionPolicy{}))

    if 1 != len(logger.levels) || loggingcontract.LevelError != logger.levels[0] || session.StorageTimeoutRule.Name != logger.contextList[0]["bound"] {
        t.Fatalf("expected one error record naming the bound, got %v %v", logger.levels, logger.contextList)
    }
}

func TestRequestSession_AClientHangupDuringTheLoadStaysAWarningNamingNoBound(t *testing.T) {
    manager := newBoundedSessionManager(bound.Of(time.Hour))
    manager.hangLoad = true
    logger := &messageContextRecordingLogger{}

    requestContext, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(20 * time.Millisecond)
        cancel()
    }()

    _, sessionErr := RequestSession(boundedLoadRequest(requestContext, manager, logger, true, httpcontract.SessionPolicy{}))

    if false == errors.Is(sessionErr, context.Canceled) || 1 != len(logger.levels) || loggingcontract.LevelWarning != logger.levels[0] || nil != logger.contextList[0]["bound"] {
        t.Fatalf("expected the hangup recorded once at warning naming no bound, got %v %v %v", sessionErr, logger.levels, logger.contextList)
    }
}

func TestRequestSession_ALoadCutByTheBoundServesTheEmptySessionUnderEmptyOnOutage(t *testing.T) {
    manager := newBoundedSessionManager(bound.Of(20 * time.Millisecond))
    manager.hangLoad = true

    sessionInstance, sessionErr := RequestSession(boundedLoadRequest(context.Background(), manager, &errorCountingLogger{}, true, httpcontract.SessionPolicy{EmptyOnOutage: true}))
    if nil != sessionErr {
        t.Fatalf("expected the empty session served, got %v", sessionErr)
    }

    if _, isUnavailable := session.SessionUnavailableCause(sessionInstance); false == isUnavailable {
        t.Fatalf("expected the unavailable empty session, got %v", sessionInstance)
    }
}

func TestRequestSession_TheLoadContextIsReleasedOnEveryPath(t *testing.T) {
    served := newBoundedSessionManager(bound.Of(time.Hour))
    if _, sessionErr := RequestSession(boundedLoadRequest(context.Background(), served, &errorCountingLogger{}, true, httpcontract.SessionPolicy{})); nil != sessionErr {
        t.Fatalf("unexpected load failure: %v", sessionErr)
    }

    panicking := newBoundedSessionManager(bound.Of(time.Hour))
    panicking.panicLoad = true
    func() {
        defer func() { _ = recover() }()
        _, _ = RequestSession(boundedLoadRequest(context.Background(), panicking, &errorCountingLogger{}, true, httpcontract.SessionPolicy{}))
    }()

    for name, manager := range map[string]*boundedSessionManager{"served": served, "panicking": panicking} {
        contextList := manager.recorded()
        if 0 == len(contextList) || nil == contextList[0].Err() {
            t.Fatalf("%s: expected the load context released once load returned", name)
        }
    }
}

func TestRequestSession_ARefusedStorageTimeoutFailsTheLoadUnmarked(t *testing.T) {
    manager := newBoundedSessionManager(bound.Of(time.Microsecond))

    _, sessionErr := RequestSession(boundedLoadRequest(context.Background(), manager, &errorCountingLogger{}, true, httpcontract.SessionPolicy{}))
    if nil == sessionErr || true == exception.AnswersUnavailable(sessionErr) || 0 != len(manager.recorded()) {
        t.Fatalf("expected the refused bound to fail the load unmarked before the storage, got %v", sessionErr)
    }
}

/* the wait gives up after two seconds, so a mutant that drops the bound fails instead of hanging the run */
func awaitStorageContext(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(2 * time.Second):
        return errors.New("the storage call outlived the test's budget")
    }
}

func TestStorageBoundExpired_ReadsAPassedDeadlineTheContextHasNotRecorded(t *testing.T) {
    if false == storageBoundExpired(lapsedStorageContext{Context: context.Background()}) {
        t.Fatalf("expected a deadline already passed read as expired before the context records it")
    }

    if true == storageBoundExpired(context.Background()) {
        t.Fatalf("expected a context with no deadline not expired")
    }
}

type lapsedStorageContext struct {
    context.Context
}

func (instance lapsedStorageContext) Deadline() (time.Time, bool) {
    return time.Now().Add(-time.Second), true
}
