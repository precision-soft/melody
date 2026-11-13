package httpclient

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func mustResolveRequestOptions(t *testing.T, options RequestOptions) resolvedRequestOptions {
    t.Helper()

    resolved, err := resolveRequestOptions(options, nil, false)
    if nil != err {
        t.Fatalf("unexpected refusal: %v", err)
    }

    return resolved
}

func TestResolveRequestOptions_MaxResponseBodyBytesDefaultAndOverride(t *testing.T) {
    if resolved := mustResolveRequestOptions(t, RequestOptions{}); 10*1024*1024 != resolved.maxResponseBodyBytes {
        t.Fatalf("unexpected default cap: %d", resolved.maxResponseBodyBytes)
    }

    if resolved := mustResolveRequestOptions(t, RequestOptions{MaxResponseBodyBytes: bound.Of(10)}); 10 != resolved.maxResponseBodyBytes {
        t.Fatalf("unexpected cap: %d", resolved.maxResponseBodyBytes)
    }
}

func TestResolveRequestOptions_MaxResponseBodyBytesBelowOneOrUnlimitedIsRefused(t *testing.T) {
    for _, capBytes := range []int{0, -1} {
        _, err := resolveRequestOptions(RequestOptions{MaxResponseBodyBytes: bound.Of(capBytes)}, nil, false)
        assertRefusedField(t, err, "RequestOptions.MaxResponseBodyBytes", "the limit is below its minimum")
    }

    _, err := resolveRequestOptions(RequestOptions{MaxResponseBodyBytes: bound.UnlimitedCount()}, nil, false)
    assertRefusedField(t, err, "RequestOptions.MaxResponseBodyBytes", "the limit does not admit unlimited")
}

func TestResolveRequestOptions_ATimeoutBelowTheMinimumIsRefusedAndUnlimitedIsKept(t *testing.T) {
    _, err := resolveRequestOptions(RequestOptions{Timeout: bound.Of(time.Millisecond - 1)}, nil, false)
    assertRefusedField(t, err, "RequestOptions.Timeout", "the limit is below its minimum")

    resolved := mustResolveRequestOptions(t, RequestOptions{Timeout: bound.UnlimitedDuration()})
    if false == resolved.timeout.IsUnlimited() {
        t.Fatalf("expected the unlimited timeout kept for the client to read, got %s", resolved.timeout)
    }

    if absent := mustResolveRequestOptions(t, RequestOptions{}); false == absent.timeout.IsAbsent() {
        t.Fatalf("expected the absent timeout kept absent, got %s", absent.timeout)
    }
}

func TestResolveRequestOptions_TheMapsAreCopied(t *testing.T) {
    callerHeaders := map[string]string{"X-Plural": "plural"}
    callerQuery := map[string]string{"page": "1"}

    resolved := mustResolveRequestOptions(t, RequestOptions{Headers: callerHeaders, Query: callerQuery})

    callerHeaders["X-Plural"] = "mutated"
    callerHeaders["X-Late"] = "late"
    callerQuery["page"] = "2"

    if "plural" != resolved.headers["X-Plural"] {
        t.Fatalf("expected the resolved options to hold their own copy of the headers, got %q", resolved.headers["X-Plural"])
    }

    if _, leaked := resolved.headers["X-Late"]; true == leaked {
        t.Fatalf("expected an entry added to the caller's map afterwards not to reach the resolved options")
    }

    if "1" != resolved.query["page"] {
        t.Fatalf("expected the resolved options to hold their own copy of the query, got %q", resolved.query["page"])
    }
}

func TestResolveRequestOptions_HeadersAreCanonicalized(t *testing.T) {
    resolved := mustResolveRequestOptions(t, RequestOptions{Headers: map[string]string{"x-api-key": "secret"}})

    if 1 != len(resolved.headers) || "secret" != resolved.headers["X-Api-Key"] {
        t.Fatalf("expected the canonical spelling stored, got %#v", resolved.headers)
    }
}

func TestResolveRequestOptions_ACollidingHeaderMapIsRefused(t *testing.T) {
    _, err := resolveRequestOptions(RequestOptions{Headers: map[string]string{"x-api-key": "old", "X-Api-Key": "new"}}, nil, false)

    if nil == err || "header keys collide after canonicalization" != err.Error() {
        t.Fatalf("expected the collision to be refused, got %v", err)
    }
}

func TestResolveRequestOptions_ABodyBesideAJsonIsRefused(t *testing.T) {
    _, err := resolveRequestOptions(RequestOptions{Body: "raw", Json: map[string]any{"a": 1}}, nil, false)

    assertRefusedField(t, err, "RequestOptions.Json", "the request body is given twice")
}

func TestResolveRequestOptions_APositionalBodyBesideABodyOrAJsonIsRefused(t *testing.T) {
    _, bodyErr := resolveRequestOptions(RequestOptions{Body: "raw"}, map[string]any{"positional": true}, true)
    assertRefusedField(t, bodyErr, "RequestOptions.Body", "the request body is given twice")

    _, jsonErr := resolveRequestOptions(RequestOptions{Json: map[string]any{"a": 1}}, map[string]any{"positional": true}, true)
    assertRefusedField(t, jsonErr, "RequestOptions.Json", "the request body is given twice")
}

func TestResolveRequestOptions_ABearerTokenBesideABasicCredentialIsRefused(t *testing.T) {
    _, err := resolveRequestOptions(RequestOptions{BearerToken: "token", BasicAuth: &BasicAuth{Username: "user"}}, nil, false)

    assertRefusedField(t, err, "RequestOptions.BasicAuth", "the request names two credentials")
}

func TestResolveRequestOptions_TheBodyFormDecidesTheJsonFlag(t *testing.T) {
    raw := mustResolveRequestOptions(t, RequestOptions{Body: "raw"})
    if "raw" != raw.body || true == raw.json {
        t.Fatalf("expected the raw body sent as it is, got %+v", raw)
    }

    encoded := mustResolveRequestOptions(t, RequestOptions{Json: map[string]any{"a": 1}})
    if nil == encoded.body || false == encoded.json {
        t.Fatalf("expected the json body marked for encoding, got %+v", encoded)
    }

    positional, err := resolveRequestOptions(RequestOptions{}, "positional", true)
    if nil != err || "positional" != positional.body || false == positional.json {
        t.Fatalf("expected the positional body marked for encoding, got %+v (%v)", positional, err)
    }
}

func TestResolveRequestOptions_TheBasicCredentialIsCopied(t *testing.T) {
    credential := &BasicAuth{Username: "user", Password: "password"}

    resolved := mustResolveRequestOptions(t, RequestOptions{BasicAuth: credential})

    credential.Password = "rewritten"

    if credential == resolved.basicAuth || "password" != resolved.basicAuth.Password {
        t.Fatalf("expected the resolved options to hold their own credential, got %+v", resolved.basicAuth)
    }
}
