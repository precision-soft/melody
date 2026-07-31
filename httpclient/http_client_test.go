package httpclient

import (
    "bytes"
    "context"
    "encoding/base64"
    "fmt"
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

    "github.com/precision-soft/melody/exception"
    httpclientcontract "github.com/precision-soft/melody/httpclient/contract"
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

func TestHttpClientRequestStream_ReturnsBodyAndCanBeClosed(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(200)
        _, _ = writer.Write([]byte("stream"))
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    streamResponse, err := client.RequestStream(http.MethodGet, "/")
    if nil != err {
        t.Fatalf("request error: %v", err)
    }
    if 200 != streamResponse.StatusCode() {
        t.Fatalf("expected status 200")
    }

    bodyBytes, err := io.ReadAll(streamResponse.Body())
    if nil != err {
        t.Fatalf("read error: %v", err)
    }
    if "stream" != string(bodyBytes) {
        t.Fatalf("unexpected body")
    }

    err = streamResponse.Close()
    if nil != err {
        t.Fatalf("close error: %v", err)
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

/* @info A nil *BasicAuthorizationOptions boxed through the public SetBasic passes a nil check on the interface, and reading the username off it dereferences nil on the request path — where the promise is an error, not a panic. */
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

/* @info A nil AuthorizationOptions interface reaches the same guard from the other side. */
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

/* @info The cap is caller input known before anything is dialled. Validating it after the exchange let a POST commit its side effect and then answered with an error phrased as though nothing had been sent, so a retry duplicated the operation. */
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

/* @info Url schemes are case-insensitive, and isSameOrigin one file over compares them with EqualFold; the absolute-url check compared them byte for byte, so an "HTTP://" target was hung under the base url as a path segment and the request went to the base host. */
func TestHttpClient_UppercaseSchemeIsAnAbsoluteUrl(t *testing.T) {
    if false == hasAbsoluteUrlScheme("HTTP://other.example/path") {
        t.Fatalf("an uppercase scheme names an absolute url")
    }
    if false == hasAbsoluteUrlScheme("HtTpS://other.example/path") {
        t.Fatalf("a mixed-case scheme names an absolute url")
    }
    if true == hasAbsoluteUrlScheme("/path") {
        t.Fatalf("a path is not an absolute url")
    }

    client := NewHttpClient(NewHttpClientConfig("https://base.example", 0, nil))

    _, err := client.buildUrl("HTTP://other.example/path", nil)
    if nil == err {
        t.Fatalf("expected an uppercase-scheme target to be judged as the foreign absolute url it is")
    }
}

/* @info An api key spelled as the password of an empty user is the ordinary shape of curl's "-u :key". The username guard dropped the whole credential and sent the request unauthenticated with nothing to say so. */
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

/* @info A bearer token and a basic credential cannot share one Authorization header; the bearer wins, and the contract says so. */
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

/* @info net/http writes the request body on its own goroutine and Do returns as soon as the response headers arrive, so a caller's []byte stays aliased into the transport after Request returned: a pooled buffer reused right after the call is a data race and torn bytes on the wire. The seam is asserted directly so the proof does not depend on scheduling. */
func TestHttpClient_ByteBodyIsCopiedFromTheCaller(t *testing.T) {
    options := NewRequestOptions()
    caller := []byte("original")
    options.SetBody(caller)

    reader, err := buildRequestBodyReader(options)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    for index := range caller {
        caller[index] = 'x'
    }

    sent, err := io.ReadAll(reader)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "original" != string(sent) {
        t.Fatalf("the request body followed the caller's slice after it was handed over: %q", string(sent))
    }
}

/* @info The same aliasing, driven through the public path against a server that answers without draining the body. Run with -race. */
func TestHttpClient_ByteBodyDoesNotRaceWithCallerReuse(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusRequestEntityTooLarge)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 5*time.Second, nil))

    buffer := bytes.Repeat([]byte{65}, 4*1024*1024)

    _, _ = client.Request(http.MethodPost, "/", WithBody(buffer))

    for index := range buffer {
        buffer[index] = 66
    }
}

/* @info The redirect policy exists so a configured secret does not reach a host the first server chose. An absolute target reaches a host the target string chose, one hop earlier, and the client attached the very same credentials to it. A client that talks to more than one origin is built without a base url. */
func TestHttpClient_AbsoluteUrlLeavingTheBaseOriginIsRefused(t *testing.T) {
    var hits int64
    var receivedApiKey string

    foreign := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        atomic.AddInt64(&hits, 1)
        receivedApiKey = request.Header.Get("X-Api-Key")
        writer.WriteHeader(http.StatusOK)
    }))
    defer foreign.Close()

    client := NewHttpClient(
        NewHttpClientConfig(
            "https://api.internal.example",
            5*time.Second,
            map[string]string{"X-Api-Key": "super-secret"},
        ),
    )

    _, err := client.Get(foreign.URL + "/attacker-chosen")
    if nil == err {
        t.Fatalf("expected the foreign absolute url to be refused")
    }

    if 0 != atomic.LoadInt64(&hits) {
        t.Fatalf("the request reached the foreign host with the api key %q", receivedApiKey)
    }
}

/* @info A client without a base url is the one that talks anywhere; the refusal must not reach it. */
func TestHttpClient_AbsoluteUrlIsAllowedWithoutABaseUrl(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig("", 5*time.Second, nil))

    response, err := client.Get(server.URL + "/anywhere")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if http.StatusOK != response.StatusCode() {
        t.Fatalf("expected status 200, got %d", response.StatusCode())
    }
}

/* @info An absolute url naming the origin the client was configured with is the same destination the base url describes, so it stays allowed. */
func TestHttpClient_AbsoluteUrlOnTheBaseOriginIsAllowed(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 5*time.Second, nil))

    response, err := client.Get(server.URL + "/same-origin")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if http.StatusOK != response.StatusCode() {
        t.Fatalf("expected status 200, got %d", response.StatusCode())
    }
}

/* @info An empty target names the base resource itself; the join appended a slash to it, so a server that tells /v1 from /v1/ answered 301 or 404 and the base resource was unreachable. */
func TestHttpClient_EmptyTargetNamesTheBaseResource(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("https://api.example.com/v1", 0, nil))

    built, err := client.buildUrl("", nil)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "https://api.example.com/v1" != built {
        t.Fatalf("expected the base resource untouched, got %q", built)
    }

    built, err = client.buildUrl("/", nil)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "https://api.example.com/v1/" != built {
        t.Fatalf("expected an explicit slash to survive, got %q", built)
    }

    built, err = client.buildUrl("/users", nil)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "https://api.example.com/v1/users" != built {
        t.Fatalf("the base url is a prefix, deliberately unlike RFC 3986 resolution: %q", built)
    }
}

/* @info An option chosen by a condition whose other branch produced nothing is a nil function value; calling it is a panic on the request path, outside any recovery this package owns. */
func TestHttpClient_NilRequestOptionIsRefused(t *testing.T) {
    var hits int64

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        atomic.AddInt64(&hits, 1)
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    _, err := client.Get("/", WithHeader("X-Present", "1"), nil)
    if nil == err {
        t.Fatalf("expected a nil option to be refused")
    }
    if false == strings.Contains(err.Error(), "nil request option") {
        t.Fatalf("expected the error to name the nil option, got %q", err.Error())
    }
    if 0 != atomic.LoadInt64(&hits) {
        t.Fatalf("the request was sent despite a nil option")
    }
}

/* @info The sibling resolveTransportConfig handles its nil argument explicitly and NewHttpClientConfig tolerates nil headers; the constructor dereferenced its own argument, so a wiring mistake died on an anonymous nil dereference instead of naming what was missing. */
func TestNewHttpClient_NilConfigurationIsRefusedByName(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected a nil configuration to be refused")
        }

        err, ok := recovered.(error)
        if false == ok {
            t.Fatalf("expected the refusal to travel as an error, got %T", recovered)
        }
        if false == strings.Contains(err.Error(), "configuration is nil") {
            t.Fatalf("expected the refusal to name the argument, got %q", err.Error())
        }
    }()

    NewHttpClient(nil)
}

/* @info net/url quotes the whole url in its own error text and the cause chain is rendered into the log record, so the most ordinary failure there is — a refused connection — wrote out a token passed through WithQuery or a password spelled in the userinfo. */
func TestHttpClient_ErrorsDoNotCarryUrlSecrets(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("", 0, nil))

    _, err := client.Get("http://127.0.0.1:1/protected", WithQuery("access_token", "QUERY-SECRET"))
    if nil == err {
        t.Fatalf("expected a connection failure")
    }

    rendered := renderErrorForLog(t, err)
    if true == strings.Contains(rendered, "QUERY-SECRET") {
        t.Fatalf("the query secret reached the log record: %s", rendered)
    }
    if false == strings.Contains(rendered, "access_token") {
        t.Fatalf("expected the parameter name to survive for diagnosis: %s", rendered)
    }
    if false == strings.Contains(rendered, "connection refused") {
        t.Fatalf("expected the reason to survive: %s", rendered)
    }

    _, err = client.Get("http://user:PASS-SECRET@example.com/\x7f")
    if nil == err {
        t.Fatalf("expected a parse failure")
    }

    rendered = renderErrorForLog(t, err)
    if true == strings.Contains(rendered, "PASS-SECRET") {
        t.Fatalf("the userinfo password reached the log record: %s", rendered)
    }

    _, err = client.Get("http://user:PASS-SECRET@example.com/\x7f", WithQuery("a", "1"))
    if nil == err {
        t.Fatalf("expected a parse failure on the query path")
    }

    rendered = renderErrorForLog(t, err)
    if true == strings.Contains(rendered, "PASS-SECRET") {
        t.Fatalf("the userinfo password reached the log record through the query path: %s", rendered)
    }
}

func renderErrorForLog(t *testing.T, err error) string {
    t.Helper()

    return fmt.Sprintf("%v %v", err.Error(), exception.LogContext(err))
}

/* @info Every client owns a hundred-connection idle pool kept for ninety seconds, and dropping the last reference releases none of it: each parked connection has a read loop keeping the transport alive. */
func TestHttpClient_CloseReleasesIdleConnections(t *testing.T) {
    var dialed int64

    server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
    }))
    server.Config.ConnState = func(connection net.Conn, state http.ConnState) {
        if http.StateNew == state {
            atomic.AddInt64(&dialed, 1)
        }
    }
    server.Start()
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 5*time.Second, nil))

    if _, err := client.Get("/"); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if _, err := client.Get("/"); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != atomic.LoadInt64(&dialed) {
        t.Fatalf("expected the second request to reuse the pooled connection, %d dialled", atomic.LoadInt64(&dialed))
    }

    if err := client.Close(); nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if _, err := client.Get("/"); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 2 != atomic.LoadInt64(&dialed) {
        t.Fatalf("expected Close to have released the idle connection, %d dialled", atomic.LoadInt64(&dialed))
    }
}

/* @info The streaming client carries no whole-request deadline, so a stream a server never ends is bounded by nothing the caller holds; a context is the remedy. */
func TestHttpClient_RequestStreamWithContextIsBoundedByTheContext(t *testing.T) {
    release := make(chan struct{})

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
        writer.(http.Flusher).Flush()
        <-release
    }))
    defer func() {
        close(release)
        server.Close()
    }()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    contextInstance, cancel := context.WithCancel(context.Background())

    stream, err := client.RequestStreamWithContext(contextInstance, http.MethodGet, "/events")
    if nil != err {
        cancel()
        t.Fatalf("unexpected error: %v", err)
    }
    defer stream.Close()

    cancel()

    /* the read is bounded here because a context that does not reach the request leaves it waiting on a server that never answers: without the timer this test would hang instead of failing. */
    readEnded := make(chan error, 1)

    go func() {
        _, readErr := io.ReadAll(stream.Body())
        readEnded <- readErr
    }()

    select {
    case readErr := <-readEnded:
        if nil == readErr {
            t.Fatalf("expected the cancelled context to end the body read")
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("the cancelled context did not reach the request: the body read is still waiting")
    }
}

/* @info A nil context names the wiring mistake instead of dying inside net/http. */
func TestHttpClient_RequestStreamWithNilContextIsRefused(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("http://127.0.0.1:1", 0, nil))

    //nolint:staticcheck // the nil context is the input under test
    _, err := client.RequestStreamWithContext(nil, http.MethodGet, "/")
    if nil == err {
        t.Fatalf("expected a nil context to be refused")
    }
}

/* @info The option promised a cap and the streaming path never read it: a caller who asked for ten bytes was handed everything the server sent, with nothing to say the guard did not exist there. */
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

/* @info The default cap belongs to Request, which holds the whole body in memory; applying it to a stream would cut a server-sent-event feed or a download at ten mebibytes. A caller who named no cap keeps an unbounded stream. */
func TestHttpClient_StreamWithoutAnExplicitCapIsUnbounded(t *testing.T) {
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

    if _, wrapped := stream.Body().(*limitedStreamBody); true == wrapped {
        t.Fatalf("a stream the caller set no cap on must not carry the buffered path's default cap")
    }

    read, err := io.ReadAll(stream.Body())
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(payload) != len(read) {
        t.Fatalf("expected the whole body, got %d bytes", len(read))
    }
}

/* @info A caller computing what is left of a deadline that has already passed hands over a negative duration; the buffered path folds it into the configured timeout and the streaming path turned it into no deadline at all — an exhausted budget yielding a stream that runs forever. */
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

/* @info A body the client cannot encode says which type it was handed. */
func TestHttpClient_UnsupportedBodyTypeNamesTheType(t *testing.T) {
    options := NewRequestOptions()
    options.SetBody(struct{ Quantity int }{Quantity: 1})

    _, err := buildRequestBodyReader(options)
    if nil == err {
        t.Fatalf("expected an unsupported body type to be refused")
    }
    if false == strings.Contains(fmt.Sprintf("%v", exception.LogContext(err)), "struct { Quantity int }") {
        t.Fatalf("expected the error to name the type, got %v", exception.LogContext(err))
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
