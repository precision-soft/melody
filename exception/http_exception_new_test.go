package exception

import (
    nethttp "net/http"
    "testing"
)

func TestHttpException_DefaultMessageWhenEmpty(t *testing.T) {
    ex := BadRequest("")
    if nethttp.StatusBadRequest != ex.StatusCode() {
        t.Fatalf("unexpected status code")
    }
    if "bad request" != ex.Message() {
        t.Fatalf("expected default message")
    }
}

/* @info net/http's WriteHeader panics below 100 and above 999 deep in the response path, and a status the writer clamps to 200 serves an exception as success; the refusal names the mistake where it is made */
func TestNewHttpException_StatusOutOfRange_Panics(t *testing.T) {
    for _, statusCode := range []int{0, 42, 99, 600, 999} {
        assertPanicsWithEmergency(t, "http status code out of range", func() {
            NewHttpException(statusCode, "boom")
        })

        assertPanicsWithEmergency(t, "http status code out of range", func() {
            NewHttpExceptionWithCause(statusCode, "boom", nil)
        })
    }
}

func TestNewHttpException_AcceptsTheRangeBoundaries(t *testing.T) {
    for _, statusCode := range []int{100, 599} {
        if statusCode != NewHttpException(statusCode, "boom").StatusCode() {
            t.Fatalf("expected status %d accepted", statusCode)
        }
    }
}
