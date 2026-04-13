package http

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
)

func TestPrefersHtml_ReturnsFalseWhenNoAccept(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false")
    }
}

func TestPrefersHtml_ReturnsTrueForHtmlWithoutJson(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "text/html,application/xhtml+xml")
    if false == PrefersHtml(request) {
        t.Fatalf("expected true")
    }
}

func TestPrefersHtml_ReturnsTrueWhenHtmlBeforeJson(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "text/html,application/json")
    if false == PrefersHtml(request) {
        t.Fatalf("expected true when text/html appears before application/json")
    }
}

func TestPrefersHtml_ReturnsFalseWhenJsonBeforeHtml(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "application/json,text/html")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false when application/json appears before text/html")
    }
}

func TestPrefersHtml_ReturnsFalseWhenNilRequest(t *testing.T) {
    if true == PrefersHtml(nil) {
        t.Fatalf("expected false for nil request")
    }
}

func TestPrefersHtml_ReturnsFalseWhenOnlyJson(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "application/json")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false when only application/json is present")
    }
}

func TestPrefersHtml_ReturnsTrueForTextHtmlOnly(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "text/html")
    if false == PrefersHtml(request) {
        t.Fatalf("expected true when only text/html is present")
    }
}

func TestPrefersHtml_ReturnsFalseForWildcard(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "*/*")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false for wildcard accept header without text/html")
    }
}

func TestPrefersHtml_CaseInsensitive(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "Text/HTML")
    if false == PrefersHtml(request) {
        t.Fatalf("expected true for case-insensitive text/html match")
    }
}
