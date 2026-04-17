package http

import (
    "mime"
    nethttp "net/http"

    "github.com/precision-soft/melody/v2/bag"
    bagcontract "github.com/precision-soft/melody/v2/bag/contract"
    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/logging"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

const (
    RequestAttributeSession = "session"
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

    if true == shouldAutoParseForm(httpRequest) {
        parseFormErr := httpRequest.ParseForm()
        if nil == parseFormErr {
            postBag = bag.NewParameterBagFromValues(httpRequest.PostForm)
        } else if nil != runtimeInstance {
            loggerInstance := logging.LoggerFromRuntime(runtimeInstance)
            if nil != loggerInstance {
                loggerInstance.Warning(
                    "failed to parse form data",
                    map[string]any{
                        "error":  parseFormErr.Error(),
                        "method": httpRequest.Method,
                        "path":   httpRequest.URL.Path,
                    },
                )
            }
        } else {
            logging.NewDefaultLogger().Warning(
                "failed to parse form data",
                map[string]any{
                    "error":  parseFormErr.Error(),
                    "method": httpRequest.Method,
                    "path":   httpRequest.URL.Path,
                },
            )
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

func (instance *Request) Input(key string) string {
    if nil != instance.post && true == instance.post.Has(key) {
        return bag.StringOrDefault(instance.post, key, "")
    }

    if nil != instance.query && true == instance.query.Has(key) {
        return bag.StringOrDefault(instance.query, key, "")
    }

    if nil != instance.params {
        value, exists := instance.params[key]
        if true == exists {
            return value
        }
    }

    return ""
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
