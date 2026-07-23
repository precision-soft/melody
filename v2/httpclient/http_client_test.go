package httpclient

import (
    "bytes"
    "encoding/base64"
    "io"
    "math"
    "net/http"
    "net/http/httptest"
    "net/url"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    httpclientcontract "github.com/precision-soft/melody/v2/httpclient/contract"
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
