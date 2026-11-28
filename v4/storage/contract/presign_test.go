package contract

import (
    "net/http"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

func TestResolvePresignExpiry_ReadsTheRule(t *testing.T) {
    for name, testCase := range map[string]struct {
        expiry   bound.Duration
        expected time.Duration
    }{
        "absent":     {bound.Duration{}, 15 * time.Minute},
        "one second": {bound.Of(time.Second), time.Second},
        "seven days": {bound.Of(7 * 24 * time.Hour), 7 * 24 * time.Hour},
        "one minute": {bound.Of(time.Minute), time.Minute},
    } {
        resolved, resolveErr := ResolvePresignExpiry(testCase.expiry)
        if nil != resolveErr || testCase.expected != resolved {
            t.Fatalf("%s: expected %s, got %s (%v)", name, testCase.expected, resolved, resolveErr)
        }
    }

    for name, expiry := range map[string]bound.Duration{
        "below one second": bound.Of(999 * time.Millisecond),
        "past seven days":  bound.Of(7*24*time.Hour + time.Second),
        "unlimited":        bound.UnlimitedDuration(),
    } {
        resolved, resolveErr := ResolvePresignExpiry(expiry)
        if nil == resolveErr || 0 != resolved {
            t.Fatalf("%s: expected the expiry refused, got %s (%v)", name, resolved, resolveErr)
        }

        if "PresignOptions.Expiry" != exception.FromError(resolveErr).Context()["field"] {
            t.Fatalf("%s: expected the refusal to name PresignOptions.Expiry, got %v", name, exception.FromError(resolveErr).Context())
        }

        if true == exception.ChainIs(resolveErr, ErrPresignUnsupported) {
            t.Fatalf("%s: expected an expiry refusal that is not ErrPresignUnsupported", name)
        }
    }
}

func TestResolvePresignMethod_AdmitsGetAndPutOnly(t *testing.T) {
    for method, expected := range map[string]string{"": http.MethodGet, http.MethodGet: http.MethodGet, http.MethodPut: http.MethodPut} {
        resolved, resolveErr := ResolvePresignMethod(method)
        if nil != resolveErr || expected != resolved {
            t.Fatalf("%q: expected %s, got %q (%v)", method, expected, resolved, resolveErr)
        }
    }

    for _, method := range []string{"get", "put", http.MethodHead, http.MethodDelete, http.MethodPost} {
        resolved, resolveErr := ResolvePresignMethod(method)
        if "" != resolved || false == exception.ChainIs(resolveErr, ErrPresignUnsupported) {
            t.Fatalf("%q: expected ErrPresignUnsupported, got %q (%v)", method, resolved, resolveErr)
        }

        if method != exception.FromError(resolveErr).Context()["method"] {
            t.Fatalf("%q: expected the refusal to name the method, got %v", method, exception.FromError(resolveErr).Context())
        }

        if "PresignOptions.Method" != exception.FromError(resolveErr).Context()["field"] {
            t.Fatalf("%q: expected the refusal to name PresignOptions.Method, got %v", method, exception.FromError(resolveErr).Context())
        }
    }
}
