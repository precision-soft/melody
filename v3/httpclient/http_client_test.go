package httpclient

import (
    "bytes"
    "encoding/base64"
    "io"
    "math"
    "net"
    "net/http"
    "net/http/httptest"
    "net/url"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    httpclientcontract "github.com/precision-soft/melody/v3/httpclient/contract"
)

func TestHttpClientBuildsUrlAndAddsQuery(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if "1" != request.URL.Query().Get("a") {
            writer.WriteHeader(400)
            return
        }
        writer.WriteHeader(200)
        _, _ = writer.Write([]byte("ok"))
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    response, err := client.Get(
        "/path",
        WithQuery("a", "1"),
    )
    if nil != err {
        t.Fatalf("request error: %v", err)
    }
    if 200 != response.StatusCode() {
        t.Fatalf("expected status 200")
    }
}

func TestHttpClientAddsBearerAuthorization(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if "Bearer token" != request.Header.Get("Authorization") {
            writer.WriteHeader(401)
            return
        }
        writer.WriteHeader(200)
        _, _ = writer.Write([]byte("ok"))
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig("", 0, nil))
    client.SetBaseUrl(server.URL)

    response, err := client.Get(
        "/",
        WithBearerToken("token"),
    )
    if nil != err {
        t.Fatalf("request error: %v", err)
    }
    if 200 != response.StatusCode() {
        t.Fatalf("expected status 200")
    }
}

func TestHttpClientRespectsRequestTimeoutOverride(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        time.Sleep(50 * time.Millisecond)
        writer.WriteHeader(200)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    _, err := client.Get(
        "/",
        WithTimeout(1*time.Millisecond),
    )
    if nil == err {
        t.Fatalf("expected timeout error")
    }
}

func TestHttpClientAddsBasicAuthorization(t *testing.T) {
    expectedUser := "u"
    expectedPass := "p"
    expectedHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(expectedUser+":"+expectedPass))

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if expectedHeader != request.Header.Get("Authorization") {
            writer.WriteHeader(401)
            return
        }
        writer.WriteHeader(200)
        _, _ = writer.Write([]byte("ok"))
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    response, err := client.Get(
        "/",
        WithBasicAuth(expectedUser, expectedPass),
    )
    if nil != err {
        t.Fatalf("request error: %v", err)
    }
    if 200 != response.StatusCode() {
        t.Fatalf("expected status 200")
    }
}

func TestHttpClientPost_SendsJsonBodyAndContentType(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if "application/json" != request.Header.Get("Content-Type") {
            writer.WriteHeader(400)
            return
        }

        bodyBytes, _ := io.ReadAll(request.Body)
        if false == bytes.Contains(bodyBytes, []byte(`"name":"a"`)) {
            writer.WriteHeader(400)
            return
        }

        writer.WriteHeader(201)
        _, _ = writer.Write([]byte(`{"ok":true}`))
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    response, err := client.Post(
        "/",
        map[string]any{
            "name": "a",
        },
    )
    if nil != err {
        t.Fatalf("request error: %v", err)
    }
    if 201 != response.StatusCode() {
        t.Fatalf("expected status 201")
    }

    target := map[string]any{}
    err = response.Json(&target)
    if nil != err {
        t.Fatalf("json error: %v", err)
    }
    if true != target["ok"].(bool) {
        t.Fatalf("unexpected json")
    }
}

func TestHttpClientRequest_UnsupportedBodyTypeReturnsError(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(200)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    type bad struct {
        A int
    }

    _, err := client.Request(
        http.MethodPost,
        "/",
        WithBody(bad{A: 1}),
    )
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestHttpClientMaxResponseBodyBytes_Enforced(t *testing.T) {
    payload := bytes.Repeat([]byte("a"), 20)

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(200)
        _, _ = writer.Write(payload)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    _, err := client.Get(
        "/",
        WithMaxResponseBodyBytes(10),
    )
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestHttpClientHeaders_MergesClientAndRequestHeaders(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if "a" != request.Header.Get("X-Client") {
            writer.WriteHeader(400)
            return
        }
        if "b" != request.Header.Get("X-Request") {
            writer.WriteHeader(400)
            return
        }
        writer.WriteHeader(200)
    }))
    defer server.Close()

    client := NewHttpClient(
        NewHttpClientConfig(
            server.URL,
            0,
            map[string]string{
                "X-Client": "a",
            },
        ),
    )

    response, err := client.Get(
        "/",
        WithHeader("X-Request", "b"),
    )
    if nil != err {
        t.Fatalf("request error: %v", err)
    }
    if 200 != response.StatusCode() {
        t.Fatalf("expected status 200")
    }
}

func TestHttpClientSetTimeout_UpdatesClientTimeout(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        time.Sleep(20 * time.Millisecond)
        writer.WriteHeader(200)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 100*time.Millisecond, nil))
    client.SetTimeout(1 * time.Millisecond)

    _, err := client.Get("/")
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestHttpClientRequestHeadersOverrideClientHeaders(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if "request" != request.Header.Get("X-Test") {
            writer.WriteHeader(400)
            return
        }
        writer.WriteHeader(200)
    }))
    defer server.Close()

    client := NewHttpClient(
        NewHttpClientConfig(
            server.URL,
            0,
            map[string]string{
                "X-Test": "client",
            },
        ),
    )

    _, err := client.Get(
        "/",
        WithHeader("X-Test", "request"),
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
}

func TestHttpClientRequest_WithJsonSetsContentType(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if "application/json" != request.Header.Get("Content-Type") {
            writer.WriteHeader(400)
            return
        }
        writer.WriteHeader(200)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    _, err := client.Request(
        http.MethodPost,
        "/",
        WithJson(map[string]any{"a": "b"}),
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
}

func TestHttpClientRequest_InvalidBaseUrlReturnsError(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig(":", 0, nil))

    _, err := client.Get("/")
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestHttpClientConcurrentSettersAndRequests(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(200)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 100*time.Millisecond, nil))

    var waitGroup sync.WaitGroup
    iterations := 50

    for workerIndex := 0; workerIndex < 4; workerIndex++ {
        waitGroup.Add(1)
        go func(workerId int) {
            defer waitGroup.Done()
            for index := 0; index < iterations; index++ {
                client.SetHeader("X-Worker-"+strconv.Itoa(workerId), strconv.Itoa(index))
                client.SetBaseUrl(server.URL)
                client.SetTimeout(100 * time.Millisecond)
            }
        }(workerIndex)
    }

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()
        for index := 0; index < iterations; index++ {
            _, _ = client.Get("/")
        }
    }()

    waitGroup.Wait()
}

/* @info net/http strips only Authorization/Cookie, and only across domains. A client-configured api-key header would otherwise be handed to whatever host the first server redirects to — a host that server's operator chooses. */
func TestHttpClient_StripsCredentialHeadersOnCrossOriginRedirect(t *testing.T) {
    var receivedApiKey string
    var receivedAuthorization string

    target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        receivedApiKey = request.Header.Get("X-Api-Key")
        receivedAuthorization = request.Header.Get("Authorization")
        writer.WriteHeader(http.StatusOK)
    }))
    defer target.Close()

    redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        http.Redirect(writer, request, target.URL+"/stolen", http.StatusFound)
    }))
    defer redirector.Close()

    client := NewHttpClient(NewHttpClientConfig(
        "",
        5*time.Second,
        map[string]string{"X-Api-Key": "super-secret", "Authorization": "Bearer super-secret"},
    ))

    if _, err := client.Get(redirector.URL); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "" != receivedApiKey {
        t.Fatalf("the api key leaked to the redirect target: %q", receivedApiKey)
    }
    if "" != receivedAuthorization {
        t.Fatalf("the authorization header leaked to the redirect target: %q", receivedAuthorization)
    }
}

/* @info A same-origin redirect is not a credential boundary; stripping there would break ordinary /login -> /home flows. */
func TestHttpClient_KeepsCredentialHeadersOnSameOriginRedirect(t *testing.T) {
    var receivedApiKey string

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if "/start" == request.URL.Path {
            http.Redirect(writer, request, "/finish", http.StatusFound)
            return
        }

        receivedApiKey = request.Header.Get("X-Api-Key")
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(
        server.URL,
        5*time.Second,
        map[string]string{"X-Api-Key": "super-secret"},
    ))

    if _, err := client.Get("/start"); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "super-secret" != receivedApiKey {
        t.Fatalf("expected the api key to survive a same-origin redirect, got %q", receivedApiKey)
    }
}

/* @info int64(math.MaxInt)+1 wraps negative, so io.LimitReader would read zero bytes and hand back an empty body with no error. */
func TestHttpClient_MaxResponseBodyBytesAtMaxIntDoesNotOverflow(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.Write([]byte("payload"))
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig("", 5*time.Second, nil))

    response, err := client.Get(server.URL, WithMaxResponseBodyBytes(math.MaxInt))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "payload" != string(response.Body()) {
        t.Fatalf("expected the body to survive a MaxInt limit, got %q", string(response.Body()))
    }
}

/* @info Per-request credential headers (WithHeader/WithHeaders) must be stripped on a cross-origin redirect exactly like the client-wide ones: the redirect target is chosen by whoever operates the first server. */
func TestHttpClient_StripsPerRequestCredentialHeadersOnCrossOriginRedirect(t *testing.T) {
    var receivedApiKey string

    target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        receivedApiKey = request.Header.Get("X-Api-Key")
        writer.WriteHeader(http.StatusOK)
    }))
    defer target.Close()

    redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        http.Redirect(writer, request, target.URL+"/stolen", http.StatusFound)
    }))
    defer redirector.Close()

    client := NewHttpClient(NewHttpClientConfig("", 5*time.Second, nil))

    if _, err := client.Get(redirector.URL, WithHeader("X-Api-Key", "super-secret")); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "" != receivedApiKey {
        t.Fatalf("the per-request api key leaked to the redirect target: %q", receivedApiKey)
    }
}

/* @info An explicitly spelled default port names the same origin as an omitted one; treating it as cross-origin would strip credentials from an ordinary same-host redirect. */
func TestHttpClient_KeepsCredentialHeadersOnSameOriginRedirectWithExplicitDefaultPort(t *testing.T) {
    if false == isSameOrigin(mustParseUrl(t, "http://example.com:80/start"), mustParseUrl(t, "http://example.com/finish")) {
        t.Fatalf("an explicit :80 must not make an http origin foreign to itself")
    }

    if false == isSameOrigin(mustParseUrl(t, "https://example.com/start"), mustParseUrl(t, "https://EXAMPLE.com:443/finish")) {
        t.Fatalf("host case and an explicit :443 must not make an https origin foreign to itself")
    }

    if true == isSameOrigin(mustParseUrl(t, "https://example.com/start"), mustParseUrl(t, "http://example.com/finish")) {
        t.Fatalf("a scheme downgrade leaves the origin")
    }

    if true == isSameOrigin(mustParseUrl(t, "https://example.com/start"), mustParseUrl(t, "https://example.com:8443/finish")) {
        t.Fatalf("a different port leaves the origin")
    }
}

func mustParseUrl(t *testing.T, value string) *url.URL {
    t.Helper()

    parsed, err := url.Parse(value)
    if nil != err {
        t.Fatalf("could not parse %q: %v", value, err)
    }

    return parsed
}

/* @info The redirect policy runs on the request goroutine; reading the client's header map there while SetHeader writes it is a concurrent map access, which the runtime kills the process for. Run with -race. */
func TestHttpClient_RedirectPolicyDoesNotRaceWithSetHeader(t *testing.T) {
    target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
    }))
    defer target.Close()

    redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        http.Redirect(writer, request, target.URL+"/moved", http.StatusFound)
    }))
    defer redirector.Close()

    client := NewHttpClient(NewHttpClientConfig("", 5*time.Second, map[string]string{"X-Api-Key": "secret"}))

    var waitGroup sync.WaitGroup

    for index := 0; index < 8; index++ {
        waitGroup.Add(2)

        go func(index int) {
            defer waitGroup.Done()
            client.SetHeader("X-Worker-"+strconv.Itoa(index), strconv.Itoa(index))
        }(index)

        go func() {
            defer waitGroup.Done()
            _, _ = client.Get(redirector.URL)
        }()
    }

    waitGroup.Wait()
}

/* @info Variadic passing does not copy the slice: Post/Put/Patch append WithJson into a spare slot of the caller's slice, so two concurrent calls sharing one slice write the same backing-array slot and can deliver one call's body to the other's endpoint. Run with -race. */
func TestHttpClientPost_DoesNotShareCallerOptionsSliceAcrossConcurrentCalls(t *testing.T) {
    var corruption atomic.Bool

    newBodyServer := func(expected string) *httptest.Server {
        return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
            bodyBytes, _ := io.ReadAll(request.Body)
            if false == bytes.Contains(bodyBytes, []byte(expected)) {
                corruption.Store(true)
            }
            writer.WriteHeader(http.StatusOK)
        }))
    }

    serverA := newBodyServer(`"v":"A"`)
    defer serverA.Close()
    serverB := newBodyServer(`"v":"B"`)
    defer serverB.Close()

    client := NewHttpClient(NewHttpClientConfig("", 5*time.Second, nil))

    for iteration := 0; iteration < 300; iteration++ {
        shared := make([]httpclientcontract.RequestOption, 0, 4)
        shared = append(shared, WithHeader("X-Shared", "1"))

        var waitGroup sync.WaitGroup
        waitGroup.Add(2)

        go func() {
            defer waitGroup.Done()
            _, _ = client.Post(serverA.URL, map[string]any{"v": "A"}, shared...)
        }()
        go func() {
            defer waitGroup.Done()
            _, _ = client.Post(serverB.URL, map[string]any{"v": "B"}, shared...)
        }()

        waitGroup.Wait()
    }

    if true == corruption.Load() {
        t.Fatalf("a request body was delivered to the wrong endpoint through a shared options slice")
    }
}

/* @info Every other guard in the file treats a non-positive timeout as unset; a negative configured timeout must fall back to the 30s default, not build a client with no deadline at all. */
func TestNewHttpClient_NegativeTimeoutFallsBackToDefault(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("", -1*time.Second, nil))

    if 30*time.Second != client.client.Timeout {
        t.Fatalf("expected a negative timeout to fall back to the 30s default, got %v", client.client.Timeout)
    }
    if 30*time.Second != client.timeout {
        t.Fatalf("expected the stored timeout to fall back to the 30s default, got %v", client.timeout)
    }
}

/* @info net/http auto-sets Referer to the full previous url, query string included, and does not strip it on a non-downgrade cross-origin hop; a secret placed in the url would otherwise reach the redirect target the first server chose. */
func TestHttpClient_StripsRefererOnCrossOriginRedirect(t *testing.T) {
    var receivedReferer string

    target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        receivedReferer = request.Header.Get("Referer")
        writer.WriteHeader(http.StatusOK)
    }))
    defer target.Close()

    redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        http.Redirect(writer, request, target.URL+"/stolen", http.StatusFound)
    }))
    defer redirector.Close()

    client := NewHttpClient(NewHttpClientConfig("", 5*time.Second, nil))

    if _, err := client.Get(redirector.URL, WithQuery("access_token", "QUERY-SECRET")); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if true == strings.Contains(receivedReferer, "QUERY-SECRET") {
        t.Fatalf("the url query secret leaked to the redirect target through Referer: %q", receivedReferer)
    }
}

func TestNewHttpClient_TransportRetainsIdleConnectionsPerHost(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("https://upstream.test", 5*time.Second, nil))

    transport, ok := client.client.Transport.(*http.Transport)
    if false == ok {
        t.Fatalf("expected the client to build a net/http transport")
    }

    if 0 == transport.MaxIdleConnsPerHost {
        t.Fatalf(
            "MaxIdleConnsPerHost is unset, so net/http retains only %d idle connections per host and MaxIdleConns %d is inert; one client per BaseUrl sends every request to a single host, so the pool closes nearly every connection it dials",
            http.DefaultMaxIdleConnsPerHost,
            transport.MaxIdleConns,
        )
    }

    if transport.MaxIdleConns != transport.MaxIdleConnsPerHost {
        t.Fatalf(
            "expected the per-host idle pool to match MaxIdleConns %d, got %d",
            transport.MaxIdleConns,
            transport.MaxIdleConnsPerHost,
        )
    }
}

func TestHttpClient_ReusesPooledConnectionsAcrossConcurrentWaves(t *testing.T) {
    const waveSize = 10

    var dialed int64

    arrived := make(chan struct{}, waveSize)
    release := make(chan struct{}, 2*waveSize)

    server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        arrived <- struct{}{}
        <-release

        writer.WriteHeader(http.StatusOK)
        _, _ = writer.Write([]byte("ok"))
    }))
    server.Config.ConnState = func(connection net.Conn, state http.ConnState) {
        if http.StateNew == state {
            atomic.AddInt64(&dialed, 1)
        }
    }
    server.Start()
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 30*time.Second, nil))

    runWave := func() {
        var wave sync.WaitGroup

        for request := 0; request < waveSize; request++ {
            wave.Add(1)

            go func() {
                defer wave.Done()

                if _, err := client.Get("/"); nil != err {
                    t.Errorf("unexpected error: %v", err)
                }
            }()
        }

        for request := 0; request < waveSize; request++ {
            <-arrived
        }

        for request := 0; request < waveSize; request++ {
            release <- struct{}{}
        }

        wave.Wait()
    }

    runWave()
    runWave()

    if int64(waveSize+3) < atomic.LoadInt64(&dialed) {
        t.Fatalf(
            "the second wave of %d concurrent requests dialled fresh sockets instead of reusing the pool: %d connections for %d requests",
            waveSize,
            atomic.LoadInt64(&dialed),
            2*waveSize,
        )
    }
}

/* A nil *BasicAuthorizationOptions boxed through the public SetBasic passes a nil check on the interface, and reading the username off it dereferences nil on the request path — where the promise is an error, not a panic. */
func TestHttpClient_TypedNilBasicAuthorizationDoesNotPanic(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    response, err := client.Get("/", func(options httpclientcontract.RequestOptions) {
        options.Authorization().SetBasic((*BasicAuthorizationOptions)(nil))
    })
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if http.StatusOK != response.StatusCode() {
        t.Fatalf("expected status 200, got %d", response.StatusCode())
    }
}

/* A nil AuthorizationOptions interface reaches the same guard from the other side. */
func TestHttpClient_NilAuthorizationDoesNotPanic(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    _, err := client.Get("/", func(options httpclientcontract.RequestOptions) {
        options.Authorization().SetBasic(nil)
    })
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
}

/* An api key spelled as the password of an empty user is the ordinary shape of curl's "-u :key". The username guard dropped the whole credential and sent the request unauthenticated with nothing to say so. */
func TestHttpClient_BasicAuthorizationWithEmptyUsernameIsSent(t *testing.T) {
    var receivedUsername string
    var receivedPassword string
    var receivedOk bool

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        receivedUsername, receivedPassword, receivedOk = request.BasicAuth()
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    if _, err := client.Get("/", WithBasicAuth("", "api-key-as-password")); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == receivedOk {
        t.Fatalf("no basic credential reached the server")
    }
    if "" != receivedUsername {
        t.Fatalf("expected an empty username, got %q", receivedUsername)
    }
    if "api-key-as-password" != receivedPassword {
        t.Fatalf("expected the password to travel, got %q", receivedPassword)
    }
}

/* A bearer token and a basic credential cannot share one Authorization header; the bearer wins, and the contract says so. */
func TestHttpClient_BearerTokenWinsOverBasicAuthorization(t *testing.T) {
    var receivedAuthorization string

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        receivedAuthorization = request.Header.Get("Authorization")
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    if _, err := client.Get("/", WithBearerToken("token"), WithBasicAuth("user", "password")); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "Bearer token" != receivedAuthorization {
        t.Fatalf("expected the bearer token to win, got %q", receivedAuthorization)
    }
}

/* The cap is caller input known before anything is dialled. Validating it after the exchange let a POST commit its side effect and then answered with an error phrased as though nothing had been sent, so a retry duplicated the operation. */
func TestHttpClient_InvalidMaxResponseBodyBytesIsRefusedBeforeTheRequestIsSent(t *testing.T) {
    var hits int64

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        atomic.AddInt64(&hits, 1)
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    _, err := client.Post("/side-effect", map[string]any{"quantity": 1}, WithMaxResponseBodyBytes(0))
    if nil == err {
        t.Fatalf("expected an error for a non-positive cap")
    }

    if 0 != atomic.LoadInt64(&hits) {
        t.Fatalf("the request was sent before the cap was validated: the server was hit %d time(s)", atomic.LoadInt64(&hits))
    }
}

/* The option promised a cap and the streaming path never read it: a caller who asked for ten bytes was handed everything the server sent, with nothing to say the guard did not exist there. */
func TestHttpClient_StreamHonoursAnExplicitResponseBodyCap(t *testing.T) {
    payload := bytes.Repeat([]byte{97}, 5000)

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
        _, _ = writer.Write(payload)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    stream, err := client.RequestStream(http.MethodGet, "/", WithMaxResponseBodyBytes(10))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    defer stream.Close()

    read, err := io.ReadAll(stream.Body())
    if nil == err {
        t.Fatalf("expected the cap to be enforced, %d bytes delivered", len(read))
    }
    if 10 < len(read) {
        t.Fatalf("expected at most the cap to be delivered, got %d bytes", len(read))
    }
}

/* The default cap binds the stream too: an unbounded body behind a bounded contract delivered whatever the server chose to send, and the caller who never named a cap is exactly the one who never audited for that. A body under the default still arrives whole. */
func TestHttpClient_StreamWithoutAnExplicitCapIsBoundedByTheDefault(t *testing.T) {
    payload := bytes.Repeat([]byte{97}, 5000)

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
        _, _ = writer.Write(payload)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    stream, err := client.RequestStream(http.MethodGet, "/")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    defer stream.Close()

    limited, wrapped := stream.Body().(*limitedStreamBody)
    if false == wrapped {
        t.Fatalf("a stream the caller set no cap on must carry the inherited default cap")
    }

    if 10*1024*1024 != limited.limit {
        t.Fatalf("expected the inherited default cap, got %d", limited.limit)
    }

    read, err := io.ReadAll(stream.Body())
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(payload) != len(read) {
        t.Fatalf("expected the whole body under the default cap, got %d bytes", len(read))
    }
}

/* the streaming path judges the cap before anything is dialled, the rule the buffered path always held and asserts by counting zero server hits: refused after the exchange, a POST that had already committed its side effect answered with an error phrased as though nothing had been sent, and a caller retrying on it duplicated the operation. */
func TestHttpClient_StreamRefusesAnInvalidCapBeforeTheRequestIsSent(t *testing.T) {
    serverHits := 0

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        serverHits = serverHits + 1

        writer.WriteHeader(http.StatusOK)
        _, _ = writer.Write([]byte("body"))
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    _, requestErr := client.RequestStream(http.MethodPost, "/", WithMaxResponseBodyBytes(0))
    if nil == requestErr {
        t.Fatalf("expected the invalid cap to be refused on the streaming path")
    }

    if "invalid max response body bytes" != requestErr.Error() {
        t.Fatalf("unexpected refusal message: %q", requestErr.Error())
    }

    if 0 != serverHits {
        t.Fatalf("expected the streaming refusal to arrive before the request is sent, got %d server hits", serverHits)
    }
}

/* the buffered path reads one byte past the cap precisely so a body ending EXACTLY at it is delivered rather than refused; the streaming sibling carries that proof in its own suite, and an off-by-one here would have refused every response that filled its budget exactly. */
func TestHttpClient_ABufferedBodyEndingExactlyAtTheCapIsDelivered(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
        _, _ = writer.Write([]byte("0123456789"))
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    response, requestErr := client.Get("/", WithMaxResponseBodyBytes(10))
    if nil != requestErr {
        t.Fatalf("expected a body ending exactly at the cap to be delivered, got %v", requestErr)
    }

    if "0123456789" != response.String() {
        t.Fatalf("unexpected body: %q", response.String())
    }

    _, oneOverErr := client.Get("/", WithMaxResponseBodyBytes(9))
    if nil == oneOverErr {
        t.Fatalf("expected a body one byte past the cap to be refused")
    }

    if "response body exceeded max size" != oneOverErr.Error() {
        t.Fatalf("unexpected refusal message: %q", oneOverErr.Error())
    }
}

/* A caller computing what is left of a deadline that has already passed hands over a negative duration; the buffered path folds it into the configured timeout and the streaming path turned it into no deadline at all — an exhausted budget yielding a stream that runs forever. */
func TestHttpClient_NegativeRequestTimeoutIsNotAnUnboundedStream(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("", 5*time.Second, nil))

    if 5*time.Second != client.clientForRequest(-2*time.Second).Timeout {
        t.Fatalf("expected the buffered path to fall back to the configured timeout")
    }

    if 5*time.Second != client.streamClientForRequest(-2*time.Second).Timeout {
        t.Fatalf(
            "expected a negative request timeout to fall back to the configured timeout on the streaming path, got %v",
            client.streamClientForRequest(-2*time.Second).Timeout,
        )
    }

    if 0 != client.streamClientForRequest(0).Timeout {
        t.Fatalf("expected an unset timeout to keep the streaming path unbounded")
    }
}

/* The dedup of the two inline authorization blocks is only real if the streaming path actually enters the shared function: nothing else pins that the stream carries a credential at all. */
func TestHttpClient_StreamCarriesTheRequestedAuthorization(t *testing.T) {
    var receivedAuthorization string

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        receivedAuthorization = request.Header.Get("Authorization")
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    stream, err := client.RequestStream(http.MethodGet, "/", WithBearerToken("token"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    defer stream.Close()

    if "Bearer token" != receivedAuthorization {
        t.Fatalf("expected the stream to carry the bearer token, got %q", receivedAuthorization)
    }
}

/* the two places a url carries a secret are the userinfo and the query values; the sanitized form keeps what makes a failure diagnosable — scheme, host, path and parameter names. */
func TestSanitizeUrlForDiagnostics_StripsUserinfoAndQueryValues(t *testing.T) {
    sanitized := sanitizeUrlForDiagnostics("https://user:PASSW0RD@host.example/path?api_key=SECRET")

    if true == strings.Contains(sanitized, "PASSW0RD") {
        t.Fatalf("the password reached the diagnostic url: %q", sanitized)
    }
    if true == strings.Contains(sanitized, "SECRET") {
        t.Fatalf("the query secret reached the diagnostic url: %q", sanitized)
    }
    for _, kept := range []string{"host.example", "/path", "api_key"} {
        if false == strings.Contains(sanitized, kept) {
            t.Fatalf("expected %q to survive sanitization, got %q", kept, sanitized)
        }
    }
}
