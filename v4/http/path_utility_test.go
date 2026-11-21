package http

import "testing"

func TestJoinPaths(t *testing.T) {
    if "/api/user" != JoinPaths("/api", "/user") {
        t.Fatalf("unexpected join result")
    }

    if "/api/user" != JoinPaths("/api/", "user") {
        t.Fatalf("unexpected join result")
    }

    if "/api/user" != JoinPaths("api", "user") {
        t.Fatalf("unexpected join result")
    }

    if "" != JoinPaths("", "") {
        t.Fatalf("unexpected join result")
    }

    if "/api" != JoinPaths("/api", "") {
        t.Fatalf("unexpected join result")
    }

    if "/user" != JoinPaths("", "/user") {
        t.Fatalf("unexpected join result")
    }
}
