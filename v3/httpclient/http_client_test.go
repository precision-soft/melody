package httpclient

import (
    "bytes"
    "context"
    "encoding/base64"
    "errors"
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

    "github.com/precision-soft/melody/v3/exception"
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

/* net/http strips only Authorization/Cookie, and only across domains. A client-configured api-key header would otherwise be handed to whatever host the first server redirects to — a host that server's operator chooses. */
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

/* A same-origin redirect is not a credential boundary; stripping there would break ordinary /login -> /home flows. */
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

/* int64(math.MaxInt)+1 wraps negative, so io.LimitReader would read zero bytes and hand back an empty body with no error. */
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

/* Per-request credential headers (WithHeader/WithHeaders) must be stripped on a cross-origin redirect exactly like the client-wide ones: the redirect target is chosen by whoever operates the first server. */
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

/* The streaming path binds the caller's context to the request, and the per-request credential names travel to the redirect policy through that same context: binding it after the request was built replaced the whole context and the names went with it, so the buffered path stripped and the streaming path did not. */
func TestHttpClient_StripsPerRequestCredentialHeadersOnCrossOriginRedirectWhileStreaming(t *testing.T) {
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

    response, err := client.RequestStream(http.MethodGet, redirector.URL, WithHeader("X-Api-Key", "super-secret"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    defer response.Close()

    if "" != receivedApiKey {
        t.Fatalf("the per-request api key leaked to the redirect target of a stream: %q", receivedApiKey)
    }
}

/* An explicitly spelled default port names the same origin as an omitted one; treating it as cross-origin would strip credentials from an ordinary same-host redirect. */
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

/* The redirect policy runs on the request goroutine; reading the client's header map there while SetHeader writes it is a concurrent map access, which the runtime kills the process for. Run with -race. */
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

/* Variadic passing does not copy the slice: Post/Put/Patch append WithJson into a spare slot of the caller's slice, so two concurrent calls sharing one slice write the same backing-array slot and can deliver one call's body to the other's endpoint. Run with -race. */
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

/* Every other guard in the file treats a non-positive timeout as unset; a negative configured timeout must fall back to the 30s default, not build a client with no deadline at all. */
func TestNewHttpClient_NegativeTimeoutFallsBackToDefault(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("", -1*time.Second, nil))

    if 30*time.Second != client.client.Timeout {
        t.Fatalf("expected a negative timeout to fall back to the 30s default, got %v", client.client.Timeout)
    }
    if 30*time.Second != client.timeout {
        t.Fatalf("expected the stored timeout to fall back to the 30s default, got %v", client.timeout)
    }
}

/* net/http auto-sets Referer to the full previous url, query string included, and does not strip it on a non-downgrade cross-origin hop; a secret placed in the url would otherwise reach the redirect target the first server chose. */
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

/* Url schemes are case-insensitive. The frozen majors repaired a prefix sniff that compared spellings byte for byte; here the target is PARSED and the judgment is made on the RESOLVED url, so an "HTTP://" spelling needs no special-casing to name the foreign host it names — the old defect is impossible by construction. Both directions are pinned: the foreign spelling refused, the base-origin spelling allowed. */
func TestHttpClient_UppercaseSchemeIsJudgedOnItsResolvedOrigin(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("https://base.example", 0, nil))

    _, err := client.buildUrl("HTTP://other.example/path", nil)
    if nil == err {
        t.Fatalf("expected an uppercase-scheme target to be judged as the foreign absolute url it is")
    }
    if false == strings.Contains(err.Error(), "leaves the origin") {
        t.Fatalf("expected the origin refusal, got %q", err.Error())
    }

    built, err := client.buildUrl("HTTPS://base.example/path", nil)
    if nil != err {
        t.Fatalf("expected an uppercase spelling of the base origin to stay allowed, got %v", err)
    }
    if "https://base.example/path" != built {
        t.Fatalf("unexpected built url: %q", built)
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

/* net/http writes the request body on its own goroutine and Do returns as soon as the response headers arrive, so a caller's []byte stays aliased into the transport after Request returned: a pooled buffer reused right after the call is a data race and torn bytes on the wire. The seam is asserted directly so the proof does not depend on scheduling. */
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

/* The same aliasing, driven through the public path against a server that answers without draining the body. Run with -race. */
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

/* The redirect policy exists so a configured secret does not reach a host the first server chose. An absolute target reaches a host the target string chose, one hop earlier, and the client attached the very same credentials to it. A client that talks to more than one origin is built without a base url. */
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

/* A client without a base url is the one that talks anywhere; the refusal must not reach it. */
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

/* An absolute url naming the origin the client was configured with is the same destination the base url describes, so it stays allowed. */
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

/* The frozen majors treat the base url as a PREFIX and their suite asserts the join; here buildUrl is RFC 3986 reference resolution, so the same fixtures answer differently and the assertions are INVERTED rather than supplemented: an empty target names the base resource WITH its trailing slash, a relative target merges over the last segment of the base path — which the constructor's slash rule makes the whole of it — and an absolute-path target replaces the base path entirely. */
func TestHttpClient_BuildUrlResolvesTheReferenceFormsByRfc3986(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("https://api.example.com/v1/", 0, nil))

    built, err := client.buildUrl("", nil)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "https://api.example.com/v1/" != built {
        t.Fatalf("expected the empty target to name the base resource itself, got %q", built)
    }

    built, err = client.buildUrl("users", nil)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "https://api.example.com/v1/users" != built {
        t.Fatalf("expected the relative target merged under the base path, got %q", built)
    }

    built, err = client.buildUrl("/users", nil)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "https://api.example.com/users" != built {
        t.Fatalf("expected the absolute-path target to replace the base path entirely, got %q", built)
    }

    built, err = client.buildUrl(".", nil)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "https://api.example.com/v1/" != built {
        t.Fatalf("expected the dot target to name the base directory, got %q", built)
    }

    built, err = client.buildUrl("https://api.example.com/absolute", nil)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "https://api.example.com/absolute" != built {
        t.Fatalf("expected the same-origin absolute target untouched, got %q", built)
    }
}

/* An option chosen by a condition whose other branch produced nothing is a nil function value; calling it is a panic on the request path, outside any recovery this package owns. */
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

/* The sibling resolveTransportConfig handles its nil argument explicitly and NewHttpClientConfig tolerates nil headers; the constructor dereferenced its own argument, so a wiring mistake died on an anonymous nil dereference instead of naming what was missing. */
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

/* net/url quotes the whole url in its own error text and the cause chain is rendered into the log record, so the most ordinary failure there is — a refused connection — wrote out a token passed through WithQuery or a password spelled in the userinfo. */
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

/* Every client owns a hundred-connection idle pool kept for ninety seconds, and dropping the last reference releases none of it: each parked connection has a read loop keeping the transport alive. */
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

/* The streaming client carries no whole-request deadline, so a stream a server never ends is bounded by nothing the caller holds; a context is the remedy. */
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

/* A nil context names the wiring mistake instead of dying inside net/http. */
func TestHttpClient_RequestStreamWithNilContextIsRefused(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("http://127.0.0.1:1", 0, nil))

    //nolint:staticcheck // the nil context is the input under test
    _, err := client.RequestStreamWithContext(nil, http.MethodGet, "/")
    if nil == err {
        t.Fatalf("expected a nil context to be refused")
    }
    if "request context is nil" != err.Error() {
        t.Fatalf("expected the refusal to name the nil context rather than fail inside net/http, got %q", err.Error())
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

/* A body the client cannot encode says which type it was handed. */
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

/* the constructor an application reaches for when it has nothing to configure had never been executed: the whole of what "default" means — a thirty-second whole-request timeout, no base url, no configured headers, and a real transport under it — went unproven, and a default drifting to zero would have made every request unbounded without a single test noticing. */
func TestNewDefaultHttpClient_CarriesTheDocumentedDefaults(t *testing.T) {
    client := NewDefaultHttpClient()
    defer client.Close()

    if 30*time.Second != client.client.Timeout {
        t.Fatalf("expected the documented thirty-second default timeout, got %v", client.client.Timeout)
    }

    if 30*time.Second != client.timeout {
        t.Fatalf("expected the per-request fallback to carry the same default, got %v", client.timeout)
    }

    if "" != client.baseUrl {
        t.Fatalf("expected no base url, got %q", client.baseUrl)
    }

    if 0 != len(client.headers) {
        t.Fatalf("expected no configured headers, got %v", client.headers)
    }

    if _, isTransport := client.client.Transport.(*http.Transport); false == isTransport {
        t.Fatalf("expected the client to own a real transport, got %T", client.client.Transport)
    }

    if nil == client.client.CheckRedirect {
        t.Fatalf("expected the credential-stripping redirect policy to be installed")
    }
}

/* Put, Patch and Delete had never been executed. The first two are Post's siblings and carry the same two obligations — the method on the wire and the json encoding of the body — and the third carries neither a body nor a content type; a verb wired to the wrong method would send a create where an update was meant, which no status code distinguishes. */
func TestHttpClient_PutPatchAndDeleteSendTheirOwnMethods(t *testing.T) {
    type recordedRequest struct {
        method      string
        contentType string
        body        string
    }

    recorded := []recordedRequest{}

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        body, _ := io.ReadAll(request.Body)

        recorded = append(recorded, recordedRequest{
            method:      request.Method,
            contentType: request.Header.Get("Content-Type"),
            body:        string(body),
        })

        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    if _, err := client.Put(server.URL, map[string]string{"name": "put"}); nil != err {
        t.Fatalf("unexpected put error: %v", err)
    }

    if _, err := client.Patch(server.URL, map[string]string{"name": "patch"}); nil != err {
        t.Fatalf("unexpected patch error: %v", err)
    }

    if _, err := client.Delete(server.URL); nil != err {
        t.Fatalf("unexpected delete error: %v", err)
    }

    if 3 != len(recorded) {
        t.Fatalf("expected three requests, got %d", len(recorded))
    }

    if http.MethodPut != recorded[0].method || "application/json" != recorded[0].contentType || `{"name":"put"}` != recorded[0].body {
        t.Fatalf("unexpected put request: %#v", recorded[0])
    }

    if http.MethodPatch != recorded[1].method || "application/json" != recorded[1].contentType || `{"name":"patch"}` != recorded[1].body {
        t.Fatalf("unexpected patch request: %#v", recorded[1])
    }

    if http.MethodDelete != recorded[2].method || "" != recorded[2].contentType || "" != recorded[2].body {
        t.Fatalf("expected delete to carry neither a body nor a content type, got %#v", recorded[2])
    }
}

/* assertBodyCarryingVerbOwnsItsOptionSlice drives one verb twice, concurrently, over a single option slice with spare capacity. The two calls are held open at the first shared option until both have appended their own body option, so the overlap is forced rather than waited for: without the capacity clamp both appends land in the same spare slot and each call then reads whichever wrote last. It takes ONE verb because a mutation is applied to one verb at a time — a test pitting Put against Patch stays green while either of them still clamps, which is exactly what a shared-slice defect in the other one looks like. */
func assertBodyCarryingVerbOwnsItsOptionSlice(
    t *testing.T,
    verbName string,
    call func(client *HttpClient, urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error),
) {
    t.Helper()

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

    firstServer := newBodyServer(`"v":"FIRST"`)
    defer firstServer.Close()
    secondServer := newBodyServer(`"v":"SECOND"`)
    defer secondServer.Close()

    client := NewHttpClient(NewHttpClientConfig("", 5*time.Second, nil))
    defer client.Close()

    var barrier sync.WaitGroup
    barrier.Add(2)

    shared := make([]httpclientcontract.RequestOption, 0, 4)
    shared = append(shared, func(options httpclientcontract.RequestOptions) {
        barrier.Done()
        barrier.Wait()
    })

    var waitGroup sync.WaitGroup
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()
        _, _ = call(client, firstServer.URL, map[string]any{"v": "FIRST"}, shared...)
    }()
    go func() {
        defer waitGroup.Done()
        _, _ = call(client, secondServer.URL, map[string]any{"v": "SECOND"}, shared...)
    }()

    waitGroup.Wait()

    if true == corruption.Load() {
        t.Fatalf("a %s body was delivered to the wrong endpoint through a shared options slice", verbName)
    }
}

/* Put appends a body option to the caller's slice, and Post has carried the proof of that clamp since the httpclient session while its two siblings had none — the caller cannot see past its own length, so a lost clamp is invisible to a sequential assertion. */
func TestHttpClient_PutDoesNotShareTheCallersOptionSliceAcrossConcurrentCalls(t *testing.T) {
    assertBodyCarryingVerbOwnsItsOptionSlice(
        t,
        "put",
        func(client *HttpClient, urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
            return client.Put(urlString, body, options...)
        },
    )
}

/* Patch carries the same clamp and needs its own proof: the two verbs are separate lines, and a test that drove both at once would stay green while either of them still clamped. */
func TestHttpClient_PatchDoesNotShareTheCallersOptionSliceAcrossConcurrentCalls(t *testing.T) {
    assertBodyCarryingVerbOwnsItsOptionSlice(
        t,
        "patch",
        func(client *HttpClient, urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
            return client.Patch(urlString, body, options...)
        },
    )
}

/* the redirect policy deletes three credential headers by name AFTER it has deleted the ones it learned from the client and from the request, and only a credential that reaches the request through NEITHER of those channels can prove that the by-name deletion is what removed it. A bearer token is exactly that: applyAuthorization writes the Authorization header straight onto the request, so its name never travels on the option map or on the request context, and this deletion is the only thing standing between it and a host the first server chose. */
func TestHttpClient_StripsABearerTokenOnCrossOriginRedirect(t *testing.T) {
    receivedAuthorization := ""

    target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        receivedAuthorization = request.Header.Get("Authorization")

        writer.WriteHeader(http.StatusOK)
    }))
    defer target.Close()

    origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        http.Redirect(writer, request, target.URL, http.StatusFound)
    }))
    defer origin.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    response, requestErr := client.Get(origin.URL, WithBearerToken("SECRET-TOKEN"))
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }

    if 200 != response.StatusCode() {
        t.Fatalf("unexpected status code: %d", response.StatusCode())
    }

    if "" != receivedAuthorization {
        t.Fatalf("the bearer token followed the redirect off its origin: %q", receivedAuthorization)
    }
}

/* the textual fallback is what sanitizes a url net/url refused to parse, which is exactly the url a caller built by hand and the one most likely to carry a secret. Only one of its shapes had ever been entered — the one with userinfo and no query — so three branches were blind: the query cut, the early return for a string with no scheme separator, and the early return for an authority with no userinfo. Each is asserted on its own shape, because they all answer with a string and a shared assertion would let any of them fall through. */
func TestSanitizeUrlTextually_CutsTheQueryWholeWhateverFollowsIt(t *testing.T) {
    sanitized := sanitizeUrlTextually("http://host/path\x7f?token=SECRET&page=2")

    if true == strings.Contains(sanitized, "SECRET") {
        t.Fatalf("the query value survived the textual fallback: %q", sanitized)
    }

    if false == strings.HasSuffix(sanitized, "?"+redactedValue) {
        t.Fatalf("expected the whole query to be replaced by one redaction, got %q", sanitized)
    }

    if false == strings.HasPrefix(sanitized, "http://host/path") {
        t.Fatalf("expected the scheme, host and path to survive, got %q", sanitized)
    }
}

/* a string with no scheme separator has no authority to cut a userinfo out of, and it is returned as it stands — a relative target a client with no base url was handed, which the failure report still has to name. The probe carries an at sign on purpose: without the early return the arithmetic underneath measures an authority that is not there and splices a redaction into the middle of a plain path, which is the only way this branch is distinguishable from the no-userinfo one below it. */
func TestSanitizeUrlTextually_AStringWithoutASchemeSeparatorIsReturnedUnchanged(t *testing.T) {
    sanitized := sanitizeUrlTextually("/relative@path\x7f")

    if "/relative@path\x7f" != sanitized {
        t.Fatalf("expected a string with no authority to be returned unchanged, got %q", sanitized)
    }

    if true == strings.Contains(sanitized, redactedValue) {
        t.Fatalf("expected no redaction to be spliced into a path with no authority, got %q", sanitized)
    }
}

/* an authority with no userinfo carries no credential to cut, and the url is returned with its host and path intact; without this early return the slice arithmetic underneath would splice a redaction into an authority that never had one. */
func TestSanitizeUrlTextually_AnAuthorityWithoutUserinfoIsReturnedUnchanged(t *testing.T) {
    sanitized := sanitizeUrlTextually("http://example.com/path\x7f")

    if "http://example.com/path\x7f" != sanitized {
        t.Fatalf("expected an authority with no userinfo to be returned unchanged, got %q", sanitized)
    }

    if true == strings.Contains(sanitized, redactedValue) {
        t.Fatalf("expected no redaction to be spliced into an authority that carried no credential, got %q", sanitized)
    }
}

/* an authority that ends the string — no path after it — is the shape where the userinfo cut has to measure to the end rather than to a slash that is not there. */
func TestSanitizeUrlTextually_AnAuthorityEndingTheStringStillLosesItsUserinfo(t *testing.T) {
    sanitized := sanitizeUrlTextually("http://user:SECRET@host\x7f")

    if true == strings.Contains(sanitized, "SECRET") {
        t.Fatalf("the userinfo password survived: %q", sanitized)
    }

    if false == strings.Contains(sanitized, redactedValue+":"+redactedValue+"@host") {
        t.Fatalf("expected the userinfo to be replaced in place, got %q", sanitized)
    }
}

/* every case pinned so far handed the sanitizer a url net/url REFUSES, so the parsed branches — the userinfo replacement and the fragment cut — had never run: a perfectly ordinary url with a password in it went through code no test had entered. The fragment matters because net/http does not send it, so a secret placed there reaches the log without ever reaching the wire. */
func TestSanitizeUrlForDiagnostics_ParsedUrlsLoseTheirUserinfoAndFragment(t *testing.T) {
    sanitized := sanitizeUrlForDiagnostics("https://user:SECRET@example.com/path?token=ALSOSECRET#fragment-SECRET")

    if true == strings.Contains(sanitized, "SECRET") {
        t.Fatalf("a secret survived the parsed sanitizer: %q", sanitized)
    }

    if false == strings.Contains(sanitized, "example.com/path") {
        t.Fatalf("expected the host and path to survive, got %q", sanitized)
    }

    if false == strings.Contains(sanitized, "token=") {
        t.Fatalf("expected the parameter NAMES to survive so a failure stays diagnosable, got %q", sanitized)
    }

    if true == strings.Contains(sanitized, "#") {
        t.Fatalf("expected the fragment to be dropped whole, got %q", sanitized)
    }
}

/* the client installs its own redirect policy, which means net/http's ten-hop cap is no longer in force unless this policy keeps it: without the refusal a server pointing at itself would spin the client forever on one call, holding a connection and a goroutine for the life of the process. */
func TestHttpClient_StopsAfterTooManyRedirects(t *testing.T) {
    hops := 0

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        hops = hops + 1

        http.Redirect(writer, request, "/next", http.StatusFound)
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    _, requestErr := client.Get(server.URL)
    if nil == requestErr {
        t.Fatalf("expected the redirect loop to be refused")
    }

    if false == strings.Contains(renderErrorForLog(t, requestErr), "stopped after too many redirects") {
        t.Fatalf("expected the redirect cap to be named, got %q", renderErrorForLog(t, requestErr))
    }

    if defaultMaxRedirects != hops {
        t.Fatalf("expected the exchange to stop at the documented cap, got %d hops", hops)
    }
}

/* the redirect policy strips three headers by name beyond the ones it learned from the client and from the request, and neither Cookie nor Proxy-Authorization had ever been proven. Both deletions are SHADOWED for anything this API can produce: a caller sets them through WithHeader, which puts their names on the request context, and the per-request stripping above removes them first. They are belt-and-braces against a channel that does not exist today — a cookie jar, a transport-level proxy credential — so this test pins the verdict, that neither reaches a host the first server chose, and not the position of the guard. The bearer-token test below is the one that proves the by-name deletion on its own. */
func TestHttpClient_StripsCookieAndProxyAuthorizationOnCrossOriginRedirect(t *testing.T) {
    receivedCookie := ""
    receivedProxyAuthorization := ""

    target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        receivedCookie = request.Header.Get("Cookie")
        receivedProxyAuthorization = request.Header.Get("Proxy-Authorization")

        writer.WriteHeader(http.StatusOK)
    }))
    defer target.Close()

    origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        http.Redirect(writer, request, target.URL, http.StatusFound)
    }))
    defer origin.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    response, requestErr := client.Get(
        origin.URL,
        WithHeader("Cookie", "session=SECRET"),
        WithHeader("Proxy-Authorization", "Basic SECRET"),
    )
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }

    if 200 != response.StatusCode() {
        t.Fatalf("unexpected status code: %d", response.StatusCode())
    }

    if "" != receivedCookie {
        t.Fatalf("the cookie followed the redirect off its origin: %q", receivedCookie)
    }

    if "" != receivedProxyAuthorization {
        t.Fatalf("the proxy credential followed the redirect off its origin: %q", receivedProxyAuthorization)
    }
}

/* the streaming path judges the explicit cap before anything is dialled, the rule the buffered path always held and asserts by counting zero server hits: refused after the exchange, a POST that had already committed its side effect answered with an error phrased as though nothing had been sent, and a caller retrying on it duplicated the operation. */
func TestHttpClient_StreamRefusesAnInvalidCapBeforeTheRequestIsSent(t *testing.T) {
    serverHits := 0

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        serverHits = serverHits + 1

        writer.WriteHeader(http.StatusOK)
        _, _ = writer.Write([]byte("body"))
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    _, requestErr := client.RequestStream(http.MethodPost, server.URL, WithMaxResponseBodyBytes(0))
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

/* the nil-option refusal had been proven on the buffered path alone, and the streaming path folds its options through the same function — but nothing had ever entered it from there, so a streaming call was relying on a guard proven for its sibling. */
func TestHttpClient_StreamRefusesANilRequestOption(t *testing.T) {
    serverHits := 0

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        serverHits = serverHits + 1

        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    _, requestErr := client.RequestStream(http.MethodGet, server.URL, nil)
    if nil == requestErr {
        t.Fatalf("expected a nil request option to be refused on the streaming path")
    }

    if "nil request option" != requestErr.Error() {
        t.Fatalf("unexpected refusal message: %q", requestErr.Error())
    }

    if 0 != serverHits {
        t.Fatalf("expected the option refusal to arrive before anything was dialled, got %d server hits", serverHits)
    }
}

/* a body json cannot encode — a channel, a function, a cyclic structure — fails before anything is dialled, and the refusal has to name the encoding rather than the transport: a caller shown a request failure would look at the network for a mistake that is in its own value. */
func TestHttpClient_AJsonBodyThatCannotBeEncodedIsRefusedBeforeDialling(t *testing.T) {
    serverHits := 0

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        serverHits = serverHits + 1

        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    _, requestErr := client.Post(server.URL, make(chan int))
    if nil == requestErr {
        t.Fatalf("expected a body json cannot encode to be refused")
    }

    if "failed to marshal json body" != requestErr.Error() {
        t.Fatalf("unexpected refusal message: %q", requestErr.Error())
    }

    if 0 != serverHits {
        t.Fatalf("expected the encoding refusal to arrive before anything was dialled, got %d server hits", serverHits)
    }
}

/* a string body is sent verbatim, with no content type invented for it — the branch is what lets a caller send xml, form-encoded text or a pre-rendered json document under a content type it names itself, and nothing had ever entered it: a string falling through to the unsupported-type refusal would have been discovered by an application, not by the suite. */
func TestHttpClient_AStringBodyIsSentVerbatimWithoutAnInventedContentType(t *testing.T) {
    receivedBody := ""
    receivedContentType := ""

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        body, _ := io.ReadAll(request.Body)
        receivedBody = string(body)
        receivedContentType = request.Header.Get("Content-Type")

        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    _, requestErr := client.Request(
        http.MethodPost,
        server.URL,
        WithBody("<document>one</document>"),
        WithHeader("Content-Type", "application/xml"),
    )
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }

    if "<document>one</document>" != receivedBody {
        t.Fatalf("expected the string body to be sent verbatim, got %q", receivedBody)
    }

    if "application/xml" != receivedContentType {
        t.Fatalf("expected the caller's own content type, got %q", receivedContentType)
    }
}

/* a body that ends before the length the server declared is a truncated response, and it has to be reported as a read failure rather than handed to the caller as a short body — a json document cut in half decodes to a zero value, and the caller acts on it. */
func TestHttpClient_ATruncatedResponseBodyIsReportedAsAReadFailure(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        hijacker, isHijacker := writer.(http.Hijacker)
        if false == isHijacker {
            t.Errorf("expected the test server to support hijacking")

            return
        }

        connection, buffered, hijackErr := hijacker.Hijack()
        if nil != hijackErr {
            t.Errorf("unexpected hijack error: %v", hijackErr)

            return
        }

        _, _ = buffered.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\ntruncated")
        _ = buffered.Flush()
        _ = connection.Close()
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    _, requestErr := client.Get(server.URL)
    if nil == requestErr {
        t.Fatalf("expected the truncated body to be reported")
    }

    if "failed to read response body" != requestErr.Error() {
        t.Fatalf("unexpected refusal message: %q", requestErr.Error())
    }
}

/* the origin check parses both sides, and each parse has a refusal of its own: a base url the configuration mangled and a target the caller mangled are different mistakes, and reporting either as the other sends a reader to the wrong file. Neither had been entered. */
func TestHttpClient_AnUnparsableBaseUrlIsNamedAsTheBase(t *testing.T) {
    client := NewDefaultHttpClient()
    defer client.Close()

    client.SetBaseUrl("http://user:SECRET@exam ple.com")

    _, requestErr := client.Get("http://other.example.com/path")
    if nil == requestErr {
        t.Fatalf("expected the unparsable base url to be refused")
    }

    if "failed to parse the base url" != requestErr.Error() {
        t.Fatalf("unexpected refusal message: %q", requestErr.Error())
    }

    rendered := renderErrorForLog(t, requestErr)

    if true == strings.Contains(rendered, "SECRET") {
        t.Fatalf("the base url credential reached the report: %q", rendered)
    }

    if false == strings.Contains(rendered, "invalid character") {
        t.Fatalf("expected net/url's own diagnosis to survive without the url it quotes, got %q", rendered)
    }
}

/* the target half of the same check, which fires for an absolute url the caller built by hand — the report has to name the request url rather than the base one, because a caller reading it is looking at its own call site. */
func TestHttpClient_AnUnparsableAbsoluteTargetIsNamedAsTheRequestUrl(t *testing.T) {
    client := NewDefaultHttpClient()
    defer client.Close()

    client.SetBaseUrl("http://example.com")

    _, requestErr := client.Get("http://example.com/pa\x7fth?token=SECRET")
    if nil == requestErr {
        t.Fatalf("expected the unparsable absolute target to be refused")
    }

    if "failed to parse request url" != requestErr.Error() {
        t.Fatalf("unexpected refusal message: %q", requestErr.Error())
    }

    if true == strings.Contains(renderErrorForLog(t, requestErr), "SECRET") {
        t.Fatalf("the query secret reached the report: %q", renderErrorForLog(t, requestErr))
    }
}

/* the buffered path reads one byte past the cap precisely so a body ending EXACTLY at it is delivered rather than refused; the streaming sibling has carried that proof since the httpclient session and the buffered one had not, so an off-by-one there would have refused every response that filled its budget exactly. */
func TestHttpClient_ABufferedBodyEndingExactlyAtTheCapIsDelivered(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(http.StatusOK)
        _, _ = writer.Write([]byte("0123456789"))
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    response, requestErr := client.Get(server.URL, WithMaxResponseBodyBytes(10))
    if nil != requestErr {
        t.Fatalf("expected a body ending exactly at the cap to be delivered, got %v", requestErr)
    }

    if "0123456789" != response.String() {
        t.Fatalf("unexpected body: %q", response.String())
    }

    _, oneOverErr := client.Get(server.URL, WithMaxResponseBodyBytes(9))
    if nil == oneOverErr {
        t.Fatalf("expected a body one byte past the cap to be refused")
    }

    if "response body exceeded max size" != oneOverErr.Error() {
        t.Fatalf("unexpected refusal message: %q", oneOverErr.Error())
    }
}

/* the origin comparison refuses a nil side rather than dereferencing it. No public path can hand it one — both call sites pass urls that are non-nil by construction — but the function is the credential boundary itself, and a fail-open answer here would keep an api key on a redirect that left the origin. Recorded in the backlog as unreachable through the public API. */
func TestIsSameOrigin_ANilSideIsNotTheSameOrigin(t *testing.T) {
    if true == isSameOrigin(nil, mustParseUrl(t, "https://example.com")) {
        t.Fatalf("expected a nil origin to be refused")
    }

    if true == isSameOrigin(mustParseUrl(t, "https://example.com"), nil) {
        t.Fatalf("expected a nil target to be refused")
    }

    if true == isSameOrigin(nil, nil) {
        t.Fatalf("expected two nil sides to be refused")
    }
}

/* the effective port of a scheme that is neither http nor https is the empty string, which makes two urls of one unknown scheme compare EQUAL on port whatever ports they name — a fail-open answer. It cannot be reached today because the schemes must match first and every path guarantees http or https on at least one side, so this pins the behaviour rather than endorsing it; carried to the backlog beside the aliasing findings. */
func TestEffectivePort_AnUnknownSchemeYieldsNoPortAtAll(t *testing.T) {
    if "" != effectivePort(mustParseUrl(t, "ftp://example.com/file")) {
        t.Fatalf("expected an unknown scheme to imply no port, got %q", effectivePort(mustParseUrl(t, "ftp://example.com/file")))
    }

    if "21" != effectivePort(mustParseUrl(t, "ftp://example.com:21/file")) {
        t.Fatalf("expected a spelled-out port to win whatever the scheme, got %q", effectivePort(mustParseUrl(t, "ftp://example.com:21/file")))
    }

    if "443" != effectivePort(mustParseUrl(t, "HTTPS://example.com")) {
        t.Fatalf("expected the scheme comparison to be case-insensitive, got %q", effectivePort(mustParseUrl(t, "HTTPS://example.com")))
    }
}

type foreignRoundTripper struct{}

func (instance foreignRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
    return nil, nil
}

/* Close reaches for the transport's own idle pool, and a transport that is not net/http's has none to release: the guard is what keeps Close a no-op instead of a panic there. Nothing can install a foreign transport through the public API today — there is no setter and no configuration hook — so the transport is replaced white-box; the guard becomes load-bearing the moment such a hook is added. Recorded in the backlog as unreachable through the public API. */
func TestHttpClient_CloseIsANoOpForATransportItDoesNotOwn(t *testing.T) {
    client := NewDefaultHttpClient()

    client.client.Transport = foreignRoundTripper{}

    if closeErr := client.Close(); nil != closeErr {
        t.Fatalf("expected Close to succeed for a foreign transport, got %v", closeErr)
    }
}

/* the parse-error sanitizer unwraps a *url.Error to drop the url its message quotes, and falls back to the error text for anything else. Every caller feeds it a *url.Error today, so the fallback is unreachable — but it is what keeps the sanitizer total: a nil return or a panic there would take out the very report a failed url was being described in. */
func TestSanitizeUrlParseError_AnErrorThatIsNotAUrlErrorKeepsItsOwnText(t *testing.T) {
    sanitized := sanitizeUrlParseError(errors.New("a plain failure"))

    if "a plain failure" != sanitized {
        t.Fatalf("unexpected sanitized text: %q", sanitized)
    }

    wrapped := &url.Error{Op: "parse", URL: "https://user:SECRET@example.com", Err: errors.New("invalid character")}

    sanitized = sanitizeUrlParseError(wrapped)

    if "invalid character" != sanitized {
        t.Fatalf("expected the quoted url to be dropped, got %q", sanitized)
    }
}

/* the unsupported-body report names the type it was handed, and a nil reaching it would have no type to name — the branch answers "nil" rather than dereferencing. A nil body is filtered one step earlier so nothing can reach it, which is why it is pinned directly. */
func TestTypeNameOf_ANilValueIsNamedRatherThanDereferenced(t *testing.T) {
    if "nil" != typeNameOf(nil) {
        t.Fatalf("unexpected name for a nil value: %q", typeNameOf(nil))
    }

    if "int" != typeNameOf(3) {
        t.Fatalf("unexpected name for an int: %q", typeNameOf(3))
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

/* SetBaseUrl is the second door a base url enters through; it holds the constructor's slash rule so a rewire cannot smuggle in the base the constructor refuses — and the refused base is not stored. */
func TestHttpClient_SetBaseUrlRefusesAPathWithoutATrailingSlash(t *testing.T) {
    client := NewDefaultHttpClient()
    defer client.Close()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the setter to refuse a base url path without a trailing slash")
        }

        err, ok := recovered.(error)
        if false == ok {
            t.Fatalf("expected the refusal to travel as an error, got %T", recovered)
        }
        if false == strings.Contains(err.Error(), "must end with a slash") {
            t.Fatalf("expected the refusal to name the missing slash, got %q", err.Error())
        }

        if "" != client.baseUrl {
            t.Fatalf("expected the refused base url not to be stored, got %q", client.baseUrl)
        }
    }()

    client.SetBaseUrl("https://api.example.com/v1")
}

/* the network-path reference is the form a prefix sniff cannot see: it names no scheme, so it read as a path and was hung under the base — while RFC resolution sends it to the host IT names, on the base's scheme. Judging the RESOLVED url refuses it as the foreign origin it is. */
func TestHttpClient_ANetworkPathReferenceIsRefusedAsAForeignOrigin(t *testing.T) {
    client := NewHttpClient(
        NewHttpClientConfig(
            "https://api.internal.example/",
            0,
            map[string]string{"X-Api-Key": "super-secret"},
        ),
    )

    _, err := client.buildUrl("//attacker.example/steal", nil)
    if nil == err {
        t.Fatalf("expected the network-path reference to be refused")
    }
    if false == strings.Contains(err.Error(), "leaves the origin") {
        t.Fatalf("expected the origin refusal, got %q", err.Error())
    }
}

/* a relative target on a client with no base url cannot name a host; it used to fall through to net/http, which answered "unsupported protocol scheme" behind a "failed to create request" — the cause was there but never named. The refusal belongs to buildUrl, which is where the base url would have supplied the missing half. */
func TestHttpClient_ARelativeTargetWithoutABaseUrlIsRefusedByName(t *testing.T) {
    client := NewDefaultHttpClient()
    defer client.Close()

    _, err := client.Get("/users")
    if nil == err {
        t.Fatalf("expected a relative target without a base url to be refused")
    }
    if "the request url is relative and the client has no base url" != err.Error() {
        t.Fatalf("unexpected refusal message: %q", err.Error())
    }
}

/* the pointer fields exist so a SET zero reaches net/http verbatim; this drives two of them through the constructor to the transport itself. */
func TestNewHttpClient_ASetZeroReachesTheTransportVerbatim(t *testing.T) {
    client := NewHttpClient(
        NewHttpClientConfig("", 0, nil).WithTransport(&TransportConfig{
            MaxIdleConns:    TransportCount(0),
            IdleConnTimeout: TransportDuration(0),
        }),
    )
    defer client.Close()

    transport, ok := client.client.Transport.(*http.Transport)
    if false == ok {
        t.Fatalf("expected the client to build a net/http transport")
    }

    if 0 != transport.MaxIdleConns {
        t.Fatalf("expected the set zero MaxIdleConns to reach the transport, got %d", transport.MaxIdleConns)
    }

    if 0 != transport.IdleConnTimeout {
        t.Fatalf("expected the set zero IdleConnTimeout to reach the transport, got %v", transport.IdleConnTimeout)
    }
}
