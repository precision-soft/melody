package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"
)

/* Every section built on this client drives ONE application: the v3 .example the dev container supervises on
EXAMPLE_BASE_URL (:8080). That application is a shared fixture, so the constraints below hold for each call written
through this client, and they are stated HERE — beside the calls — rather than in a section a later reader may
never open:

  - /ratelimit/demo is off limits. EXAMPLE OVER HTTP (http.go) clears the example's redis counters and then
    asserts the EXACT call at which the budget is exhausted; one extra request from anywhere else moves that
    point and turns an unrelated section red for a reason nothing in its own output explains;
  - /outbox/* is off limits. stack.sh's OUTBOX FACTORIES check asserts its sent count GREW because of its own
    enqueue; a row relayed from here would satisfy that assertion without the check having caused it;
  - nothing here logs in, and this client deliberately carries NO cookie jar. A session cookie picked up by a
    shared jar would travel onto later requests and could authenticate a route a section means to probe
    anonymously — and worse, it would put the login sections' assertions at the mercy of call ordering.

The client exists at all because exampleClient.call (example.go) cannot set arbitrary request headers, and every
new section turns on one: Authorization, X-Switch-User, X-Melody-Internal-Auth, X-2FA-Code, Accept. */
type liveExampleClient struct {
    baseUrl string
    client  *http.Client
}

func newLiveExampleClient(baseUrl string) *liveExampleClient {
    return &liveExampleClient{
        baseUrl: strings.TrimRight(baseUrl, "/"),
        client: &http.Client{
            Timeout: 15 * time.Second,
            /* a redirect is an assertion in its own right here — the TRANSLATION section proves the token
               firewall answers 401 json instead of the global entry point's 302 — so the status has to reach the
               caller unfollowed */
            CheckRedirect: func(request *http.Request, previous []*http.Request) error {
                return http.ErrUseLastResponse
            },
        },
    }
}

/* liveExampleRequest describes one call. The header map is what this client adds over exampleClient.call; a
contentType of "" leaves the header unset, which matters for the GET probes (a Content-Type on a body-less GET
would change the request the framework sees). */
type liveExampleRequest struct {
    method      string
    path        string
    contentType string
    headerList  map[string]string
    body        []byte
}

type liveExampleResponse struct {
    statusCode  int
    location    string
    contentType string
    headerList  http.Header
    body        []byte
}

func (instance liveExampleResponse) bodyText() string {
    return string(instance.body)
}

/* call issues one request and reads the whole response. The path is appended to the base url as a raw string so
an encoded segment reaches the application still encoded. */
func (instance *liveExampleClient) call(label string, callRequest liveExampleRequest) liveExampleResponse {
    var reader io.Reader
    if 0 < len(callRequest.body) {
        reader = bytes.NewReader(callRequest.body)
    }

    request, requestErr := http.NewRequest(callRequest.method, instance.baseUrl+callRequest.path, reader)
    if nil != requestErr {
        fail("%s: build a %s %s request: %v", label, callRequest.method, callRequest.path, requestErr)
    }

    if "" != callRequest.contentType {
        request.Header.Set("Content-Type", callRequest.contentType)
    }
    for name, value := range callRequest.headerList {
        request.Header.Set(name, value)
    }

    response, responseErr := instance.client.Do(request)
    if nil != responseErr {
        fail("%s: %s %s: %v", label, callRequest.method, callRequest.path, responseErr)
    }
    defer func() {
        _ = response.Body.Close()
    }()

    body, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        fail("%s: read the %s %s body: %v", label, callRequest.method, callRequest.path, readErr)
    }

    return liveExampleResponse{
        statusCode:  response.StatusCode,
        location:    response.Header.Get("Location"),
        contentType: response.Header.Get("Content-Type"),
        headerList:  response.Header,
        body:        body,
    }
}

func (instance *liveExampleClient) get(label string, path string) liveExampleResponse {
    return instance.call(label, liveExampleRequest{method: "GET", path: path})
}

/* openStream starts a request and hands back the still-open response so the caller can read the body as it
arrives. It is used by the server-sent-event section and by nothing else: the client here carries no overall
Timeout, because a streaming response never completes and a Client.Timeout would cut the stream mid-check and be
read as the application closing it. The caller MUST close the returned body — an abandoned stream leaves a
subscriber and a goroutine inside the long-lived supervised application. */
func (instance *liveExampleClient) openStream(label string, path string, headerList map[string]string) *http.Response {
    request, requestErr := http.NewRequest("GET", instance.baseUrl+path, nil)
    if nil != requestErr {
        fail("%s: build the streaming request for %s: %v", label, path, requestErr)
    }

    for name, value := range headerList {
        request.Header.Set(name, value)
    }

    streamingClient := &http.Client{
        Transport: &http.Transport{
            /* the headers still have to arrive promptly: without this a stream whose handler never wrote its
               preamble would hang the section instead of failing it */
            ResponseHeaderTimeout: 10 * time.Second,
            DisableCompression:    true,
        },
    }

    response, responseErr := streamingClient.Do(request)
    if nil != responseErr {
        fail("%s: open the stream at %s: %v", label, path, responseErr)
    }

    return response
}

/* liveExampleEnvelope is the shape the example's presenter wraps every api payload in
(presenter/error_presenter.go): success, payload, errors. Decoding it is what separates "the route answered 200"
from "the route answered its payload" — an html error page or a proxy notice returning 200 fails the decode. */
type liveExampleEnvelope struct {
    Success bool            `json:"success"`
    Payload json.RawMessage `json:"payload"`
    Errors  []string        `json:"errors"`
}

func decodeLiveExampleEnvelope(label string, response liveExampleResponse) liveExampleEnvelope {
    envelope := liveExampleEnvelope{}

    decoder := json.NewDecoder(bytes.NewReader(response.body))
    if decodeErr := decoder.Decode(&envelope); nil != decodeErr {
        fail(
            "%s: the %d response is not the example's json envelope (%v): %s",
            label,
            response.statusCode,
            decodeErr,
            exampleTruncate(response.bodyText()),
        )
    }

    return envelope
}

/* decodeLiveExamplePayload decodes the envelope and then its payload into target, refusing an envelope that
reports failure — a section that decoded a payload out of {"success":false} would be asserting on zero values. */
func decodeLiveExamplePayload(label string, response liveExampleResponse, target any) {
    envelope := decodeLiveExampleEnvelope(label, response)

    if false == envelope.Success {
        fail(
            "%s: the %d response reports success=false with errors %v: %s",
            label,
            response.statusCode,
            envelope.Errors,
            exampleTruncate(response.bodyText()),
        )
    }

    if decodeErr := json.Unmarshal(envelope.Payload, target); nil != decodeErr {
        fail("%s: decode the envelope payload (%v): %s", label, decodeErr, exampleTruncate(response.bodyText()))
    }
}

/* requireLiveExampleStatus is the one-line guard every section opens with, and it names the label so a run whose
supervised application is stale reports WHICH section could not be exercised rather than a bare status mismatch. */
func requireLiveExampleStatus(label string, what string, response liveExampleResponse, expected int) {
    if expected != response.statusCode {
        fail(
            "%s: %s answered %d, wanted %d: %s",
            label,
            what,
            response.statusCode,
            expected,
            exampleTruncate(response.bodyText()),
        )
    }
}

/* liveExampleUnique builds a per-run suffix for every identifier a section writes into shared infrastructure.
Reusing a fixed one makes a section pass on a row an EARLIER run created, which is the same vacuous-pass class the
outbox and object-storage sections guard against with their own per-run namespaces. */
func liveExampleUnique(prefix string) string {
    return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
