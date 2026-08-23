package httpclient

import (
    "net/textproto"
    "time"

    httpclientcontract "github.com/precision-soft/melody/v3/httpclient/contract"
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

/* Headers hands out a copy: the live map invited writes that bypass the canonicalization SetHeader exists to enforce, and a non-canonical spelling planted through the getter next to the canonical one made the request-time winner a map-iteration choice — in what is often a credential header, the exact nondeterminism the setters refuse. The setters remain the one door that writes. */
func (instance *RequestOptions) Headers() map[string]string {
    return copyStringMap(instance.headers)
}

/* Query hands out a copy under the same single-door rule as Headers. */
func (instance *RequestOptions) Query() map[string]string {
    return copyStringMap(instance.query)
}

func copyStringMap(values map[string]string) map[string]string {
    if nil == values {
        return nil
    }

    copied := make(map[string]string, len(values))
    for key, value := range values {
        copied[key] = value
    }

    return copied
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

/* SetHeader stores the key canonicalized, so two spellings of one header land on one entry deterministically — the last sequential write wins — instead of surviving as two map entries whose request-time winner map iteration chose. */
func (instance *RequestOptions) SetHeader(key string, value string) {
    instance.headers[textproto.CanonicalMIMEHeaderKey(key)] = value
}

/* SetHeaders refuses a map carrying two spellings that collapse onto one header, the way the client config constructor does: inside one map there is no sequential order to make the survivor deterministic. */
func (instance *RequestOptions) SetHeaders(headers map[string]string) {
    for key, value := range canonicalHeaderMap(headers) {
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
