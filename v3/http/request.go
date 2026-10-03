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
        /* a urlencoded body is buffered and restored around ParseForm, so a later reader, the HMAC internal-auth source among them, sees the raw bytes; a multipart body is left untouched, to keep its disk spooling */
        var rawBody []byte
        bufferedBody := false
        if true == isUrlEncodedForm(httpRequest) {
            rawBody, bufferedBody, bodyReadErr = readRequestBodyBytes(httpRequest)
            if true == bufferedBody {
                restoreRequestBody(httpRequest, rawBody)
            }
        }

        /* a body whose read failed is not parsed; the read error is recorded and the kernel refuses the request */
        if nil == bodyReadErr {
            parseFormErr := httpRequest.ParseForm()

            if true == bufferedBody {
                restoreRequestBody(httpRequest, rawBody)
            }

            /* ParseForm reports the body's failure before the query's, so its error alone does not say which half broke */
            if nil == parseFormErr {
                postBag = bag.NewParameterBagFromValues(httpRequest.PostForm)
            } else if bodyParseErr := urlEncodedBodyParseError(bufferedBody, rawBody); nil != bodyParseErr {
                /* a form that does not parse is recorded, and the kernel refuses the request; the half it yielded is not published */
                bodyReadErr = exception.NewError(
                    "failed to parse form data",
                    map[string]any{
                        "method": httpRequest.Method,
                        "path":   httpRequest.URL.Path,
                    },
                    bodyParseErr,
                )
            } else {
                /* the body parsed and only the query did not: that is not the handler's form, and Request reads the query through URL.Query() */
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
    /* the error that stopped the urlencoded body from being buffered; the kernel refuses the request when it is set */
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

/* urlEncodedBodyParseError reports the failure of the body half of ParseForm, by parsing the buffered bytes as ParseForm does. A multipart body or a request with none has no body half. */
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

/* readRequestBodyBytes reads the body into memory, reporting whether one was present and the error that interrupted the read; it does not restore the body. */
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

/* restoreRequestBody replaces Body and GetBody with fresh readers over the given bytes. */
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

/* Input answers a request parameter by name from the POST body, then the query string, then the route parameters: the first source that has the key answers. A handler that must have the value the router bound reads Params. A repeated key answers its first value, as FormValue does; bag.StringSlice reads the whole array. */
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
