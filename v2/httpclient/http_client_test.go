package httpclient

import (
    "bytes"
    "encoding/base64"
    "io"
    "net/http"
    "net/http/httptest"
    "strconv"
    "sync"
    "testing"
    "time"
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
