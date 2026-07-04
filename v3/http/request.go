package http

import (
    "bytes"
    "io"
    "mime"
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/bag"
    bagcontract "github.com/precision-soft/melody/v3/bag/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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
        /* @important a urlencoded body is drained by ParseForm; buffer it first and restore Body/GetBody
        afterwards so a later reader that needs the raw bytes still sees them — in particular the HMAC
        internal-auth source, whose signed body-hash check would otherwise verify against an empty body and
        silently accept a tampered form-encoded request. multipart bodies are left untouched: ParseForm does
        not read them (a handler streams them through ParseMultipartForm), so buffering there would defeat
        the large-upload disk spooling for no benefit. */
        var rawBody []byte
        bufferedBody := false
        if true == isUrlEncodedForm(httpRequest) {
            rawBody, bufferedBody = readRequestBodyBytes(httpRequest)
            if true == bufferedBody {
                restoreRequestBody(httpRequest, rawBody)
            }
        }

        parseFormErr := httpRequest.ParseForm()

        if true == bufferedBody {
            restoreRequestBody(httpRequest, rawBody)
        }

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

/* isUrlEncodedForm reports whether the request carries an application/x-www-form-urlencoded body — the one
auto-parsed form type whose body ParseForm consumes (multipart is streamed separately), so only this one
needs its body buffered and restored for later readers. */
func isUrlEncodedForm(httpRequest *nethttp.Request) bool {
    mediaType, _, parseErr := mime.ParseMediaType(httpRequest.Header.Get("Content-Type"))
    if nil != parseErr {
        return false
    }

    return "application/x-www-form-urlencoded" == mediaType
}

/* readRequestBodyBytes reads the request body fully into memory, reporting whether a body was present. It
does not restore the body; the caller restores it through restoreRequestBody once (or twice, around a
draining parse) as needed. */
func readRequestBodyBytes(httpRequest *nethttp.Request) ([]byte, bool) {
    if nil == httpRequest.Body {
        return nil, false
    }

    bodyBytes, readErr := io.ReadAll(httpRequest.Body)
    if nil != readErr {
        return nil, false
    }

    _ = httpRequest.Body.Close()

    return bodyBytes, true
}

/* restoreRequestBody replaces Body and GetBody with fresh readers over the given bytes, so a consumer that
already drained the body (ParseForm) does not strand it empty for the next reader. */
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
