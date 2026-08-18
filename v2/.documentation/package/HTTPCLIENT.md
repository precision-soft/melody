# HTTPCLIENT

The [`httpclient`](../../httpclient) package provides a small outbound HTTP client abstraction with a typed request options builder and typed response helpers.

## Scope

This package is intended for simple outbound HTTP calls from userland and framework modules. It wraps Go’s `net/http` client with:

- a reusable base URL + default headers configuration,
- composable request options (headers, query params, body, JSON),
- response helpers for decoding JSON and inspecting status classes,
- optional streaming responses.

## Subpackages

- [`httpclient/contract`](../../httpclient/contract)  
  Public contracts for the client, request options, and response types.

## Responsibilities

- Client construction and configuration:
    - [`HttpClient`](../../httpclient/http_client.go)
    - [`NewDefaultHttpClient`](../../httpclient/http_client.go)
    - [`NewHttpClient`](../../httpclient/http_client.go)
    - [`HttpClientConfig`](../../httpclient/http_client_config.go)
    - [`NewHttpClientConfig`](../../httpclient/http_client_config.go)
- Request option builders:
    - [`RequestOptions`](../../httpclient/request_option.go)
    - [`NewRequestOptions`](../../httpclient/request_option.go)
    - `WithHeader`, `WithQuery`, `WithJson`, `WithTimeout`, … in [`request_option.go`](../../httpclient/request_option.go)
- Response types and helpers:
    - [`Response`](../../httpclient/response.go) / [`NewResponse`](../../httpclient/response.go)
    - [`StreamResponse`](../../httpclient/stream_response.go) / [`NewStreamResponse`](../../httpclient/stream_response.go)
- Authorization option helpers:
    - [`AuthorizationOptions`](../../httpclient/authorization_options.go)
    - [`BasicAuthorizationOptions`](../../httpclient/authorization_options.go)

## Usage

The example below performs a GET request and decodes a JSON response.

A client owns a connection pool, so it is built once and held for as long as the application calls the service it points at — never per call.

```go
package main

import (
	"time"

	"github.com/precision-soft/melody/v2/httpclient"
)

type HealthResponse struct {
	Status string `json:"status"`
}

var healthClient = httpclient.NewHttpClient(
	httpclient.NewHttpClientConfig(
		"https://api.example.com",
		5*time.Second,
		map[string]string{
			"accept": "application/json",
		},
	),
)

func callHealthEndpoint() (string, error) {
	response, requestErr := healthClient.Get(
		"/health",
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
```

## Footguns & caveats

- **A client is held, not built per call, and closed when it is done.** Every `NewHttpClient` builds its own transport with an idle pool — a hundred connections per host, kept for ninety seconds — and dropping the last reference to the client releases none of it: each parked connection has a read loop of its own keeping the transport alive. `Close()` releases the idle connections; it does not abort requests in flight, and the client keeps working afterwards by dialling again.
- **A `StreamResponse` belongs to the caller, on every path.** `RequestStream` hands back a body that is still on the wire, and by default the streaming client carries no whole-request deadline, so a stream that is not closed pins its connection and its descriptor for the life of the process. The default is what is dropped, not the option: a **positive** `WithTimeout` is honoured on this path too and bounds the whole exchange, body read included, which is exactly what a long-lived stream usually must not have. A non-positive one reads as unset. Close it even when the status is one you do not like and you never read the body. `RequestStreamWithContext` is the variant that can end a stream a server never ends.
- **`Body()` after `Close()` reads as a failure, not as nil.** A watchdog goroutine closing an indefinite stream is the ordinary shape, so the accessor never hands back a nil reader; the first read reports that the stream is closed, and the reader's own `Close` succeeds.
- **The base url is a prefix, and it binds the client to one origin.** `https://host/v1` plus `/users` names `https://host/v1/users` — deliberately unlike RFC 3986 reference resolution, which Symfony and Guzzle implement and where an absolute path would replace `/v1` entirely. An empty target names the base resource itself. An absolute url that leaves the base origin is refused rather than sent, because the headers and the authorization configured on the client would otherwise travel to a host the url string chose — the leak the redirect policy stops one hop later. A client that talks to several origins is built without a base url.
- **`WithMaxResponseBodyBytes` on a stream applies only when you name it.** `Request` caps the body it reads whole into memory, at ten mebibytes by default. On `RequestStream` the default does not apply, because a long-lived feed is exactly what it would cut; a cap you set explicitly is enforced there too, and the read fails once it is passed.
- **A secret belongs in a header, not in the url — but an error will not spill it.** Every url this package puts into a message or an error context is sanitized: the userinfo and the query values are replaced, while the scheme, host, path and parameter names survive for diagnosis. On a cross-origin redirect the client also strips `Referer`, which net/http would otherwise populate with the full previous url.
- **`TransportConfig` reads every non-positive value as "not set".** The meanings net/http gives to zero — an unbounded idle pool, no idle or response-header deadline — and the meaning `net.Dialer` gives to a negative keep-alive cannot be reached through this type; a deployment that needs one asks for a duration large enough to be the same thing in practice.
- **A `[]byte` body is copied when the request is built**, because net/http writes the body on its own goroutine and `Do` returns as soon as the response headers arrive; a caller reusing a pooled buffer right after the call would otherwise race the transport.
- **A bearer token wins over a basic credential** when both are set, because the two cannot share one `Authorization` header. A basic credential travels whenever it was asked for, empty halves included — `WithBasicAuth("", key)` is the ordinary shape of an api key spelled as a password.
- `Response.Json` unmarshals the response body as-is; it does not validate content-type headers.
- `NewHttpClientConfig` copies headers defensively; modifications to the input map after construction are not observed.
- `NewDefaultHttpClient` uses an empty base URL and a default timeout. Set a base URL via `HttpClientConfig` or `SetBaseUrl`.

## Userland API

### Contracts (`httpclient/contract`)

- [`type Client`](../../httpclient/contract/http_client.go)
- [`type RequestOption`](../../httpclient/contract/request_option.go)
- [`type RequestOptions`](../../httpclient/contract/request_option.go)
- [`type AuthorizationOptions`](../../httpclient/contract/request_option.go)
- [`type BasicAuthorizationOptions`](../../httpclient/contract/request_option.go)
- [`type Response`](../../httpclient/contract/response.go)
- [`type StreamResponse`](../../httpclient/contract/stream_response.go)

### Implementations (`httpclient`)

- [`type HttpClient`](../../httpclient/http_client.go)
    - [`NewDefaultHttpClient()`](../../httpclient/http_client.go)
    - [`NewHttpClient(*HttpClientConfig)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Get(urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Post(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Put(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Patch(urlString string, body any, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Delete(urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Request(method string, urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.Response, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).RequestStream(method string, urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.StreamResponse, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).RequestStreamWithContext(contextInstance context.Context, method string, urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.StreamResponse, error)`](../../httpclient/http_client.go)
    - [`(*HttpClient).SetBaseUrl(baseUrl string)`](../../httpclient/http_client.go)
    - [`(*HttpClient).SetHeader(key string, value string)`](../../httpclient/http_client.go) — stores under the canonical spelling, the one the constructor stores under, so rotating a credential overwrites the entry it means to instead of leaving two that collapse onto one header
    - [`(*HttpClient).SetTimeout(timeout time.Duration)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Close() error`](../../httpclient/http_client.go)
- [`type HttpClientConfig`](../../httpclient/http_client_config.go)
    - [`NewHttpClientConfig(baseUrl string, timeout time.Duration, headers map[string]string) *HttpClientConfig`](../../httpclient/http_client_config.go)
    - [`WithTransport(transport *TransportConfig) *HttpClientConfig`](../../httpclient/http_client_config.go) — the door a transport comes through; the footgun below is about how its fields are read
- [`type TransportConfig`](../../httpclient/transport_config.go)
    - [`DefaultTransportConfig() *TransportConfig`](../../httpclient/transport_config.go)
- Request options:
    - [`NewRequestOptions()`](../../httpclient/request_option.go)
    - `WithHeader`, `WithHeaders`, `WithQuery`, `WithQueryParams`, `WithBody`, `WithJson`, `WithTimeout`, `WithBearerToken`, `WithBasicAuth`, `WithMaxResponseBodyBytes`
- Responses:
    - [`type Response`](../../httpclient/response.go)
        - [`NewResponse(...)`](../../httpclient/response.go)
        - `StatusCode`, `Status`, `Headers`, `Body`, `Request`, `Json`, `String`, `IsSuccess`, `IsClientError`, `IsServerError`
    - [`type StreamResponse`](../../httpclient/stream_response.go)
        - [`NewStreamResponse(...)`](../../httpclient/stream_response.go)
        - `StatusCode`, `Headers`, `Body`, `Close` — the four it carries, against the ten its sibling above has: the body is still on the wire, so the caller closes it
