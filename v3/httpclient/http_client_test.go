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

func TestNewHttpClient_NegativeTimeoutFallsBackToDefault(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("", -1*time.Second, nil))

    if 30*time.Second != client.client.Timeout {
        t.Fatalf("expected a negative timeout to fall back to the 30s default, got %v", client.client.Timeout)
    }
    if 30*time.Second != client.timeout {
        t.Fatalf("expected the stored timeout to fall back to the 30s default, got %v", client.timeout)
    }
}

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

func TestHttpClient_RequestStreamWithNilContextIsRefused(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("http://127.0.0.1:1", 0, nil))

    _, err := client.RequestStreamWithContext(nil, http.MethodGet, "/")
    if nil == err {
        t.Fatalf("expected a nil context to be refused")
    }
    if "request context is nil" != err.Error() {
        t.Fatalf("expected the refusal to name the nil context rather than fail inside net/http, got %q", err.Error())
    }
}

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

func TestHttpClient_PutDoesNotShareTheCallersOptionSliceAcrossConcurrentCalls(t *testing.T) {
    assertBodyCarryingVerbOwnsItsOptionSlice(
        t,
        "put",
        func(client *HttpClient, urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
            return client.Put(urlString, body, options...)
        },
    )
}

func TestHttpClient_PatchDoesNotShareTheCallersOptionSliceAcrossConcurrentCalls(t *testing.T) {
    assertBodyCarryingVerbOwnsItsOptionSlice(
        t,
        "patch",
        func(client *HttpClient, urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error) {
            return client.Patch(urlString, body, options...)
        },
    )
}

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

func TestSanitizeUrlTextually_AStringWithoutAnAuthorityIsReturnedUnchanged(t *testing.T) {
    sanitized := sanitizeUrlTextually("/relative@path\x7f")

    if "/relative@path\x7f" != sanitized {
        t.Fatalf("expected a string with no authority to be returned unchanged, got %q", sanitized)
    }

    if true == strings.Contains(sanitized, redactedValue) {
        t.Fatalf("expected no redaction to be spliced into a path with no authority, got %q", sanitized)
    }
}

func TestSanitizeUrlTextually_AnAuthorityWithoutUserinfoIsReturnedUnchanged(t *testing.T) {
    sanitized := sanitizeUrlTextually("http://example.com/path\x7f")

    if "http://example.com/path\x7f" != sanitized {
        t.Fatalf("expected an authority with no userinfo to be returned unchanged, got %q", sanitized)
    }

    if true == strings.Contains(sanitized, redactedValue) {
        t.Fatalf("expected no redaction to be spliced into an authority that carried no credential, got %q", sanitized)
    }
}

func TestSanitizeUrlTextually_AnAuthorityEndingTheStringStillLosesItsUserinfo(t *testing.T) {
    sanitized := sanitizeUrlTextually("http://user:SECRET@host\x7f")

    if true == strings.Contains(sanitized, "SECRET") {
        t.Fatalf("the userinfo password survived: %q", sanitized)
    }

    if false == strings.Contains(sanitized, redactedValue+":"+redactedValue+"@host") {
        t.Fatalf("expected the userinfo to be replaced in place, got %q", sanitized)
    }
}

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

func TestHttpClient_CloseIsANoOpForATransportItDoesNotOwn(t *testing.T) {
    client := NewDefaultHttpClient()

    client.client.Transport = foreignRoundTripper{}

    if closeErr := client.Close(); nil != closeErr {
        t.Fatalf("expected Close to succeed for a foreign transport, got %v", closeErr)
    }
}

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

func TestTypeNameOf_ANilValueIsNamedRatherThanDereferenced(t *testing.T) {
    if "nil" != typeNameOf(nil) {
        t.Fatalf("unexpected name for a nil value: %q", typeNameOf(nil))
    }

    if "int" != typeNameOf(3) {
        t.Fatalf("unexpected name for an int: %q", typeNameOf(3))
    }
}

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

func TestSanitizeUrlTextually_ASchemeRelativeAuthorityLosesItsUserinfo(t *testing.T) {
    for _, currentCase := range []struct {
        name  string
        value string
    }{
        {"a port that is not a number", "//user:SECRET@host:notaport/path"},
        {"a control character in the authority", "//user:SECRET@host\x00/path"},
        {"a broken percent escape", "//user:SECRET@ho%zzst/path"},
        {"an unclosed bracket", "//user:SECRET@[fe80::1/path"},
    } {
        if _, err := url.Parse(currentCase.value); nil == err {
            t.Fatalf("%s: the probe no longer reaches the textual fallback — net/url parsed %q", currentCase.name, currentCase.value)
        }

        sanitized := sanitizeUrlTextually(currentCase.value)

        if true == strings.Contains(sanitized, "SECRET") {
            t.Fatalf("%s: the credential survived the textual fallback: %q", currentCase.name, sanitized)
        }

        if false == strings.HasPrefix(sanitized, "//"+redactedValue+":"+redactedValue+"@") {
            t.Fatalf("%s: expected the userinfo replaced in place, got %q", currentCase.name, sanitized)
        }
    }
}

func TestSanitizeUrlForDiagnostics_AnOpaqueUrlLosesItsUserinfo(t *testing.T) {
    for _, currentCase := range []struct {
        name  string
        value string
    }{
        {"a scheme with one slash missing", "http:user:SECRET@host/path"},
        {"no slashes at all", "user:SECRET@host/path"},
        {"an authority ending the reference", "http:user:SECRET@host"},
    } {
        parsed, err := url.Parse(currentCase.value)
        if nil != err {
            t.Fatalf("%s: the probe no longer exercises the parsed branch — net/url refused %q", currentCase.name, currentCase.value)
        }

        if "" == parsed.Opaque {
            t.Fatalf("%s: the probe no longer parses in opaque form: %#v", currentCase.name, parsed)
        }

        if nil != parsed.User {
            t.Fatalf("%s: the probe no longer bypasses the userinfo branch: %v", currentCase.name, parsed.User)
        }

        sanitized := sanitizeUrlForDiagnostics(currentCase.value)

        if true == strings.Contains(sanitized, "SECRET") {
            t.Fatalf("%s: the credential survived the parsed sanitizer: %q", currentCase.name, sanitized)
        }

        if false == strings.Contains(sanitized, "host") {
            t.Fatalf("%s: expected the host to survive so a failure stays diagnosable, got %q", currentCase.name, sanitized)
        }
    }
}

func TestSanitizeUrlForDiagnostics_AParsableSchemeRelativeUrlKeepsItsRedaction(t *testing.T) {
    sanitized := sanitizeUrlForDiagnostics("//user:SECRET@host/path")

    if true == strings.Contains(sanitized, "SECRET") {
        t.Fatalf("the credential survived: %q", sanitized)
    }

    if "//"+redactedValue+":"+redactedValue+"@host/path" != sanitized {
        t.Fatalf("expected the parsed branch to redact in place, got %q", sanitized)
    }
}

func TestSchemeSeparatorIndex_ReadsOnlyARealScheme(t *testing.T) {
    for _, currentCase := range []struct {
        value    string
        expected int
    }{
        {"http:rest", 4},
        {"a:rest", 1},
        {"ab+c-d.e:rest", 8},
        {"HTTP:rest", 4},
        {":rest", -1},
        {"1http:rest", -1},
        {"/relative@path", -1},
        {"no-colon-at-all", -1},
        {"has space:rest", -1},
        {"/path:with@colon", -1},
    } {
        if currentCase.expected != schemeSeparatorIndex(currentCase.value) {
            t.Fatalf("expected %q to report %d, got %d", currentCase.value, currentCase.expected, schemeSeparatorIndex(currentCase.value))
        }
    }
}

func TestSanitizeUrlTextually_AnOpaqueReferenceLosesItsUserinfo(t *testing.T) {
    for _, currentCase := range []struct {
        name  string
        value string
    }{
        {"a scheme with one slash missing", "http:user:SECRET@host\x00/path"},
        {"no slashes at all", "user:SECRET@host\x00/path"},
    } {
        if _, err := url.Parse(currentCase.value); nil == err {
            t.Fatalf("%s: the probe no longer reaches the textual fallback — net/url parsed %q", currentCase.name, currentCase.value)
        }

        sanitized := sanitizeUrlTextually(currentCase.value)

        if true == strings.Contains(sanitized, "SECRET") {
            t.Fatalf("%s: the credential survived the textual fallback: %q", currentCase.name, sanitized)
        }

        if false == strings.Contains(sanitized, redactedValue+":"+redactedValue+"@host") {
            t.Fatalf("%s: expected the userinfo replaced in place, got %q", currentCase.name, sanitized)
        }
    }
}

func TestHttpClient_WithoutRedirectsAnswersTheRedirectAsTheResponse(t *testing.T) {
    sinkPosts := 0
    pageGets := 0

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if "/sink" == request.URL.Path {
            sinkPosts = sinkPosts + 1
            http.Redirect(writer, request, "/page", http.StatusFound)

            return
        }

        pageGets = pageGets + 1
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig("", 5*time.Second, nil).WithoutRedirects())
    defer client.Close()

    response, requestErr := client.Post(server.URL+"/sink", map[string]any{"reading": 1})
    if nil != requestErr {
        t.Fatalf("expected the redirect to be answered, not refused: %v", requestErr)
    }

    if http.StatusFound != response.StatusCode() || "/page" != response.Headers().Get("Location") {
        t.Fatalf("expected the 302 and its Location to reach the caller, got %d %q", response.StatusCode(), response.Headers().Get("Location"))
    }

    if 1 != sinkPosts || 0 != pageGets {
        t.Fatalf("expected one post at the sink and no get at the page, got %d posts and %d gets", sinkPosts, pageGets)
    }
}

func TestHttpClient_FollowsRedirectsUnlessToldOtherwise(t *testing.T) {
    lastMethod := ""

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if "/sink" == request.URL.Path {
            http.Redirect(writer, request, "/page", http.StatusFound)

            return
        }

        lastMethod = request.Method
        writer.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    config := NewHttpClientConfig("", 5*time.Second, nil)
    if false == config.FollowsRedirects() {
        t.Fatal("expected a config that was not told otherwise to follow redirects")
    }

    client := NewHttpClient(config)
    defer client.Close()

    response, requestErr := client.Post(server.URL+"/sink", map[string]any{"reading": 1})
    if nil != requestErr {
        t.Fatalf("expected the redirect to be followed: %v", requestErr)
    }

    if http.StatusOK != response.StatusCode() || http.MethodGet != lastMethod {
        t.Fatalf("expected the page to answer a GET, got %d after %q", response.StatusCode(), lastMethod)
    }
}
