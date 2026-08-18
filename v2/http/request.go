package http

import (
    "bytes"
    "io"
    "mime"
    nethttp "net/http"

    "github.com/precision-soft/melody/v2/bag"
    bagcontract "github.com/precision-soft/melody/v2/bag/contract"
    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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
        /* a urlencoded body is drained by ParseForm; buffer it first and restore Body/GetBody
           afterwards so a later reader that needs the raw bytes still sees them — in particular the HMAC
           internal-auth source, whose signed body-hash check would otherwise verify against an empty body and
           silently accept a tampered form-encoded request. multipart bodies are left untouched: ParseForm does
           not read them (a handler streams them through ParseMultipartForm), so buffering there would defeat
           the large-upload disk spooling for no benefit. */
        var rawBody []byte
        bufferedBody := false
        if true == isUrlEncodedForm(httpRequest) {
            rawBody, bufferedBody, bodyReadErr = readRequestBodyBytes(httpRequest)
            if true == bufferedBody {
                restoreRequestBody(httpRequest, rawBody)
            }
        }

        /* a body whose read failed is not parsed: the reader is poisoned mid-stream, so ParseForm could only report a second symptom of the same failure — the read error is recorded on the request instead, and the kernel refuses the request before a handler mistakes an oversized or truncated submission for an empty one */
        if nil == bodyReadErr {
            parseFormErr := httpRequest.ParseForm()

            if true == bufferedBody {
                restoreRequestBody(httpRequest, rawBody)
            }

            if nil == parseFormErr {
                postBag = bag.NewParameterBagFromValues(httpRequest.PostForm)
            } else {
                /* a form that does not parse is recorded the way a body that does not read is, and the kernel refuses the request for both: a warning that let the request continue handed the handler an empty form for a real submission — "field missing" answered about a field the client sent */
                bodyReadErr = exception.NewError(
                    "failed to parse form data",
                    map[string]any{
                        "method": httpRequest.Method,
                        "path":   httpRequest.URL.Path,
                    },
                    parseFormErr,
                )
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
    /* the error that stopped the urlencoded body from being buffered for form parsing; the kernel refuses the request when it is set, so a handler never sees the failed read as an empty form */
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

/* readRequestBodyBytes reads the request body fully into memory, reporting whether a body was present and
the error that interrupted the read — a MaxBytesReader refusing an oversized body, or a client aborting
mid-upload. It does not restore the body; the caller restores it through restoreRequestBody once (or twice,
around a draining parse) as needed. */
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

/* Input answers the first value of a repeated key, as FormValue beside it and url.Values.Get already do.
The request bags keep a single key and a repeated one apart by type and bag.String refuses a slice with a
panic — right where the key is the programmer's, wrong here, because the shape of a request parameter is
the client's to choose: "?tag=a&tag=b" turned every handler reading a parameter by name into a 500 with a
full stack record, unauthenticated and free to repeat. A handler that needs the whole array reads it with
bag.StringSlice, which is what the panic was pointing at. */
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
