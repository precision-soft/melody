package main

import (
    "io"
    nethttp "net/http"
    "net/http/cookiejar"
    "net/http/httptest"
    "net/url"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/session"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

const (
    sessionCartKey     = "cart"
    sessionCartValue   = "SKU-1"
    sessionIdentityKey = "userId"
    sessionIdentity    = "u-1"
)

/* sessionProbeSeparator joins the fields the probe routes report back. Every field is a session id or a value
this harness itself wrote, so none of them can contain it. */
const sessionProbeSeparator = "|"

/* runSessionRotationCheck drives http.RegenerateRequestSession over a real loopback http server, through a
real kernel and a real on-disk session store, with a cookie-jar client standing in for the browser.

It runs in process rather than against the .example application because no example route rotates a session and
the harness may not add one: driving it over EXAMPLE_BASE_URL would mean reshaping an application this task
puts out of bounds, and the surface would otherwise stay unexercised end to end. Nothing that matters is
faked, though — the kernel's own session load and save path runs, the store is a file the assertions read back
from disk, and the client keeps only what the responses told it to keep.

Three properties are asserted: the rotation changes the id, carries the values over and advertises the new id
to the client; the id the client used to hold is gone from storage and buys nothing when presented again; and a
rotation whose result is NOT republished on the request logs the client out cleanly, with a clearing cookie,
rather than leaving it presenting an id the store no longer has. This section needs no external backend, so it
always runs. */
func runSessionRotationCheck() {
    storageDirectory, storageDirectoryErr := os.MkdirTemp("", "melody-e2e-session")
    if nil != storageDirectoryErr {
        fail("session rotation: temporary directory: %v", storageDirectoryErr)
    }
    /* fail() exits the process, so a deferred cleanup never runs on the failing path; the hook is what removes the store when an assertion below gives up */
    removeStorageOnFailure := pushFailureCleanup(func() {
        _ = os.RemoveAll(storageDirectory)
    })
    defer removeStorageOnFailure()
    defer os.RemoveAll(storageDirectory)

    storagePath := filepath.Join(storageDirectory, "sessions.json")

    storage, storageErr := session.NewFileStorageFromPath(storagePath)
    if nil != storageErr {
        fail("session rotation: file storage: %v", storageErr)
    }

    closeStorageOnFailure := pushFailureCleanup(func() {
        _ = storage.Close()
    })
    defer closeStorageOnFailure()
    defer storage.Close()

    serviceContainer := newSessionServiceContainer(storage, storageDirectory)
    sessionManager := session.SessionMustFromContainer(serviceContainer)

    router := melodyhttp.NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/login",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            currentSession := requestSession(request)
            currentSession.Set(sessionCartKey, sessionCartValue)

            if saveErr := sessionManager.SaveSession(currentSession); nil != saveErr {
                return nil, saveErr
            }

            previousId := currentSession.Id()

            rotated, rotateErr := melodyhttp.RegenerateRequestSession(request)
            if nil != rotateErr {
                return nil, rotateErr
            }

            rotated.Set(sessionIdentityKey, sessionIdentity)

            return sessionProbeResponse(previousId, rotated.Id(), rotated.String(sessionCartKey)), nil
        },
    )

    router.Handle(
        nethttp.MethodGet,
        "/whoami",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            currentSession := requestSession(request)

            return sessionProbeResponse(
                currentSession.Id(),
                currentSession.String(sessionIdentityKey),
                currentSession.String(sessionCartKey),
            ), nil
        },
    )

    router.Handle(
        nethttp.MethodGet,
        "/rotate-without-publishing",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            currentSession := requestSession(request)

            abandonedId := currentSession.Id()

            /* the storage-level primitive, and the result deliberately dropped instead of republished on the
               request: the fail-safe under test is what the response path does with the session the kernel is
               still holding once the rotation has marked it cleared */
            rotated, rotateErr := sessionManager.RegenerateSession(currentSession)
            if nil != rotateErr {
                return nil, rotateErr
            }

            return sessionProbeResponse(abandonedId, rotated.Id()), nil
        },
    )

    server := httptest.NewServer(melodyhttp.NewKernel(router).ServeHttp(serviceContainer))
    defer server.Close()

    serverUrl, serverUrlErr := url.Parse(server.URL)
    if nil != serverUrlErr {
        fail("session rotation: parse the server url: %v", serverUrlErr)
    }

    jar, jarErr := cookiejar.New(nil)
    if nil != jarErr {
        fail("session rotation: cookie jar: %v", jarErr)
    }

    browser := &nethttp.Client{Jar: jar, Timeout: 10 * time.Second}

    /* (1) the rotation changes the id, carries the pre-rotation values over, and the response advertises the
       new id — a rotation the client is never told about would destroy the id it holds and nothing else */
    _, loginFields := sessionProbe(browser, server.URL+"/login", "")
    if 3 != len(loginFields) {
        fail("session rotation: the login probe reported %d fields, wanted 3", len(loginFields))
    }

    previousId, rotatedId, carriedCart := loginFields[0], loginFields[1], loginFields[2]

    if previousId == rotatedId {
        fail("session rotation: the rotation left the session id unchanged (%q)", rotatedId)
    }
    if sessionCartValue != carriedCart {
        fail("session rotation: the value written before the rotation was not carried over — wanted %q, got %q", sessionCartValue, carriedCart)
    }

    advertisedId := jarSessionId(jar, serverUrl)
    if rotatedId != advertisedId {
        fail("session rotation: the response advertised %q, wanted the rotated id %q", advertisedId, rotatedId)
    }
    pass("(1) rotation changed the session id, carried %q over and advertised the rotated id to the client", sessionCartKey)

    /* (2) the entry the previous id pointed at is gone from the store on disk, and the rotated one holds the
       identity the handler wrote after the rotation */
    if nil != sessionManager.Session(previousId) {
        fail("session rotation: the pre-rotation session %q is still in storage", previousId)
    }
    if true == strings.Contains(readSessionStore(storagePath), previousId) {
        fail("session rotation: the pre-rotation id %q is still written in the store at %s", previousId, storagePath)
    }

    rotatedFromStore := sessionManager.Session(rotatedId)
    if nil == rotatedFromStore {
        fail("session rotation: the rotated session %q was never stored", rotatedId)
    }
    if sessionIdentity != rotatedFromStore.String(sessionIdentityKey) {
        fail("session rotation: the identity written after the rotation was not stored, got %q", rotatedFromStore.String(sessionIdentityKey))
    }
    pass("(2) the pre-rotation entry is gone from the store on disk and the rotated one holds the identity")

    /* (3) a client presenting the destroyed id gets a fresh anonymous session, not the one it used to name */
    _, staleFields := sessionProbe(&nethttp.Client{Timeout: 10 * time.Second}, server.URL+"/whoami", previousId)
    if previousId == staleFields[0] {
        fail("session rotation: the destroyed id %q was accepted again", previousId)
    }
    if "" != staleFields[1] {
        fail("session rotation: the destroyed id resurrected the identity %q", staleFields[1])
    }
    if "" != staleFields[2] {
        fail("session rotation: the destroyed id resurrected the value %q", staleFields[2])
    }
    pass("(3) presenting the destroyed id yielded a fresh anonymous session %q", staleFields[0])

    /* (4) the fail-safe: a rotation that is not republished on the request must clear the client's cookie, not
       leave it presenting an id the rotation already deleted */
    unpublishedResponse, unpublishedFields := sessionProbe(browser, server.URL+"/rotate-without-publishing", "")
    if 2 != len(unpublishedFields) {
        fail("session rotation: the unpublished-rotation probe reported %d fields, wanted 2", len(unpublishedFields))
    }

    abandonedId, unpublishedId := unpublishedFields[0], unpublishedFields[1]

    if rotatedId != abandonedId {
        fail("session rotation: the unpublished rotation abandoned %q, wanted the logged-in id %q", abandonedId, rotatedId)
    }

    clearingCookie := responseSessionCookie(unpublishedResponse)
    if nil == clearingCookie {
        fail("session rotation: the unpublished rotation emitted no session cookie at all — the client is left presenting a deleted id")
    }
    if "" != clearingCookie.Value || 0 <= clearingCookie.MaxAge {
        fail("session rotation: wanted a clearing session cookie, got value=%q MaxAge=%d", clearingCookie.Value, clearingCookie.MaxAge)
    }
    if "" != jarSessionId(jar, serverUrl) {
        fail("session rotation: the client still holds a session cookie after the clearing response")
    }
    if nil != sessionManager.Session(abandonedId) {
        fail("session rotation: the abandoned session %q survived in storage", abandonedId)
    }
    if nil != sessionManager.Session(unpublishedId) {
        fail("session rotation: the unpublished rotated session %q was stored even though it was never published", unpublishedId)
    }
    pass("(4) an unpublished rotation cleared the client's cookie and stored neither id")

    /* (5) and the client is therefore logged out cleanly: a fresh anonymous session on its next request */
    _, loggedOutFields := sessionProbe(browser, server.URL+"/whoami", "")
    if abandonedId == loggedOutFields[0] || unpublishedId == loggedOutFields[0] {
        fail("session rotation: the logged-out client came back under the deleted id %q", loggedOutFields[0])
    }
    if "" != loggedOutFields[1] {
        fail("session rotation: the logged-out client is still carrying the identity %q", loggedOutFields[1])
    }
    pass("(5) the logged-out client came back anonymous under a fresh id instead of a deleted one")
}

func sessionProbeResponse(fields ...string) httpcontract.Response {
    return melodyhttp.TextResponse(nethttp.StatusOK, strings.Join(fields, sessionProbeSeparator))
}

/* sessionProbe calls one of the probe routes and returns both the raw response — the Set-Cookie header is an
assertion of its own — and the fields the route reported. */
func sessionProbe(client *nethttp.Client, requestUrl string, presentedSessionId string) (*nethttp.Response, []string) {
    request, requestErr := nethttp.NewRequest(nethttp.MethodGet, requestUrl, nil)
    if nil != requestErr {
        fail("session rotation: build the request for %s: %v", requestUrl, requestErr)
    }

    if "" != presentedSessionId {
        request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: presentedSessionId})
    }

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("session rotation: %s: %v", requestUrl, responseErr)
    }
    defer response.Body.Close()

    body, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        fail("session rotation: read %s: %v", requestUrl, readErr)
    }

    if nethttp.StatusOK != response.StatusCode {
        fail("session rotation: %s returned %d (%s)", requestUrl, response.StatusCode, body)
    }

    return response, strings.Split(string(body), sessionProbeSeparator)
}

func requestSession(request httpcontract.Request) sessioncontract.Session {
    attributeValue, exists := request.Attributes().Get(melodyhttp.RequestAttributeSession)
    if false == exists {
        fail("session rotation: the request carries no session")
    }

    sessionInstance, isSession := attributeValue.(sessioncontract.Session)
    if false == isSession {
        fail("session rotation: the session attribute holds a %T", attributeValue)
    }

    return sessionInstance
}

func responseSessionCookie(response *nethttp.Response) *nethttp.Cookie {
    for _, cookie := range response.Cookies() {
        if session.SessionCookieName == cookie.Name {
            return cookie
        }
    }

    return nil
}

func jarSessionId(jar nethttp.CookieJar, serverUrl *url.URL) string {
    for _, cookie := range jar.Cookies(serverUrl) {
        if session.SessionCookieName == cookie.Name {
            return cookie.Value
        }
    }

    return ""
}

func readSessionStore(path string) string {
    content, readErr := os.ReadFile(path)
    if nil != readErr {
        fail("session rotation: read the session store at %s: %v", path, readErr)
    }

    return string(content)
}

/* newSessionServiceContainer registers the four services the kernel resolves while it serves a request: the
logger, the configuration, the session manager over the given storage, and the event dispatcher. */
func newSessionServiceContainer(storage sessioncontract.Storage, projectDirectory string) containercontract.Container {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
    )

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            environment, environmentErr := config.NewEnvironment(&staticEnvironmentSource{
                values: map[string]string{config.EnvKey: config.EnvDevelopment},
            })
            if nil != environmentErr {
                return nil, environmentErr
            }

            return config.NewConfiguration(environment, projectDirectory)
        },
    )

    serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return session.NewManager(storage, 30*time.Minute), nil
        },
    )

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return event.NewEventDispatcher(clock.NewSystemClock()), nil
        },
    )

    return serviceContainer
}

type staticEnvironmentSource struct {
    values map[string]string
}

func (instance *staticEnvironmentSource) Load() (map[string]string, error) {
    copied := make(map[string]string, len(instance.values))

    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}
