package event

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "slices"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    examplesecurity "github.com/precision-soft/melody/v4/.example/security"
    "github.com/precision-soft/melody/v4/.example/subscriber"
    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
)

/* routedRequestAs is a request the router matched to the route named, for a caller the firewall authenticated with the roles given */
func routedRequestAs(t *testing.T, routeName string, target string, roles []string) (*melodyhttp.Request, melodycontainercontract.Runtime) {
    t.Helper()

    request, runtimeInstance := streamRequestAs(t, target, roles)
    request.Attributes().Set(melodyhttp.RouteAttributeName, routeName)

    return request, runtimeInstance
}

/* revisionDisabled is the configuration of the pins about everything but the revision */
var revisionDisabled = StreamRevisionConfig{Disabled: true}

const streamAccountPasswordHash = "password-hash-1"

var errDirectoryUnavailable = errors.New("the directory is unavailable")

/* streamAccount is the directory and the session storage an open stream's revision re-reads, each changeable while the stream is open */
type streamAccount struct {
    mutex       sync.Mutex
    user        *entity.User
    sessionLive bool
    readErr     error
    reads       int
    sessionIds  []string
}

func newStreamAccount(roles []string) *streamAccount {
    return &streamAccount{
        user:        entity.NewUser("reader-1", "reader", streamAccountPasswordHash, roles),
        sessionLive: true,
    }
}

func (instance *streamAccount) lookup(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.reads = instance.reads + 1

    if nil != instance.readErr {
        return nil, false, instance.readErr
    }

    if nil == instance.user || userId != instance.user.Id {
        return nil, false, nil
    }

    return entity.NewUser(instance.user.Id, instance.user.Username, instance.user.Password, slices.Clone(instance.user.Roles)), true, nil
}

func (instance *streamAccount) live(runtimeInstance melodycontainercontract.Runtime, sessionId string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.sessionIds = append(instance.sessionIds, sessionId)

    return instance.sessionLive, nil
}

func (instance *streamAccount) change(apply func(account *streamAccount)) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    apply(instance)
}

func (instance *streamAccount) readCount() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.reads
}

func (instance *streamAccount) revision(interval time.Duration) StreamRevisionConfig {
    return StreamRevisionConfig{Interval: interval, LookupUser: instance.lookup, SessionLive: instance.live}
}

/* signedInStream is a stream request the main firewall authenticated from a signed-in session of the account, with the hub and a recording logger on its container, and the cancel of its request context */
type signedInStream struct {
    request         *melodyhttp.Request
    runtimeInstance melodycontainercontract.Runtime
    hub             *melodyhttp.ServerSentEventHub
    logger          *warningRecordingLogger
    cancel          context.CancelFunc
    sessionId       string
}

func signedInStreamRequest(t *testing.T, target string, account *streamAccount, withSession bool) *signedInStream {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    logger := &warningRecordingLogger{Logger: melodylogging.NewNopLogger()}

    melodycontainer.MustRegister(
        containerInstance,
        subscriber.ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
            return hub, nil
        },
        melodycontainer.RegisterOptions{},
    )
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
        melodycontainer.RegisterOptions{},
    )

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())
    authenticateOnRuntime(runtimeInstance, account.user.Id, account.user.Roles)

    requestContext, cancel := context.WithCancel(context.Background())
    t.Cleanup(cancel)

    request := melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodGet, target, nil).WithContext(requestContext),
        runtimeInstance,
        melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("stream-revision-test", time.Now())},
    )

    stream := &signedInStream{request: request, runtimeInstance: runtimeInstance, hub: hub, logger: logger, cancel: cancel}

    if true == withSession {
        manager := melodysession.NewManager(melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{}), melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)})
        sessionInstance, sessionErr := manager.NewSession(runtimeInstance)
        if nil != sessionErr {
            t.Fatalf("opening the session failed: %v", sessionErr)
        }

        sessionInstance.Set(examplesecurity.SessionKeySecurityCredentialVersion, examplesecurity.SessionCredentialVersion(account.user.Password))
        if setErr := melodyhttp.SetRequestSession(request, sessionInstance); nil != setErr {
            t.Fatalf("publishing the session failed: %v", setErr)
        }

        stream.sessionId = sessionInstance.Id()
    }

    return stream
}

/* awaitClosed waits for a handler run in the background to return */
func awaitClosed(t *testing.T, done <-chan struct{}, within time.Duration, what string) {
    t.Helper()

    select {
    case <-done:
    case <-time.After(within):
        t.Fatalf("expected %s within %s", what, within)
    }
}
