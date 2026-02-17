package security

import (
	nethttp "net/http"
	"testing"

	"github.com/precision-soft/melody/v2/http"
)

func TestPathPrefixMatcher_Matches(t *testing.T) {
	httpRequest, _ := nethttp.NewRequest("GET", "http://localhost/admin/products", nil)

	request := http.NewRequest(
		httpRequest,
		nil,
		nil,
		nil,
	)

	matcher := NewPathPrefixMatcher("/admin")

	if false == matcher.Matches(request) {
		t.Fatalf("expected matcher to match")
	}
}

func TestPathPrefixMatcher_DoesNotMatch(t *testing.T) {
	httpRequest, _ := nethttp.NewRequest("GET", "http://localhost/api/products", nil)

	request := http.NewRequest(
		httpRequest,
		nil,
		nil,
		nil,
	)

	matcher := NewPathPrefixMatcher("/admin")

	if true == matcher.Matches(request) {
		t.Fatalf("expected matcher to not match")
	}
}
