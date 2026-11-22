package security

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

const DefaultJsonEntryPointChallenge = "Bearer"

/* JsonEntryPointConfig is what a json entry point is built from; the zero value challenges with DefaultJsonEntryPointChallenge. */
type JsonEntryPointConfig struct {
    /* Challenge is the WWW-Authenticate value of the 401; a control character or a line break is refused. */
    Challenge string
}

func NewJsonEntryPoint(config JsonEntryPointConfig) *JsonEntryPoint {
    challenge := config.Challenge
    if "" == challenge {
        challenge = DefaultJsonEntryPointChallenge
    }

    /* a header value holding a line break would split the response */
    if challenge != internal.EscapeControlCharacters(challenge) {
        exception.Panic(
            exception.NewError(
                "json entry point challenge holds a control character or a line break",
                exceptioncontract.Context{"field": "JsonEntryPointConfig.Challenge"},
                nil,
            ),
        )
    }

    return &JsonEntryPoint{challenge: challenge}
}

type JsonEntryPoint struct {
    challenge string
}

func (instance *JsonEntryPoint) Start(
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
) (httpcontract.Response, error) {
    response := http.JsonErrorResponse(nethttp.StatusUnauthorized, "unauthorized")
    response.Headers().Set("WWW-Authenticate", instance.challenge)

    return response, nil
}

var _ securitycontract.EntryPoint = (*JsonEntryPoint)(nil)
