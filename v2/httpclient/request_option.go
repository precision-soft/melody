package httpclient

import (
	"time"

	httpclientcontract "github.com/precision-soft/melody/v2/httpclient/contract"
)

type RequestOptions struct {
	headers              map[string]string
	query                map[string]string
	body                 any
	contentType          string
	timeout              time.Duration
	authorization        httpclientcontract.AuthorizationOptions
	maxResponseBodyBytes int
}

func NewRequestOptions() *RequestOptions {
	return &RequestOptions{
		headers:              make(map[string]string),
		query:                make(map[string]string),
		authorization:        NewAuthorizationOptions(),
		maxResponseBodyBytes: 10 * 1024 * 1024,
	}
}

func (instance *RequestOptions) Headers() map[string]string {
	return instance.headers
}

func (instance *RequestOptions) Query() map[string]string {
	return instance.query
}

func (instance *RequestOptions) Body() any {
	return instance.body
}

func (instance *RequestOptions) ContentType() string {
	return instance.contentType
}

func (instance *RequestOptions) Timeout() time.Duration {
	return instance.timeout
}

func (instance *RequestOptions) Authorization() httpclientcontract.AuthorizationOptions {
	return instance.authorization
}

func (instance *RequestOptions) MaxResponseBodyBytes() int {
	return instance.maxResponseBodyBytes
}

func (instance *RequestOptions) SetMaxResponseBodyBytes(maxResponseBodyBytes int) {
	instance.maxResponseBodyBytes = maxResponseBodyBytes
}

func (instance *RequestOptions) SetHeader(key string, value string) {
	instance.headers[key] = value
}

func (instance *RequestOptions) SetHeaders(headers map[string]string) {
	for key, value := range headers {
		instance.headers[key] = value
	}
}

func (instance *RequestOptions) SetQuery(key string, value string) {
	instance.query[key] = value
}

func (instance *RequestOptions) SetQueryParams(parameters map[string]string) {
	for key, value := range parameters {
		instance.query[key] = value
	}
}

func (instance *RequestOptions) SetBody(body any) {
	instance.body = body
}

func (instance *RequestOptions) SetJson(data any) {
	instance.body = data
	instance.contentType = "application/json"
}

func (instance *RequestOptions) SetTimeout(timeout time.Duration) {
	instance.timeout = timeout
}

func (instance *RequestOptions) SetBearerToken(token string) {
	instance.authorization.SetBearer(token)
}

func (instance *RequestOptions) SetBasicAuth(username string, password string) {
	instance.authorization.SetBasic(
		&BasicAuthorizationOptions{
			username: username,
			password: password,
		},
	)
}

var _ httpclientcontract.RequestOptions = (*RequestOptions)(nil)

func WithHeader(key string, value string) httpclientcontract.RequestOption {
	return func(instance httpclientcontract.RequestOptions) {
		instance.SetHeader(key, value)
	}
}

func WithHeaders(headers map[string]string) httpclientcontract.RequestOption {
	return func(instance httpclientcontract.RequestOptions) {
		instance.SetHeaders(headers)
	}
}

func WithQuery(key string, value string) httpclientcontract.RequestOption {
	return func(instance httpclientcontract.RequestOptions) {
		instance.SetQuery(key, value)
	}
}

func WithQueryParams(parameters map[string]string) httpclientcontract.RequestOption {
	return func(instance httpclientcontract.RequestOptions) {
		instance.SetQueryParams(parameters)
	}
}

func WithBody(body any) httpclientcontract.RequestOption {
	return func(instance httpclientcontract.RequestOptions) {
		instance.SetBody(body)
	}
}

func WithJson(data any) httpclientcontract.RequestOption {
	return func(instance httpclientcontract.RequestOptions) {
		instance.SetJson(data)
	}
}

func WithTimeout(timeout time.Duration) httpclientcontract.RequestOption {
	return func(instance httpclientcontract.RequestOptions) {
		instance.SetTimeout(timeout)
	}
}

func WithBearerToken(token string) httpclientcontract.RequestOption {
	return func(instance httpclientcontract.RequestOptions) {
		instance.SetBearerToken(token)
	}
}

func WithBasicAuth(username string, password string) httpclientcontract.RequestOption {
	return func(instance httpclientcontract.RequestOptions) {
		instance.SetBasicAuth(username, password)
	}
}

func WithMaxResponseBodyBytes(maxResponseBodyBytes int) httpclientcontract.RequestOption {
	return func(instance httpclientcontract.RequestOptions) {
		instance.SetMaxResponseBodyBytes(maxResponseBodyBytes)
	}
}
