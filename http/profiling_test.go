package http

import (
    nethttp "net/http"
    "testing"
    "time"
)

func TestNewHttpRequestProfile_ReportsEveryFieldItWasGiven(t *testing.T) {
    startedAt := time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)
    finishedAt := startedAt.Add(250 * time.Millisecond)

    profile := NewHttpRequestProfile(
        "request-1",
        nethttp.MethodPost,
        "/articles",
        "article.create",
        "/articles",
        nethttp.StatusCreated,
        startedAt,
        finishedAt,
        250*time.Millisecond,
    )

    if "request-1" != profile.RequestId() {
        t.Fatalf("unexpected request id: %q", profile.RequestId())
    }

    if nethttp.MethodPost != profile.Method() {
        t.Fatalf("unexpected method: %q", profile.Method())
    }

    if "/articles" != profile.Path() {
        t.Fatalf("unexpected path: %q", profile.Path())
    }

    if "article.create" != profile.RouteName() {
        t.Fatalf("unexpected route name: %q", profile.RouteName())
    }

    if "/articles" != profile.RoutePattern() {
        t.Fatalf("unexpected route pattern: %q", profile.RoutePattern())
    }

    if nethttp.StatusCreated != profile.StatusCode() {
        t.Fatalf("unexpected status code: %d", profile.StatusCode())
    }

    if false == startedAt.Equal(profile.StartedAt()) {
        t.Fatalf("unexpected start: %v", profile.StartedAt())
    }

    if false == finishedAt.Equal(profile.FinishedAt()) {
        t.Fatalf("unexpected finish: %v", profile.FinishedAt())
    }

    if 250*time.Millisecond != profile.Duration() {
        t.Fatalf("unexpected duration: %v", profile.Duration())
    }

    if false == profile.FinishedAt().After(profile.StartedAt()) {
        t.Fatalf("expected the finish to follow the start, got %v then %v", profile.StartedAt(), profile.FinishedAt())
    }
}

func TestNewHttpRequestProfile_KeepsThePathOfAnUnmatchedRequest(t *testing.T) {
    profile := NewHttpRequestProfile(
        "request-2",
        nethttp.MethodGet,
        "/nothing-here",
        "",
        "",
        nethttp.StatusNotFound,
        time.Now(),
        time.Now(),
        0,
    )

    if "/nothing-here" != profile.Path() {
        t.Fatalf("expected the requested path to survive, got: %q", profile.Path())
    }

    if "" != profile.RouteName() {
        t.Fatalf("expected an unmatched request to name no route, got: %q", profile.RouteName())
    }

    if "" != profile.RoutePattern() {
        t.Fatalf("expected an unmatched request to carry no pattern, got: %q", profile.RoutePattern())
    }
}
