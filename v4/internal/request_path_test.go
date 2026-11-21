package internal

import (
    "net/url"
    "testing"
)

func TestRequestPathAsSent_KeepsTheRawSpellingEscapedPathWouldRespell(t *testing.T) {
    for target, expected := range map[string]string{
        "/admin%2Fusers":         "/admin%2Fusers",
        "/admin%2Fusers{":        "/admin%2Fusers{",
        "/admin%2Fusers\xc3\xa9": "/admin%2Fusers\xc3\xa9",
        "/caf%C3%A9":             "/caf%C3%A9",
        "/admin/users":           "/admin/users",
        "/admin/users{":          "/admin/users{",
    } {
        requestUrl, parseErr := url.ParseRequestURI(target)
        if nil != parseErr {
            t.Fatalf("parse %q: %v", target, parseErr)
        }

        if actual := RequestPathAsSent(requestUrl); expected != actual {
            t.Fatalf("expected %q to be read as %q, got %q", target, expected, actual)
        }
    }
}

func TestRequestPathAsSent_ReadsTheEscapedPathWhenTheRawSpellingNoLongerMatchesThePath(t *testing.T) {
    requestUrl, parseErr := url.ParseRequestURI("/admin%2Fusers{")
    if nil != parseErr {
        t.Fatalf("parse: %v", parseErr)
    }

    requestUrl.Path = "/rewritten{"

    if actual := RequestPathAsSent(requestUrl); "/rewritten%7B" != actual {
        t.Fatalf("expected a raw spelling that no longer unescapes to the path to be ignored, got %q", actual)
    }
}

func TestRequestRawPathIsStale_AnswersWhetherTheRawSpellingStillNamesThePath(t *testing.T) {
    requestUrl, parseErr := url.ParseRequestURI("/api/admin%2Fusers")
    if nil != parseErr {
        t.Fatalf("parse: %v", parseErr)
    }

    if true == RequestRawPathIsStale(requestUrl) {
        t.Fatal("expected the raw spelling of an untouched request not to be stale")
    }

    requestUrl.Path = "/admin/users"

    if false == RequestRawPathIsStale(requestUrl) {
        t.Fatal("expected a raw spelling the rewritten path no longer unescapes from to be stale")
    }

    requestUrl.RawPath = ""

    if true == RequestRawPathIsStale(requestUrl) {
        t.Fatal("expected an empty raw spelling not to be stale")
    }
}

func TestRequestPathCarriesLiteralEncodedSeparator_ReadsEachSegmentAsSent(t *testing.T) {
    for target, expected := range map[string]bool{
        "/files/a%252Fb": true,
        "/files/a%252fb": false,
        "/files/a%2Fb":   false,
        "/files/a%2525b": false,
        "/files/a/b":     false,
    } {
        requestUrl, parseErr := url.ParseRequestURI(target)
        if nil != parseErr {
            t.Fatalf("parse %q: %v", target, parseErr)
        }

        if actual := RequestPathCarriesLiteralEncodedSeparator(requestUrl); expected != actual {
            t.Fatalf("expected %q to answer %v, got %v", target, expected, actual)
        }
    }
}
