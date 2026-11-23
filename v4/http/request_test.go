package http

import (
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strconv"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bag"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/session"
)

/* every attribute the kernel owns carries the framework's prefix, so an application attribute cannot collide with one by name */
func TestRequestAttributes_TheSessionAndTheSchemeCarryTheFrameworkPrefix(t *testing.T) {
    if "_session" != requestAttributeSession {
        t.Fatalf("expected the session attribute under the framework prefix, got %q", requestAttributeSession)
    }

    if "_scheme" != RequestAttributeScheme {
        t.Fatalf("expected the scheme attribute under the framework prefix, got %q", RequestAttributeScheme)
    }
}

func TestNewRequest_ValidHttpRequest(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test?foo=bar&baz=qux", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{RouteParameters: map[string]string{"id": "42"}})

    if nil == request {
        t.Fatalf("expected non-nil request")
    }

    if httpRequest != request.HttpRequest() {
        t.Fatalf("expected same http request reference")
    }

    value, exists := request.Parameter("id")
    if false == exists {
        t.Fatalf("expected param 'id' to exist")
    }
    if "42" != value {
        t.Fatalf("expected param 'id' to be '42', got: %s", value)
    }

    queryBag := request.Query()
    if nil == queryBag {
        t.Fatalf("expected non-nil query bag")
    }
    if false == queryBag.Has("foo") {
        t.Fatalf("expected query param 'foo' to exist")
    }

    /* a key that appeared once is stored as the string it really is; only a repeated key stays a string slice */
    fooRaw, fooExists := queryBag.Get("foo")
    if false == fooExists {
        t.Fatalf("expected query param 'foo' to exist in bag")
    }
    fooValue, ok := fooRaw.(string)
    if false == ok {
        t.Fatalf("expected single-occurrence query param 'foo' to be stored as a string, got: %T", fooRaw)
    }
    if "bar" != fooValue {
        t.Fatalf("expected query param 'foo' to be 'bar', got: %s", fooValue)
    }
}

func TestNewRequest_NilRouteParameters(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{})

    parameters := request.Parameters()
    if nil == parameters {
        t.Fatalf("expected non-nil parameters map")
    }
    if 0 != len(parameters) {
        t.Fatalf("expected empty parameters map, got %d entries", len(parameters))
    }
}

func TestNewRequest_NilHttpRequest_Panics(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected panic for nil http request")
        }
    }()

    NewRequest(nil, nil, RequestConfig{})
}

func TestNewRequest_PostFormParsing(t *testing.T) {
    body := strings.NewReader("username=john&password=secret")
    httpRequest := httptest.NewRequest("POST", "/login", body)
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(httpRequest, nil, formTestRequestConfig())

    postBag, postErr := request.Post()
    if nil != postErr {
        t.Fatalf("unexpected form error: %v", postErr)
    }
    if nil == postBag {
        t.Fatalf("expected non-nil post bag")
    }
    if false == postBag.Has("username") {
        t.Fatalf("expected post param 'username' to exist")
    }
    if false == postBag.Has("password") {
        t.Fatalf("expected post param 'password' to exist")
    }

    username, _ := request.Input("username")
    if "john" != username {
        t.Fatalf("expected username 'john', got: %s", username)
    }

    password, _ := request.Input("password")
    if "secret" != password {
        t.Fatalf("expected password 'secret', got: %s", password)
    }
}

func TestNewRequest_PutFormParsing(t *testing.T) {
    body := strings.NewReader("name=updated")
    httpRequest := httptest.NewRequest("PUT", "/resource/1", body)
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(httpRequest, nil, formTestRequestConfig())

    postBag, postErr := request.Post()
    if nil != postErr {
        t.Fatalf("unexpected form error: %v", postErr)
    }
    if nil == postBag {
        t.Fatalf("expected non-nil post bag")
    }
    if false == postBag.Has("name") {
        t.Fatalf("expected post param 'name' to exist")
    }

    name, _ := request.Input("name")
    if "updated" != name {
        t.Fatalf("expected name 'updated', got: %s", name)
    }
}

func TestNewRequest_PatchFormParsing(t *testing.T) {
    body := strings.NewReader("field=value")
    httpRequest := httptest.NewRequest("PATCH", "/resource/1", body)
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(httpRequest, nil, formTestRequestConfig())

    postBag, postErr := request.Post()
    if nil != postErr {
        t.Fatalf("unexpected form error: %v", postErr)
    }
    if nil == postBag {
        t.Fatalf("expected non-nil post bag")
    }
    if false == postBag.Has("field") {
        t.Fatalf("expected post param 'field' to exist")
    }

    field, _ := request.Input("field")
    if "value" != field {
        t.Fatalf("expected field 'value', got: %s", field)
    }
}

func TestNewRequest_GetDoesNotParseForm(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test?key=value", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{})

    postBag, postErr := request.Post()
    if nil != postErr {
        t.Fatalf("unexpected form error: %v", postErr)
    }
    if nil == postBag {
        t.Fatalf("expected non-nil post bag")
    }

    if true == postBag.Has("key") {
        t.Fatalf("GET request should not have post parameters")
    }
}

func TestRequest_Input_FallsBackToParameters(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{RouteParameters: map[string]string{"pkey": "pval"}})

    pval, _ := request.Input("pkey")
    if "pval" != pval {
        t.Fatalf("expected param value, got: %s", pval)
    }

    missing, _ := request.Input("missing")
    if "" != missing {
        t.Fatalf("expected empty string for missing key, got: %s", missing)
    }
}

func TestRequest_ParametersCopied(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)
    originalParameters := map[string]string{"id": "1"}

    request := NewRequest(httpRequest, nil, RequestConfig{RouteParameters: originalParameters})

    parameters := request.Parameters()
    parameters["id"] = "modified"

    value, _ := request.Parameter("id")
    if "1" != value {
        t.Fatalf("original parameters should not be modified, got: %s", value)
    }
}

func TestRequest_Path(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/api/v1/users", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{})

    if "/api/v1/users" != request.Path() {
        t.Fatalf("unexpected path: %s", request.Path())
    }
}

func TestRequest_Method(t *testing.T) {
    httpRequest := httptest.NewRequest("DELETE", "/resource", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{})

    if "DELETE" != request.Method() {
        t.Fatalf("unexpected method: %s", request.Method())
    }
}

func TestRequest_Header(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)
    httpRequest.Header.Set("X-Custom-Header", "custom-value")

    request := NewRequest(httpRequest, nil, RequestConfig{})

    if "custom-value" != request.Header("X-Custom-Header") {
        t.Fatalf("unexpected header value: %s", request.Header("X-Custom-Header"))
    }
}

func TestNewRequest_DoesNotParseFormForJsonContentType(t *testing.T) {
    body := strings.NewReader(`{"name":"melody"}`)
    httpRequest := httptest.NewRequest("POST", "/", body)
    httpRequest.Header.Set("Content-Type", "application/json")

    request := NewRequest(httpRequest, nil, RequestConfig{})

    if postBag, _ := request.Post(); true == postBag.Has("name") {
        t.Fatalf("JSON body must not be parsed as form")
    }

    remaining, readErr := io.ReadAll(httpRequest.Body)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    if `{"name":"melody"}` != string(remaining) {
        t.Fatalf("expected body intact after NewRequest, got: %s", string(remaining))
    }
}

func TestNewRequest_DoesNotParseFormWhenContentTypeMissing(t *testing.T) {
    body := strings.NewReader("username=alice")
    httpRequest := httptest.NewRequest("POST", "/", body)

    request := NewRequest(httpRequest, nil, RequestConfig{})

    if postBag, _ := request.Post(); true == postBag.Has("username") {
        t.Fatalf("body without Content-Type must not be parsed as form")
    }
}

func TestRequest_ContentType(t *testing.T) {
    httpRequest := httptest.NewRequest("POST", "/test", nil)
    httpRequest.Header.Set("Content-Type", "application/json; charset=utf-8")

    request := NewRequest(httpRequest, nil, RequestConfig{})

    if "application/json" != request.ContentType() {
        t.Fatalf("unexpected content type: %s", request.ContentType())
    }
}

func TestRequest_ContentType_Empty(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{})

    if "" != request.ContentType() {
        t.Fatalf("expected empty content type, got: %s", request.ContentType())
    }
}

func TestRequest_RequestContext(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{})

    if nil != request.RequestContext() {
        t.Fatalf("expected nil request context when none provided")
    }
}

func TestRequest_RuntimeInstance(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{})

    if nil != request.RuntimeInstance() {
        t.Fatalf("expected nil runtime instance when none provided")
    }
}

func TestRequest_Post_Multipart_EmptyAndBodyUntouched(t *testing.T) {
    multipartBody := "--boundary\r\nContent-Disposition: form-data; name=\"field\"\r\n\r\nvalue\r\n--boundary--\r\n"
    httpRequest := httptest.NewRequest("POST", "/test", strings.NewReader(multipartBody))
    httpRequest.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")

    request := NewRequest(httpRequest, nil, RequestConfig{})

    postBag, postErr := request.Post()
    if nil != postErr || nil == postBag || true == postBag.Has("field") {
        t.Fatalf("expected the empty form and no error for a multipart body, got %v %v", postBag, postErr)
    }

    remaining, _ := io.ReadAll(httpRequest.Body)
    if multipartBody != string(remaining) {
        t.Fatalf("expected the multipart body untouched, got %q", string(remaining))
    }
}

func TestRequest_Input_DeliversQueryAndPostValues(t *testing.T) {
    queryRequest := httptest.NewRequest("GET", "/search?term=melody", nil)
    request := NewRequest(queryRequest, nil, formTestRequestConfig())

    if term, _ := request.Input("term"); "melody" != term {
        t.Fatalf("expected the query value, got %q", term)
    }

    formBody := strings.NewReader("field=abc")
    postRequest := httptest.NewRequest("POST", "/submit", formBody)
    postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    postingRequest := NewRequest(postRequest, nil, formTestRequestConfig())

    if field, _ := postingRequest.Input("field"); "abc" != field {
        t.Fatalf("expected the post value, got %q", field)
    }
}

/* The shape of a request parameter is the client's to choose, so repeating one is not a programming error to refuse loudly: the refusal was a 500 with a full stack record that any client could raise at will. The whole array is still reachable, through bag.StringSlice. */
func TestRequest_Input_AnswersTheFirstValueOfARepeatedKey(t *testing.T) {
    repeatedRequest := httptest.NewRequest("GET", "/search?a=1&a=2", nil)
    request := NewRequest(repeatedRequest, nil, RequestConfig{})

    if value, _ := request.Input("a"); "1" != value {
        t.Fatalf("expected the first value of the repeated key, got %q", value)
    }

    values, exists := bag.StringSlice(request.Query(), "a")
    if false == exists || 2 != len(values) || "1" != values[0] || "2" != values[1] {
        t.Fatalf("expected the repeated key to stay readable whole, got %v", values)
    }
}

func TestRequest_Input_AnswersTheFirstValueOfARepeatedFormKey(t *testing.T) {
    formBody := strings.NewReader("a=1&a=2")
    postRequest := httptest.NewRequest("POST", "/submit", formBody)
    postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(postRequest, nil, formTestRequestConfig())

    if value, _ := request.Input("a"); "1" != value {
        t.Fatalf("expected the first value of the repeated form key, got %q", value)
    }
}

/* a form that does not parse answers its error from every read: a handler handed an empty form for a real submission would process it as a blank one */
func TestRequest_Post_MalformedBody_400AndTheSameErrorOnEveryRead(t *testing.T) {
    formBody := strings.NewReader("a=%zz&csrf=token")
    postRequest := httptest.NewRequest("POST", "/submit", formBody)
    postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(postRequest, nil, formTestRequestConfig())

    postBag, firstErr := request.Post()
    httpException := exception.AsHttpException(firstErr)
    if nil == httpException || nethttp.StatusBadRequest != httpException.StatusCode() {
        t.Fatalf("expected a 400 for the unparsable form, got %v", firstErr)
    }

    if true == postBag.Has("csrf") {
        t.Fatalf("expected no half-parsed form to reach the handler")
    }

    secondBag, secondErr := request.Post()
    if firstErr != secondErr || true == secondBag.Has("csrf") {
        t.Fatalf("expected the same error on the second read, got %v", secondErr)
    }
}

func TestRequest_Post_AnUnparsableFormRefusalBoundsThePathItCarries(t *testing.T) {
    postRequest := httptest.NewRequest("POST", "/"+strings.Repeat("a", 600*1024), strings.NewReader("a=%zz"))
    postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(postRequest, nil, formTestRequestConfig())

    _, postErr := request.Post()
    var refusal *exception.Error
    if false == exception.ChainAs(postErr, &refusal) {
        t.Fatalf("expected the parse refusal as an exception error in the chain, got %T", postErr)
    }

    path, _ := refusal.Context()["path"].(string)
    if 600 < len(path) || false == strings.Contains(path, "...(truncated ") {
        t.Fatalf("expected the refusal's path bounded, got %d bytes", len(path))
    }
}

/* the read error is the cause itself, not a parse failure of a body that never arrived */
func TestRequest_Post_ARawBodyReadFailure_400CarryingTheReadError(t *testing.T) {
    readErr := errors.New("the client vanished mid-upload")

    postRequest := httptest.NewRequest("POST", "/submit", &failingBodyReader{err: readErr})
    postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(postRequest, nil, formTestRequestConfig())

    _, postErr := request.Post()
    httpException := exception.AsHttpException(postErr)
    if nil == httpException || nethttp.StatusBadRequest != httpException.StatusCode() || readErr != httpException.CauseErr() {
        t.Fatalf("expected a 400 caused by the read error itself, got %v", postErr)
    }
}

type failingBodyReader struct {
    err error
}

func (instance *failingBodyReader) Read(buffer []byte) (int, error) {
    return 0, instance.err
}

/* the cookie accessors, the locale, the route pattern and the two error constructors were all at zero coverage. The cookie pair is the one an authentication middleware reads, and an accessor reading the wrong header would report every client as carrying no session at all. */

func TestRequest_CookieAccessorsReadWhatTheClientSent(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/articles", nil)
    httpRequest.AddCookie(&nethttp.Cookie{Name: "session", Value: "abc"})
    httpRequest.AddCookie(&nethttp.Cookie{Name: "locale", Value: "de"})

    request := NewRequest(httpRequest, nil, RequestConfig{})

    sessionCookie, cookieErr := request.Cookie("session")
    if nil != cookieErr {
        t.Fatalf("unexpected error reading a cookie the client sent: %v", cookieErr)
    }

    if "abc" != sessionCookie.Value {
        t.Fatalf("unexpected cookie value: %q", sessionCookie.Value)
    }

    if 2 != len(request.Cookies()) {
        t.Fatalf("expected both cookies, got: %d", len(request.Cookies()))
    }

    if _, missingErr := request.Cookie("nothing-here"); nil == missingErr {
        t.Fatalf("expected a cookie the client did not send to be reported missing")
    }
}

/* the locale and the route pattern are read off the route attributes the router published, not off the url — the pattern is what an application groups metrics and audit records by, and reading the concrete path instead would make every identifier its own route. */

func TestRequest_LocaleAndRoutePatternComeFromTheRouteAttributes(t *testing.T) {
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/articles/42", nil), nil, RequestConfig{})

    if "" != request.Locale() {
        t.Fatalf("expected no locale before the router publishes one, got: %q", request.Locale())
    }

    if "" != request.RoutePattern() {
        t.Fatalf("expected no route pattern before the router publishes one, got: %q", request.RoutePattern())
    }

    request.Attributes().Set(RouteAttributeLocale, "de")
    request.Attributes().Set(RouteAttributePattern, "/articles/:id")
    request.Attributes().Set(RouteAttributeName, "article.show")

    if "de" != request.Locale() {
        t.Fatalf("unexpected locale: %q", request.Locale())
    }

    if "/articles/:id" != request.RoutePattern() {
        t.Fatalf("unexpected route pattern: %q", request.RoutePattern())
    }

    if "article.show" != request.RouteName() {
        t.Fatalf("unexpected route name: %q", request.RouteName())
    }

    if request.RoutePattern() == request.Path() {
        t.Fatalf("the pattern must not collapse onto the concrete path")
    }
}

/* the precedence Input documents was implemented but pinned nowhere: every existing case populates a single source, so none of them would notice the order changing. */
func TestRequest_Input_PrefersThePostBodyOverTheQuery(t *testing.T) {
    httpRequest := httptest.NewRequest("POST", "/submit?field=fromQuery", strings.NewReader("field=fromBody"))
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(httpRequest, nil, formTestRequestConfig())

    if field, _ := request.Input("field"); "fromBody" != field {
        t.Fatalf("expected the post body to win over the query, got %q", field)
    }
}

func TestRequest_Input_PrefersTheQueryOverTheRouteParameters(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/articles/fromParameters?id=fromQuery", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{RouteParameters: map[string]string{"id": "fromParameters"}})

    if id, _ := request.Input("id"); "fromQuery" != id {
        t.Fatalf("expected the query to win over the route parameters, got %q", id)
    }
}

func TestRequest_Input_FallsThroughToTheRouteParametersOnly(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/articles/fromParameters", nil)

    request := NewRequest(httpRequest, nil, RequestConfig{RouteParameters: map[string]string{"id": "fromParameters"}})

    if id, _ := request.Input("id"); "fromParameters" != id {
        t.Fatalf("expected the route parameter when no other source carries the key, got %q", id)
    }
}

/* a query the client wrote with a legacy semicolon separator is not a form that failed to parse: ParseForm reports the query's failure through the same return value as the body's, and refusing on it answered 400 to a submission whose body was perfectly valid, with the handler's own form emptied on the way there */
func TestNewRequest_AMalformedQueryDoesNotRefuseAValidForm(t *testing.T) {
    formBody := strings.NewReader("field=value&csrf=token")
    postRequest := httptest.NewRequest("POST", "/submit?a=b;c=d", formBody)
    postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(postRequest, nil, formTestRequestConfig())

    field, fieldErr := request.Input("field")
    if nil != fieldErr || "value" != field {
        t.Fatalf("expected the parsed form to reach the handler, got %q %v", field, fieldErr)
    }

    if csrf, _ := request.Input("csrf"); "token" != csrf {
        t.Fatalf("expected every parsed form key to reach the handler, got %q", csrf)
    }
}

func serveUrlEncodedFormUnderALimit(t *testing.T, bodyLimitBytes int, valueBytes int) (int, int) {
    t.Helper()

    postedValueBytes := -1

    router := NewRouter()
    router.Handle(
        "/form",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            postBag, formErr := request.Post()
            if nil != formErr {
                return nil, formErr
            }

            value, _, _ := bag.StringAt(postBag, "value", 0)
            postedValueBytes = len(value)
            if len(request.HttpRequest().PostForm.Get("value")) != postedValueBytes {
                t.Errorf("expected PostForm to carry the posted value beside the form")
            }

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    serviceContainer := newHttpTestContainerWithSessionStorageAndEnvironmentValues(
        session.NewInMemoryStorage(session.InMemoryStorageConfig{}),
        map[string]string{
            config.HttpMaxRequestBodyBytesKey: strconv.Itoa(bodyLimitBytes),
        },
    )

    request := httptest.NewRequest(nethttp.MethodPost, "/form?page=1", strings.NewReader("value="+strings.Repeat("a", valueBytes)))
    request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    recorder := httptest.NewRecorder()

    NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer).ServeHTTP(recorder, request)

    return recorder.Code, postedValueBytes
}

func TestRequest_AUrlencodedBodyPastTheNetHttpCeilingIsParsedUnderTheConfiguredLimit(t *testing.T) {
    statusCode, postedValueBytes := serveUrlEncodedFormUnderALimit(t, 12<<20, 10<<20+1)

    if nethttp.StatusOK != statusCode || 10<<20+1 != postedValueBytes {
        t.Fatalf("expected the form parsed, got %d with %d bytes posted", statusCode, postedValueBytes)
    }
}

func TestRequest_AUrlencodedBodyPastTheConfiguredLimitIsRefused(t *testing.T) {
    statusCode, postedValueBytes := serveUrlEncodedFormUnderALimit(t, 12<<20, 12<<20)

    if nethttp.StatusRequestEntityTooLarge != statusCode || -1 != postedValueBytes {
        t.Fatalf("expected 413 with no form handed to the handler, got %d with %d bytes posted", statusCode, postedValueBytes)
    }
}

func TestRequest_AUrlencodedBodyUnderTheNetHttpCeilingIsParsedByNetHttp(t *testing.T) {
    statusCode, postedValueBytes := serveUrlEncodedFormUnderALimit(t, 12<<20, 9<<20)

    if nethttp.StatusOK != statusCode || 9<<20 != postedValueBytes {
        t.Fatalf("expected the form parsed, got %d with %d bytes posted", statusCode, postedValueBytes)
    }
}

func TestNewRequest_ZeroConfigHasNoRouteParametersAndNoRequestContext(t *testing.T) {
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/", nil), nil, RequestConfig{})

    if parameters := request.Parameters(); nil == parameters || 0 != len(parameters) {
        t.Fatalf("expected no route parameter as an empty map, got %#v", parameters)
    }

    if _, exists := request.Parameter("id"); true == exists {
        t.Fatalf("expected no route parameter to be found")
    }

    if nil != request.RequestContext() {
        t.Fatalf("expected no request context, got %v", request.RequestContext())
    }

    requestContext := NewRequestContext("minted", time.Unix(0, 0))
    configured := NewRequest(
        httptest.NewRequest(nethttp.MethodGet, "/", nil),
        nil,
        RequestConfig{RouteParameters: map[string]string{"id": "7"}, RequestContext: requestContext},
    )

    if value, exists := configured.Parameter("id"); false == exists || "7" != value {
        t.Fatalf("expected the configured route parameter, got %q %v", value, exists)
    }

    if requestContext != configured.RequestContext() {
        t.Fatalf("expected the configured request context")
    }
}

func newUrlEncodedRequest(body io.ReadCloser) *nethttp.Request {
    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/form?page=1", nil)
    httpRequest.Body = body
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    return httpRequest
}

func TestNewRequest_ReadsNoBody(t *testing.T) {
    body := newCountingBody("value=a")

    NewRequest(newUrlEncodedRequest(body), nil, formTestRequestConfig())

    if 0 != body.reads.Load() {
        t.Fatalf("expected no read at construction, got %d", body.reads.Load())
    }
}

func TestRequest_Post_ParsesOnFirstReadAndOnce(t *testing.T) {
    body := newCountingBody("value=a")
    request := NewRequest(newUrlEncodedRequest(body), nil, formTestRequestConfig())

    firstBag, _ := request.Post()
    readsAfterFirst := body.reads.Load()
    secondBag, _ := request.Post()

    if 0 == readsAfterFirst || readsAfterFirst != body.reads.Load() {
        t.Fatalf("expected the body read by the first call only, got %d then %d", readsAfterFirst, body.reads.Load())
    }

    if firstBag != secondBag || false == firstBag.Has("value") {
        t.Fatalf("expected the same parsed form on every call")
    }
}

func TestRequest_Post_RestoresTheBodyForALaterReader(t *testing.T) {
    httpRequest := newUrlEncodedRequest(io.NopCloser(strings.NewReader("value=a&other=b")))
    request := NewRequest(httpRequest, nil, formTestRequestConfig())

    _, _ = request.Post()

    remaining, readErr := io.ReadAll(httpRequest.Body)
    if nil != readErr || "value=a&other=b" != string(remaining) {
        t.Fatalf("expected the raw body restored, got %q %v", string(remaining), readErr)
    }

    if "a" != httpRequest.PostForm.Get("value") || "a" != httpRequest.Form.Get("value") || "1" != httpRequest.Form.Get("page") {
        t.Fatalf("expected PostForm and Form published as ParseForm does, got %v and %v", httpRequest.PostForm, httpRequest.Form)
    }
}

func TestRequest_Post_ConcurrentFirstReads_ParseOnce(t *testing.T) {
    body := newCountingBody("value=a")
    request := NewRequest(newUrlEncodedRequest(body), nil, formTestRequestConfig())

    start := make(chan struct{})
    results := make(chan bool, 8)
    for index := 0; index < 8; index++ {
        go func() {
            <-start
            postBag, postErr := request.Post()
            results <- nil == postErr && true == postBag.Has("value")
        }()
    }
    close(start)

    for index := 0; index < 8; index++ {
        if false == <-results {
            t.Fatalf("expected every concurrent reader to see the parsed form")
        }
    }

    /* "value=a" is read in one read and one end-of-file read */
    if 2 < body.reads.Load() {
        t.Fatalf("expected the body read once, got %d reads", body.reads.Load())
    }
}

func TestRequest_Post_OverTheCap_413CarryingTheSentinelAndTheMaxBytesError(t *testing.T) {
    httpRequest := newUrlEncodedRequest(nil)
    httpRequest.Body = nethttp.MaxBytesReader(httptest.NewRecorder(), io.NopCloser(strings.NewReader("value="+strings.Repeat("a", 32))), 16)
    request := NewRequest(httpRequest, nil, RequestConfig{MaxBodyBytes: bound.Of(16)})

    _, postErr := request.Post()

    httpException := exception.AsHttpException(postErr)
    if nil == httpException || nethttp.StatusRequestEntityTooLarge != httpException.StatusCode() || "payload too large" != httpException.Message() {
        t.Fatalf("expected a 413, got %v", postErr)
    }

    var maxBytesError *nethttp.MaxBytesError
    if false == exception.ChainIs(postErr, ErrRequestBodyTooLarge) || false == exception.ChainAs(postErr, &maxBytesError) {
        t.Fatalf("expected the sentinel and the max-bytes error in the chain, got %v", postErr)
    }

    if 16 != httpException.Context()["maxBodyBytes"] {
        t.Fatalf("expected the cap in the context, got %v", httpException.Context())
    }
}

/* a request built by hand carries no MaxBytesReader: the cap is the request's own */
func TestRequest_Post_OverTheCapWithoutAReaderLimit_413CarryingTheSentinel(t *testing.T) {
    request := NewRequest(newUrlEncodedRequest(io.NopCloser(strings.NewReader("value="+strings.Repeat("a", 11)))), nil, RequestConfig{MaxBodyBytes: bound.Of(16)})

    _, postErr := request.Post()

    httpException := exception.AsHttpException(postErr)
    if nil == httpException || nethttp.StatusRequestEntityTooLarge != httpException.StatusCode() || false == exception.ChainIs(postErr, ErrRequestBodyTooLarge) {
        t.Fatalf("expected a 413 carrying the sentinel, got %v", postErr)
    }
}

func TestRequest_Post_AtTheCap_Parsed(t *testing.T) {
    request := NewRequest(newUrlEncodedRequest(io.NopCloser(strings.NewReader("value="+strings.Repeat("a", 10)))), nil, RequestConfig{MaxBodyBytes: bound.Of(16)})

    postBag, postErr := request.Post()
    if nil != postErr || false == postBag.Has("value") {
        t.Fatalf("expected a body at the cap parsed, got %v", postErr)
    }
}

func TestRequest_Post_JsonBody_EmptyAndBodyUntouched(t *testing.T) {
    body := newCountingBody(`{"value":"a"}`)
    httpRequest := newUrlEncodedRequest(body)
    httpRequest.Header.Set("Content-Type", "application/json")
    request := NewRequest(httpRequest, nil, formTestRequestConfig())

    postBag, postErr := request.Post()
    if nil != postErr || true == postBag.Has("value") || 0 != body.reads.Load() {
        t.Fatalf("expected the empty form and an untouched body, got %v and %d reads", postErr, body.reads.Load())
    }
}

func TestRequest_Post_GetRequest_EmptyAndBodyUntouched(t *testing.T) {
    body := newCountingBody("value=a")
    httpRequest := newUrlEncodedRequest(body)
    httpRequest.Method = nethttp.MethodGet
    request := NewRequest(httpRequest, nil, formTestRequestConfig())

    postBag, postErr := request.Post()
    if nil != postErr || true == postBag.Has("value") || 0 != body.reads.Load() {
        t.Fatalf("expected the empty form and an untouched body, got %v and %d reads", postErr, body.reads.Load())
    }
}

func TestRequest_Input_FailedForm_AnswersTheError(t *testing.T) {
    request := NewRequest(newUrlEncodedRequest(io.NopCloser(strings.NewReader("value=%zz"))), nil, formTestRequestConfig())

    value, inputErr := request.Input("page")
    if nil == inputErr || "" != value {
        t.Fatalf("expected the form's error before the query is consulted, got %q %v", value, inputErr)
    }
}

func TestRequest_MaxBodyBytes_ConfigValueThenConfiguration(t *testing.T) {
    serviceContainer := newHttpTestContainerWithSessionStorageAndEnvironmentValues(
        session.NewInMemoryStorage(session.InMemoryStorageConfig{}),
        map[string]string{config.HttpMaxRequestBodyBytesKey: "4096"},
    )
    runtimeInstance := runtime.NewRequest(t.Context(), serviceContainer.NewScope())

    configured := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/", nil), runtimeInstance, RequestConfig{MaxBodyBytes: bound.Of(64)})
    if 64 != configured.MaxBodyBytes() {
        t.Fatalf("expected the config value, got %d", configured.MaxBodyBytes())
    }

    absent := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/", nil), runtimeInstance, RequestConfig{})
    if 4096 != absent.MaxBodyBytes() {
        t.Fatalf("expected the configuration's cap, got %d", absent.MaxBodyBytes())
    }
}

func TestNewRequest_MaxBodyBytesUnlimited_Panics(t *testing.T) {
    assertPanicsNamingField(t, func() {
        NewRequest(httptest.NewRequest(nethttp.MethodGet, "/", nil), nil, RequestConfig{MaxBodyBytes: bound.UnlimitedCount()})
    }, "the limit does not admit unlimited", "RequestConfig.MaxBodyBytes")
}

func TestNewRequest_MaxBodyBytesBelowOne_Panics(t *testing.T) {
    assertPanicsNamingField(t, func() {
        NewRequest(httptest.NewRequest(nethttp.MethodGet, "/", nil), nil, RequestConfig{MaxBodyBytes: bound.Of(0)})
    }, "the limit is below its minimum", "RequestConfig.MaxBodyBytes")
}

func TestHttpTestRequestMaxBodyBytes_IsTheConfigurationDefault(t *testing.T) {
    if config.DefaultHttpMaxRequestBodyBytes != testhelper.HttpTestRequestMaxBodyBytes {
        t.Fatalf("expected the test request's cap to be the configuration default")
    }
}
