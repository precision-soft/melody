package http

import (
    nethttp "net/http"
    "strings"
    "testing"
)

func TestSetCookie_AddsHeader(t *testing.T) {
    response := EmptyResponse(200)

    SetCookie(
        response,
        &nethttp.Cookie{
            Name:  "a",
            Value: "b",
            Path:  "/",
        },
    )

    if "" == response.Headers().Get("Set-Cookie") {
        t.Fatalf("expected set-cookie header")
    }
}

func TestSetCookie_PanicsWhenNameIsEmpty(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    response := EmptyResponse(200)

    SetCookie(
        response,
        &nethttp.Cookie{
            Name:  "",
            Value: "b",
        },
    )
}

func TestDeleteCookie_SetsDefaultPath(t *testing.T) {
    response := EmptyResponse(200)

    DeleteCookie(response, "sid", "")

    value := response.Headers().Get("Set-Cookie")
    if "" == value {
        t.Fatalf("expected set-cookie header")
    }
    if false == containsString(value, "Path=/") {
        t.Fatalf("expected default path")
    }
    if false == containsString(value, "Max-Age=0") && false == containsString(value, "Max-Age=-1") {
        t.Fatalf("expected max-age delete semantics")
    }
}

func TestDeleteCookie_PanicsWhenNameIsEmpty(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    response := EmptyResponse(200)

    DeleteCookie(response, "", "/")
}

func containsString(value string, needle string) bool {
    if "" == needle {
        return true
    }

    return -1 != indexOf(value, needle)
}

func indexOf(value string, needle string) int {
    for i := 0; i+len(needle) <= len(value); i++ {
        if value[i:i+len(needle)] == needle {
            return i
        }
    }

    return -1
}

/* Nil headers are a state the contract permits — SetHeaders stores the nil it is given — and every other
consumer of Headers() in the chain checks for it before writing. */
func TestSetCookie_AllocatesTheHeaderMapWhenTheResponseHasNone(t *testing.T) {
    response := EmptyResponse(200)
    response.SetHeaders(nil)

    SetCookie(
        response,
        &nethttp.Cookie{
            Name:  "a",
            Value: "b",
        },
    )

    if "" == response.Headers().Get("Set-Cookie") {
        t.Fatalf("expected the cookie to be set on a response whose header map was nil")
    }
}

/* the browser enforces the prefix contracts on the deleting Set-Cookie too: without Secure — and for __Host- without path "/" — the deletion is rejected in silence and the cookie stays. The __Host- case pins that the one acceptable path wins over the one the caller passed. */
func TestDeleteCookie_HonoursThePrefixContractsSoTheDeletionCanLand(t *testing.T) {
    hostResponse := EmptyResponse(200)
    DeleteCookie(hostResponse, "__Host-session", "/app")

    hostHeader := hostResponse.Headers().Get("Set-Cookie")
    if false == strings.Contains(hostHeader, "__Host-session=") {
        t.Fatalf("expected the deletion header, got %q", hostHeader)
    }
    if false == strings.Contains(hostHeader, "Secure") {
        t.Fatalf("expected the __Host- deletion to carry Secure, got %q", hostHeader)
    }
    if true == strings.Contains(hostHeader, "Path=/app") {
        t.Fatalf("expected the caller's path to lose to the prefix contract, got %q", hostHeader)
    }
    if false == strings.Contains(hostHeader, "Path=/") {
        t.Fatalf("expected the __Host- deletion to carry the one path the browser accepts, got %q", hostHeader)
    }

    secureResponse := EmptyResponse(200)
    DeleteCookie(secureResponse, "__Secure-session", "/app")

    secureHeader := secureResponse.Headers().Get("Set-Cookie")
    if false == strings.Contains(secureHeader, "Secure") {
        t.Fatalf("expected the __Secure- deletion to carry Secure, got %q", secureHeader)
    }
    if false == strings.Contains(secureHeader, "Path=/app") {
        t.Fatalf("expected the __Secure- deletion to keep the caller's path, got %q", secureHeader)
    }

    plainResponse := EmptyResponse(200)
    DeleteCookie(plainResponse, "session", "/app")

    plainHeader := plainResponse.Headers().Get("Set-Cookie")
    if true == strings.Contains(plainHeader, "Secure") {
        t.Fatalf("expected the unprefixed deletion to stay as it was, got %q", plainHeader)
    }
}
