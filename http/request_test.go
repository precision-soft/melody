package http

import (
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/bag"
)

func TestNewRequest_ValidHttpRequest(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test?foo=bar&baz=qux", nil)

    request := NewRequest(httpRequest, map[string]string{"id": "42"}, nil, nil)

    if nil == request {
        t.Fatalf("expected non-nil request")
    }

    if httpRequest != request.HttpRequest() {
        t.Fatalf("expected same http request reference")
    }

    value, exists := request.Param("id")
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

func TestNewRequest_NilRouteParams(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)

    request := NewRequest(httpRequest, nil, nil, nil)

    params := request.Params()
    if nil == params {
        t.Fatalf("expected non-nil params map")
    }
    if 0 != len(params) {
        t.Fatalf("expected empty params map, got %d entries", len(params))
    }
}

func TestNewRequest_NilHttpRequest_Panics(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected panic for nil http request")
        }
    }()

    NewRequest(nil, nil, nil, nil)
}

func TestNewRequest_PostFormParsing(t *testing.T) {
    body := strings.NewReader("username=john&password=secret")
    httpRequest := httptest.NewRequest("POST", "/login", body)
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(httpRequest, nil, nil, nil)

    postBag := request.Post()
    if nil == postBag {
        t.Fatalf("expected non-nil post bag")
    }
    if false == postBag.Has("username") {
        t.Fatalf("expected post param 'username' to exist")
    }
    if false == postBag.Has("password") {
        t.Fatalf("expected post param 'password' to exist")
    }

    username := request.FormValue("username")
    if "john" != username {
        t.Fatalf("expected username 'john', got: %s", username)
    }

    password := request.FormValue("password")
    if "secret" != password {
        t.Fatalf("expected password 'secret', got: %s", password)
    }
}

func TestNewRequest_PutFormParsing(t *testing.T) {
    body := strings.NewReader("name=updated")
    httpRequest := httptest.NewRequest("PUT", "/resource/1", body)
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(httpRequest, nil, nil, nil)

    postBag := request.Post()
    if nil == postBag {
        t.Fatalf("expected non-nil post bag")
    }
    if false == postBag.Has("name") {
        t.Fatalf("expected post param 'name' to exist")
    }

    name := request.FormValue("name")
    if "updated" != name {
        t.Fatalf("expected name 'updated', got: %s", name)
    }
}

func TestNewRequest_PatchFormParsing(t *testing.T) {
    body := strings.NewReader("field=value")
    httpRequest := httptest.NewRequest("PATCH", "/resource/1", body)
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(httpRequest, nil, nil, nil)

    postBag := request.Post()
    if nil == postBag {
        t.Fatalf("expected non-nil post bag")
    }
    if false == postBag.Has("field") {
        t.Fatalf("expected post param 'field' to exist")
    }

    field := request.FormValue("field")
    if "value" != field {
        t.Fatalf("expected field 'value', got: %s", field)
    }
}

func TestNewRequest_GetDoesNotParseForm(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test?key=value", nil)

    request := NewRequest(httpRequest, nil, nil, nil)

    postBag := request.Post()
    if nil == postBag {
        t.Fatalf("expected non-nil post bag")
    }

    if true == postBag.Has("key") {
        t.Fatalf("GET request should not have post params")
    }
}

func TestRequest_Input_FallsBackToParams(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)

    request := NewRequest(httpRequest, map[string]string{"pkey": "pval"}, nil, nil)

    pval := request.Input("pkey")
    if "pval" != pval {
        t.Fatalf("expected param value, got: %s", pval)
    }

    missing := request.Input("missing")
    if "" != missing {
        t.Fatalf("expected empty string for missing key, got: %s", missing)
    }
}

func TestRequest_ParamsCopied(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)
    originalParams := map[string]string{"id": "1"}

    request := NewRequest(httpRequest, originalParams, nil, nil)

    params := request.Params()
    params["id"] = "modified"

    value, _ := request.Param("id")
    if "1" != value {
        t.Fatalf("original params should not be modified, got: %s", value)
    }
}

func TestRequest_Path(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/api/v1/users", nil)

    request := NewRequest(httpRequest, nil, nil, nil)

    if "/api/v1/users" != request.Path() {
        t.Fatalf("unexpected path: %s", request.Path())
    }
}

func TestRequest_Method(t *testing.T) {
    httpRequest := httptest.NewRequest("DELETE", "/resource", nil)

    request := NewRequest(httpRequest, nil, nil, nil)

    if "DELETE" != request.Method() {
        t.Fatalf("unexpected method: %s", request.Method())
    }
}

func TestRequest_Header(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)
    httpRequest.Header.Set("X-Custom-Header", "custom-value")

    request := NewRequest(httpRequest, nil, nil, nil)

    if "custom-value" != request.Header("X-Custom-Header") {
        t.Fatalf("unexpected header value: %s", request.Header("X-Custom-Header"))
    }
}

func TestNewRequest_DoesNotParseFormForJsonContentType(t *testing.T) {
    body := strings.NewReader(`{"name":"melody"}`)
    httpRequest := httptest.NewRequest("POST", "/", body)
    httpRequest.Header.Set("Content-Type", "application/json")

    request := NewRequest(httpRequest, nil, nil, nil)

    if true == request.Post().Has("name") {
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

    request := NewRequest(httpRequest, nil, nil, nil)

    if true == request.Post().Has("username") {
        t.Fatalf("body without Content-Type must not be parsed as form")
    }
}

func TestRequest_ContentType(t *testing.T) {
    httpRequest := httptest.NewRequest("POST", "/test", nil)
    httpRequest.Header.Set("Content-Type", "application/json; charset=utf-8")

    request := NewRequest(httpRequest, nil, nil, nil)

    if "application/json" != request.ContentType() {
        t.Fatalf("unexpected content type: %s", request.ContentType())
    }
}

func TestRequest_ContentType_Empty(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)

    request := NewRequest(httpRequest, nil, nil, nil)

    if "" != request.ContentType() {
        t.Fatalf("expected empty content type, got: %s", request.ContentType())
    }
}

func TestRequest_RequestContext(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)

    request := NewRequest(httpRequest, nil, nil, nil)

    if nil != request.RequestContext() {
        t.Fatalf("expected nil request context when none provided")
    }
}

func TestRequest_RuntimeInstance(t *testing.T) {
    httpRequest := httptest.NewRequest("GET", "/test", nil)

    request := NewRequest(httpRequest, nil, nil, nil)

    if nil != request.RuntimeInstance() {
        t.Fatalf("expected nil runtime instance when none provided")
    }
}

func TestRequest_ParseFormBody(t *testing.T) {
    body := strings.NewReader("key=value")
    httpRequest := httptest.NewRequest("POST", "/test", body)
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(httpRequest, nil, nil, nil)

    err := request.ParseFormBody()
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    postBag := request.Post()
    if false == postBag.Has("key") {
        t.Fatalf("expected post param 'key' to exist after ParseFormBody")
    }

    value := request.FormValue("key")
    if "value" != value {
        t.Fatalf("expected 'value', got: %s", value)
    }
}

func TestRequest_FormValue(t *testing.T) {
    body := strings.NewReader("field=formval")
    httpRequest := httptest.NewRequest("POST", "/test", body)
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(httpRequest, nil, nil, nil)

    value := request.FormValue("field")
    if "formval" != value {
        t.Fatalf("expected 'formval', got: %s", value)
    }
}

func TestNewRequest_ParseFormError_PostBagIsEmpty(t *testing.T) {
    httpRequest := httptest.NewRequest("POST", "/test", nil)
    httpRequest.Header.Set("Content-Type", "multipart/form-data; boundary=invalid")
    httpRequest.Body = io.NopCloser(strings.NewReader("not valid multipart"))

    request := NewRequest(httpRequest, nil, nil, nil)

    postBag := request.Post()
    if nil == postBag {
        t.Fatalf("expected non-nil post bag")
    }

    if true == postBag.Has("anything") {
        t.Fatalf("expected empty post bag when form parsing fails")
    }
}

func TestNewRequest_ParseFormError_NilRuntime_NoPanic(t *testing.T) {
    httpRequest := httptest.NewRequest("POST", "/test", nil)
    httpRequest.Header.Set("Content-Type", "multipart/form-data; boundary=invalid")
    httpRequest.Body = io.NopCloser(strings.NewReader("not valid multipart"))

    request := NewRequest(httpRequest, nil, nil, nil)

    if nil == request {
        t.Fatalf("expected non-nil request even when form parsing fails with nil runtime")
    }
}

/* Input delivers the query and post values it silently lost: the request bags stored every value as a list and the lax string accessor answered ("", true) for a list, so a provided parameter read as an empty field */
func TestRequest_Input_DeliversQueryAndPostValues(t *testing.T) {
    queryRequest := httptest.NewRequest("GET", "/search?term=melody", nil)
    request := NewRequest(queryRequest, nil, nil, nil)

    if "melody" != request.Input("term") {
        t.Fatalf("expected the query value, got %q", request.Input("term"))
    }

    formBody := strings.NewReader("field=abc")
    postRequest := httptest.NewRequest("POST", "/submit", formBody)
    postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    postingRequest := NewRequest(postRequest, nil, nil, nil)

    if "abc" != postingRequest.Input("field") {
        t.Fatalf("expected the post value, got %q", postingRequest.Input("field"))
    }
}

/* The shape of a request parameter is the client's to choose, so repeating one is not a programming error
to refuse loudly: the refusal was a 500 with a full stack record that any client could raise at will. The
whole array is still reachable, through bag.StringSlice. */
func TestRequest_Input_AnswersTheFirstValueOfARepeatedKey(t *testing.T) {
    repeatedRequest := httptest.NewRequest("GET", "/search?a=1&a=2", nil)
    request := NewRequest(repeatedRequest, nil, nil, nil)

    if "1" != request.Input("a") {
        t.Fatalf("expected the first value of the repeated key, got %q", request.Input("a"))
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

    request := NewRequest(postRequest, nil, nil, nil)

    if "1" != request.Input("a") {
        t.Fatalf("expected the first value of the repeated form key, got %q", request.Input("a"))
    }
}

/* a form that does not parse is refused the way a body that does not read is: a warning that let the request continue handed the handler an empty form for a real submission */
func TestNewRequest_UnparsableFormIsRecordedForRefusal(t *testing.T) {
    formBody := strings.NewReader("a=%zz&csrf=token")
    postRequest := httptest.NewRequest("POST", "/submit", formBody)
    postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(postRequest, nil, nil, nil)

    if nil == request.bodyReadErr {
        t.Fatalf("expected the unparsable form to be recorded for the kernel's refusal")
    }

    if true == request.Post().Has("csrf") {
        t.Fatalf("expected no half-parsed form to reach the handler")
    }
}

/* the cookie accessors, the locale, the route pattern and the two error constructors were all at zero coverage. The cookie pair is the one an authentication middleware reads, and an accessor reading the wrong header would report every client as carrying no session at all. */

func TestRequest_CookieAccessorsReadWhatTheClientSent(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/articles", nil)
    httpRequest.AddCookie(&nethttp.Cookie{Name: "session", Value: "abc"})
    httpRequest.AddCookie(&nethttp.Cookie{Name: "locale", Value: "de"})

    request := NewRequest(httpRequest, nil, nil, nil)

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
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "/articles/42", nil), nil, nil, nil)

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

/* the two error constructors are public sentinels an application compares against; each has to carry its own message, because two errors sharing one would make a content-type refusal indistinguishable from a trailing-data refusal in every log that renders them. */

func TestRequestErrors_CarryDistinctMessages(t *testing.T) {
    unsupportedContentType := ErrorUnsupportedContentType()
    extraData := ErrorJsonBodyHasExtraData()

    if nil == unsupportedContentType || nil == extraData {
        t.Fatalf("expected both error constructors to produce an error")
    }

    if "unsupported content type" != unsupportedContentType.Error() {
        t.Fatalf("unexpected message: %q", unsupportedContentType.Error())
    }

    if "json body has extra data" != extraData.Error() {
        t.Fatalf("unexpected message: %q", extraData.Error())
    }

    if unsupportedContentType.Error() == extraData.Error() {
        t.Fatalf("the two refusals must not share a message")
    }
}
