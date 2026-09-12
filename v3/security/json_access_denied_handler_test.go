package security

import (
    "io"
    nethttp "net/http"
    "strings"
    "testing"
)

/* the denied handler is what an unauthorized caller actually receives: the status must be 403 and the body must not name what was refused, because the reason is the authorization decision and a client that may not read the resource may not read why either */
func TestJsonAccessDeniedHandler_AnswersForbiddenWithoutTheDecisionReason(t *testing.T) {
    handler := NewJsonAccessDeniedHandler()

    response, handleErr := handler.Handle(nil, nil, &accessDeniedProbeError{})
    if nil != handleErr {
        t.Fatalf("unexpected error: %v", handleErr)
    }

    if nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected 403, got %d", response.StatusCode())
    }

    bodyBytes, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    body := string(bodyBytes)
    if false == strings.Contains(body, "forbidden") {
        t.Fatalf("expected the forbidden message, got %q", body)
    }

    if true == strings.Contains(body, "secret reason") {
        t.Fatalf("expected the decision reason to stay out of the response, got %q", body)
    }
}

type accessDeniedProbeError struct{}

func (instance *accessDeniedProbeError) Error() string {
    return "secret reason"
}
