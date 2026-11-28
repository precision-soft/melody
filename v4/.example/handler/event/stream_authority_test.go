package event

import (
    "bufio"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "slices"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v4"
    "github.com/precision-soft/melody/v4/.example/entity"
    examplesecurity "github.com/precision-soft/melody/v4/.example/security"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
)

const revisionTestInterval = 20 * time.Millisecond

const revisionTestRoute = "example.websocket"

func runStreamHandler(stream *signedInStream, revision StreamRevisionConfig, writer *recordingResponseWriter) <-chan struct{} {
    done := make(chan struct{})
    go func() {
        defer close(done)

        _, _ = StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), revision)(stream.runtimeInstance, writer, stream.request)
    }()

    return done
}

func TestOpenStreamAuthority_OpensOnTheTokenAndTheSessionAndReadsNoDirectory(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/events/stream/?topic=visitor", account, true)

    authority, refusal := OpenStreamAuthority(stream.runtimeInstance, stream.request, account.revision(time.Second))
    if nil != refusal || nil == authority {
        t.Fatalf("expected the authority opened, got the refusal %v", refusal)
    }

    if 0 != account.readCount() {
        t.Fatalf("expected the open to read no directory, read %d times", account.readCount())
    }

    reason, holdErr := authority.StillHolds(stream.runtimeInstance)
    if nil != holdErr || "" != reason {
        t.Fatalf("expected the unchanged account to hold, got %q, %v", reason, holdErr)
    }

    if 1 != account.readCount() || false == slices.Equal([]string{stream.sessionId}, account.sessionIds) {
        t.Fatalf("expected one directory read and the session asked by its id, got %d reads, sessions %v", account.readCount(), account.sessionIds)
    }
}

func TestOpenStreamAuthority_RefusesARequestWithoutASession(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/events/stream/?topic=visitor", account, false)

    authority, refusal := OpenStreamAuthority(stream.runtimeInstance, stream.request, account.revision(time.Second))
    if nil != authority || nil == refusal || nethttp.StatusUnauthorized != refusal.StatusCode() {
        t.Fatalf("expected a 401 without a session, got %v, %v", authority, refusal)
    }
}

func TestOpenStreamAuthority_RefusesARequestWithoutAnAccount(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    request, runtimeInstance := streamRequest(t, "/events/stream/?topic=visitor", false)

    authority, refusal := OpenStreamAuthority(runtimeInstance, request, account.revision(time.Second))
    if nil != authority || nil == refusal || nethttp.StatusUnauthorized != refusal.StatusCode() {
        t.Fatalf("expected a 401 without an account, got %v, %v", authority, refusal)
    }
}

func TestStreamHandler_RefusesAStreamWithoutASessionWithoutCommitting(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/events/stream/?topic=visitor", account, false)
    writer := &recordingResponseWriter{}

    response, handlerErr := StreamHandler(NewStreamSlots(StreamCapPerUser, StreamCapPerProcess), account.revision(time.Second))(stream.runtimeInstance, writer, stream.request)
    if nil != handlerErr || nil == response || nethttp.StatusUnauthorized != response.StatusCode() || true == writer.HeadersWritten() {
        t.Fatalf("expected an uncommitted 401, got %v, %v, committed %v", response, handlerErr, writer.HeadersWritten())
    }
}

func TestStreamHandler_ClosesWhenTheAccountChanges(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        change func(account *streamAccount)
        reason string
    }{
        {"roles", func(account *streamAccount) { account.user.Roles = []string{entity.RoleUser, entity.RoleEditor} }, StreamAuthorityRevisionChanged},
        {"password", func(account *streamAccount) { account.user.Password = "password-hash-2" }, StreamAuthorityRevisionChanged},
        {"deleted", func(account *streamAccount) { account.user = nil }, StreamAuthorityAccountAbsent},
        {"session", func(account *streamAccount) { account.sessionLive = false }, StreamAuthoritySessionEnded},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            account := newStreamAccount([]string{entity.RoleUser})
            stream := signedInStreamRequest(t, "/events/stream/?topic=visitor", account, true)
            writer := &recordingResponseWriter{}

            done := runStreamHandler(stream, account.revision(revisionTestInterval), writer)

            awaitReads(t, account, 2, 2*time.Second)
            select {
            case <-done:
                t.Fatal("expected the stream open while the account holds")
            default:
            }

            account.change(testCase.change)
            awaitClosed(t, done, 2*time.Second, "the stream closed once the account changed")

            if false == strings.Contains(string(writer.body), ": closed: "+testCase.reason) {
                t.Fatalf("expected the stream to end with the reason %q, wrote %q", testCase.reason, writer.body)
            }
        })
    }
}

func TestStreamHandler_ClosesWhenTheAccountCannotBeReRead(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/events/stream/?topic=visitor", account, true)
    writer := &recordingResponseWriter{}

    done := runStreamHandler(stream, account.revision(revisionTestInterval), writer)
    account.change(func(account *streamAccount) { account.readErr = errDirectoryUnavailable })
    awaitClosed(t, done, 2*time.Second, "the stream closed once the account could not be re-read")

    if false == slices.Contains(stream.logger.recorded(), "event stream closed, the account could not be re-read") {
        t.Fatalf("expected the failed re-read journaled as a warning, got %v", stream.logger.recorded())
    }

    if false == strings.Contains(string(writer.body), ": closed: "+StreamAuthorityAccountUnreadable) {
        t.Fatalf("expected the stream to end with the unreadable reason, wrote %q", writer.body)
    }
}

func TestStreamHandler_AClientLeavingMidReReadIsNotJournaledAsUnreadable(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/events/stream/?topic=visitor", account, true)
    writer := &recordingResponseWriter{}

    readStarted := make(chan struct{}, 1)
    revision := account.revision(revisionTestInterval)
    revision.LookupUser = func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
        select {
        case readStarted <- struct{}{}:
        default:
        }
        requestContext := stream.request.HttpRequest().Context()
        <-requestContext.Done()

        return nil, false, requestContext.Err()
    }

    done := runStreamHandler(stream, revision, writer)
    select {
    case <-readStarted:
    case <-time.After(2 * time.Second):
        t.Fatal("expected a re-read within 2s")
    }

    stream.cancel()
    awaitClosed(t, done, 2*time.Second, "the stream closed with its client")

    if 0 != len(stream.logger.recorded()) {
        t.Fatalf("expected no warning for a client that left, got %v", stream.logger.recorded())
    }

    if true == strings.Contains(string(writer.body), "closed:") {
        t.Fatalf("expected no closing comment to a client that left, wrote %q", writer.body)
    }
}

/* events arrive faster than the interval for five intervals; a revision tick reset by every event would never fire */
func TestStreamHandler_ChecksTheRevisionOnABusyStream(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/events/stream/?topic=visitor", account, true)
    writer := &recordingResponseWriter{}

    done := runStreamHandler(stream, account.revision(revisionTestInterval), writer)

    stopPublishing := make(chan struct{})
    publisherDone := make(chan struct{})
    go func() {
        defer close(publisherDone)

        for {
            select {
            case <-stopPublishing:
                return
            case <-time.After(2 * time.Millisecond):
                stream.hub.Broadcast(broadcastRuntime(), "visitor", melodyhttp.ServerSentEvent{Data: "busy"})
            }
        }
    }()
    defer func() {
        close(stopPublishing)
        <-publisherDone
    }()

    time.Sleep(5 * revisionTestInterval)
    account.change(func(account *streamAccount) { account.user.Roles = []string{entity.RoleAdmin} })

    awaitClosed(t, done, 2*time.Second, "the busy stream closed once the roles changed")
}

func TestStreamHandler_KeepsTheStreamWhileTheRevisionHolds(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/events/stream/?topic=visitor", account, true)
    writer := &recordingResponseWriter{}

    done := runStreamHandler(stream, account.revision(revisionTestInterval), writer)

    awaitReads(t, account, 3, 2*time.Second)

    select {
    case <-done:
        t.Fatalf("expected the stream kept open while the account holds, wrote %q", writer.body)
    default:
    }

    stream.cancel()
    awaitClosed(t, done, 2*time.Second, "the stream closed with its client")
}

func TestStreamHandler_RevisionCheckDisabledNeverReadsTheDirectory(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/events/stream/?topic=visitor", account, false)
    writer := &recordingResponseWriter{}

    disabled := account.revision(revisionTestInterval)
    disabled.Disabled = true
    done := runStreamHandler(stream, disabled, writer)

    time.Sleep(5 * revisionTestInterval)
    select {
    case <-done:
        t.Fatal("expected the stream open without a session while the re-read is off")
    default:
    }

    stream.cancel()
    awaitClosed(t, done, 2*time.Second, "the stream closed with its client")

    if 0 != account.readCount() || nethttp.StatusOK != writer.CommittedStatusCode() {
        t.Fatalf("expected a stream opened without a session and no read, got %d reads, status %d", account.readCount(), writer.CommittedStatusCode())
    }
}

func TestStreamHandler_RefusesAnIncompleteRevisionConfigurationNamingTheField(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})

    for field, revision := range map[string]StreamRevisionConfig{
        "StreamRevisionConfig.Interval":    {LookupUser: account.lookup, SessionLive: account.live},
        "StreamRevisionConfig.LookupUser":  {Interval: time.Second, SessionLive: account.live},
        "StreamRevisionConfig.SessionLive": {Interval: time.Second, LookupUser: account.lookup},
    } {
        refusedField := func() (refused any) {
            defer func() {
                if recovered, isError := recover().(*melodyexception.Error); true == isError {
                    refused = recovered.Context()["field"]
                }
            }()

            StreamHandler(NewStreamSlots(1, 1), revision)

            return nil
        }()
        if field != refusedField {
            t.Errorf("expected the configuration refused naming %s, got %v", field, refusedField)
        }
    }
}

func waitingHandler(handed *atomic.Pointer[melodyhttpcontract.Request]) melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        handed.Store(&request)
        <-request.HttpRequest().Context().Done()

        return nil, nil
    }
}

func runRevisionMiddleware(stream *signedInStream, revision StreamRevisionConfig, next melodyhttpcontract.Handler) (<-chan struct{}, *melodyhttpcontract.Response) {
    done := make(chan struct{})
    var response melodyhttpcontract.Response
    go func() {
        defer close(done)

        response, _ = StreamRevisionMiddleware(revisionTestRoute, revision)(next)(stream.runtimeInstance, httptest.NewRecorder(), stream.request)
    }()

    return done, &response
}

func TestStreamRevisionMiddleware_CancelsTheConnectionContextWhenTheRevisionMoves(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/ws?topic=visitor", account, true)
    stream.request.Attributes().Set(melodyhttp.RouteAttributeName, revisionTestRoute)

    handed := &atomic.Pointer[melodyhttpcontract.Request]{}
    done, _ := runRevisionMiddleware(stream, account.revision(revisionTestInterval), waitingHandler(handed))

    awaitReads(t, account, 2, 2*time.Second)
    select {
    case <-done:
        t.Fatal("expected the connection open while the account holds")
    default:
    }

    account.change(func(account *streamAccount) { account.user.Roles = []string{entity.RoleAdmin} })
    awaitClosed(t, done, 2*time.Second, "the connection's context cancelled once the roles changed")

    if nil != stream.request.HttpRequest().Context().Err() {
        t.Fatal("expected the request's own context left alone, the connection's alone cancelled")
    }
}

func TestStreamRevisionMiddleware_PassesEveryOtherRouteUntouched(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/products/", account, false)
    stream.request.Attributes().Set(melodyhttp.RouteAttributeName, "example.products.list")

    var handed melodyhttpcontract.Request
    next := func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        handed = request

        return nil, nil
    }

    response, _ := StreamRevisionMiddleware(revisionTestRoute, account.revision(revisionTestInterval))(next)(stream.runtimeInstance, httptest.NewRecorder(), stream.request)
    if nil != response || stream.request != handed {
        t.Fatalf("expected another route handed its own request, got %v and %v", response, handed)
    }

    time.Sleep(3 * revisionTestInterval)
    if 0 != account.readCount() {
        t.Fatalf("expected another route never re-read, read %d times", account.readCount())
    }
}

func TestStreamRevisionMiddleware_JoinsItsWatcherBeforeReturning(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/ws?topic=visitor", account, true)
    stream.request.Attributes().Set(melodyhttp.RouteAttributeName, revisionTestRoute)

    readStarted := make(chan struct{}, 1)
    var readFinished atomic.Bool
    revision := account.revision(revisionTestInterval)
    revision.LookupUser = func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
        select {
        case readStarted <- struct{}{}:
        default:
        }
        time.Sleep(100 * time.Millisecond)
        readFinished.Store(true)

        return account.lookup(runtimeInstance, userId)
    }

    next := func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        <-readStarted

        return nil, nil
    }

    _, _ = StreamRevisionMiddleware(revisionTestRoute, revision)(next)(stream.runtimeInstance, httptest.NewRecorder(), stream.request)
    if false == readFinished.Load() {
        t.Fatal("expected the middleware to return only once its watcher's re-read finished")
    }
}

func TestStreamRevisionMiddleware_RefusesBeforeTheUpgradeWithoutASession(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/ws?topic=visitor", account, false)
    stream.request.Attributes().Set(melodyhttp.RouteAttributeName, revisionTestRoute)

    handlerRan := false
    next := func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        handlerRan = true

        return nil, nil
    }

    response, _ := StreamRevisionMiddleware(revisionTestRoute, account.revision(revisionTestInterval))(next)(stream.runtimeInstance, httptest.NewRecorder(), stream.request)
    if nil == response || nethttp.StatusUnauthorized != response.StatusCode() || true == handlerRan {
        t.Fatalf("expected a 401 before the handler, got %v, handler ran %v", response, handlerRan)
    }
}

func TestStreamRevisionMiddleware_TheDecoratedRequestKeepsEveryOtherDoorOfTheRequest(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/ws?topic=visitor", account, true)
    stream.request.Attributes().Set(melodyhttp.RouteAttributeName, revisionTestRoute)
    stream.request.Attributes().Set("example.marker", "kept")

    handed := &atomic.Pointer[melodyhttpcontract.Request]{}
    done, _ := runRevisionMiddleware(stream, account.revision(time.Hour), waitingHandler(handed))

    decorated := awaitHanded(t, handed, 2*time.Second)

    marker, _ := decorated.Attributes().Get("example.marker")
    if revisionTestRoute != decorated.RouteName() || "visitor" != TopicOf(decorated) || "kept" != marker || stream.runtimeInstance != decorated.RuntimeInstance() {
        t.Fatalf("expected the route name, the query, the attributes and the runtime of the request, got %q, %q, %v", decorated.RouteName(), TopicOf(decorated), marker)
    }

    if stream.request.HttpRequest().Context() == decorated.HttpRequest().Context() || stream.request.HttpRequest().URL.String() != decorated.HttpRequest().URL.String() {
        t.Fatal("expected the same http request bound to the connection's own context")
    }

    stream.cancel()
    awaitClosed(t, done, 2*time.Second, "the connection closed with its client")
}

func TestStreamRevisionMiddleware_ClosesARealWebsocketConnectionWhenTheRevisionMoves(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/ws?topic=visitor", account, true)

    websocketHandler := melodywebsocket.NewStreamHandler(stream.hub, melodywebsocket.StreamHandlerConfig{TopicResolver: TopicOf})
    handler := StreamRevisionMiddleware(revisionTestRoute, account.revision(revisionTestInterval))(websocketHandler)

    signedInSession, sessionErr := melodyhttp.RequestSession(stream.request)
    if nil != sessionErr {
        t.Fatalf("reading the signed-in session failed: %v", sessionErr)
    }

    handlerDone := make(chan struct{})
    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, httpRequest *nethttp.Request) {
        defer close(handlerDone)

        request := melodyhttp.NewRequest(httpRequest, stream.runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("websocket-revision", time.Now())})
        request.Attributes().Set(melodyhttp.RouteAttributeName, revisionTestRoute)
        _ = melodyhttp.SetRequestSession(request, signedInSession)

        _, _ = handler(stream.runtimeInstance, writer, request)
    }))
    t.Cleanup(server.Close)

    connection, dialErr := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
    if nil != dialErr {
        t.Fatalf("dialing the server failed: %v", dialErr)
    }
    defer connection.Close()

    _, _ = connection.Write([]byte("GET /ws?topic=visitor HTTP/1.1\r\nHost: " + strings.TrimPrefix(server.URL, "http://") + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"))
    reader := bufio.NewReader(connection)
    statusLine, _ := reader.ReadString('\n')
    if false == strings.Contains(statusLine, "101") {
        t.Fatalf("expected the upgrade answered 101, got %q", statusLine)
    }

    awaitReads(t, account, 2, 2*time.Second)
    account.change(func(account *streamAccount) { account.user.Roles = []string{entity.RoleAdmin} })

    awaitClosed(t, handlerDone, 3*time.Second, "the websocket handler returned once the roles changed")
}

func TestOpenStreamAuthority_RefusesASessionWithoutAPasswordVersion(t *testing.T) {
    for name, version := range map[string]any{"empty": "", "not a text": 42} {
        account := newStreamAccount([]string{entity.RoleUser})
        stream := signedInStreamRequest(t, "/events/stream/?topic=visitor", account, true)
        sessionInstance, sessionErr := melodyhttp.RequestSession(stream.request)
        if nil != sessionErr {
            t.Fatalf("reading the session failed: %v", sessionErr)
        }
        sessionInstance.Set(examplesecurity.SessionKeySecurityCredentialVersion, version)

        authority, refusal := OpenStreamAuthority(stream.runtimeInstance, stream.request, account.revision(time.Second))
        if nil != authority || nil == refusal || nethttp.StatusUnauthorized != refusal.StatusCode() {
            t.Errorf("expected a 401 for a password version %s, got %v, %v", name, authority, refusal)
        }
    }
}

func TestStreamRevisionMiddleware_DisabledPassesTheConnectionUntouched(t *testing.T) {
    for name, withSession := range map[string]bool{"signed in": true, "without a session": false} {
        t.Run(name, func(t *testing.T) {
            account := newStreamAccount([]string{entity.RoleUser})
            stream := signedInStreamRequest(t, "/ws?topic=visitor", account, withSession)
            stream.request.Attributes().Set(melodyhttp.RouteAttributeName, revisionTestRoute)

            disabled := account.revision(0)
            disabled.Disabled = true

            var handed melodyhttpcontract.Request
            next := func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
                handed = request

                return nil, nil
            }

            response, _ := StreamRevisionMiddleware(revisionTestRoute, disabled)(next)(stream.runtimeInstance, httptest.NewRecorder(), stream.request)
            if nil != response || stream.request != handed {
                t.Fatalf("expected the connection handed its own request, got %v and %v", response, handed)
            }

            if 0 != account.readCount() {
                t.Fatalf("expected no re-read while the re-read is off, read %d times", account.readCount())
            }
        })
    }
}

func TestStreamRevisionMiddleware_APanickingReReadEndsTheConnectionAlone(t *testing.T) {
    account := newStreamAccount([]string{entity.RoleUser})
    stream := signedInStreamRequest(t, "/ws?topic=visitor", account, true)
    stream.request.Attributes().Set(melodyhttp.RouteAttributeName, revisionTestRoute)

    revision := account.revision(revisionTestInterval)
    revision.LookupUser = func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
        panic("the directory panicked")
    }

    handed := &atomic.Pointer[melodyhttpcontract.Request]{}
    done, _ := runRevisionMiddleware(stream, revision, waitingHandler(handed))
    awaitClosed(t, done, 2*time.Second, "the connection's context cancelled once the re-read panicked")

    if false == slices.Contains(stream.logger.recordedErrors(), "event stream watcher panicked") {
        t.Fatalf("expected the panic journaled as an error, got %v", stream.logger.recordedErrors())
    }

    if nil != stream.request.HttpRequest().Context().Err() {
        t.Fatal("expected the request's own context left alone, the connection's alone cancelled")
    }
}
