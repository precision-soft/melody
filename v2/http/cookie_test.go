package http

import (
	nethttp "net/http"
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
