package main

import (
    "errors"
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

    /* the body the in-flight handler produces, so the assertion can tell the handler's own response apart from the 500 a storage outage would substitute */
    sessionHandlerBody = "handler-body"
)

/* sessionProbeSeparator joins the fields the probe routes report back. Every field is a session id or a value this harness itself wrote, so none of them can contain it. */
const sessionProbeSeparator = "|"

/* runSessionRotationCheck drives http.RegenerateRequestSession over a real loopback http server, through a real kernel and a real on-disk session store, with a cookie-jar client standing in for the browser.

   It runs in process rather than against the .example application, and stays that way now that the login doors of the two published majors do rotate: the fixation window a rotation closes cannot be OPENED from outside those applications. No example route writes to the session before authentication, and the cookie is emitted only for a session that was modified, so a client never reaches the login holding an id of its own; a forged one is not adopted either, since the manager answers an unknown id with a fresh session under an id of its own. A probe over EXAMPLE_BASE_URL would therefore pass just as readily with the rotation removed. Nothing that matters is faked here, though — the kernel's own session load and save path runs, the store is a file the assertions read back from disk, and the client keeps only what the responses told it to keep.

   Three properties are asserted: the rotation changes the id, carries the values over and advertises the new id to the client; the id the client used to hold is gone from storage and buys nothing when presented again; and a rotation whose result is NOT republished on the request logs the client out cleanly, with a clearing cookie, rather than leaving it presenting an id the store no longer has. This section needs no external backend, so it always runs. */
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

            /* the storage-level primitive, and the result deliberately dropped instead of republished on the request: the fail-safe under test is what the response path does with the session the kernel is still holding once the rotation has marked it cleared */
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

    /* (1) the rotation changes the id, carries the pre-rotation values over, and the response advertises the new id — a rotation the client is never told about would destroy the id it holds and nothing else */
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

    /* (2) the entry the previous id pointed at is gone from the store on disk, and the rotated one holds the identity the handler wrote after the rotation */
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

    /* (4) the fail-safe: a rotation that is not republished on the request must clear the client's cookie, not leave it presenting an id the rotation already deleted */
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

/* sessionProbe calls one of the probe routes and returns both the raw response — the Set-Cookie header is an assertion of its own — and the fields the route reported. */
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

/* newSessionServiceContainer registers the four services the kernel resolves while it serves a request: the logger, the configuration, the session manager over the given storage, and the event dispatcher. */
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
            return session.NewManagerOwningStorage(storage, 30*time.Minute), nil
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

/* runSessionTombstoneCheck drives the write-back refusal over a real loopback http server, with two CONCURRENT requests naming the same session — which is the only shape the guarantee has: one request loads the session, a second one ends it, and the first one then tries to save what it loaded. Nothing here is faked; the two requests run through the same kernel, against the same on-disk store, and the barrier only decides the interleaving the guarantee is written for instead of waiting for it to happen by luck.

   The reason this needs a live section at all is that mutation cannot reach it: the refusal is a guard against a RACE, so a mutant that moves the check outside the critical section survives every sequential run. Measured before it was written, the guarantee had no live check on any major — the section above probes only the rotation.

   What is asserted is STATE, never the framework's word about itself: the error the held save answers, the bytes of the session file read back from disk, the Set-Cookie the client actually received, and the status of the response the handler produced. */
func runSessionTombstoneCheck() {
    storageDirectory, storageDirectoryErr := os.MkdirTemp("", "melody-e2e-tombstone")
    if nil != storageDirectoryErr {
        fail("session tombstone: temporary directory: %v", storageDirectoryErr)
    }
    removeStorageOnFailure := pushFailureCleanup(func() {
        _ = os.RemoveAll(storageDirectory)
    })
    defer removeStorageOnFailure()
    defer os.RemoveAll(storageDirectory)

    storagePath := filepath.Join(storageDirectory, "sessions.json")

    storage, storageErr := session.NewFileStorageFromPath(storagePath)
    if nil != storageErr {
        fail("session tombstone: file storage: %v", storageErr)
    }

    closeStorageOnFailure := pushFailureCleanup(func() {
        _ = storage.Close()
    })
    defer closeStorageOnFailure()
    defer storage.Close()

    serviceContainer := newSessionServiceContainer(storage, storageDirectory)
    sessionManager := session.SessionMustFromContainer(serviceContainer)

    /* the barrier: the in-flight handler reports the id it loaded and then waits, so the harness can land the ending request in the window the guarantee is about instead of racing for it */
    inFlightLoaded := make(chan string, 1)
    sessionEnded := make(chan struct{}, 2)

    router := melodyhttp.NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/start",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            currentSession := requestSession(request)
            currentSession.Set(sessionIdentityKey, sessionIdentity)

            if saveErr := sessionManager.SaveSession(currentSession); nil != saveErr {
                return nil, saveErr
            }

            return sessionProbeResponse(currentSession.Id()), nil
        },
    )

    router.Handle(
        nethttp.MethodGet,
        "/hold-then-save",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            currentSession := requestSession(request)
            currentSession.Set(sessionCartKey, sessionCartValue)

            inFlightLoaded <- currentSession.Id()
            <-sessionEnded

            /* the save the whole guarantee is about: the session this request loaded was ended under it while it was running, and the write must be refused rather than re-creating the entry with the pre-logout identity intact */
            saveErr := sessionManager.SaveSession(currentSession)

            refusal := "not-refused"
            if true == errors.Is(saveErr, session.ErrSessionDeleted) {
                refusal = "refused-as-deleted"
            } else if nil != saveErr {
                refusal = "refused-as-" + saveErr.Error()
            }

            return sessionProbeResponse(currentSession.Id(), refusal, sessionHandlerBody), nil
        },
    )

    router.Handle(
        nethttp.MethodGet,
        "/logout",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            currentSession := requestSession(request)
            endedId := currentSession.Id()

            currentSession.Clear()

            return sessionProbeResponse(endedId), nil
        },
    )

    router.Handle(
        nethttp.MethodGet,
        "/rotate",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            currentSession := requestSession(request)
            previousId := currentSession.Id()

            rotated, rotateErr := melodyhttp.RegenerateRequestSession(request)
            if nil != rotateErr {
                return nil, rotateErr
            }

            return sessionProbeResponse(previousId, rotated.Id()), nil
        },
    )

    server := httptest.NewServer(melodyhttp.NewKernel(router).ServeHttp(serviceContainer))
    defer server.Close()

    anonymous := func() *nethttp.Client {
        return &nethttp.Client{Timeout: 10 * time.Second}
    }

    /* (1) a save held open across a concurrent delete is refused, and refused by NAME: the cause is session.ErrSessionDeleted, which is what lets the response path tell the session ending apart from a storage outage */
    _, startFields := sessionProbe(anonymous(), server.URL+"/start", "")
    liveId := startFields[0]

    heldResponse := make(chan *nethttp.Response, 1)
    heldFields := make(chan []string, 1)

    go func() {
        response, fields := sessionProbe(anonymous(), server.URL+"/hold-then-save", liveId)
        heldResponse <- response
        heldFields <- fields
    }()

    inFlightId := <-inFlightLoaded
    if liveId != inFlightId {
        fail("session tombstone: the in-flight request loaded %q, wanted the live session %q", inFlightId, liveId)
    }

    _, logoutFields := sessionProbe(anonymous(), server.URL+"/logout", liveId)
    if liveId != logoutFields[0] {
        fail("session tombstone: the logout ended %q, wanted %q", logoutFields[0], liveId)
    }

    sessionEnded <- struct{}{}

    inFlightResponse := <-heldResponse
    inFlightFields := <-heldFields

    if 3 != len(inFlightFields) {
        fail("session tombstone: the in-flight probe reported %d fields, wanted 3", len(inFlightFields))
    }
    if "refused-as-deleted" != inFlightFields[1] {
        fail("session tombstone: the held save answered %q, wanted the refusal to carry session.ErrSessionDeleted", inFlightFields[1])
    }
    pass("(1) a save held open across a concurrent delete was refused with session.ErrSessionDeleted")

    /* (2) the deleted id does not come back into the store the in-flight request could still have written it to — read from the file on disk, not from the manager */
    if nil != sessionManager.Session(liveId) {
        fail("session tombstone: the deleted session %q was resurrected by the in-flight save", liveId)
    }
    if true == strings.Contains(readSessionStore(storagePath), liveId) {
        fail("session tombstone: the deleted id %q is still written in the store at %s", liveId, storagePath)
    }
    pass("(2) the deleted id never reappeared in the session file on disk")

    /* (3) the client of the in-flight request is logged out cleanly rather than answered 500: the session ending is not a storage outage, so the handler's own response is served untouched and only the cookie is expired. That difference is the whole reason the cause is a distinct error. */
    if nethttp.StatusOK != inFlightResponse.StatusCode {
        fail("session tombstone: the in-flight request answered %d, wanted the handler's own response", inFlightResponse.StatusCode)
    }
    if sessionHandlerBody != inFlightFields[2] {
        fail("session tombstone: the handler's response was replaced — wanted %q, got %q", sessionHandlerBody, inFlightFields[2])
    }

    expiringCookie := responseSessionCookie(inFlightResponse)
    if nil == expiringCookie {
        fail("session tombstone: the in-flight response emitted no session cookie — the client keeps presenting a deleted id")
    }
    if "" != expiringCookie.Value || 0 <= expiringCookie.MaxAge {
        fail("session tombstone: wanted an expiring session cookie, got value=%q MaxAge=%d", expiringCookie.Value, expiringCookie.MaxAge)
    }
    pass("(3) the client got the expiring cookie and the handler's own response, not a 500")

    /* (4) a rotated-away id is remembered exactly the same way: the rotation retires an id, and a request holding a snapshot from before it must not be able to buy it back */
    _, secondStartFields := sessionProbe(anonymous(), server.URL+"/start", "")
    rotatingId := secondStartFields[0]

    secondHeldFields := make(chan []string, 1)

    go func() {
        _, fields := sessionProbe(anonymous(), server.URL+"/hold-then-save", rotatingId)
        secondHeldFields <- fields
    }()

    if rotatingId != <-inFlightLoaded {
        fail("session tombstone: the second in-flight request loaded the wrong session")
    }

    _, rotateFields := sessionProbe(anonymous(), server.URL+"/rotate", rotatingId)
    if rotatingId != rotateFields[0] || rotatingId == rotateFields[1] {
        fail("session tombstone: the rotation reported %v, wanted %q rotated to a fresh id", rotateFields, rotatingId)
    }

    sessionEnded <- struct{}{}

    secondFields := <-secondHeldFields
    if "refused-as-deleted" != secondFields[1] {
        fail("session tombstone: the save held across a rotation answered %q, wanted the same refusal a delete produces", secondFields[1])
    }
    if true == strings.Contains(readSessionStore(storagePath), rotatingId) {
        fail("session tombstone: the rotated-away id %q was written back into the store", rotatingId)
    }
    pass("(4) an id retired by a rotation buys nothing either — the held save was refused the same way")
}
