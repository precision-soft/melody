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

```go
package main

import (
	"time"

	"github.com/precision-soft/melody/v3/httpclient"
)

type HealthResponse struct {
	Status string `json:"status"`
}

func callHealthEndpoint() (string, error) {
	client := httpclient.NewHttpClient(
		httpclient.NewHttpClientConfig(
			"https://api.example.com",
			5*time.Second,
			map[string]string{
				"accept": "application/json",
			},
		),
	)

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
```

## Footguns & caveats

- `Response.Json` unmarshals the response body as-is; it does not validate content-type headers.
- `NewHttpClientConfig` copies headers defensively; modifications to the input map after construction are not observed.
- `NewDefaultHttpClient` uses an empty base URL and a default timeout. Set a base URL via `HttpClientConfig` or `SetBaseUrl`.
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
    - [`(*HttpClient).SetBaseUrl(baseUrl string)`](../../httpclient/http_client.go)
    - [`(*HttpClient).SetHeader(key string, value string)`](../../httpclient/http_client.go)
    - [`(*HttpClient).SetTimeout(timeout time.Duration)`](../../httpclient/http_client.go)
- [`type HttpClientConfig`](../../httpclient/http_client_config.go)
    - [`NewHttpClientConfig(baseUrl string, timeout time.Duration, headers map[string]string) *HttpClientConfig`](../../httpclient/http_client_config.go)
    - [`WithTransport(transport *TransportConfig) *HttpClientConfig`](../../httpclient/http_client_config.go) — the door a transport comes through
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
