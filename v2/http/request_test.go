package http

import (
    "io"
    "net/http/httptest"
    "strings"
    "testing"
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

    fooRaw, fooExists := queryBag.Get("foo")
    if false == fooExists {
        t.Fatalf("expected query param 'foo' to exist in bag")
    }
    fooValues, ok := fooRaw.([]string)
    if false == ok || 0 == len(fooValues) {
        t.Fatalf("expected query param 'foo' to be []string with values")
    }
    if "bar" != fooValues[0] {
        t.Fatalf("expected query param 'foo' to be 'bar', got: %s", fooValues[0])
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
    httpRequest := httptest.NewRequest("GET", "/test?key=val", nil)

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

    val := request.FormValue("key")
    if "value" != val {
        t.Fatalf("expected 'value', got: %s", val)
    }
}

func TestRequest_FormValue(t *testing.T) {
    body := strings.NewReader("field=formval")
    httpRequest := httptest.NewRequest("POST", "/test", body)
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := NewRequest(httpRequest, nil, nil, nil)

    val := request.FormValue("field")
    if "formval" != val {
        t.Fatalf("expected 'formval', got: %s", val)
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
