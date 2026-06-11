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
