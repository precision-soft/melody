package http

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v2/internal/testhelper"
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

/* the sibling door refuses the same empty name three lines above with a message of its own, so an unqualified recover reads that refusal as this one; the name of the door that refused is what separates them. */
func TestDeleteCookie_PanicsWhenNameIsEmpty(t *testing.T) {
    response := EmptyResponse(200)

    testhelper.AssertPanicsWithError(t, func() {
        DeleteCookie(response, "", "/")
    }, "the cookie name is empty and can not be deleted")
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

/* Nil headers are a state the contract permits — SetHeaders stores the nil it is given — and every other consumer of Headers() in the chain checks for it before writing. */
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
