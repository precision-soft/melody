package http

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v2/internal/testhelper"
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
        t.Fatalf("expected true")
    }
}

func TestPrefersHtml_ReturnsFalseWhenJsonBeforeHtml(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "application/json,text/html")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false")
    }
}

func TestPrefersHtml_ReturnsFalseWhenOnlyJson(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "application/json")
    if true == PrefersHtml(request) {
        t.Fatalf("expected false when only json present")
    }
}

func TestPrefersHtml_ReturnsFalseWhenNilRequest(t *testing.T) {
    if true == PrefersHtml(nil) {
        t.Fatalf("expected false for nil request")
    }
}

func TestPrefersHtml_CaseInsensitive(t *testing.T) {
    request := testhelper.NewHttpTestRequestWithAccept(nethttp.MethodGet, "http://example.com/", "Text/HTML,Application/JSON")
    if false == PrefersHtml(request) {
        t.Fatalf("expected true for case-insensitive html before json")
    }
}
