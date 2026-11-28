package http

import (
    "crypto/rand"
    "crypto/subtle"
    "encoding/base64"
    nethttp "net/http"

    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

const (
    /* DefaultCsrfHeaderName is the header a firewall reads the csrf token from when its configuration names none. */
    DefaultCsrfHeaderName = "X-CSRF-Token"

    /* DefaultCsrfFieldName is the urlencoded form field a firewall reads the csrf token from when its configuration names none. */
    DefaultCsrfFieldName = "_csrf_token"

    /* SessionKeyCsrf is the session key the csrf secret is kept under; a session holding no other key reads as empty to the check. */
    SessionKeyCsrf = "_csrf"

    csrfSecretLength = 32
)

/* IsSafeMethod answers whether the method is one a state change may not ride: exactly GET, HEAD, OPTIONS and TRACE, upper case, the spelling net/http sends. */
func IsSafeMethod(method string) bool {
    switch method {
    case nethttp.MethodGet, nethttp.MethodHead, nethttp.MethodOptions, nethttp.MethodTrace:
        return true
    }

    return false
}

/* CsrfToken answers a masked csrf token of the request's session, issuing its secret on the first call; every answer is another text that verifies against the same secret. A session the storage could not answer is the error. */
func CsrfToken(request httpcontract.Request) (string, error) {
    sessionInstance, sessionErr := csrfSession(request)
    if nil != sessionErr {
        return "", sessionErr
    }

    secret, hasSecret := csrfSecret(sessionInstance)
    if false == hasSecret {
        secret = make([]byte, csrfSecretLength)
        if _, readErr := rand.Read(secret); nil != readErr {
            return "", exception.NewError("the csrf secret could not be generated", nil, readErr)
        }

        sessionInstance.Set(SessionKeyCsrf, base64.RawURLEncoding.EncodeToString(secret))
    }

    pad := make([]byte, csrfSecretLength)
    if _, readErr := rand.Read(pad); nil != readErr {
        return "", exception.NewError("the csrf token mask could not be generated", nil, readErr)
    }

    masked := make([]byte, 0, 2*csrfSecretLength)
    masked = append(masked, pad...)
    for index := range secret {
        masked = append(masked, secret[index]^pad[index])
    }

    return base64.RawURLEncoding.EncodeToString(masked), nil
}

/* VerifyCsrfToken answers whether the presented text is a token of the request's session secret; a session holding no secret answers false and issues none. */
func VerifyCsrfToken(request httpcontract.Request, presented string) (bool, error) {
    sessionInstance, sessionErr := csrfSession(request)
    if nil != sessionErr {
        return false, sessionErr
    }

    secret, hasSecret := csrfSecret(sessionInstance)
    if false == hasSecret {
        return false, nil
    }

    decoded, decodeErr := base64.RawURLEncoding.DecodeString(presented)
    if nil != decodeErr || 2*csrfSecretLength != len(decoded) {
        return false, nil
    }

    unmasked := make([]byte, csrfSecretLength)
    for index := range unmasked {
        unmasked[index] = decoded[csrfSecretLength+index] ^ decoded[index]
    }

    return 1 == subtle.ConstantTimeCompare(unmasked, secret), nil
}

/* RotateCsrfToken drops the session's csrf secret, so no token issued before verifies; the next CsrfToken issues another. */
func RotateCsrfToken(request httpcontract.Request) error {
    sessionInstance, sessionErr := csrfSession(request)
    if nil != sessionErr {
        return sessionErr
    }

    sessionInstance.Delete(SessionKeyCsrf)

    return nil
}

func csrfSession(request httpcontract.Request) (sessioncontract.Session, error) {
    sessionInstance, sessionErr := RequestSession(request)
    if nil != sessionErr {
        return nil, sessionErr
    }

    if cause, isUnavailable := session.SessionUnavailableCause(sessionInstance); true == isUnavailable {
        if false == exception.AnswersUnavailable(cause) {
            cause = exception.MarkInfrastructureFailure(cause)
        }

        return nil, markSessionUnavailable(cause)
    }

    return sessionInstance, nil
}

func csrfSecret(sessionInstance sessioncontract.Session) ([]byte, bool) {
    encoded := sessionInstance.String(SessionKeyCsrf)
    if "" == encoded {
        return nil, false
    }

    secret, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
    if nil != decodeErr || csrfSecretLength != len(secret) {
        return nil, false
    }

    return secret, true
}
