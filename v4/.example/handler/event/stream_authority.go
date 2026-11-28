package event

import (
    "context"
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    examplejournal "github.com/precision-soft/melody/v4/.example/journal"
    "github.com/precision-soft/melody/v4/.example/presenter"
    examplesecurity "github.com/precision-soft/melody/v4/.example/security"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

const (
    /* StreamAuthorityAccountAbsent ends a stream whose account was deleted. */
    StreamAuthorityAccountAbsent = "account-absent"

    /* StreamAuthorityRevisionChanged ends a stream whose account's roles or password changed. */
    StreamAuthorityRevisionChanged = "revision-changed"

    /* StreamAuthoritySessionEnded ends a stream whose session was signed out or ended by the account's cap. */
    StreamAuthoritySessionEnded = "session-ended"

    /* StreamAuthorityAccountUnreadable ends a stream whose account or session could not be re-read. */
    StreamAuthorityAccountUnreadable = "account-unreadable"
)

/* SessionLiveCheck answers whether the session storage still holds the session named. */
type SessionLiveCheck func(runtimeInstance melodycontainercontract.Runtime, sessionId string) (bool, error)

/* StreamRevisionConfig is how the two stream doors re-read the account an open stream was authorised on. The zero Disabled keeps the re-read on; Interval, LookupUser and SessionLive are required while it is. */
type StreamRevisionConfig struct {
    /* Disabled turns the re-read off: a stream then outlives a change to its account until it closes on its own */
    Disabled bool

    Interval time.Duration

    LookupUser examplesecurity.SessionUserLookup

    SessionLive SessionLiveCheck
}

/* mustBeComplete refuses, at registration, a configuration that keeps the re-read on without what it re-reads with */
func (instance StreamRevisionConfig) mustBeComplete(door string) {
    if true == instance.Disabled {
        return
    }

    field := ""
    switch {
    case 0 >= instance.Interval:
        field = "StreamRevisionConfig.Interval"
    case nil == instance.LookupUser:
        field = "StreamRevisionConfig.LookupUser"
    case nil == instance.SessionLive:
        field = "StreamRevisionConfig.SessionLive"
    default:
        return
    }

    melodyexception.Panic(melodyexception.NewError(
        "the stream revision configuration is incomplete",
        melodyexceptioncontract.Context{"door": door, "field": field},
        nil,
    ))
}

/* StreamAuthority is what an open stream was authorised on: the account, its revision and the session, opened from the token and the session the request was authenticated with, so opening it reads no directory. */
type StreamAuthority struct {
    userId      string
    revision    string
    sessionId   string
    lookupUser  examplesecurity.SessionUserLookup
    sessionLive SessionLiveCheck
}

/* OpenStreamAuthority opens the authority of the request's stream, or answers the 401 of a request that carries no account or no signed-in session. */
func OpenStreamAuthority(
    runtimeInstance melodycontainercontract.Runtime,
    request melodyhttpcontract.Request,
    revision StreamRevisionConfig,
) (*StreamAuthority, melodyhttpcontract.Response) {
    token, exists := examplesecurity.TokenFromRuntime(runtimeInstance)
    if false == exists || false == token.IsAuthenticated() || "" == token.UserIdentifier() {
        return nil, presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "the event stream needs a signed-in session")
    }

    sessionInstance, sessionErr := melodyhttp.RequestSession(request)
    if nil != sessionErr {
        return nil, presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "the event stream needs a signed-in session")
    }

    credentialVersion, isText := sessionInstance.Get(examplesecurity.SessionKeySecurityCredentialVersion).(string)
    if false == isText || "" == credentialVersion {
        return nil, presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "the event stream needs a signed-in session")
    }

    return &StreamAuthority{
        userId:      token.UserIdentifier(),
        revision:    examplesecurity.AccountRevision(token.UserIdentifier(), credentialVersion, token.Roles()),
        sessionId:   sessionInstance.Id(),
        lookupUser:  revision.LookupUser,
        sessionLive: revision.SessionLive,
    }, nil
}

func (instance *StreamAuthority) UserId() string {
    return instance.userId
}

/* StillHolds re-reads the account and the session, uncached, and answers why the authority ended, empty while it holds; an error is a read that failed, which ends the stream too. */
func (instance *StreamAuthority) StillHolds(runtimeInstance melodycontainercontract.Runtime) (string, error) {
    user, found, lookupErr := instance.lookupUser(runtimeInstance, instance.userId)
    if nil != lookupErr {
        return "", lookupErr
    }

    if false == found || nil == user {
        return StreamAuthorityAccountAbsent, nil
    }

    if instance.revision != currentRevisionOf(user) {
        return StreamAuthorityRevisionChanged, nil
    }

    live, liveErr := instance.sessionLive(runtimeInstance, instance.sessionId)
    if nil != liveErr {
        return "", liveErr
    }

    if false == live {
        return StreamAuthoritySessionEnded, nil
    }

    return "", nil
}

func currentRevisionOf(user *entity.User) string {
    return examplesecurity.AccountRevision(user.Id, examplesecurity.SessionCredentialVersion(user.Password), user.Roles)
}

/* streamClientLeft ends a stream whose re-read failed because its connection had already ended: nothing is journaled and nothing is written to a client that is gone */
const streamClientLeft = "client-left"

/* checkStreamAuthority asks the authority once and journals the end it answers; it answers the reason the stream ends with, empty while it holds */
func checkStreamAuthority(runtimeInstance melodycontainercontract.Runtime, connectionContext context.Context, authority *StreamAuthority, topic string) string {
    reason, holdErr := authority.StillHolds(runtimeInstance)
    logger := examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger())

    /* a read cut by the connection's end answers the context's error, which is no outage of the directory */
    if nil != holdErr && nil != connectionContext.Err() {
        return streamClientLeft
    }

    if nil != holdErr {
        logger.Warning(
            "event stream closed, the account could not be re-read",
            melodyexception.LogContext(holdErr, melodyexception.LogContextOptions{Extra: melodyexceptioncontract.Context{"userId": authority.UserId(), "topic": topic}}),
        )

        return StreamAuthorityAccountUnreadable
    }

    if "" != reason {
        logger.Info(
            "event stream closed, the account's authority ended",
            melodyloggingcontract.Context{"userId": authority.UserId(), "topic": topic, "reason": reason},
        )
    }

    return reason
}

/* StreamRevisionMiddleware re-reads, on the route named, the account of a connection the websocket integration's handler streams, which this application does not write: it opens the authority before the upgrade, hands the handler a request whose context it cancels when the authority ends, and joins its watcher before it returns, so nothing reads the request scope after the kernel closes it. It stands after the slot count. Every other route passes through untouched. */
func StreamRevisionMiddleware(routeName string, revision StreamRevisionConfig) melodyhttpcontract.Middleware {
    revision.mustBeComplete("handlerevent.StreamRevisionMiddleware")

    return func(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
        return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            if routeName != request.RouteName() || true == revision.Disabled {
                return next(runtimeInstance, writer, request)
            }

            authority, refusal := OpenStreamAuthority(runtimeInstance, request, revision)
            if nil != refusal {
                return refusal, nil
            }

            watchContext, cancel := context.WithCancel(request.HttpRequest().Context())
            watcherDone := make(chan struct{})
            go func() {
                defer close(watcherDone)
                /* the re-read runs application functions on a goroutine of its own, where a panic would end the process: it ends this connection alone, as a failed re-read does */
                defer func() {
                    if recovered := recover(); nil != recovered {
                        examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Error(
                            "event stream watcher panicked",
                            melodyloggingcontract.Context{"userId": authority.UserId(), "topic": TopicOf(request), "panic": recovered},
                        )
                        cancel()
                    }
                }()

                watchStreamAuthority(runtimeInstance, watchContext, cancel, authority, revision.Interval, TopicOf(request))
            }()
            defer func() {
                cancel()
                <-watcherDone
            }()

            return next(runtimeInstance, writer, &watchedStreamRequest{Request: request, httpRequest: request.HttpRequest().WithContext(watchContext)})
        }
    }
}

func watchStreamAuthority(
    runtimeInstance melodycontainercontract.Runtime,
    watchContext context.Context,
    cancel context.CancelFunc,
    authority *StreamAuthority,
    interval time.Duration,
    topic string,
) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-watchContext.Done():
            return
        case <-ticker.C:
            if "" != checkStreamAuthority(runtimeInstance, watchContext, authority, topic) {
                cancel()

                return
            }
        }
    }
}

/* watchedStreamRequest is the request with its http request bound to the watcher's context; every other door answers the original's */
type watchedStreamRequest struct {
    melodyhttpcontract.Request
    httpRequest *nethttp.Request
}

func (instance *watchedStreamRequest) HttpRequest() *nethttp.Request {
    return instance.httpRequest
}
