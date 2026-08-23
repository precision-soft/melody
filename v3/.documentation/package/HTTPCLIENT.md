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

The example below builds one client for the service, holds it, and closes it when the application is done with the service — the client owns an idle connection pool, so building one per call leaks parked connections and building it inside the called function was exactly that.

```go
package main

import (
	"time"

	"github.com/precision-soft/melody/v3/httpclient"
)

type HealthResponse struct {
	Status string `json:"status"`
}

func newApiClient() *httpclient.HttpClient {
	return httpclient.NewHttpClient(
		httpclient.NewHttpClientConfig(
			"https://api.example.com",
			5*time.Second,
			map[string]string{
				"accept": "application/json",
			},
		),
	)
}

func callHealthEndpoint(client *httpclient.HttpClient) (string, error) {
	response, requestErr := client.Get(
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

func main() {
	client := newApiClient()
	defer client.Close()

	_, _ = callHealthEndpoint(client)
}
```

## Footguns & caveats

- `Response.Json` unmarshals the response body as-is; it does not validate content-type headers.
- **The target is resolved against the base url by RFC 3986, and a base url with a path wants its trailing slash.** An absolute-path target (`/users`) replaces the base path entirely, a relative one (`users`) merges over the last segment of the base path, and an empty target names the base resource itself, with its trailing slash. Because the merge cuts the last segment of a base spelled without a trailing slash, `NewHttpClientConfig` and `SetBaseUrl` refuse such a base by panic; a base with an empty path (`https://host`) is legal.
- **A based client refuses a target whose RESOLVED url leaves the base origin** — the configured headers and authorization would otherwise travel to a host the target string chose. The judgment on the resolved url covers the network-path form (`//other.example/x`) and any spelling of the scheme. A caller that talks to more than one origin builds a client without a base url; a relative target on such a client is refused by name, because the request it would build can name no host at all.
- `NewHttpClientConfig` copies headers defensively; modifications to the input map after construction are not observed. **Header maps are canonicalized at every door**: the constructor and `SetHeaders` refuse a map whose two spellings collapse onto one header, `SetHeader` stores the canonical spelling so a rotation overwrites the entry it means to, and `RequestOptions.Headers()`/`Query()` hand out copies — the setters are the one door that writes.
- **A `TransportConfig` field left nil inherits the default beside it; a SET field reaches `net/http` verbatim**, zero and negative included, with the meaning `net/http` and `net.Dialer` give it — `MaxIdleConns: 0` is an unbounded pool, `IdleConnTimeout: 0` waits forever, a negative `KeepAlive` disables the probes. `TransportDuration` and `TransportCount` build the pointers in place; `DefaultTransportConfig()` is the fully populated statement of the defaults.
- `NewDefaultHttpClient` uses an empty base URL and a default timeout. Set a base URL via `HttpClientConfig` or `SetBaseUrl`.
- **Hold the client, close it when done.** Every client builds its own transport with an idle connection pool — a hundred connections per host, kept for ninety seconds — and dropping the last reference releases none of them. `Close` releases the idle pool; it does not abort requests in flight, and the client stays usable afterwards.
- **A `StreamResponse` belongs to the caller, on every path.** `RequestStream` hands back a body that is still on the wire, and by default the streaming client carries no whole-request deadline, so a stream that is not closed pins its connection and its descriptor for the life of the process. The default is what is dropped, not the option: a **positive** `WithTimeout` is honoured on this path too and bounds the whole exchange, body read included, which is exactly what a long-lived stream usually must not have. A negative one folds into the configured timeout rather than unbounding the stream. Close it even when the status is one you do not like and you never read the body.
- **`Body()` after `Close()` reads as a failure, not as nil.** A watchdog goroutine closing an indefinite stream is the ordinary shape, so the accessor never hands back a nil reader; the first read reports that the stream is closed, and the reader's own `Close` succeeds.
- **The response body cap bounds the stream too, the inherited default included.** `Request` caps the body it reads whole into memory, at ten mebibytes by default, and `RequestStream` enforces the same cap on the body as it is read: a stream that legitimately carries more names its own cap through `WithMaxResponseBodyBytes`. A non-positive cap is refused before anything is dialled, on both paths.
- **A bearer token wins over a basic credential** when both are set, because the two cannot share one `Authorization` header. A basic credential travels whenever it was asked for, empty halves included — `WithBasicAuth("", key)` is the ordinary shape of an api key spelled as a password.

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
    - [`(*HttpClient).RequestStreamWithContext(contextInstance context.Context, method string, urlString string, options ...httpclientcontract.RequestOption) (httpclientcontract.StreamResponse, error)`](../../httpclient/http_client.go) — `RequestStream` bounded from the outside; cancelling the context ends the request and the body read
    - [`(*HttpClient).SetBaseUrl(baseUrl string)`](../../httpclient/http_client.go) — refuses a base whose path lacks its trailing slash, the constructor's rule
    - [`(*HttpClient).SetHeader(key string, value string)`](../../httpclient/http_client.go) — stores the canonical spelling
    - [`(*HttpClient).SetTimeout(timeout time.Duration)`](../../httpclient/http_client.go)
    - [`(*HttpClient).Close() error`](../../httpclient/http_client.go) — releases the idle connections the client's transport holds
- [`type HttpClientConfig`](../../httpclient/http_client_config.go)
    - [`NewHttpClientConfig(baseUrl string, timeout time.Duration, headers map[string]string) *HttpClientConfig`](../../httpclient/http_client_config.go)
    - [`WithTransport(transport *TransportConfig) *HttpClientConfig`](../../httpclient/http_client_config.go) — the door a transport comes through
- [`type TransportConfig`](../../httpclient/transport_config.go) — pointer fields: nil inherits the default, a set value reaches `net/http` verbatim
    - [`DefaultTransportConfig() *TransportConfig`](../../httpclient/transport_config.go)
    - [`TransportDuration(value time.Duration) *time.Duration`](../../httpclient/transport_config.go) — keeps a configuration literal a literal
    - [`TransportCount(value int) *int`](../../httpclient/transport_config.go) — `TransportDuration` for the connection-count fields
- Request options:
    - [`NewRequestOptions()`](../../httpclient/request_option.go)
    - `WithHeader`, `WithHeaders`, `WithQuery`, `WithQueryParams`, `WithBody`, `WithJson`, `WithTimeout`, `WithBearerToken`, `WithBasicAuth`, `WithMaxResponseBodyBytes`
- Responses:
    - [`type Response`](../../httpclient/response.go)
        - [`NewResponse(...)`](../../httpclient/response.go)
        - `StatusCode`, `Status`, `Headers`, `Body`, `Request`, `Json`, `String`, `IsSuccess`, `IsClientError`, `IsServerError`
    - [`type StreamResponse`](../../httpclient/stream_response.go)
        - [`NewStreamResponse(...)`](../../httpclient/stream_response.go)
