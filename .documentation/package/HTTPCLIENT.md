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
package example

import (
	"time"

	"github.com/precision-soft/melody/httpclient"
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
    - `Get`, `Post`, `Put`, `Patch`, `Delete`, `Request`, `RequestStream`
    - `SetBaseUrl`, `SetHeader`, `SetTimeout`
- [`type HttpClientConfig`](../../httpclient/http_client_config.go)
    - [`NewHttpClientConfig(baseUrl string, timeout time.Duration, headers map[string]string) *HttpClientConfig`](../../httpclient/http_client_config.go)
- Request options:
    - [`NewRequestOptions()`](../../httpclient/request_option.go)
    - `WithHeader`, `WithHeaders`, `WithQuery`, `WithQueryParams`, `WithBody`, `WithJson`, `WithTimeout`, `WithBearerToken`, `WithBasicAuth`, `WithMaxResponseBodyBytes`
- Responses:
    - [`type Response`](../../httpclient/response.go)
        - [`NewResponse(...)`](../../httpclient/response.go)
        - `StatusCode`, `Status`, `Headers`, `Body`, `Request`, `Json`, `String`, `IsSuccess`, `IsClientError`, `IsServerError`
    - [`type StreamResponse`](../../httpclient/stream_response.go)
        - [`NewStreamResponse(...)`](../../httpclient/stream_response.go)
