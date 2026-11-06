package security

import (
    "encoding/json"
    "errors"
    "mime"
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v4/.example/twofactor"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* LoginChain is what the sign-in door authenticates through: LoginAuthentication, the password and, where it is wired, the second factor in front of the session. */
type LoginChain interface {
    Authenticate(request melodyhttpcontract.Request) (melodysecuritycontract.Token, error)
}

/* LoginDoorAuthenticator reads the sign-in's json body and authenticates it through the chain Bind hands over: the module declares the firewall before the kernel-built parts of the chain exist. */
type LoginDoorAuthenticator struct {
    chain LoginChain
}

/* Bind hands over the chain; it runs at boot, before the first request. */
func (instance *LoginDoorAuthenticator) Bind(chain LoginChain) {
    instance.chain = chain
}

func (instance *LoginDoorAuthenticator) Supports(request melodyhttpcontract.Request) bool {
    return true
}

/* Authenticate reads an application/json body alone and refuses any other with 415: a top-level cross-site form can post urlencoded, multipart or text/plain, never json. A malformed request and an outage are errors, which raise no security.login.failure; refused credentials are an unauthenticated token, which does. */
func (instance *LoginDoorAuthenticator) Authenticate(request melodyhttpcontract.Request) (melodysecuritycontract.Token, error) {
    type loginBody struct {
        Username string `json:"username"`
        Password string `json:"password"`
    }

    httpRequest := request.HttpRequest()

    mediaType, _, mediaTypeErr := mime.ParseMediaType(httpRequest.Header.Get("Content-Type"))
    if nil != mediaTypeErr || "application/json" != mediaType {
        return nil, melodyexception.NewHttpException(nethttp.StatusUnsupportedMediaType, "the sign-in reads application/json")
    }

    var body loginBody

    if decoderErr := json.NewDecoder(httpRequest.Body).Decode(&body); nil != decoderErr {
        var maxBytesErr *nethttp.MaxBytesError
        if true == errors.As(decoderErr, &maxBytesErr) {
            return nil, melodyexception.NewHttpException(nethttp.StatusRequestEntityTooLarge, "payload too large")
        }

        return nil, melodyexception.NewHttpError(nethttp.StatusBadRequest, melodyexception.HttpErrorConfig{Message: "invalid json", Cause: decoderErr})
    }

    username := strings.TrimSpace(body.Username)
    password := strings.TrimSpace(body.Password)

    if "" == username || "" == password {
        return nil, melodyexception.NewHttpException(nethttp.StatusBadRequest, "invalid credentials input")
    }

    /* the authenticators read the credentials parsed here, and the password leaves the request as soon as they have */
    request.Attributes().Set(RequestAttributeLoginUsername, username)
    request.Attributes().Set(RequestAttributeLoginPassword, password)

    token, authenticationErr := instance.chain.Authenticate(request)

    request.Attributes().Remove(RequestAttributeLoginPassword)

    if nil == authenticationErr {
        return token, nil
    }

    if true == errors.Is(authenticationErr, ErrSecondFactorBudgetSpent) {
        return nil, chainRefusal(nethttp.StatusTooManyRequests, "too many attempts", authenticationErr)
    }

    if true == errors.Is(authenticationErr, ErrSecondFactorBudgetUnavailable) || true == errors.Is(authenticationErr, twofactor.ErrStoreUnavailable) {
        return nil, chainRefusal(nethttp.StatusServiceUnavailable, "the second factor is unavailable", authenticationErr)
    }

    /* the cause names internals and this door is unauthenticated, so the public message says nothing of it */
    return nil, chainRefusal(nethttp.StatusInternalServerError, "authentication failed", authenticationErr)
}

/* chainRefusal carries the chain's error as the cause; one the chain already filed, the limiter's outage, is not filed a second time under the refusal. */
func chainRefusal(statusCode int, message string, causeErr error) error {
    refusal := melodyexception.NewHttpError(statusCode, melodyexception.HttpErrorConfig{Message: message, Cause: causeErr})

    if true == melodyexception.IsAlreadyLogged(causeErr) {
        refusal.MarkAsLogged()
    }

    return refusal
}

var _ melodysecuritycontract.Authenticator = (*LoginDoorAuthenticator)(nil)
