package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/precision-soft/melody/v2/clock"
	"github.com/precision-soft/melody/v2/internal/testhelper"
)

func TestGetClientIp_ExtractsHostFromHostPort(t *testing.T) {
	request := testhelper.NewHttpTestRequest("GET", "http://example.com/")
	request.HttpRequest().RemoteAddr = "10.1.2.3:4567"

	ip := getClientIp(request)
	if "10.1.2.3" != ip {
		t.Fatalf("expected 10.1.2.3, got %s", ip)
	}
}

func TestGetClientIp_FallsBackToRawAddress(t *testing.T) {
	request := testhelper.NewHttpTestRequest("GET", "http://example.com/")
	request.HttpRequest().RemoteAddr = "10.1.2.3"

	ip := getClientIp(request)
	if "10.1.2.3" != ip {
		t.Fatalf("expected 10.1.2.3, got %s", ip)
	}
}

func TestGetClientIp_HandlesIpv6WithPort(t *testing.T) {
	request := testhelper.NewHttpTestRequest("GET", "http://example.com/")
	request.HttpRequest().RemoteAddr = "[::1]:1234"

	ip := getClientIp(request)
	if "::1" != ip {
		t.Fatalf("expected ::1, got %s", ip)
	}
}

func TestGetClientIp_HandlesIpv6WithoutPort(t *testing.T) {
	request := testhelper.NewHttpTestRequest("GET", "http://example.com/")
	request.HttpRequest().RemoteAddr = "::1"

	ip := getClientIp(request)
	if "::1" != ip {
		t.Fatalf("expected ::1, got %s", ip)
	}
}

func TestTokenBucketLimiter_AllowsWithinRate(t *testing.T) {
	frozenTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clockInstance := clock.NewFrozenClock(frozenTime)

	limiter := NewTokenBucketLimiterWithClock(clockInstance, 3, time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected first request to be allowed")
	}
	if false == limiter.Allow("key") {
		t.Fatalf("expected second request to be allowed")
	}
	if false == limiter.Allow("key") {
		t.Fatalf("expected third request to be allowed")
	}
	if true == limiter.Allow("key") {
		t.Fatalf("expected fourth request to be denied")
	}
}

func TestTokenBucketLimiter_RefillsAfterWindow(t *testing.T) {
	frozenTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clockInstance := clock.NewFrozenClock(frozenTime)

	limiter := NewTokenBucketLimiterWithClock(clockInstance, 1, time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected first request to be allowed")
	}
	if true == limiter.Allow("key") {
		t.Fatalf("expected second request to be denied")
	}

	clockInstance.Advance(61 * time.Second)

	if false == limiter.Allow("key") {
		t.Fatalf("expected request to be allowed after window")
	}
}

func TestTokenBucketLimiter_Reset_ClearsBucket(t *testing.T) {
	frozenTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clockInstance := clock.NewFrozenClock(frozenTime)

	limiter := NewTokenBucketLimiterWithClock(clockInstance, 1, time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected first request to be allowed")
	}
	if true == limiter.Allow("key") {
		t.Fatalf("expected second request to be denied")
	}

	limiter.Reset("key")

	if false == limiter.Allow("key") {
		t.Fatalf("expected request to be allowed after reset")
	}
}

func TestSlidingWindowLimiter_AllowsWithinLimit(t *testing.T) {
	frozenTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clockInstance := clock.NewFrozenClock(frozenTime)

	limiter := NewSlidingWindowLimiterWithClock(clockInstance, 2, time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected first request to be allowed")
	}
	if false == limiter.Allow("key") {
		t.Fatalf("expected second request to be allowed")
	}
	if true == limiter.Allow("key") {
		t.Fatalf("expected third request to be denied")
	}
}

func TestSlidingWindowLimiter_AllowsAfterOldRequestsExpire(t *testing.T) {
	frozenTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clockInstance := clock.NewFrozenClock(frozenTime)

	limiter := NewSlidingWindowLimiterWithClock(clockInstance, 1, time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected first request to be allowed")
	}
	if true == limiter.Allow("key") {
		t.Fatalf("expected second request to be denied")
	}

	clockInstance.Advance(61 * time.Second)

	if false == limiter.Allow("key") {
		t.Fatalf("expected request to be allowed after old requests expired")
	}
}

func TestDefaultKeyExtractor_IncludesIpAndPath(t *testing.T) {
	request := httptest.NewRequest("GET", "http://example.com/test", nil)
	request.RemoteAddr = "192.168.1.1:9999"

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

	key := defaultKeyExtractor(melodyRequest)
	if "192.168.1.1:/test" != key {
		t.Fatalf("unexpected key: %s", key)
	}
}

func TestIpKeyExtractor_ReturnsOnlyIp(t *testing.T) {
	request := httptest.NewRequest("GET", "http://example.com/test", nil)
	request.RemoteAddr = "192.168.1.1:9999"

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

	key := ipKeyExtractor(melodyRequest)
	if "192.168.1.1" != key {
		t.Fatalf("unexpected key: %s", key)
	}
}
