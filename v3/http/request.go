package http

import (
    "bytes"
    "io"
    "mime"
    nethttp "net/http"
    "net/url"

    "github.com/precision-soft/melody/v3/bag"
    bagcontract "github.com/precision-soft/melody/v3/bag/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    RequestAttributeSession = "_session"
    RequestAttributeScheme  = "_scheme"
)

func ErrorUnsupportedContentType() error {
    return exception.NewError("unsupported content type", map[string]any{}, nil)
}

func ErrorJsonBodyHasExtraData() error {
    return exception.NewError("json body has extra data", map[string]any{}, nil)
}

func NewRequest(
    httpRequest *nethttp.Request,
    routeParams map[string]string,
    runtimeInstance runtimecontract.Runtime,
    requestContext httpcontract.RequestContext,
) *Request {
    if nil == httpRequest {
        exception.Panic(
            exception.NewError("the http request is nil", nil, nil),
        )
    }

    if nil == routeParams {
        routeParams = map[string]string{}
    }

    queryBag := bag.NewParameterBagFromValues(httpRequest.URL.Query())
    postBag := bag.NewParameterBag()
    var bodyReadErr error

    if true == shouldAutoParseForm(httpRequest) {

        var rawBody []byte
        bufferedBody := false
        if true == isUrlEncodedForm(httpRequest) {
            rawBody, bufferedBody, bodyReadErr = readRequestBodyBytes(httpRequest)
            if true == bufferedBody {
                restoreRequestBody(httpRequest, rawBody)
            }
        }

        if nil == bodyReadErr {
            parseFormErr := httpRequest.ParseForm()

            if true == bufferedBody {
                restoreRequestBody(httpRequest, rawBody)
            }

            if nil == parseFormErr {
                postBag = bag.NewParameterBagFromValues(httpRequest.PostForm)
            } else if bodyParseErr := urlEncodedBodyParseError(bufferedBody, rawBody); nil != bodyParseErr {

                bodyReadErr = exception.NewError(
                    "failed to parse form data",
                    map[string]any{
                        "method": httpRequest.Method,
                        "path":   httpRequest.URL.Path,
                    },
                    bodyParseErr,
                )
            } else {

                postBag = bag.NewParameterBagFromValues(httpRequest.PostForm)
            }
        }
    }

    attributesBag := bag.NewParameterBag()

    return &Request{
        httpRequest:     httpRequest,
        params:          routeParams,
        query:           queryBag,
        post:            postBag,
        attributes:      attributesBag,
        runtimeInstance: runtimeInstance,
        requestContext:  requestContext,
        bodyReadErr:     bodyReadErr,
    }
}

type Request struct {
    httpRequest     *nethttp.Request
    params          map[string]string
    query           bagcontract.ParameterBag
    post            bagcontract.ParameterBag
    attributes      bagcontract.ParameterBag
    runtimeInstance runtimecontract.Runtime
    requestContext  httpcontract.RequestContext

    bodyReadErr error
}

func (instance *Request) HttpRequest() *nethttp.Request {
    return instance.httpRequest
}

func (instance *Request) Param(name string) (string, bool) {
    value, exists := instance.params[name]

    return value, exists
}

func (instance *Request) Params() map[string]string {
    copied := make(map[string]string, len(instance.params))

    for key, value := range instance.params {
        copied[key] = value
    }

    return copied
}

func (instance *Request) Query() bagcontract.ParameterBag {
    return instance.query
}

func (instance *Request) Post() bagcontract.ParameterBag {
    return instance.post
}

func (instance *Request) Attributes() bagcontract.ParameterBag {
    return instance.attributes
}

func (instance *Request) Header(name string) string {
    return instance.httpRequest.Header.Get(name)
}

func urlEncodedBodyParseError(bufferedBody bool, rawBody []byte) error {
    if false == bufferedBody {
        return nil
    }

    _, parseErr := url.ParseQuery(string(rawBody))

    return parseErr
}

func shouldAutoParseForm(httpRequest *nethttp.Request) bool {
    if nethttp.MethodPost != httpRequest.Method &&
        nethttp.MethodPut != httpRequest.Method &&
        nethttp.MethodPatch != httpRequest.Method {
        return false
    }

    contentType := httpRequest.Header.Get("Content-Type")
    if "" == contentType {
        return false
    }

    mediaType, _, parseErr := mime.ParseMediaType(contentType)
    if nil != parseErr {
        return false
    }

    return "application/x-www-form-urlencoded" == mediaType || "multipart/form-data" == mediaType
}

func isUrlEncodedForm(httpRequest *nethttp.Request) bool {
    mediaType, _, parseErr := mime.ParseMediaType(httpRequest.Header.Get("Content-Type"))
    if nil != parseErr {
        return false
    }

    return "application/x-www-form-urlencoded" == mediaType
}

func readRequestBodyBytes(httpRequest *nethttp.Request) ([]byte, bool, error) {
    if nil == httpRequest.Body {
        return nil, false, nil
    }

    bodyBytes, readErr := io.ReadAll(httpRequest.Body)
    if nil != readErr {
        return nil, false, readErr
    }

    _ = httpRequest.Body.Close()

    return bodyBytes, true, nil
}

func restoreRequestBody(httpRequest *nethttp.Request, bodyBytes []byte) {
    httpRequest.Body = io.NopCloser(bytes.NewReader(bodyBytes))
    httpRequest.GetBody = func() (io.ReadCloser, error) {
        return io.NopCloser(bytes.NewReader(bodyBytes)), nil
    }
}

func (instance *Request) ContentType() string {
    contentType := instance.Header("Content-Type")
    if "" == contentType {
        return ""
    }

    mediaType, _, parseMediaTypeErr := mime.ParseMediaType(contentType)
    if nil != parseMediaTypeErr {
        return contentType
    }

    return mediaType
}

func (instance *Request) ParseFormBody() error {
    err := instance.httpRequest.ParseForm()
    if nil != err {
        return err
    }

    instance.post = bag.NewParameterBagFromValues(instance.httpRequest.PostForm)

    return nil
}

func (instance *Request) FormValue(key string) string {
    return instance.httpRequest.FormValue(key)
}

/* Input reads the first present value from POST body, query string, then route parameters. Repeated keys return their first value. Use Params for the server-bound route value and bag.StringSlice for all repeated values. */
func (instance *Request) Input(key string) string {
    if nil != instance.post && true == instance.post.Has(key) {
        return firstStringValue(instance.post, key)
    }

    if nil != instance.query && true == instance.query.Has(key) {
        return firstStringValue(instance.query, key)
    }

    if nil != instance.params {
        value, exists := instance.params[key]
        if true == exists {
            return value
        }
    }

    return ""
}

func firstStringValue(parameterBag bagcontract.ParameterBag, key string) string {
    value, exists, err := bag.StringAt(parameterBag, key, 0)
    if false == exists || nil != err {
        return ""
    }

    return value
}

func (instance *Request) Cookie(name string) (*nethttp.Cookie, error) {
    return instance.httpRequest.Cookie(name)
}

func (instance *Request) Cookies() []*nethttp.Cookie {
    return instance.httpRequest.Cookies()
}

func (instance *Request) Locale() string {
    return bag.StringOrDefault(instance.attributes, RouteAttributeLocale, "")
}

func (instance *Request) RouteName() string {
    return bag.StringOrDefault(instance.attributes, RouteAttributeName, "")
}

func (instance *Request) RoutePattern() string {
    return bag.StringOrDefault(instance.attributes, RouteAttributePattern, "")
}

func (instance *Request) Path() string {
    return instance.httpRequest.URL.Path
}

func (instance *Request) Method() string {
    return instance.httpRequest.Method
}

func (instance *Request) RuntimeInstance() runtimecontract.Runtime {
    return instance.runtimeInstance
}

func (instance *Request) RequestContext() httpcontract.RequestContext {
    return instance.requestContext
}

var _ httpcontract.Request = (*Request)(nil)
