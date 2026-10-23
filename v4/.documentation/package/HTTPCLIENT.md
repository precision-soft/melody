# HTTPCLIENT

The [`httpclient`](../../httpclient) package provides a small outbound HTTP client abstraction with typed request options and typed response helpers.

## Scope

This package is intended for simple outbound HTTP calls from userland and framework modules. It wraps Go’s `net/http` client with:

- one configuration struct for the base URL, the default headers, the timeout, the transport and the redirect policy,
- one request options struct (headers, query params, body, JSON, timeout, credential, body cap),
- response helpers for decoding JSON and inspecting status classes,
- optional streaming responses.

## Subpackages

- [`httpclient/contract`](../../httpclient/contract)\
  Public contracts for the client, request options, and response types.

## Responsibilities

- Client construction and configuration:
    - [`HttpClient`](../../httpclient/http_client.go)
    - [`NewDefaultHttpClient`](../../httpclient/http_client.go)
    - [`NewHttpClient`](../../httpclient/http_client.go)
    - [`HttpClientConfig`](../../httpclient/http_client_config.go)
    - [`TransportConfig`](../../httpclient/transport_config.go)
- Request options:
    - [`RequestOptions`](../../httpclient/request_option.go) and [`BasicAuth`](../../httpclient/request_option.go), the contract's structs named in this package
- Response types and helpers:
    - [`Response`](../../httpclient/response.go) / [`NewResponse`](../../httpclient/response.go)
    - [`StreamResponse`](../../httpclient/stream_response.go) / [`NewStreamResponse`](../../httpclient/stream_response.go)

Every limit and timeout is a [`bound`](BOUND.md) field: left out it takes its default, `bound.Of` sets a value, and a value below the field's minimum, or unlimited on a field that does not admit it, is refused by `NewHttpClient` or by the request, naming the field.

## Usage

The example below builds one client for the service, holds it, and closes it when the application is done with the service — the client owns an idle connection pool, so building one per call leaks parked connections and building it inside the called function was exactly that.

```go
package main

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/httpclient"
)

type HealthResponse struct {
    Status string `json:"status"`
}

func newApiClient() (*httpclient.HttpClient, error) {
    return httpclient.NewHttpClient(
        httpclient.HttpClientConfig{
            BaseUrl: "https://api.example.com",
            Timeout: bound.Of(5 * time.Second),
            Headers: map[string]string{
                "accept": "application/json",
            },
        },
    )
}

func callHealthEndpoint(client *httpclient.HttpClient) (string, error) {
    response, requestErr := client.Get(
        "/health",
        httpclient.RequestOptions{},
    )
    if nil != requestErr {
        return "", requestErr
    }

    var payload HealthResponse
    decodeErr := response.Json(&payload)
    if nil != decodeErr {
        return "", decodeErr
    }

    return payload.Status, nil
}

func main() {
    client, clientErr := newApiClient()
    if nil != clientErr {
        panic(clientErr)
    }
    defer client.Close()

    _, _ = callHealthEndpoint(client)
}
```

## Footguns & caveats

- `Response.Json` unmarshals the response body as-is; it does not validate content-type headers.
- **The target is resolved against the base url by RFC 3986, and a base url with a path wants its trailing slash.** An absolute-path target (`/users`) replaces the base path entirely, a relative one (`users`) merges over the last segment of the base path, and an empty target names the base resource itself, with its trailing slash. Because the merge cuts the last segment of a base spelled without a trailing slash, `NewHttpClient` refuses such a base; a base with an empty path (`https://host`) is legal.
- **A based client refuses a target whose RESOLVED url leaves the base origin** — the configured headers and authorization would otherwise travel to a host the target string chose. The judgment on the resolved url covers the network-path form (`//other.example/x`) and any spelling of the scheme. A caller that talks to more than one origin builds a client without a base url; a relative target on such a client is refused by name, because the request it would build can name no host at all.
- `NewHttpClient` and every request copy the maps they are given; a write into a map after the call is not observed. **Header maps are canonicalized at every door**: `NewHttpClient` refuses a configuration whose `Headers` carry two spellings that collapse onto one header, and a request whose `RequestOptions.Headers` do fails before anything is dialled; `SetHeader` stores the canonical spelling so a rotation overwrites the entry it means to.
- **A query option replaces the url's pair of the same name and leaves every other pair as written.** `RequestOptions.Query` is appended to the url's own query, a pair the url already names under an option key replaced; every other pair, one net/url would refuse included, reaches the server byte for byte.
- **A secret belongs in a header, not in the url — but an error will not spill it.** Every url this package puts into a message or an error context is sanitized: the userinfo and the query values are replaced, while the scheme, host, path and parameter names survive for diagnosis. A url net/url refuses keeps only its scheme when an "@" sits past the first "/", "?" or "#" of its authority, since a password holding one of them leaves no trustworthy end to the userinfo, and the parse error it is reported with has every quoted span redacted. A url that parses with a userinfo, or with a host and port, and an "@" in its path, in a query name or in its fragment keeps only its scheme for the same reason, since a password holding "/", "?" or "#" is read past its authority; an "@" in a query value is the value's own and is redacted with it. The `*url.Error` net/http answers a failed exchange with stays in the error chain — `errors.As(err, &urlErr)` is the form retry and breaker code is written
  in, and `net.Error`, `errors.Is(err, context.DeadlineExceeded)` and the refused-connection cause all read through it — carrying the sanitized url in place of the one it quoted. On a cross-origin redirect the client also strips `Referer`, which net/http would otherwise populate with the full previous url.
- **A `TransportConfig` field left out takes its default; no set value borrows a meaning from net/http.** The defaults, minimums and unlimited:

    | Field | Default | Minimum | Unlimited |
    | --- | --- | --- | --- |
    | `DialTimeout` | 10s | 1ms | refused |
    | `KeepAlive` | 30s | 1s | refused; `KeepAliveDisabled` turns the probes off |
    | `MaxIdleConns` | 100 | 1 | admitted |
    | `MaxIdleConnsPerHost` | follows `MaxIdleConns` set to a value, else 100 | 1 | admitted |
    | `IdleConnTimeout` | 90s | 1s | admitted |
    | `TlsHandshakeTimeout` | 10s | 1ms | refused |
    | `ExpectContinueTimeout` | 1s | 0, which sends the body at once | refused |
    | `ResponseHeaderTimeout` | 15s | 1ms | refused |

    Every timeout but `IdleConnTimeout` refuses unlimited: the dial, handshake, response-header and 100-continue waits each guard against a peer that never answers, and `KeepAlive` has `KeepAliveDisabled`. `KeepAlive` set beside `KeepAliveDisabled` is refused.
- `NewDefaultHttpClient` is `NewHttpClient(HttpClientConfig{})`: no base url, a 30s timeout, no header, the default transport and redirects followed up to net/http's cap of ten requests. The base url and the timeout are fixed at construction; a caller that needs another builds another client.
- **`HttpClientConfig.Timeout` bounds one whole exchange**: 30s by default, at least 1ms, and unlimited admitted for a client that must never cut an exchange. A request's `RequestOptions.Timeout` replaces it for that request, unlimited included. `MaxRedirects` bounds the requests an exchange makes while it follows redirects, the first included, ten by default as net/http counts its own cap, and is refused beside `RedirectsDisabled`.
- **Hold the client, close it when done.** Every client builds its own transport with an idle connection pool — a hundred connections per host, kept for ninety seconds — and dropping the last reference releases none of them. `Close` releases the idle pool; it does not abort requests in flight, and the client stays usable afterwards.
- **A `StreamResponse` belongs to the caller, on every path.** `RequestStream` hands back a body that is still on the wire, and by default the streaming client carries no whole-request deadline, so a stream that is not closed pins its connection and its descriptor for the life of the process. The default is what is dropped, not the option: a `RequestOptions.Timeout` set to a value is honoured on this path too and bounds the whole exchange, body read included, which is exactly what a long-lived stream usually must not have; left out or unlimited, the stream has no whole-stream deadline, and the context `RequestStream` takes bounds it from outside. Close it even when the status is one you do not like and you never read the body.
- **`Body()` after `Close()` reads as a failure, not as nil.** A watchdog goroutine closing an indefinite stream is the ordinary shape, so the accessor never hands back a nil reader; the first read reports that the stream is closed, and the reader's own `Close` succeeds.
- **The response body cap bounds the stream too, the inherited default included.** `Request` caps the body it reads whole into memory, at ten mebibytes by default, and `RequestStream` enforces the same cap on the body as it is read: a stream that legitimately carries more names its own cap through `RequestOptions.MaxResponseBodyBytes`. A cap below one byte, and unlimited, are refused before anything is dialled, on both paths.
- **A request names one credential**: `BearerToken` and `BasicAuth` set together are refused, because the two cannot share one `Authorization` header. A basic credential travels whenever it was asked for, empty halves included — `&httpclient.BasicAuth{Password: key}` is the ordinary shape of an api key spelled as a password, and `&httpclient.BasicAuth{}` sends `Basic` with two empty halves because that is what was asked for. **The typed credential is applied last**: it wins over an `Authorization` header written through the client's configuration or `RequestOptions.Headers`, the credential being the caller's statement about the request's identity.
- **A request carries one body.** `Body` (a string or a `[]byte`, sent as it is) and `Json` (marshalled, sent as `application/json`) set together are refused, and so is either beside the body `Post`, `Put` and `Patch` take positionally, which they send as json.

## Userland API

### Contracts (`httpclient/contract`)

- [`type Client`](../../httpclient/contract/http_client.go)
- [`type RequestOptions`](../../httpclient/contract/request_option.go) — `Headers`, `Query`, `Body`, `Json`, `Timeout bound.Duration`, `BearerToken`, `BasicAuth`, `MaxResponseBodyBytes bound.Count`
- [`type BasicAuth`](../../httpclient/contract/request_option.go) — `Username`, `Password`
- [`type Response`](../../httpclient/contract/response.go)
- [`type StreamResponse`](../../httpclient/contract/stream_response.go)

### Implementations (`httpclient`)

- [`type HttpClient`](../../httpclient/http_client.go)
    - [`NewDefaultHttpClient()`](../../httpclient/http_client.go)
    - [`NewHttpClient(HttpClientConfig) (*HttpClient, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Get(urlString string, options RequestOptions) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Post(urlString string, body any, options RequestOptions) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Put(urlString string, body any, options RequestOptions) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Patch(urlString string, body any, options RequestOptions) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Delete(urlString string, options RequestOptions) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Request(method string, urlString string, options RequestOptions) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).RequestStream(contextInstance context.Context, method string, urlString string, options RequestOptions) (httpclientcontract.StreamResponse, error)`](../../httpclient/http_client.go) — cancelling the context ends the request and the body read
    - [`(*HttpClient).SetHeader(key string, value string)`](../../httpclient/http_client.go) — stores the canonical spelling
    - [`(*HttpClient).Close() error`](../../httpclient/http_client.go) — releases the idle connections the client's transport holds
- [`type HttpClientConfig`](../../httpclient/http_client_config.go) — `BaseUrl`, `Timeout bound.Duration`, `Headers`, `Transport TransportConfig`, `RedirectsDisabled`, `MaxRedirects bound.Count`; `RedirectsDisabled` hands a redirect back as the response it is, status and `Location` included, instead of following it, where a `POST` answered 301, 302 or 303 is otherwise re-sent by `net/http` as a `GET` without its body
- [`type TransportConfig`](../../httpclient/transport_config.go) — `bound` fields and `KeepAliveDisabled`
- [`type RequestOptions`](../../httpclient/request_option.go) and [`type BasicAuth`](../../httpclient/request_option.go) — the contract's structs
- Responses:
    - [`type Response`](../../httpclient/response.go)
        - [`NewResponse(...)`](../../httpclient/response.go)
        - `StatusCode`, `Status`, `Headers`, `Body`, `Request`, `Json`, `String`, `IsSuccess`, `IsClientError`, `IsServerError`
    - [`type StreamResponse`](../../httpclient/stream_response.go)
        - [`NewStreamResponse(...)`](../../httpclient/stream_response.go)
