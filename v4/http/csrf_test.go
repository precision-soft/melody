package http

import (
    "context"
    "encoding/base64"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/session"
)

func TestCsrfToken_IssuesTheSecretOnceAndMasksEveryAnswer(t *testing.T) {
    request := csrfTestRequest()

    first, firstErr := CsrfToken(request)
    sessionInstance, _ := RequestSession(request)
    secret := sessionInstance.String(SessionKeyCsrf)

    second, secondErr := CsrfToken(request)
    if nil != firstErr || nil != secondErr {
        t.Fatalf("unexpected errors: %v %v", firstErr, secondErr)
    }

    if "" == secret || secret != sessionInstance.String(SessionKeyCsrf) {
        t.Fatalf("expected one secret issued and kept, got %q then %q", secret, sessionInstance.String(SessionKeyCsrf))
    }

    if 86 != len(first) || 86 != len(second) || first == second {
        t.Fatalf("expected two different 86-character masks, got %q and %q", first, second)
    }

    decodedFirst, _ := base64.RawURLEncoding.DecodeString(first)
    decodedSecond, _ := base64.RawURLEncoding.DecodeString(second)
    if string(decodedFirst[:32]) == string(decodedSecond[:32]) {
        t.Fatalf("expected a fresh pad on every answer")
    }
}

func TestCsrfToken_AnswersTheSessionErrorOnAnOutage(t *testing.T) {
    storeErr := exception.MarkInfrastructureFailure(errors.New("session store is down"))
    request := NewRequest(httptest.NewRequest(nethttp.MethodPost, "http://example.com/", nil), nil, RequestConfig{})
    _ = SetRequestSession(request, session.NewUnavailableSession(storeErr))

    if token, tokenErr := CsrfToken(request); "" != token || false == errors.Is(tokenErr, session.ErrSessionUnavailable) {
        t.Fatalf("expected the session outage, got %q %v", token, tokenErr)
    }

    if _, verifyErr := VerifyCsrfToken(request, "anything"); false == exception.AnswersUnavailable(verifyErr) {
        t.Fatalf("expected the outage from the verification, got %v", verifyErr)
    }

    if rotateErr := RotateCsrfToken(request); nil == rotateErr {
        t.Fatalf("expected the outage from the rotation")
    }
}

func TestVerifyCsrfToken_AcceptsEveryMaskOfTheSecret(t *testing.T) {
    request := csrfTestRequest()

    for index := 0; index < 5; index++ {
        token, _ := CsrfToken(request)
        if verified, verifyErr := VerifyCsrfToken(request, token); true != verified || nil != verifyErr {
            t.Fatalf("expected the mask %d to verify, got %v %v", index, verified, verifyErr)
        }
    }
}

func TestVerifyCsrfToken_RefusesAnotherSessionsToken(t *testing.T) {
    request := csrfTestRequest()
    other := csrfTestRequest()

    _, _ = CsrfToken(request)
    foreign, _ := CsrfToken(other)

    if verified, _ := VerifyCsrfToken(request, foreign); true == verified {
        t.Fatalf("expected another session's token refused")
    }
}

func TestVerifyCsrfToken_RefusesAMalformedText(t *testing.T) {
    request := csrfTestRequest()
    token, _ := CsrfToken(request)
    sessionInstance, _ := RequestSession(request)
    decoded, _ := base64.RawURLEncoding.DecodeString(token)

    for name, presented := range map[string]string{
        "empty":           "",
        "not base64url":   "!" + token[1:],
        "63 bytes":        base64.RawURLEncoding.EncodeToString(decoded[:63]),
        "65 bytes":        base64.RawURLEncoding.EncodeToString(append(append([]byte{}, decoded...), 0)),
        "the raw secret":  sessionInstance.String(SessionKeyCsrf),
        "padded encoding": base64.URLEncoding.EncodeToString(decoded) + "==",
    } {
        if verified, verifyErr := VerifyCsrfToken(request, presented); true == verified || nil != verifyErr {
            t.Fatalf("%s: expected a refusal, got %v %v", name, verified, verifyErr)
        }
    }
}

func TestVerifyCsrfToken_SessionWithoutASecret_RefusesAndIssuesNone(t *testing.T) {
    request := csrfTestRequest()

    if verified, verifyErr := VerifyCsrfToken(request, ""); true == verified || nil != verifyErr {
        t.Fatalf("expected an empty text refused over an empty session, got %v %v", verified, verifyErr)
    }

    zeroToken := base64.RawURLEncoding.EncodeToString(make([]byte, 64))
    if verified, _ := VerifyCsrfToken(request, zeroToken); true == verified {
        t.Fatalf("expected a token refused over a session holding no secret")
    }

    sessionInstance, _ := RequestSession(request)
    if true == sessionInstance.Has(SessionKeyCsrf) {
        t.Fatalf("expected no secret issued by the verification")
    }
}

func TestRotateCsrfToken_TheOldTokenNoLongerVerifies(t *testing.T) {
    request := csrfTestRequest()
    old, _ := CsrfToken(request)

    if rotateErr := RotateCsrfToken(request); nil != rotateErr {
        t.Fatalf("unexpected error: %v", rotateErr)
    }

    fresh, _ := CsrfToken(request)

    if verified, _ := VerifyCsrfToken(request, old); true == verified {
        t.Fatalf("expected the old token refused after the rotation")
    }

    if verified, _ := VerifyCsrfToken(request, fresh); false == verified {
        t.Fatalf("expected the fresh token to verify")
    }
}

func TestIsSafeMethod_ExactUpperCaseNamesOnly(t *testing.T) {
    for method, safe := range map[string]bool{
        "GET": true, "HEAD": true, "OPTIONS": true, "TRACE": true,
        "get": false, "Head": false, " GET": false, "POST": false, "PUT": false, "PATCH": false, "DELETE": false, "CONNECT": false, "": false,
    } {
        if safe != IsSafeMethod(method) {
            t.Fatalf("expected IsSafeMethod(%q) %v", method, safe)
        }
    }
}

func TestRequestCarriesSessionCookie_PresentedAndAbsent(t *testing.T) {
    withCookie := httptest.NewRequest(nethttp.MethodPost, "http://example.com/", nil)
    withCookie.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: "stale"})

    if false == RequestCarriesSessionCookie(NewRequest(withCookie, nil, RequestConfig{})) {
        t.Fatalf("expected the presented cookie read")
    }

    otherCookie := httptest.NewRequest(nethttp.MethodPost, "http://example.com/", nil)
    otherCookie.AddCookie(&nethttp.Cookie{Name: "theme", Value: "dark"})

    if true == RequestCarriesSessionCookie(NewRequest(otherCookie, nil, RequestConfig{})) {
        t.Fatalf("expected another cookie not read as the session's")
    }

    if true == RequestCarriesSessionCookie(nil) {
        t.Fatalf("expected a nil request to carry no cookie")
    }
}

func TestRegenerateRequestSession_DropsTheCsrfSecret(t *testing.T) {
    request := csrfRotationRequest()
    old, _ := CsrfToken(request)

    rotated, rotateErr := RegenerateRequestSession(request)
    if nil != rotateErr {
        t.Fatalf("unexpected error: %v", rotateErr)
    }

    if true == rotated.Has(SessionKeyCsrf) {
        t.Fatalf("expected the rotated session to hold no csrf secret")
    }

    if verified, _ := VerifyCsrfToken(request, old); true == verified {
        t.Fatalf("expected the token rendered before the rotation refused")
    }
}

func TestRegenerateRequestSession_KeepsEveryOtherValue(t *testing.T) {
    request := csrfRotationRequest()
    _, _ = CsrfToken(request)
    sessionInstance, _ := RequestSession(request)
    sessionInstance.Set("cart", "three items")

    rotated, rotateErr := RegenerateRequestSession(request)
    if nil != rotateErr || "three items" != rotated.String("cart") {
        t.Fatalf("expected the other values kept, got %v %v", rotated, rotateErr)
    }
}

func csrfTestRequest() *Request {
    request := NewRequest(httptest.NewRequest(nethttp.MethodPost, "http://example.com/", nil), nil, RequestConfig{})
    _ = SetRequestSession(request, mustSession(newCountingSessionManager().NewSession(sessionRuntime())))

    return request
}

func csrfRotationRequest() *Request {
    manager := newCountingSessionManager()
    serviceContainer := newHttpTestContainerWithSessionManager(manager)

    request := NewRequest(httptest.NewRequest(nethttp.MethodPost, "http://example.com/", nil), runtime.NewRequest(context.Background(), serviceContainer.NewScope()), RequestConfig{})
    request.Attributes().Set(requestAttributeSession, newRequestSession(sessionRuntime(), manager, request, &errorCountingLogger{}, "", false, httpcontract.SessionPolicy{}))

    return request
}
