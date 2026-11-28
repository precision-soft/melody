package http

import (
    "bytes"
    "io"
    "mime"
    nethttp "net/http"
    "net/url"
    "sync"
    "sync/atomic"

    "github.com/precision-soft/melody/v4/bag"
    bagcontract "github.com/precision-soft/melody/v4/bag/contract"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* RequestAttributeScheme names the scheme the kernel publishes after the route's attributes, so a route attribute of the same name cannot replace it; it carries the framework's underscore prefix */
const RequestAttributeScheme = "_scheme"

const requestAttributeSession = "_session"

/* RequestConfig is what a request is built with beyond its http request and its runtime; every zero value is the default. */
type RequestConfig struct {
    /* RouteParameters are the parameters the matched route extracted from the path; nil is no parameter. */
    RouteParameters map[string]string

    /* RequestContext is the identity the kernel minted for the request; nil leaves Request.RequestContext answering nil. */
    RequestContext httpcontract.RequestContext

    /* MaxBodyBytes is the body cap the form read and BindJson apply; absent is the configuration's MELODY_HTTP_MAX_REQUEST_BODY_BYTES, read from the runtime at first need; unlimited and a value below 1 are refused. */
    MaxBodyBytes bound.Count
}

var requestMaxBodyBytesRule = bound.Rule[int]{Name: "RequestConfig.MaxBodyBytes", Minimum: 1}

/* NewRequest reads nothing of the body: the urlencoded form is parsed by its first reader. */
func NewRequest(
    httpRequest *nethttp.Request,
    runtimeInstance containercontract.Runtime,
    config RequestConfig,
) *Request {
    if nil == httpRequest {
        exception.Panic(
            exception.NewError("the http request is nil", nil, nil),
        )
    }

    maxBodyBytes := 0
    if false == config.MaxBodyBytes.IsAbsent() {
        resolvedMaxBodyBytes, resolveErr := bound.Resolve(config.MaxBodyBytes, requestMaxBodyBytesRule)
        if nil != resolveErr {
            /* bound.Resolve refuses with an exception error */
            exception.Panic(resolveErr.(*exception.Error))
        }

        maxBodyBytes = resolvedMaxBodyBytes.Value
    }

    routeParameters := config.RouteParameters
    if nil == routeParameters {
        routeParameters = map[string]string{}
    }

    return &Request{
        httpRequest:     httpRequest,
        parameters:      routeParameters,
        query:           bag.NewParameterBagFromValues(httpRequest.URL.Query()),
        attributes:      bag.NewParameterBag(),
        runtimeInstance: runtimeInstance,
        requestContext:  config.RequestContext,
        maxBodyBytes:    maxBodyBytes,
    }
}

type Request struct {
    httpRequest     *nethttp.Request
    parameters      map[string]string
    query           bagcontract.ParameterBag
    attributes      bagcontract.ParameterBag
    runtimeInstance containercontract.Runtime
    requestContext  httpcontract.RequestContext
    /* zero reads the configuration's cap */
    maxBodyBytes int

    formOnce   sync.Once
    formLoaded atomic.Bool
    post       bagcontract.ParameterBag
    formErr    error
}

func (instance *Request) HttpRequest() *nethttp.Request {
    return instance.httpRequest
}

func (instance *Request) Parameter(name string) (string, bool) {
    value, exists := instance.parameters[name]

    return value, exists
}

func (instance *Request) Parameters() map[string]string {
    copied := make(map[string]string, len(instance.parameters))

    for key, value := range instance.parameters {
        copied[key] = value
    }

    return copied
}

func (instance *Request) Query() bagcontract.ParameterBag {
    return instance.query
}

func (instance *Request) Post() (bagcontract.ParameterBag, error) {
    instance.formOnce.Do(instance.loadForm)

    return instance.post, instance.formErr
}

func (instance *Request) MaxBodyBytes() int {
    if 0 < instance.maxBodyBytes {
        return instance.maxBodyBytes
    }

    return config.ConfigMustFromRuntime(instance.runtimeInstance).Http().MaxRequestBodyBytes().Value
}

/* failedForm answers the error of a form read that ran, nil while no reader asked for the form */
func (instance *Request) failedForm() error {
    if false == instance.formLoaded.Load() {
        return nil
    }

    return instance.formErr
}

/* loadForm buffers a urlencoded body under the cap, restores it so a later reader, the HMAC internal-auth source among them, sees the raw bytes, and parses it; a multipart body is never read, so an upload is not held in memory */
func (instance *Request) loadForm() {
    defer instance.formLoaded.Store(true)

    instance.post = bag.NewParameterBag()

    httpRequest := instance.httpRequest
    if false == isUrlEncodedFormSubmission(httpRequest) || nil == httpRequest.Body {
        return
    }

    maxBodyBytes := instance.MaxBodyBytes()

    rawBody, readErr := io.ReadAll(io.LimitReader(httpRequest.Body, overCapAllowance(maxBodyBytes)))
    if nil != readErr {
        var maxBytesError *nethttp.MaxBytesError
        if true == exception.ChainAs(readErr, &maxBytesError) {
            instance.formErr = newRequestBodyTooLargeError(maxBodyBytes, readErr)

            return
        }

        instance.formErr = exception.NewHttpError(nethttp.StatusBadRequest, exception.HttpErrorConfig{Message: "bad request", Cause: readErr})

        return
    }

    _ = httpRequest.Body.Close()
    restoreRequestBody(httpRequest, rawBody)

    if maxBodyBytes < len(rawBody) {
        instance.formErr = newRequestBodyTooLargeError(maxBodyBytes, nil)

        return
    }

    /* parsed here and not by ParseForm, which refuses a body past net/http's own ceiling whatever the cap */
    postForm, parseErr := url.ParseQuery(string(rawBody))
    if nil != parseErr {
        instance.formErr = exception.NewHttpError(
            nethttp.StatusBadRequest,
            exception.HttpErrorConfig{
                Message: "bad request",
                Cause: exception.NewError(
                    "failed to parse form data",
                    map[string]any{
                        "method": internal.BoundDiagnosticText(httpRequest.Method),
                        "path":   internal.BoundDiagnosticText(httpRequest.URL.Path),
                    },
                    parseErr,
                ),
            },
        )

        return
    }

    /* published as ParseForm would, so a reader of the net/http request finds the form the framework read; with PostForm set, ParseForm only merges the query into Form, and a malformed query is not the form's failure */
    httpRequest.PostForm = postForm
    _ = httpRequest.ParseForm()
    instance.post = bag.NewParameterBagFromValues(postForm)
}

func (instance *Request) Attributes() bagcontract.ParameterBag {
    return instance.attributes
}

func (instance *Request) Header(name string) string {
    return instance.httpRequest.Header.Get(name)
}

/* isUrlEncodedFormSubmission reports a request of a method that is not safe carrying the urlencoded media type, the one body the form read parses; the method set is the one the csrf check guards, so a token in the field is read wherever it is asked for */
func isUrlEncodedFormSubmission(httpRequest *nethttp.Request) bool {
    if true == IsSafeMethod(httpRequest.Method) {
        return false
    }

    mediaType, _, parseErr := mime.ParseMediaType(httpRequest.Header.Get("Content-Type"))
    if nil != parseErr {
        return false
    }

    return "application/x-www-form-urlencoded" == mediaType
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

/* Input answers a request parameter by name from the urlencoded form of a request whose method is not safe, then the query string, then the route parameters: the first source that has the key answers. A multipart or json body is not a source. The error is the failed form read's, answered before the query is consulted, so a broken submission is never read as a blank one. A handler that must have the value the router bound reads Parameters. A repeated key answers its first value; bag.StringSlice reads the whole array. */
func (instance *Request) Input(key string) (string, error) {
    postBag, formErr := instance.Post()
    if nil != formErr {
        return "", formErr
    }

    if true == postBag.Has(key) {
        return firstStringValue(postBag, key), nil
    }

    if nil != instance.query && true == instance.query.Has(key) {
        return firstStringValue(instance.query, key), nil
    }

    if nil != instance.parameters {
        value, exists := instance.parameters[key]
        if true == exists {
            return value, nil
        }
    }

    return "", nil
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

func (instance *Request) RuntimeInstance() containercontract.Runtime {
    return instance.runtimeInstance
}

func (instance *Request) RequestContext() httpcontract.RequestContext {
    return instance.requestContext
}

var _ httpcontract.Request = (*Request)(nil)
