package httpclient

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestResponseHelpers_StatusClassificationAndString(t *testing.T) {
    response := NewResponse(201, "201 Created", http.Header{}, []byte("ok"), nil)

    if false == response.IsSuccess() {
        t.Fatalf("expected success")
    }
    if true == response.IsClientError() {
        t.Fatalf("expected not client error")
    }
    if true == response.IsServerError() {
        t.Fatalf("expected not server error")
    }
    if "ok" != response.String() {
        t.Fatalf("unexpected string")
    }

    response = NewResponse(404, "404 Not Found", http.Header{}, []byte("no"), nil)
    if true == response.IsSuccess() {
        t.Fatalf("expected not success")
    }
    if false == response.IsClientError() {
        t.Fatalf("expected client error")
    }

    response = NewResponse(500, "500 Internal Server Error", http.Header{}, []byte("error"), nil)
    if false == response.IsServerError() {
        t.Fatalf("expected server error")
    }
}

/* @info the three accessors that carry everything about a response except its body and its status code had never been executed. Status is the reason phrase a caller logs, Headers is what a caller reads a content type or a rate-limit budget out of, and Request is the only way back to the url the exchange actually reached after the base url and the query were folded in — a redirect chain ends somewhere the caller never spelled, and this is where it says so. */
func TestResponse_StatusHeadersAndRequestCarryWhatTheExchangeProduced(t *testing.T) {
    requestedUrl := ""

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        requestedUrl = request.URL.String()

        writer.Header().Set("X-Budget", "17")
        writer.WriteHeader(http.StatusCreated)
        _, _ = writer.Write([]byte("made"))
    }))
    defer server.Close()

    client := NewDefaultHttpClient()
    defer client.Close()

    response, requestErr := client.Get(server.URL+"/things", WithQuery("page", "2"))
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }

    if "201 Created" != response.Status() {
        t.Fatalf("expected the reason phrase the server sent, got %q", response.Status())
    }

    if "17" != response.Headers().Get("X-Budget") {
        t.Fatalf("expected the response headers to carry the server's own, got %v", response.Headers())
    }

    if nil == response.Request() {
        t.Fatalf("expected the response to carry the request that produced it")
    }

    if server.URL+"/things?page=2" != response.Request().URL.String() {
        t.Fatalf("expected the request to carry the url the exchange actually reached, got %q", response.Request().URL.String())
    }

    if "/things?page=2" != requestedUrl {
        t.Fatalf("expected the server to have been asked for the resolved url, got %q", requestedUrl)
    }
}

/* @info Headers hands back the map it was constructed with, without a copy, and the buffered path constructs it with the transport's own header map while the STREAMING path clones. This test records the behaviour as it stands today, not as it ought to be: a caller that mutates what Headers returns changes what every later reader of the same response sees, where the sibling API does not. Carried to the backlog as a decision to take, alongside the plural request options. */
func TestResponse_HeadersHandsBackTheLiveMapWhereTheStreamingSiblingClones(t *testing.T) {
    headers := http.Header{}
    headers.Set("X-Budget", "17")

    response := NewResponse(200, "200 OK", headers, []byte("ok"), nil)

    response.Headers().Set("X-Budget", "mutated")

    if "mutated" != headers.Get("X-Budget") {
        t.Fatalf("expected the buffered response to hand back the live map it was given, got %q", headers.Get("X-Budget"))
    }

    streamHeaders := http.Header{}
    streamHeaders.Set("X-Budget", "17")

    streamResponse := NewStreamResponse(200, streamHeaders.Clone(), nil)

    streamResponse.Headers().Set("X-Budget", "mutated")

    if "17" != streamHeaders.Get("X-Budget") {
        t.Fatalf("expected the streaming path's clone to keep the caller's map intact, got %q", streamHeaders.Get("X-Budget"))
    }
}
